// Package store defines the use-case-shaped persistence interface Gripe's api + workers
// use. Implementations live in store/mongo and store/postgres. See CLAUDE.md and
// docs/plan.md — do not add CRUD-shaped methods here.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
)

// Sentinel errors. Impls MUST return these (or wrap them) — callers switch on identity.
var (
	ErrNotFound          = errors.New("store: not found")
	ErrForbidden         = errors.New("store: actor cannot access this resource")
	ErrInvalidState      = errors.New("store: invalid state transition")
	ErrOverRefund        = errors.New("store: refund exceeds remaining refundable amount")
	ErrCurrencyMismatch  = errors.New("store: refund currency must match payment currency")
	ErrIdempotencyReplay = errors.New("store: idempotency key already used for a different request")
)

// Store hides the how. Each method describes a use-case or a domain question,
// not a storage operation. Impls are free to be idiomatic (Mongo aggregation vs
// SQL CTE, Change Streams vs LISTEN/NOTIFY, etc.).
type Store interface {
	// Payments — writes carry an idempotency key so retries are safe.
	CreatePayment(ctx context.Context, in domain.CreatePaymentInput, idempotencyKey string) (domain.Payment, error)
	CapturePayment(ctx context.Context, paymentID string) (domain.Payment, error)
	// SettlePayment moves captured|pending -> settled and, atomically with the flip,
	// credits merchant balance by (amount - GripeFee(amount)) and adds the fee to
	// the merchant's fees-paid total. Any other starting state is ErrInvalidState.
	SettlePayment(ctx context.Context, paymentID string) (domain.Payment, error)
	RefundPayment(ctx context.Context, paymentID string, amountMinor int64, idempotencyKey string) (domain.Refund, error)
	// SettleRefund moves a refund created -> settled and, atomically, debits merchant
	// balance by (amount - GripeFee(amount)) and subtracts the fee from fees paid —
	// Gripe returns its cut on refunds. Double-settle is ErrInvalidState.
	SettleRefund(ctx context.Context, refundID string) (domain.Refund, error)

	// Reads — actor-scoped. Admin sees everything, merchant sees own, customer sees own.
	GetPayment(ctx context.Context, id string, actor domain.Actor) (domain.Payment, error)
	ListMerchantPayments(ctx context.Context, merchantID string, filters domain.Filters, cursor domain.Cursor) (domain.Page, error)
	ListAllPayments(ctx context.Context, filters domain.Filters, cursor domain.Cursor) (domain.Page, error) // admin

	// Balances — per currency; no single-currency variant.
	GetMerchantBalances(ctx context.Context, merchantID string) ([]domain.Balance, error)

	// Reports — admin (employee console). Volume per merchant per UTC day per currency,
	// excluding declined payments. from inclusive, to exclusive. Rows ordered by
	// (merchant_id, day, currency) ascending.
	AdminVolumeReport(ctx context.Context, from, to time.Time) ([]domain.MerchantDailyVolume, error)
	// Every merchant's per-currency balance + fees paid, ordered (merchant_id, currency) ascending.
	AdminBalanceReport(ctx context.Context) ([]domain.MerchantBalanceRow, error)
	// Gripe's fee revenue per currency, ordered by currency ascending.
	AdminRevenueReport(ctx context.Context) ([]domain.CurrencyTotal, error)

	// Subscriptions.
	CreateSubscription(ctx context.Context, in domain.CreateSubscriptionInput) (domain.Subscription, error)
	CancelSubscription(ctx context.Context, subscriptionID string) (domain.Subscription, error)
	DueSubscriptions(ctx context.Context, asOf time.Time, limit int) ([]domain.Subscription, error) // cycler pulls this

	// Lifecycle.
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}
