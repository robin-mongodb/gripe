// Package seed generates realistic-ish demo data via the Store interface.
// Deterministic when a seed is passed — the demo script can rely on the same ids each run.
package seed

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

type Options struct {
	Merchants     int
	CustomersPer  int
	PaymentsPer   int
	Subscriptions int // per merchant
	Seed          uint64
}

func DefaultOptions() Options {
	return Options{Merchants: 5, CustomersPer: 20, PaymentsPer: 40, Subscriptions: 3, Seed: 42}
}

type Report struct {
	Merchants     int
	Payments      int
	Settled       int
	Subscriptions int
	Duration      time.Duration
}

func Run(ctx context.Context, s store.Store, opt Options) (Report, error) {
	start := time.Now()
	// v2 rand is fully deterministic from a seed pair; nice for demo replay.
	r := rand.New(rand.NewPCG(opt.Seed, opt.Seed^0xdeadbeef))

	currencies := []domain.Currency{domain.USD, domain.GBP, domain.EUR}
	methods := []domain.PaymentMethod{
		domain.MethodCard, domain.MethodApplePay, domain.MethodGooglePay,
		domain.MethodDirectDebit, domain.MethodBankTransfer,
	}
	cadences := []domain.SubscriptionCadence{domain.CadenceDaily, domain.CadenceWeekly, domain.CadenceMonthly}

	rep := Report{}
	for m := 0; m < opt.Merchants; m++ {
		merchantID := fmt.Sprintf("mer_seed_%03d", m)
		rep.Merchants++

		// One-shot payments.
		for i := 0; i < opt.PaymentsPer; i++ {
			customerID := fmt.Sprintf("cus_seed_%03d_%03d", m, r.IntN(opt.CustomersPer))
			amount := int64(r.IntN(19_000)) + 1_000 // 10.00 to 200.00
			key := fmt.Sprintf("seed-p-%d-%d", m, i)
			p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
				MerchantID:  merchantID,
				CustomerID:  customerID,
				AmountMinor: amount,
				Currency:    currencies[r.IntN(len(currencies))],
				Method:      methods[r.IntN(len(methods))],
			}, key)
			if err != nil {
				return rep, fmt.Errorf("seed payment m=%d i=%d: %w", m, i, err)
			}
			rep.Payments++

			// Settle ~60% of settleable payments so balances + fee revenue show up
			// in the demo (captured or pending are the settleable states).
			if (p.Status == domain.StatusCaptured || p.Status == domain.StatusPending) && r.IntN(10) < 6 {
				if _, err := s.SettlePayment(ctx, p.ID); err != nil {
					return rep, fmt.Errorf("seed settle m=%d i=%d: %w", m, i, err)
				}
				rep.Settled++
			}
		}

		// Subscriptions with mixed cadences and start times (some due immediately).
		for i := 0; i < opt.Subscriptions; i++ {
			customerID := fmt.Sprintf("cus_seed_%03d_%03d", m, r.IntN(opt.CustomersPer))
			// Half start in the past (immediately due), half in the future.
			var start time.Time
			if i%2 == 0 {
				start = time.Now().UTC().Add(-time.Duration(r.IntN(72)+1) * time.Hour)
			} else {
				start = time.Now().UTC().Add(time.Duration(r.IntN(72)+1) * time.Hour)
			}
			_, err := s.CreateSubscription(ctx, domain.CreateSubscriptionInput{
				MerchantID:  merchantID,
				CustomerID:  customerID,
				AmountMinor: int64(r.IntN(9_000)) + 500,
				Currency:    currencies[r.IntN(len(currencies))],
				Method:      methods[r.IntN(len(methods))],
				Cadence:     cadences[r.IntN(len(cadences))],
				StartAt:     start,
			})
			if err != nil {
				return rep, fmt.Errorf("seed sub m=%d i=%d: %w", m, i, err)
			}
			rep.Subscriptions++
		}
	}
	rep.Duration = time.Since(start)
	return rep, nil
}
