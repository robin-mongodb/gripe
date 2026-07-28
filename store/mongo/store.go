// Package mongo is the MongoDB implementation of internal/store.Store.
// See CLAUDE.md — hides how; each method is use-case-shaped. Money is int64 minor units
// end-to-end; only the DB boundary knows Decimal128 (once we add currency-aware conversion).
package mongo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/robin-mongodb/gripe/internal/api"
	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

const (
	colPayments = "payments"
	colIdemKeys = "idempotency_keys"
	colRefunds  = "refunds"

	// Amounts ending in .13 (minor units mod 100 == 13) are mock-declined.
	// Only relevant to card / Apple Pay / Google Pay.
	declineTail = 13
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

// New opens a Mongo client, pings, and ensures the indexes this impl needs.
func New(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	s := &Store{client: client, db: client.Database(dbName)}
	if err := s.ensureIndexes(ctx); err != nil {
		return nil, fmt.Errorf("mongo ensureIndexes: %w", err)
	}
	return s, nil
}

func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }
func (s *Store) Ping(ctx context.Context) error  { return s.client.Ping(ctx, nil) }

// ensureIndexes creates the indexes tasks 8/17 need. Full schema comes later (tasks 10/11).
func (s *Store) ensureIndexes(ctx context.Context) error {
	_, err := s.db.Collection(colPayments).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "created_at", Value: -1}}},
	})
	if err != nil {
		return fmt.Errorf("payments indexes: %w", err)
	}
	_, err = s.db.Collection(colRefunds).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "payment_id", Value: 1}, {Key: "created_at", Value: -1}}},
	})
	if err != nil {
		return fmt.Errorf("refunds indexes: %w", err)
	}
	// TTL on expires_at vacuums expired idempotency records automatically.
	_, err = s.db.Collection(colIdemKeys).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return fmt.Errorf("idempotency indexes: %w", err)
	}
	return nil
}

// ---------- Payment doc + mapping ----------

type paymentDoc struct {
	ID            string    `bson:"_id"`
	MerchantID    string    `bson:"merchant_id"`
	CustomerID    string    `bson:"customer_id"`
	AmountMinor   int64     `bson:"amount_minor"`
	Currency      string    `bson:"currency"`
	Method        string    `bson:"method"`
	Status        string    `bson:"status"`
	RefundedMinor int64     `bson:"refunded_minor"`
	CreatedAt     time.Time `bson:"created_at"`
	UpdatedAt     time.Time `bson:"updated_at"`
}

func (p paymentDoc) toDomain() domain.Payment {
	return domain.Payment{
		ID:            p.ID,
		MerchantID:    p.MerchantID,
		CustomerID:    p.CustomerID,
		AmountMinor:   p.AmountMinor,
		Currency:      domain.Currency(p.Currency),
		Method:        domain.PaymentMethod(p.Method),
		Status:        domain.PaymentStatus(p.Status),
		RefundedMinor: p.RefundedMinor,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

// ---------- IdempotencyStore ----------

type idemDoc struct {
	Key        string    `bson:"_id"`
	ActorRole  string    `bson:"actor_role"`
	ActorID    string    `bson:"actor_id"`
	BodyHash   string    `bson:"body_hash"`
	StatusCode int       `bson:"status"`
	Response   []byte    `bson:"response"`
	CreatedAt  time.Time `bson:"created_at"`
	ExpiresAt  time.Time `bson:"expires_at"`
}

func (s *Store) PutIdempotencyRecord(ctx context.Context, rec api.IdempotencyRecord) error {
	_, err := s.db.Collection(colIdemKeys).InsertOne(ctx, idemDoc{
		Key:        rec.Key,
		ActorRole:  rec.ActorRole,
		ActorID:    rec.ActorID,
		BodyHash:   rec.BodyHash,
		StatusCode: rec.StatusCode,
		Response:   rec.Response,
		CreatedAt:  rec.CreatedAt,
		ExpiresAt:  rec.ExpiresAt,
	})
	if mongo.IsDuplicateKeyError(err) {
		return api.ErrIdempotencyExists
	}
	return err
}

func (s *Store) GetIdempotencyRecord(ctx context.Context, key string) (api.IdempotencyRecord, error) {
	var d idemDoc
	err := s.db.Collection(colIdemKeys).FindOne(ctx, bson.M{"_id": key}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return api.IdempotencyRecord{}, store.ErrNotFound
	}
	if err != nil {
		return api.IdempotencyRecord{}, err
	}
	return api.IdempotencyRecord{
		Key:        d.Key,
		ActorRole:  d.ActorRole,
		ActorID:    d.ActorID,
		BodyHash:   d.BodyHash,
		StatusCode: d.StatusCode,
		Response:   d.Response,
		CreatedAt:  d.CreatedAt,
		ExpiresAt:  d.ExpiresAt,
	}, nil
}

// ---------- Store methods ----------

// CreatePayment — task 8/12.
//   - card / Apple Pay / Google Pay: sync auth+capture; amount_minor mod 100 == 13 -> declined.
//   - direct debit: authorized, cycler settles (task 14).
//   - bank transfer: pending, async settle event (task 15).
func (s *Store) CreatePayment(ctx context.Context, in domain.CreatePaymentInput, _ string) (domain.Payment, error) {
	if !in.Currency.Valid() {
		return domain.Payment{}, fmt.Errorf("%w: currency %q", store.ErrInvalidState, in.Currency)
	}
	if !in.Method.Valid() {
		return domain.Payment{}, fmt.Errorf("%w: method %q", store.ErrInvalidState, in.Method)
	}
	if in.AmountMinor <= 0 {
		return domain.Payment{}, fmt.Errorf("%w: amount_minor must be > 0", store.ErrInvalidState)
	}
	if strings.TrimSpace(in.MerchantID) == "" {
		return domain.Payment{}, fmt.Errorf("%w: merchant_id required", store.ErrInvalidState)
	}

	now := time.Now().UTC()
	status := domain.StatusCaptured
	switch in.Method {
	case domain.MethodCard, domain.MethodApplePay, domain.MethodGooglePay:
		if in.AmountMinor%100 == declineTail {
			status = domain.StatusDeclined
		}
	case domain.MethodDirectDebit:
		status = domain.StatusAuthorized
	case domain.MethodBankTransfer:
		status = domain.StatusPending
	}

	doc := paymentDoc{
		ID:          newID("pay"),
		MerchantID:  in.MerchantID,
		CustomerID:  in.CustomerID,
		AmountMinor: in.AmountMinor,
		Currency:    string(in.Currency),
		Method:      string(in.Method),
		Status:      string(status),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.db.Collection(colPayments).InsertOne(ctx, doc); err != nil {
		return domain.Payment{}, fmt.Errorf("insert payment: %w", err)
	}
	return doc.toDomain(), nil
}

// GetPayment enforces actor scoping in the store, not the handler.
func (s *Store) GetPayment(ctx context.Context, id string, actor domain.Actor) (domain.Payment, error) {
	var d paymentDoc
	err := s.db.Collection(colPayments).FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.Payment{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, err
	}
	switch actor.Role {
	case domain.RoleAdmin:
		// sees all
	case domain.RoleMerchant:
		if d.MerchantID != actor.ID {
			return domain.Payment{}, store.ErrForbidden
		}
	case domain.RoleCustomer:
		if d.CustomerID != actor.ID {
			return domain.Payment{}, store.ErrForbidden
		}
	default:
		return domain.Payment{}, store.ErrForbidden
	}
	return d.toDomain(), nil
}

// ---------- Refund ----------

type refundDoc struct {
	ID          string    `bson:"_id"`
	PaymentID   string    `bson:"payment_id"`
	AmountMinor int64     `bson:"amount_minor"`
	Currency    string    `bson:"currency"`
	CreatedAt   time.Time `bson:"created_at"`
}

func (r refundDoc) toDomain() domain.Refund {
	return domain.Refund{
		ID:          r.ID,
		PaymentID:   r.PaymentID,
		AmountMinor: r.AmountMinor,
		Currency:    domain.Currency(r.Currency),
		CreatedAt:   r.CreatedAt,
	}
}

// RefundPayment — task 17. Merchant chooses amount. Constraint: 0 < amount <= (captured - already_refunded).
//
// Concurrency-safe: uses a conditional update on `refunded_minor`. The `$expr` in the
// filter guarantees only one refund can drive refunded_minor past captured_amount.
// If the update matches zero rows, either the payment doesn't exist, wasn't captured,
// or the refund would exceed the remaining amount — we probe to tell which.
func (s *Store) RefundPayment(ctx context.Context, paymentID string, amountMinor int64, _ string) (domain.Refund, error) {
	if amountMinor <= 0 {
		return domain.Refund{}, fmt.Errorf("%w: refund amount must be > 0", store.ErrInvalidState)
	}

	// Conditional: only touch the payment if the refund fits and the payment is captured.
	// ponytail: single conditional update; upgrade to a multi-doc txn when SettleRefund lands (task 52).
	filter := bson.M{
		"_id":    paymentID,
		"status": string(domain.StatusCaptured),
		"$expr": bson.M{
			"$lte": bson.A{
				bson.M{"$add": bson.A{"$refunded_minor", amountMinor}},
				"$amount_minor",
			},
		},
	}
	now := time.Now().UTC()
	update := bson.M{
		"$inc": bson.M{"refunded_minor": amountMinor},
		"$set": bson.M{"updated_at": now},
	}

	res := s.db.Collection(colPayments).FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After))
	var updated paymentDoc
	err := res.Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Diagnose: does the payment exist at all?
		var current paymentDoc
		findErr := s.db.Collection(colPayments).FindOne(ctx, bson.M{"_id": paymentID}).Decode(&current)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return domain.Refund{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Refund{}, findErr
		}
		if current.Status != string(domain.StatusCaptured) {
			return domain.Refund{}, fmt.Errorf("%w: payment status is %s", store.ErrInvalidState, current.Status)
		}
		// Must be the over-refund case.
		return domain.Refund{}, store.ErrOverRefund
	}
	if err != nil {
		return domain.Refund{}, err
	}

	// Optional: if the payment is now fully refunded, flip status. Contract test asserts remaining behaviour;
	// keep this update conservative — only when refunded_minor == amount_minor.
	if updated.RefundedMinor == updated.AmountMinor {
		_, _ = s.db.Collection(colPayments).UpdateOne(ctx,
			bson.M{"_id": paymentID, "refunded_minor": updated.AmountMinor},
			bson.M{"$set": bson.M{"status": string(domain.StatusRefunded), "updated_at": now}})
	}

	r := refundDoc{
		ID:          newID("re"),
		PaymentID:   paymentID,
		AmountMinor: amountMinor,
		Currency:    updated.Currency,
		CreatedAt:   now,
	}
	if _, err := s.db.Collection(colRefunds).InsertOne(ctx, r); err != nil {
		return domain.Refund{}, fmt.Errorf("insert refund: %w", err)
	}
	return r.toDomain(), nil
}

// ---------- Not yet implemented ----------

// CapturePayment — task 16 (UC-3). Moves an `authorized` payment to `captured`.
// Conditional update: only succeeds if the current status is `authorized`. Zero
// matched rows means either the payment doesn't exist or the state moved under
// us — we probe to distinguish (same pattern as RefundPayment).
func (s *Store) CapturePayment(ctx context.Context, paymentID string) (domain.Payment, error) {
	now := time.Now().UTC()
	filter := bson.M{
		"_id":    paymentID,
		"status": string(domain.StatusAuthorized),
	}
	update := bson.M{
		"$set": bson.M{
			"status":     string(domain.StatusCaptured),
			"updated_at": now,
		},
	}

	res := s.db.Collection(colPayments).FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After))
	var updated paymentDoc
	err := res.Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		var current paymentDoc
		findErr := s.db.Collection(colPayments).FindOne(ctx, bson.M{"_id": paymentID}).Decode(&current)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return domain.Payment{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Payment{}, findErr
		}
		return domain.Payment{}, fmt.Errorf("%w: cannot capture from status %s (only from authorized)", store.ErrInvalidState, current.Status)
	}
	if err != nil {
		return domain.Payment{}, err
	}
	return updated.toDomain(), nil
}
func (s *Store) SettlePayment(_ context.Context, _ string) (domain.Payment, error) {
	return domain.Payment{}, errors.ErrUnsupported
}
func (s *Store) SettleRefund(_ context.Context, _ string) (domain.Refund, error) {
	return domain.Refund{}, errors.ErrUnsupported
}
// ListMerchantPayments — task 22 (UC-5). Merchant sees own payments, newest first.
// Cursor is base64-of-{created_at,id}; opaque to callers. Page size is capped at listPageMax.
//
// ponytail: single compound index (merchant_id, created_at DESC) handles the sort + filter;
// if callers add sort-by-amount later, add another index rather than sort in memory.
func (s *Store) ListMerchantPayments(ctx context.Context, merchantID string, filters domain.Filters, cursor domain.Cursor) (domain.Page, error) {
	return s.listPayments(ctx, merchantID, filters, cursor)
}

// ListAllPayments — task 24 (UC-6). Admin variant: no merchant scoping unless the
// admin explicitly filters by merchant_id.
func (s *Store) ListAllPayments(ctx context.Context, filters domain.Filters, cursor domain.Cursor) (domain.Page, error) {
	return s.listPayments(ctx, "", filters, cursor)
}

const listPageMax = 50

// listPayments is the shared implementation. If merchantScope != "" the query is pinned
// to that merchant; otherwise (admin), filters.MerchantID may narrow the query.
func (s *Store) listPayments(ctx context.Context, merchantScope string, filters domain.Filters, cursor domain.Cursor) (domain.Page, error) {
	q := bson.M{}
	switch {
	case merchantScope != "":
		q["merchant_id"] = merchantScope
	case filters.MerchantID != "":
		q["merchant_id"] = filters.MerchantID
	}
	if filters.Status != "" {
		q["status"] = string(filters.Status)
	}
	if filters.Method != "" {
		q["method"] = string(filters.Method)
	}
	if filters.Currency != "" {
		q["currency"] = string(filters.Currency)
	}
	if !filters.FromTime.IsZero() || !filters.ToTime.IsZero() {
		created := bson.M{}
		if !filters.FromTime.IsZero() {
			created["$gte"] = filters.FromTime
		}
		if !filters.ToTime.IsZero() {
			created["$lte"] = filters.ToTime
		}
		q["created_at"] = created
	}
	if filters.MinMinor > 0 || filters.MaxMinor > 0 {
		amt := bson.M{}
		if filters.MinMinor > 0 {
			amt["$gte"] = filters.MinMinor
		}
		if filters.MaxMinor > 0 {
			amt["$lte"] = filters.MaxMinor
		}
		q["amount_minor"] = amt
	}

	// Cursor decode: keyset pagination on (created_at, _id). Assumes created_at is
	// unique-enough that the id tiebreak only kicks in on same-timestamp inserts.
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return domain.Page{}, fmt.Errorf("%w: bad cursor", store.ErrInvalidState)
		}
		// keyset predicate: (created_at, _id) < cursor when sorting DESC by created_at.
		q["$or"] = bson.A{
			bson.M{"created_at": bson.M{"$lt": c.CreatedAt}},
			bson.M{"created_at": c.CreatedAt, "_id": bson.M{"$lt": c.ID}},
		}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(listPageMax) + 1) // fetch one extra to detect "more".

	cur, err := s.db.Collection(colPayments).Find(ctx, q, opts)
	if err != nil {
		return domain.Page{}, err
	}
	defer cur.Close(ctx)

	var docs []paymentDoc
	if err := cur.All(ctx, &docs); err != nil {
		return domain.Page{}, err
	}

	var next domain.Cursor
	if len(docs) > listPageMax {
		last := docs[listPageMax-1]
		next = encodeCursor(paymentCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		docs = docs[:listPageMax]
	}

	items := make([]domain.Payment, len(docs))
	for i, d := range docs {
		items[i] = d.toDomain()
	}
	return domain.Page{Items: items, NextCursor: next}, nil
}

// paymentCursor is the keyset shape. base64(json) — opaque to callers.
type paymentCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func encodeCursor(c paymentCursor) domain.Cursor {
	b, _ := json.Marshal(c)
	return domain.Cursor(base64.RawURLEncoding.EncodeToString(b))
}

func decodeCursor(c domain.Cursor) (paymentCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(string(c))
	if err != nil {
		return paymentCursor{}, err
	}
	var out paymentCursor
	if err := json.Unmarshal(b, &out); err != nil {
		return paymentCursor{}, err
	}
	return out, nil
}
func (s *Store) GetMerchantBalances(_ context.Context, _ string) ([]domain.Balance, error) {
	return nil, errors.ErrUnsupported
}
func (s *Store) CreateSubscription(_ context.Context, _ domain.CreateSubscriptionInput) (domain.Subscription, error) {
	return domain.Subscription{}, errors.ErrUnsupported
}
func (s *Store) CancelSubscription(_ context.Context, _ string) (domain.Subscription, error) {
	return domain.Subscription{}, errors.ErrUnsupported
}
func (s *Store) DueSubscriptions(_ context.Context, _ time.Time, _ int) ([]domain.Subscription, error) {
	return nil, errors.ErrUnsupported
}

// Compile-time checks.
var (
	_ store.Store          = (*Store)(nil)
	_ api.IdempotencyStore = (*Store)(nil)
)

func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
