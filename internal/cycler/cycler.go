// Package cycler polls DueSubscriptions and creates the next payment per cycle.
// Idempotent per (subscription_id, cycle_index): if two workers race, the conditional
// AdvanceSubscription ensures only one wins; the loser's payment insert is a no-op
// via the unique idempotency key baked into CreatePayment's key argument.
package cycler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
)

// Advancer is what the cycler needs on top of the Store interface. The Mongo store
// implements it directly; when PG lands, promote to the Store interface.
type Advancer interface {
	DueSubscriptions(ctx context.Context, asOf time.Time, limit int) ([]domain.Subscription, error)
	CreatePayment(ctx context.Context, in domain.CreatePaymentInput, idempotencyKey string) (domain.Payment, error)
	AdvanceSubscription(ctx context.Context, subscriptionID string, currentCycleIdx int64, nextChargeAt time.Time) error
}

type Cycler struct {
	store    Advancer
	log      *slog.Logger
	batch    int           // how many due subs to pull per tick
	interval time.Duration // sleep between ticks when nothing is due
}

func New(store Advancer, log *slog.Logger, batch int, interval time.Duration) *Cycler {
	if batch <= 0 {
		batch = 100
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Cycler{store: store, log: log, batch: batch, interval: interval}
}

// Run blocks until ctx is cancelled. Each iteration: fetch due subs, charge them,
// advance next_charge_at. If a tick has work, immediately loop again (drain).
func (c *Cycler) Run(ctx context.Context) error {
	c.log.Info("cycler started", "batch", c.batch, "interval", c.interval)
	for {
		select {
		case <-ctx.Done():
			c.log.Info("cycler stopping")
			return ctx.Err()
		default:
		}

		n, err := c.Tick(ctx)
		if err != nil {
			c.log.Error("cycler tick", "err", err)
		}
		if n == 0 {
			// Nothing to do — sleep before the next poll.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.interval):
			}
		}
	}
}

// Tick services one batch. Returns how many subs it charged (0 or more).
// Exposed for tests: contract test can drive Tick directly instead of Run.
func (c *Cycler) Tick(ctx context.Context) (int, error) {
	due, err := c.store.DueSubscriptions(ctx, time.Now().UTC(), c.batch)
	if err != nil {
		return 0, fmt.Errorf("due subs: %w", err)
	}
	charged := 0
	for _, sub := range due {
		if err := c.chargeOne(ctx, sub); err != nil {
			c.log.Warn("cycle failed", "sub_id", sub.ID, "err", err)
			continue
		}
		charged++
	}
	return charged, nil
}

// chargeOne creates a payment for the sub's current cycle and advances the cycle.
// Idempotency key is deterministic per (subscription_id, cycle_index) — if a previous
// crash already inserted this payment, the store returns the replay and we still advance.
func (c *Cycler) chargeOne(ctx context.Context, sub domain.Subscription) error {
	idemKey := fmt.Sprintf("sub-%s-cycle-%d", sub.ID, sub.NextCycleIdx)
	_, err := c.store.CreatePayment(ctx, domain.CreatePaymentInput{
		MerchantID:  sub.MerchantID,
		CustomerID:  sub.CustomerID,
		AmountMinor: sub.AmountMinor,
		Currency:    sub.Currency,
		Method:      sub.Method,
	}, idemKey)
	if err != nil {
		return fmt.Errorf("create payment: %w", err)
	}
	nextAt := sub.Cadence.Advance(sub.NextChargeAt)
	if err := c.store.AdvanceSubscription(ctx, sub.ID, sub.NextCycleIdx, nextAt); err != nil {
		return fmt.Errorf("advance: %w", err)
	}
	return nil
}
