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
	"sort"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/robin-mongodb/gripe/internal/api"
	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

const (
	colPayments  = "payments"
	colIdemKeys  = "idempotency_keys"
	colSubs      = "subscriptions"
	colMerchants = "merchants"
	colCustomers = "customers"

	// Amounts ending in .13 (minor units mod 100 == 13) are mock-declined.
	// Only relevant to card / Apple Pay / Google Pay.
	declineTail = 13
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
	// seen caches actor IDs already onboarded ("m:"/"c:" prefixed). Merchants and
	// customers are never deleted, so seen-once == exists; skips 2 upserts per
	// create at steady state (task 42). Process-local: a cold process re-upserts
	// once per actor, which is harmless (idempotent $setOnInsert).
	seen sync.Map
}

// New opens a Mongo client, pings, and ensures the indexes this impl needs.
func New(ctx context.Context, uri, dbName string) (*Store, error) {
	// maxPoolSize 200 (default 100): under stress, in-flight ops exceed 100 and
	// requests queue in the API waiting for a connection (task 42 finding).
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetMaxPoolSize(200))
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
		// _id in the key so the (created_at, _id) keyset sort is fully index-order —
		// without it every merchant list pays an in-memory sort (task 42 finding).
		{Keys: bson.D{{Key: "merchant_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}},
		// Same, for the unscoped admin list (PG parity: (created_at DESC, id DESC)).
		{Keys: bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}},
		// Multikey index so SettleRefund can address an embedded refund by ID.
		// Non-unique on purpose: a unique index on an array path indexes null for
		// docs with an empty/missing array, so the second refund-less payment
		// would collide. IDs are 96-bit random — uniqueness comes from newID.
		{Keys: bson.D{{Key: "refunds.id", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("payments indexes: %w", err)
	}
	// Subscriptions: the cycler queries {status, next_charge_at <= now}. Compound
	// (status, next_charge_at) supports the equality-then-range pattern efficiently.
	_, err = s.db.Collection(colSubs).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "next_charge_at", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("subscriptions indexes: %w", err)
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

// ---------- Merchants + customers ----------

// currencyBalance is one currency's ledger inside merchants.balances.
// balances is a currency-keyed subdocument (map, not array) so a settle is a
// single atomic dotted-path $inc — no array-filter or read-modify-write.
type currencyBalance struct {
	BalanceMinor int64 `bson:"balance_minor"`
	FeesMinor    int64 `bson:"fees_minor"`
}

type merchantDoc struct {
	ID       string                     `bson:"_id"`
	Balances map[string]currencyBalance `bson:"balances"`
}

// onboardActors implicitly creates the merchant and customer docs on first
// sight. $setOnInsert-with-upsert is idempotent: repeat pairs are no-ops.
func (s *Store) onboardActors(ctx context.Context, merchantID, customerID string) error {
	now := time.Now().UTC()
	if _, ok := s.seen.Load("m:" + merchantID); !ok {
		_, err := s.db.Collection(colMerchants).UpdateOne(ctx,
			bson.M{"_id": merchantID},
			bson.M{"$setOnInsert": bson.M{"created_at": now, "balances": bson.M{}}},
			options.UpdateOne().SetUpsert(true))
		if err != nil {
			return fmt.Errorf("onboard merchant: %w", err)
		}
		s.seen.Store("m:"+merchantID, struct{}{})
	}
	if _, ok := s.seen.Load("c:" + customerID); !ok {
		_, err := s.db.Collection(colCustomers).UpdateOne(ctx,
			bson.M{"_id": customerID},
			bson.M{"$setOnInsert": bson.M{"created_at": now}},
			options.UpdateOne().SetUpsert(true))
		if err != nil {
			return fmt.Errorf("onboard customer: %w", err)
		}
		s.seen.Store("c:"+customerID, struct{}{})
	}
	return nil
}

// ---------- Payment doc + mapping ----------

type paymentDoc struct {
	ID              string    `bson:"_id"`
	MerchantID      string    `bson:"merchant_id"`
	CustomerID      string    `bson:"customer_id"`
	AmountMinor     int64     `bson:"amount_minor"`
	Currency        string    `bson:"currency"`
	Method          string    `bson:"method"`
	Status          string    `bson:"status"`
	RefundedMinor   int64     `bson:"refunded_minor"`
	NetworkFeeMinor int64     `bson:"network_fee_minor,omitempty"` // set-once by SettleNetworkFee; absent == 0
	CreatedAt       time.Time `bson:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at"`
	// Refunds are embedded: bounded (sum ≤ amount) and always read with the payment.
	Refunds []refundElem `bson:"refunds,omitempty"`
}

// refundElem is an embedded refund. No merchant_id/currency/payment_id — the
// parent payment doc carries them; toDomain fills them in from the parent.
type refundElem struct {
	ID          string    `bson:"id"`
	AmountMinor int64     `bson:"amount_minor"`
	Status      string    `bson:"status"`
	CreatedAt   time.Time `bson:"created_at"`
}

func (r refundElem) toDomain(parent paymentDoc) domain.Refund {
	return domain.Refund{
		ID:          r.ID,
		PaymentID:   parent.ID,
		MerchantID:  parent.MerchantID,
		AmountMinor: r.AmountMinor,
		Currency:    domain.Currency(parent.Currency),
		Status:      domain.RefundStatus(r.Status),
		CreatedAt:   r.CreatedAt,
	}
}

func (p paymentDoc) toDomain() domain.Payment {
	return domain.Payment{
		ID:              p.ID,
		MerchantID:      p.MerchantID,
		CustomerID:      p.CustomerID,
		AmountMinor:     p.AmountMinor,
		Currency:        domain.Currency(p.Currency),
		Method:          domain.PaymentMethod(p.Method),
		Status:          domain.PaymentStatus(p.Status),
		RefundedMinor:   p.RefundedMinor,
		NetworkFeeMinor: p.NetworkFeeMinor,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
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
	if strings.TrimSpace(in.CustomerID) == "" {
		return domain.Payment{}, fmt.Errorf("%w: customer_id required", store.ErrInvalidState)
	}

	// Onboard before the payment insert: a crash in between leaves a harmless
	// orphan merchant/customer, never a payment pointing at nothing.
	if err := s.onboardActors(ctx, in.MerchantID, in.CustomerID); err != nil {
		return domain.Payment{}, err
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

// RefundPayment — task 17. Merchant chooses amount. Constraint: 0 < amount <= (captured - already_refunded).
//
// Concurrency-safe AND atomic: one FindOneAndUpdate with an aggregation-pipeline
// update bumps refunded_minor, flips status to refunded when fully consumed, and
// appends the embedded refund element — a single document write, so there is no
// window where the counter and the refund list disagree. The `$expr` in the filter
// guarantees only one refund can drive refunded_minor past amount_minor. If the
// update matches zero rows, either the payment doesn't exist, wasn't captured/settled,
// or the refund would exceed the remaining amount — we probe to tell which.
func (s *Store) RefundPayment(ctx context.Context, paymentID string, amountMinor int64, _ string) (domain.Refund, error) {
	if amountMinor <= 0 {
		return domain.Refund{}, fmt.Errorf("%w: refund amount must be > 0", store.ErrInvalidState)
	}

	// Conditional: only touch the payment if the refund fits and the payment is
	// captured or settled (task 52: settled payments are refundable; the balance
	// debit happens later in SettleRefund).
	filter := bson.M{
		"_id":    paymentID,
		"status": bson.M{"$in": bson.A{string(domain.StatusCaptured), string(domain.StatusSettled)}},
		"$expr": bson.M{
			"$lte": bson.A{
				bson.M{"$add": bson.A{"$refunded_minor", amountMinor}},
				"$amount_minor",
			},
		},
	}
	now := time.Now().UTC()
	refundID := newID("re")
	newRefunded := bson.M{"$add": bson.A{"$refunded_minor", amountMinor}}
	// Pipeline update: $set expressions can reference current field values, which
	// lets the counter bump, the status flip, and the array append happen in one write.
	update := mongo.Pipeline{
		{{Key: "$set", Value: bson.M{
			"refunded_minor": newRefunded,
			// Fully refunded -> flip status; otherwise keep whatever it was.
			"status": bson.M{"$cond": bson.A{
				bson.M{"$eq": bson.A{newRefunded, "$amount_minor"}},
				string(domain.StatusRefunded),
				"$status",
			}},
			// $ifNull: older docs may lack the array entirely.
			"refunds": bson.M{"$concatArrays": bson.A{
				bson.M{"$ifNull": bson.A{"$refunds", bson.A{}}},
				bson.A{bson.M{
					"id":           refundID,
					"amount_minor": amountMinor,
					"status":       string(domain.RefundCreated),
					"created_at":   now,
				}},
			}},
			"updated_at": now,
		}}},
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
		if current.Status != string(domain.StatusCaptured) && current.Status != string(domain.StatusSettled) {
			return domain.Refund{}, fmt.Errorf("%w: payment status is %s", store.ErrInvalidState, current.Status)
		}
		// Must be the over-refund case.
		return domain.Refund{}, store.ErrOverRefund
	}
	if err != nil {
		return domain.Refund{}, err
	}

	// task 63: refund currency is pinned to the payment's — never caller-supplied.
	// toDomain(updated) fills PaymentID/MerchantID/Currency from the parent doc.
	return refundElem{
		ID:          refundID,
		AmountMinor: amountMinor,
		Status:      string(domain.RefundCreated),
		CreatedAt:   now,
	}.toDomain(updated), nil
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

// ---------- Settlement + balances (tasks 51/52/53) ----------

// applyBalanceDelta $inc's the merchant's per-currency counters via dotted paths
// into the balances map. Money stays int64 minor units (task 18 decision) — $inc
// on int64 is atomic and exact. Dotted-path $inc creates missing intermediate
// subdocs and the upsert covers a missing merchant doc, so there is no first-write
// race and no dup-key retry needed (the old collection-per-row shape needed one).
func (s *Store) applyBalanceDelta(ctx context.Context, merchantID, currency string, balanceDelta, feesDelta int64) error {
	_, err := s.db.Collection(colMerchants).UpdateOne(ctx,
		bson.M{"_id": merchantID},
		bson.M{
			"$inc": bson.M{
				"balances." + currency + ".balance_minor": balanceDelta,
				"balances." + currency + ".fees_minor":    feesDelta,
			},
			"$set": bson.M{"updated_at": time.Now().UTC()},
		},
		options.UpdateOne().SetUpsert(true))
	return err
}

// SettlePayment — task 51 (UC on settle). captured|pending -> settled, then credit
// the merchant's per-currency balance with (amount - fee) and record the fee.
//
// The conditional status flip is the exactly-once guard: only the caller that wins
// the flip performs the balance $inc, so double-settle can never double-credit.
//
// ponytail: flip-then-inc is two writes on a standalone Mongo (testcontainers runs
// mongo:7 without a replica set, so no multi-doc txns). Crash window: process dies
// after the flip but before the $inc -> payment reads settled, balance missing the
// credit. Atlas in prod IS a replica set — upgrade path is wrapping both writes in
// client.StartSession + WithTransaction and deleting this comment.
func (s *Store) SettlePayment(ctx context.Context, paymentID string) (domain.Payment, error) {
	now := time.Now().UTC()
	filter := bson.M{
		"_id":    paymentID,
		"status": bson.M{"$in": bson.A{string(domain.StatusCaptured), string(domain.StatusPending)}},
	}
	update := bson.M{"$set": bson.M{"status": string(domain.StatusSettled), "updated_at": now}}

	res := s.db.Collection(colPayments).FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After))
	var updated paymentDoc
	err := res.Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Zero matches: missing, or an unsettleable state. Probe to tell which.
		var current paymentDoc
		findErr := s.db.Collection(colPayments).FindOne(ctx, bson.M{"_id": paymentID}).Decode(&current)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return domain.Payment{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Payment{}, findErr
		}
		return domain.Payment{}, fmt.Errorf("%w: cannot settle from status %s (only captured or pending)", store.ErrInvalidState, current.Status)
	}
	if err != nil {
		return domain.Payment{}, err
	}

	fee := domain.GripeFee(updated.AmountMinor)
	if err := s.applyBalanceDelta(ctx, updated.MerchantID, updated.Currency, updated.AmountMinor-fee, fee); err != nil {
		return domain.Payment{}, fmt.Errorf("settle balance credit: %w", err)
	}
	return updated.toDomain(), nil
}

// SettleRefund — task 52. Refund created -> settled, then debit the merchant's
// balance by (amount - fee) and hand the fee back (Gripe returns its cut on refunds).
// Same exactly-once shape as SettlePayment: the conditional flip guards the $inc.
//
// ponytail: same standalone-Mongo crash window as SettlePayment; same txn upgrade path.
func (s *Store) SettleRefund(ctx context.Context, refundID string) (domain.Refund, error) {
	now := time.Now().UTC()
	// $elemMatch pins id AND status to the SAME element; the positional $ then
	// flips exactly that element. The multikey refunds.id index serves the lookup.
	filter := bson.M{"refunds": bson.M{"$elemMatch": bson.M{
		"id":     refundID,
		"status": string(domain.RefundCreated),
	}}}
	update := bson.M{"$set": bson.M{
		"refunds.$.status": string(domain.RefundSettled),
		"updated_at":       now,
	}}

	res := s.db.Collection(colPayments).FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After))
	var updated paymentDoc
	err := res.Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Zero matches: refund missing entirely, or already past `created`.
		findErr := s.db.Collection(colPayments).FindOne(ctx,
			bson.M{"refunds.id": refundID}).Err()
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return domain.Refund{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Refund{}, findErr
		}
		return domain.Refund{}, fmt.Errorf("%w: refund %s is not in created status", store.ErrInvalidState, refundID)
	}
	if err != nil {
		return domain.Refund{}, err
	}

	// Locate the element we just flipped; merchant/currency come from the parent.
	var elem refundElem
	for _, r := range updated.Refunds {
		if r.ID == refundID {
			elem = r
			break
		}
	}
	if elem.ID == "" {
		return domain.Refund{}, fmt.Errorf("refund %s vanished from payment %s after update", refundID, updated.ID)
	}

	fee := domain.GripeFee(elem.AmountMinor)
	if err := s.applyBalanceDelta(ctx, updated.MerchantID, updated.Currency, -(elem.AmountMinor - fee), -fee); err != nil {
		return domain.Refund{}, fmt.Errorf("settle refund balance debit: %w", err)
	}
	return elem.toDomain(updated), nil
}

// SettleNetworkFee — fee-worker's mock network fee, set exactly once. SQS is
// at-least-once, so redeliveries (possibly with a different mock fee) must be
// harmless: the filter only matches while network_fee_minor is unset/0, making
// the $set idempotent by construction — no read-then-write race.
// Never touches merchant_balances or payment status.
func (s *Store) SettleNetworkFee(ctx context.Context, paymentID string, feeMinor int64) (domain.Payment, error) {
	if feeMinor <= 0 {
		return domain.Payment{}, fmt.Errorf("%w: network fee must be > 0", store.ErrInvalidState)
	}

	// nil matches a missing field; 0 covers docs written with an explicit zero.
	filter := bson.M{
		"_id":               paymentID,
		"network_fee_minor": bson.M{"$in": bson.A{nil, int64(0)}},
	}
	update := bson.M{"$set": bson.M{
		"network_fee_minor": feeMinor,
		"updated_at":        time.Now().UTC(),
	}}

	res := s.db.Collection(colPayments).FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After))
	var updated paymentDoc
	err := res.Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Missing payment, or fee already set — probe to tell which.
		var current paymentDoc
		findErr := s.db.Collection(colPayments).FindOne(ctx, bson.M{"_id": paymentID}).Decode(&current)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return domain.Payment{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Payment{}, findErr
		}
		// Redelivery: fee already recorded — no-op, return the payment as-is.
		return current.toDomain(), nil
	}
	if err != nil {
		return domain.Payment{}, err
	}
	return updated.toDomain(), nil
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

// GetMerchantBalances — task 53. The whole per-currency ledger is one _id lookup:
// balances lives as a map on the merchant doc. Sort in Go — a handful of currencies.
func (s *Store) GetMerchantBalances(ctx context.Context, merchantID string) ([]domain.Balance, error) {
	var d merchantDoc
	err := s.db.Collection(colMerchants).FindOne(ctx, bson.M{"_id": merchantID}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return []domain.Balance{}, nil // unknown merchant: empty ledger, not an error
	}
	if err != nil {
		return nil, err
	}
	currencies := make([]string, 0, len(d.Balances))
	for c := range d.Balances {
		currencies = append(currencies, c)
	}
	sort.Strings(currencies)
	out := make([]domain.Balance, len(currencies))
	for i, c := range currencies {
		out[i] = domain.Balance{
			Currency:     domain.Currency(c),
			BalanceMinor: d.Balances[c].BalanceMinor,
			FeesMinor:    d.Balances[c].FeesMinor,
		}
	}
	return out, nil
}

// AdminBalanceReport — every merchant's per-currency balance + fees, ordered
// (merchant_id, currency) asc. $objectToArray pivots the balances map into rows
// server-side — the DB does the reshape + sort, Go only maps rows.
func (s *Store) AdminBalanceReport(ctx context.Context) ([]domain.MerchantBalanceRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.M{
			"balances": bson.M{"$objectToArray": bson.M{"$ifNull": bson.A{"$balances", bson.M{}}}},
		}}},
		{{Key: "$unwind", Value: "$balances"}},
		{{Key: "$project", Value: bson.M{
			"_id":           0,
			"merchant_id":   "$_id",
			"currency":      "$balances.k",
			"balance_minor": "$balances.v.balance_minor",
			"fees_minor":    "$balances.v.fees_minor",
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "merchant_id", Value: 1}, {Key: "currency", Value: 1}}}},
	}
	cur, err := s.db.Collection(colMerchants).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("balance report aggregate: %w", err)
	}
	defer cur.Close(ctx)

	var rows []struct {
		MerchantID   string `bson:"merchant_id"`
		Currency     string `bson:"currency"`
		BalanceMinor int64  `bson:"balance_minor"`
		FeesMinor    int64  `bson:"fees_minor"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("balance report decode: %w", err)
	}
	out := make([]domain.MerchantBalanceRow, len(rows))
	for i, r := range rows {
		out[i] = domain.MerchantBalanceRow{
			MerchantID:   r.MerchantID,
			Currency:     domain.Currency(r.Currency),
			BalanceMinor: r.BalanceMinor,
			FeesMinor:    r.FeesMinor,
		}
	}
	return out, nil
}

// AdminRevenueReport — Gripe's fee take per currency. Revenue is derived from the
// merchants' embedded balances (fees_minor already nets out refund fee returns):
// $objectToArray pivots the map, then a single $group over the small merchants
// collection is the whole report — no fee_ledger, no re-scan of payments.
func (s *Store) AdminRevenueReport(ctx context.Context) ([]domain.CurrencyTotal, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.M{
			"balances": bson.M{"$objectToArray": bson.M{"$ifNull": bson.A{"$balances", bson.M{}}}},
		}}},
		{{Key: "$unwind", Value: "$balances"}},
		{{Key: "$group", Value: bson.M{
			"_id":         "$balances.k",
			"total_minor": bson.M{"$sum": "$balances.v.fees_minor"},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	}
	cur, err := s.db.Collection(colMerchants).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("revenue report aggregate: %w", err)
	}
	defer cur.Close(ctx)

	var rows []struct {
		Currency   string `bson:"_id"`
		TotalMinor int64  `bson:"total_minor"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("revenue report decode: %w", err)
	}
	out := make([]domain.CurrencyTotal, len(rows))
	for i, r := range rows {
		out[i] = domain.CurrencyTotal{Currency: domain.Currency(r.Currency), TotalMinor: r.TotalMinor}
	}
	return out, nil
}

// AdminVolumeReport — admin aggregate: non-declined payment volume grouped by
// (merchant, UTC day, currency), sorted ascending. Single aggregation pipeline;
// the DB does the group + sort, Go only maps rows.
//
// ponytail: $match leads with the created_at range, which the existing
// (merchant_id, created_at) index can't serve — a created_at collection scan is
// fine at this scale; add {created_at: 1} if the payments collection grows hot.
func (s *Store) AdminVolumeReport(ctx context.Context, from, to time.Time) ([]domain.MerchantDailyVolume, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"status":     bson.M{"$ne": string(domain.StatusDeclined)},
			"created_at": bson.M{"$gte": from, "$lt": to},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"merchant_id": "$merchant_id",
				// $dateToString defaults to UTC when timezone is unset — pin it anyway.
				"day": bson.M{"$dateToString": bson.M{
					"format": "%Y-%m-%d", "date": "$created_at", "timezone": "UTC",
				}},
				"currency": "$currency",
			},
			"total_minor": bson.M{"$sum": "$amount_minor"},
			"count":       bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "_id.merchant_id", Value: 1},
			{Key: "_id.day", Value: 1},
			{Key: "_id.currency", Value: 1},
		}}},
	}

	cur, err := s.db.Collection(colPayments).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("volume report aggregate: %w", err)
	}
	defer cur.Close(ctx)

	var rows []struct {
		ID struct {
			MerchantID string `bson:"merchant_id"`
			Day        string `bson:"day"`
			Currency   string `bson:"currency"`
		} `bson:"_id"`
		TotalMinor int64 `bson:"total_minor"`
		Count      int64 `bson:"count"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("volume report decode: %w", err)
	}

	out := make([]domain.MerchantDailyVolume, len(rows))
	for i, r := range rows {
		out[i] = domain.MerchantDailyVolume{
			MerchantID: r.ID.MerchantID,
			Day:        r.ID.Day,
			Currency:   domain.Currency(r.ID.Currency),
			TotalMinor: r.TotalMinor,
			Count:      r.Count,
		}
	}
	return out, nil
}

// ---------- Subscriptions ----------

type subscriptionDoc struct {
	ID           string    `bson:"_id"`
	MerchantID   string    `bson:"merchant_id"`
	CustomerID   string    `bson:"customer_id"`
	AmountMinor  int64     `bson:"amount_minor"`
	Currency     string    `bson:"currency"`
	Method       string    `bson:"method"`
	Cadence      string    `bson:"cadence"`
	Status       string    `bson:"status"`
	NextChargeAt time.Time `bson:"next_charge_at"`
	NextCycleIdx int64     `bson:"next_cycle_index"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at,omitempty"`
}

func (d subscriptionDoc) toDomain() domain.Subscription {
	return domain.Subscription{
		ID:           d.ID,
		MerchantID:   d.MerchantID,
		CustomerID:   d.CustomerID,
		AmountMinor:  d.AmountMinor,
		Currency:     domain.Currency(d.Currency),
		Method:       domain.PaymentMethod(d.Method),
		Cadence:      domain.SubscriptionCadence(d.Cadence),
		Status:       domain.SubscriptionStatus(d.Status),
		NextChargeAt: d.NextChargeAt,
		NextCycleIdx: d.NextCycleIdx,
		CreatedAt:    d.CreatedAt,
	}
}

// CreateSubscription — task 19 (UC-7). Persists an active subscription with next_charge_at set.
func (s *Store) CreateSubscription(ctx context.Context, in domain.CreateSubscriptionInput) (domain.Subscription, error) {
	if strings.TrimSpace(in.MerchantID) == "" {
		return domain.Subscription{}, fmt.Errorf("%w: merchant_id required", store.ErrInvalidState)
	}
	if strings.TrimSpace(in.CustomerID) == "" {
		return domain.Subscription{}, fmt.Errorf("%w: customer_id required", store.ErrInvalidState)
	}
	if in.AmountMinor <= 0 {
		return domain.Subscription{}, fmt.Errorf("%w: amount_minor must be > 0", store.ErrInvalidState)
	}
	if !in.Currency.Valid() {
		return domain.Subscription{}, fmt.Errorf("%w: currency %q", store.ErrInvalidState, in.Currency)
	}
	if !in.Method.Valid() {
		return domain.Subscription{}, fmt.Errorf("%w: method %q", store.ErrInvalidState, in.Method)
	}
	if !in.Cadence.Valid() {
		return domain.Subscription{}, fmt.Errorf("%w: cadence %q", store.ErrInvalidState, in.Cadence)
	}
	// Same implicit onboarding as CreatePayment (crash between = harmless orphan).
	if err := s.onboardActors(ctx, in.MerchantID, in.CustomerID); err != nil {
		return domain.Subscription{}, err
	}
	start := in.StartAt
	if start.IsZero() {
		start = time.Now().UTC()
	}
	now := time.Now().UTC()
	doc := subscriptionDoc{
		ID:           newID("sub"),
		MerchantID:   in.MerchantID,
		CustomerID:   in.CustomerID,
		AmountMinor:  in.AmountMinor,
		Currency:     string(in.Currency),
		Method:       string(in.Method),
		Cadence:      string(in.Cadence),
		Status:       string(domain.SubActive),
		NextChargeAt: start,
		NextCycleIdx: 0,
		CreatedAt:    now,
	}
	if _, err := s.db.Collection(colSubs).InsertOne(ctx, doc); err != nil {
		return domain.Subscription{}, fmt.Errorf("insert subscription: %w", err)
	}
	return doc.toDomain(), nil
}

// CancelSubscription — task 21 (UC-9). Idempotent: cancelling already-cancelled is fine.
func (s *Store) CancelSubscription(ctx context.Context, subscriptionID string) (domain.Subscription, error) {
	now := time.Now().UTC()
	res := s.db.Collection(colSubs).FindOneAndUpdate(ctx,
		bson.M{"_id": subscriptionID},
		bson.M{"$set": bson.M{"status": string(domain.SubCancelled), "updated_at": now}},
		options.FindOneAndUpdate().SetReturnDocument(options.After))
	var d subscriptionDoc
	err := res.Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.Subscription{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Subscription{}, err
	}
	return d.toDomain(), nil
}

// DueSubscriptions — task 20 (UC-8). The cycler pulls these on every tick.
// ponytail: no locking — cycler uses conditional AdvanceSubscription (matches on
// current cycle_index) to make retries safe.
func (s *Store) DueSubscriptions(ctx context.Context, asOf time.Time, limit int) ([]domain.Subscription, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cur, err := s.db.Collection(colSubs).Find(ctx,
		bson.M{
			"status":         string(domain.SubActive),
			"next_charge_at": bson.M{"$lte": asOf},
		},
		options.Find().
			SetSort(bson.D{{Key: "next_charge_at", Value: 1}}).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []subscriptionDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]domain.Subscription, len(docs))
	for i, d := range docs {
		out[i] = d.toDomain()
	}
	return out, nil
}

// AdvanceSubscription moves next_charge_at forward and bumps NextCycleIdx atomically.
// Called by the cycler after a successful CreatePayment for the current cycle. The
// conditional match on current cycle_index makes retries a no-op.
//
// Not on the Store interface yet — worker calls it directly on the concrete store.
// Promote to interface when the PG port lands.
func (s *Store) AdvanceSubscription(ctx context.Context, subscriptionID string, currentCycleIdx int64, nextChargeAt time.Time) error {
	_, err := s.db.Collection(colSubs).UpdateOne(ctx,
		bson.M{"_id": subscriptionID, "next_cycle_index": currentCycleIdx},
		bson.M{"$set": bson.M{
			"next_charge_at":   nextChargeAt,
			"next_cycle_index": currentCycleIdx + 1,
			"updated_at":       time.Now().UTC(),
		}},
	)
	return err
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
