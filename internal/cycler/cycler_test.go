package cycler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
)

// fakeStore satisfies Advancer for a table-driven unit test — no Mongo.
type fakeStore struct {
	mu       sync.Mutex
	subs     map[string]*domain.Subscription
	payments []domain.Payment
	// track idempotency keys we've seen so retries don't insert twice.
	seenIdem map[string]domain.Payment
}

func newFake() *fakeStore {
	return &fakeStore{
		subs:     map[string]*domain.Subscription{},
		seenIdem: map[string]domain.Payment{},
	}
}

func (f *fakeStore) put(s domain.Subscription) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc := s
	f.subs[s.ID] = &sc
}

func (f *fakeStore) DueSubscriptions(_ context.Context, asOf time.Time, limit int) ([]domain.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Subscription
	for _, s := range f.subs {
		if s.Status == domain.SubActive && !s.NextChargeAt.After(asOf) {
			out = append(out, *s)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeStore) CreatePayment(_ context.Context, in domain.CreatePaymentInput, key string) (domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.seenIdem[key]; ok {
		return p, nil
	}
	p := domain.Payment{
		ID:          "pay_" + key,
		MerchantID:  in.MerchantID,
		CustomerID:  in.CustomerID,
		AmountMinor: in.AmountMinor,
		Currency:    in.Currency,
		Method:      in.Method,
		Status:      domain.StatusCaptured,
		CreatedAt:   time.Now(),
	}
	f.payments = append(f.payments, p)
	f.seenIdem[key] = p
	return p, nil
}

func (f *fakeStore) AdvanceSubscription(_ context.Context, subID string, currentCycleIdx int64, nextChargeAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.subs[subID]
	if !ok {
		return nil
	}
	if s.NextCycleIdx != currentCycleIdx {
		return nil // race: another worker already advanced; benign
	}
	s.NextCycleIdx++
	s.NextChargeAt = nextChargeAt
	return nil
}

// silent slog for tests.
func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCycler_ChargesOneDueSub(t *testing.T) {
	fs := newFake()
	fs.put(domain.Subscription{
		ID:           "sub_1",
		MerchantID:   "mer_A",
		CustomerID:   "cus_A",
		AmountMinor:  500,
		Currency:     domain.USD,
		Method:       domain.MethodCard,
		Cadence:      domain.CadenceDaily,
		Status:       domain.SubActive,
		NextChargeAt: time.Now().Add(-time.Minute), // due
		NextCycleIdx: 0,
	})
	c := New(fs, silent(), 100, time.Millisecond)
	n, err := c.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n != 1 {
		t.Fatalf("charged=%d, want 1", n)
	}
	if len(fs.payments) != 1 {
		t.Fatalf("payments=%d, want 1", len(fs.payments))
	}
	s := fs.subs["sub_1"]
	if s.NextCycleIdx != 1 {
		t.Fatalf("cycle_idx=%d, want 1", s.NextCycleIdx)
	}
	if !s.NextChargeAt.After(time.Now()) {
		t.Fatalf("next_charge_at not advanced: %v", s.NextChargeAt)
	}
}

func TestCycler_TwoTicksBackToBack_NoDoubleCharge(t *testing.T) {
	fs := newFake()
	fs.put(domain.Subscription{
		ID:           "sub_1",
		MerchantID:   "mer_A", CustomerID: "cus_A",
		AmountMinor: 500, Currency: domain.USD, Method: domain.MethodCard,
		Cadence:      domain.CadenceMonthly,
		Status:       domain.SubActive,
		NextChargeAt: time.Now().Add(-time.Hour),
	})
	c := New(fs, silent(), 100, time.Millisecond)
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second tick immediately: sub's next_charge_at is a month out now, so nothing due.
	n, err := c.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second tick charged=%d, want 0", n)
	}
	if len(fs.payments) != 1 {
		t.Fatalf("payments=%d, want 1", len(fs.payments))
	}
}

func TestCycler_SkipsCancelled(t *testing.T) {
	fs := newFake()
	fs.put(domain.Subscription{
		ID: "sub_1", MerchantID: "mer_A", CustomerID: "cus_A",
		AmountMinor: 500, Currency: domain.USD, Method: domain.MethodCard,
		Cadence:      domain.CadenceDaily,
		Status:       domain.SubCancelled,
		NextChargeAt: time.Now().Add(-time.Hour),
	})
	c := New(fs, silent(), 100, time.Millisecond)
	n, err := c.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fs.payments) != 0 {
		t.Fatalf("cancelled sub charged: n=%d payments=%d", n, len(fs.payments))
	}
}
