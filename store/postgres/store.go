// Package postgres is the PostgreSQL implementation of internal/store.Store.
// Targets RDS PostgreSQL; tests use vanilla PG via testcontainers.
// See .claude/agents/postgres-idiom.md for the idiom cheatsheet.
package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/robin-mongodb/gripe/internal/api"
	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	pool *pgxpool.Pool
}

// New opens a pool, pings, and runs pending migrations.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgx pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		return nil, fmt.Errorf("pg ping: %w", err)
	}
	if err := runMigrations(ctx, dsn); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return &Store{pool: pool}, nil
}

// runMigrations opens a database/sql handle just for goose, then closes it.
// pgxpool doesn't expose database/sql, but stdlib.OpenDBFromPool does. Small dance;
// keeps goose happy without a second pool.
func runMigrations(ctx context.Context, dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

func (s *Store) Close(ctx context.Context) error { s.pool.Close(); return nil }
func (s *Store) Ping(ctx context.Context) error  { return s.pool.Ping(ctx) }

// ---------- IdempotencyStore ----------

func (s *Store) PutIdempotencyRecord(ctx context.Context, rec api.IdempotencyRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO idempotency_keys (key, actor_role, actor_id, body_hash, status, response, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rec.Key, rec.ActorRole, rec.ActorID, rec.BodyHash, rec.StatusCode, rec.Response, rec.CreatedAt, rec.ExpiresAt,
	)
	if isUniqueViolation(err) {
		return api.ErrIdempotencyExists
	}
	return err
}

func (s *Store) GetIdempotencyRecord(ctx context.Context, key string) (api.IdempotencyRecord, error) {
	var r api.IdempotencyRecord
	err := s.pool.QueryRow(ctx, `
		SELECT key, actor_role, actor_id, body_hash, status, response, created_at, expires_at
		FROM idempotency_keys WHERE key = $1`, key,
	).Scan(&r.Key, &r.ActorRole, &r.ActorID, &r.BodyHash, &r.StatusCode, &r.Response, &r.CreatedAt, &r.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.IdempotencyRecord{}, store.ErrNotFound
	}
	if err != nil {
		return api.IdempotencyRecord{}, err
	}
	return r, nil
}

// ---------- Payments ----------

const declineTail = 13

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

	p := domain.Payment{
		ID:          newID("pay"),
		MerchantID:  in.MerchantID,
		CustomerID:  in.CustomerID,
		AmountMinor: in.AmountMinor,
		Currency:    in.Currency,
		Method:      in.Method,
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO payments (id, merchant_id, customer_id, amount_minor, currency, method, status, refunded_minor, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,0,$8,$9)`,
		p.ID, p.MerchantID, p.CustomerID, p.AmountMinor, string(p.Currency), string(p.Method), string(p.Status), p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return domain.Payment{}, fmt.Errorf("insert payment: %w", err)
	}
	return p, nil
}

func (s *Store) GetPayment(ctx context.Context, id string, actor domain.Actor) (domain.Payment, error) {
	p, err := s.selectPaymentByID(ctx, id)
	if err != nil {
		return domain.Payment{}, err
	}
	switch actor.Role {
	case domain.RoleAdmin:
		// sees all
	case domain.RoleMerchant:
		if p.MerchantID != actor.ID {
			return domain.Payment{}, store.ErrForbidden
		}
	case domain.RoleCustomer:
		if p.CustomerID != actor.ID {
			return domain.Payment{}, store.ErrForbidden
		}
	default:
		return domain.Payment{}, store.ErrForbidden
	}
	return p, nil
}

func (s *Store) selectPaymentByID(ctx context.Context, id string) (domain.Payment, error) {
	var p domain.Payment
	var cur, meth, stat string
	err := s.pool.QueryRow(ctx, `
		SELECT id, merchant_id, customer_id, amount_minor, currency, method, status, refunded_minor, created_at, updated_at
		FROM payments WHERE id = $1`, id,
	).Scan(&p.ID, &p.MerchantID, &p.CustomerID, &p.AmountMinor, &cur, &meth, &stat, &p.RefundedMinor, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Payment{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, err
	}
	p.Currency = domain.Currency(cur)
	p.Method = domain.PaymentMethod(meth)
	p.Status = domain.PaymentStatus(stat)
	return p, nil
}

// CapturePayment — task 16. authorized -> captured only.
func (s *Store) CapturePayment(ctx context.Context, id string) (domain.Payment, error) {
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx, `
		UPDATE payments SET status = $1, updated_at = $2
		WHERE id = $3 AND status = $4`,
		string(domain.StatusCaptured), now, id, string(domain.StatusAuthorized),
	)
	if err != nil {
		return domain.Payment{}, err
	}
	if tag.RowsAffected() == 0 {
		// Distinguish "no such payment" from "wrong state".
		cur, findErr := s.selectPaymentByID(ctx, id)
		if errors.Is(findErr, store.ErrNotFound) {
			return domain.Payment{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Payment{}, findErr
		}
		return domain.Payment{}, fmt.Errorf("%w: cannot capture from status %s (only from authorized)", store.ErrInvalidState, cur.Status)
	}
	return s.selectPaymentByID(ctx, id)
}

// RefundPayment — task 17. Conditional UPDATE on refunded_minor keeps concurrent refunds safe.
func (s *Store) RefundPayment(ctx context.Context, paymentID string, amountMinor int64, _ string) (domain.Refund, error) {
	if amountMinor <= 0 {
		return domain.Refund{}, fmt.Errorf("%w: refund amount must be > 0", store.ErrInvalidState)
	}
	now := time.Now().UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Refund{}, err
	}
	defer tx.Rollback(ctx)

	var currency string
	var amount, refunded int64
	// Conditional update inside the tx: only bump if the new sum fits and status is captured.
	err = tx.QueryRow(ctx, `
		UPDATE payments
		   SET refunded_minor = refunded_minor + $1, updated_at = $2
		 WHERE id = $3
		   AND status = 'captured'
		   AND refunded_minor + $1 <= amount_minor
		RETURNING currency, amount_minor, refunded_minor`,
		amountMinor, now, paymentID,
	).Scan(&currency, &amount, &refunded)
	if errors.Is(err, pgx.ErrNoRows) {
		// Diagnose which failure mode.
		cur, findErr := s.selectPaymentByID(ctx, paymentID)
		if errors.Is(findErr, store.ErrNotFound) {
			return domain.Refund{}, store.ErrNotFound
		}
		if findErr != nil {
			return domain.Refund{}, findErr
		}
		if cur.Status != domain.StatusCaptured {
			return domain.Refund{}, fmt.Errorf("%w: payment status is %s", store.ErrInvalidState, cur.Status)
		}
		return domain.Refund{}, store.ErrOverRefund
	}
	if err != nil {
		return domain.Refund{}, err
	}

	// If fully refunded, flip status.
	if refunded == amount {
		_, err = tx.Exec(ctx, `UPDATE payments SET status='refunded', updated_at=$1 WHERE id=$2`, now, paymentID)
		if err != nil {
			return domain.Refund{}, err
		}
	}

	r := domain.Refund{
		ID:          newID("re"),
		PaymentID:   paymentID,
		AmountMinor: amountMinor,
		Currency:    domain.Currency(currency),
		CreatedAt:   now,
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO refunds (id, payment_id, amount_minor, currency, created_at)
		VALUES ($1,$2,$3,$4,$5)`,
		r.ID, r.PaymentID, r.AmountMinor, string(r.Currency), r.CreatedAt,
	)
	if err != nil {
		return domain.Refund{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Refund{}, err
	}
	return r, nil
}

// ---------- Listing ----------

const listPageMax = 50

func (s *Store) ListMerchantPayments(ctx context.Context, merchantID string, filters domain.Filters, cursor domain.Cursor) (domain.Page, error) {
	return s.listPayments(ctx, merchantID, filters, cursor)
}
func (s *Store) ListAllPayments(ctx context.Context, filters domain.Filters, cursor domain.Cursor) (domain.Page, error) {
	return s.listPayments(ctx, "", filters, cursor)
}

func (s *Store) listPayments(ctx context.Context, merchantScope string, f domain.Filters, cursor domain.Cursor) (domain.Page, error) {
	// Build the WHERE dynamically with placeholders. Args grow with the params we actually use.
	var (
		where []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	switch {
	case merchantScope != "":
		where = append(where, "merchant_id = "+arg(merchantScope))
	case f.MerchantID != "":
		where = append(where, "merchant_id = "+arg(f.MerchantID))
	}
	if f.Status != "" {
		where = append(where, "status = "+arg(string(f.Status)))
	}
	if f.Method != "" {
		where = append(where, "method = "+arg(string(f.Method)))
	}
	if f.Currency != "" {
		where = append(where, "currency = "+arg(string(f.Currency)))
	}
	if !f.FromTime.IsZero() {
		where = append(where, "created_at >= "+arg(f.FromTime))
	}
	if !f.ToTime.IsZero() {
		where = append(where, "created_at <= "+arg(f.ToTime))
	}
	if f.MinMinor > 0 {
		where = append(where, "amount_minor >= "+arg(f.MinMinor))
	}
	if f.MaxMinor > 0 {
		where = append(where, "amount_minor <= "+arg(f.MaxMinor))
	}
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return domain.Page{}, fmt.Errorf("%w: bad cursor", store.ErrInvalidState)
		}
		// Keyset predicate matches Mongo: (created_at, id) < cursor when sorting DESC.
		where = append(where, fmt.Sprintf("(created_at < %s OR (created_at = %s AND id < %s))",
			arg(c.CreatedAt), arg(c.CreatedAt), arg(c.ID)))
	}

	sqlText := `SELECT id, merchant_id, customer_id, amount_minor, currency, method, status, refunded_minor, created_at, updated_at FROM payments`
	if len(where) > 0 {
		sqlText += " WHERE " + strings.Join(where, " AND ")
	}
	sqlText += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT %d", listPageMax+1)

	rows, err := s.pool.Query(ctx, sqlText, args...)
	if err != nil {
		return domain.Page{}, err
	}
	defer rows.Close()

	items := make([]domain.Payment, 0, listPageMax+1)
	for rows.Next() {
		var p domain.Payment
		var cur, meth, stat string
		if err := rows.Scan(&p.ID, &p.MerchantID, &p.CustomerID, &p.AmountMinor, &cur, &meth, &stat, &p.RefundedMinor, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return domain.Page{}, err
		}
		p.Currency = domain.Currency(cur)
		p.Method = domain.PaymentMethod(meth)
		p.Status = domain.PaymentStatus(stat)
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return domain.Page{}, err
	}

	var next domain.Cursor
	if len(items) > listPageMax {
		last := items[listPageMax-1]
		next = encodeCursor(paymentCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		items = items[:listPageMax]
	}
	return domain.Page{Items: items, NextCursor: next}, nil
}

// ---------- Subscriptions ----------

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
	start := in.StartAt
	if start.IsZero() {
		start = time.Now().UTC()
	}
	now := time.Now().UTC()
	sub := domain.Subscription{
		ID:           newID("sub"),
		MerchantID:   in.MerchantID,
		CustomerID:   in.CustomerID,
		AmountMinor:  in.AmountMinor,
		Currency:     in.Currency,
		Method:       in.Method,
		Cadence:      in.Cadence,
		Status:       domain.SubActive,
		NextChargeAt: start,
		NextCycleIdx: 0,
		CreatedAt:    now,
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO subscriptions (id, merchant_id, customer_id, amount_minor, currency, method, cadence, status, next_charge_at, next_cycle_index, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,0,$10)`,
		sub.ID, sub.MerchantID, sub.CustomerID, sub.AmountMinor, string(sub.Currency), string(sub.Method),
		string(sub.Cadence), string(sub.Status), sub.NextChargeAt, sub.CreatedAt,
	)
	if err != nil {
		return domain.Subscription{}, err
	}
	return sub, nil
}

func (s *Store) CancelSubscription(ctx context.Context, id string) (domain.Subscription, error) {
	now := time.Now().UTC()
	var sub domain.Subscription
	var cur, meth, cad, stat string
	err := s.pool.QueryRow(ctx, `
		UPDATE subscriptions SET status = $1, updated_at = $2
		WHERE id = $3
		RETURNING id, merchant_id, customer_id, amount_minor, currency, method, cadence, status, next_charge_at, next_cycle_index, created_at`,
		string(domain.SubCancelled), now, id,
	).Scan(&sub.ID, &sub.MerchantID, &sub.CustomerID, &sub.AmountMinor, &cur, &meth, &cad, &stat, &sub.NextChargeAt, &sub.NextCycleIdx, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Subscription{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Subscription{}, err
	}
	sub.Currency = domain.Currency(cur)
	sub.Method = domain.PaymentMethod(meth)
	sub.Cadence = domain.SubscriptionCadence(cad)
	sub.Status = domain.SubscriptionStatus(stat)
	return sub, nil
}

func (s *Store) DueSubscriptions(ctx context.Context, asOf time.Time, limit int) ([]domain.Subscription, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// SKIP LOCKED so multiple cyclers can safely fan out (not currently used, but cheap to enable).
	rows, err := s.pool.Query(ctx, `
		SELECT id, merchant_id, customer_id, amount_minor, currency, method, cadence, status, next_charge_at, next_cycle_index, created_at
		FROM subscriptions
		WHERE status = 'active' AND next_charge_at <= $1
		ORDER BY next_charge_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED`,
		asOf, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Subscription
	for rows.Next() {
		var sub domain.Subscription
		var cur, meth, cad, stat string
		if err := rows.Scan(&sub.ID, &sub.MerchantID, &sub.CustomerID, &sub.AmountMinor, &cur, &meth, &cad, &stat, &sub.NextChargeAt, &sub.NextCycleIdx, &sub.CreatedAt); err != nil {
			return nil, err
		}
		sub.Currency = domain.Currency(cur)
		sub.Method = domain.PaymentMethod(meth)
		sub.Cadence = domain.SubscriptionCadence(cad)
		sub.Status = domain.SubscriptionStatus(stat)
		out = append(out, sub)
	}
	return out, rows.Err()
}

// AdvanceSubscription — cycler uses this. Conditional on current cycle_index so replays no-op.
func (s *Store) AdvanceSubscription(ctx context.Context, subscriptionID string, currentCycleIdx int64, nextChargeAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE subscriptions
		   SET next_charge_at = $1, next_cycle_index = $2, updated_at = $3
		 WHERE id = $4 AND next_cycle_index = $5`,
		nextChargeAt, currentCycleIdx+1, time.Now().UTC(), subscriptionID, currentCycleIdx,
	)
	return err
}

// ---------- Admin reports ----------

// AdminVolumeReport — task 31. Cross-merchant daily volume, non-declined only.
// created_at is timestamptz; AT TIME ZONE 'UTC' pins the day bucket to UTC
// regardless of the session TimeZone setting.
func (s *Store) AdminVolumeReport(ctx context.Context, from, to time.Time) ([]domain.MerchantDailyVolume, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT merchant_id,
		       to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day,
		       currency,
		       sum(amount_minor) AS total_minor,
		       count(*) AS cnt
		FROM payments
		WHERE status <> 'declined'
		  AND created_at >= $1 AND created_at < $2
		GROUP BY merchant_id, day, currency
		ORDER BY merchant_id ASC, day ASC, currency ASC`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.MerchantDailyVolume
	for rows.Next() {
		var r domain.MerchantDailyVolume
		var cur string
		if err := rows.Scan(&r.MerchantID, &r.Day, &cur, &r.TotalMinor, &r.Count); err != nil {
			return nil, err
		}
		r.Currency = domain.Currency(cur)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- Not yet implemented ----------

func (s *Store) SettlePayment(_ context.Context, _ string) (domain.Payment, error) {
	return domain.Payment{}, errors.ErrUnsupported
}
func (s *Store) SettleRefund(_ context.Context, _ string) (domain.Refund, error) {
	return domain.Refund{}, errors.ErrUnsupported
}
func (s *Store) GetMerchantBalances(_ context.Context, _ string) ([]domain.Balance, error) {
	return nil, errors.ErrUnsupported
}

// Compile-time checks.
var (
	_ store.Store          = (*Store)(nil)
	_ api.IdempotencyStore = (*Store)(nil)
	_ = sql.ErrNoRows // keep the database/sql import happy alongside pgx
)

// ---------- cursor ----------

// Same base64(json{c,i}) shape as the Mongo cursor so opaque tokens look identical.
// If we ever want a joint helper, promote to internal/store.

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

// ---------- helpers ----------

func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
