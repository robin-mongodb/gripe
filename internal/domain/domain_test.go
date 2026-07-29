package domain

import (
	"testing"
	"time"
)

func TestCurrencyValid(t *testing.T) {
	for _, c := range []Currency{USD, GBP, EUR} {
		if !c.Valid() {
			t.Fatalf("%s should be valid", c)
		}
	}
	for _, c := range []Currency{"AUD", "", "usd"} {
		if c.Valid() {
			t.Fatalf("%q should be invalid", c)
		}
	}
}

func TestPaymentMethodValid(t *testing.T) {
	for _, m := range []PaymentMethod{MethodCard, MethodDirectDebit, MethodBankTransfer, MethodApplePay, MethodGooglePay} {
		if !m.Valid() {
			t.Fatalf("%s should be valid", m)
		}
	}
	for _, m := range []PaymentMethod{"crypto", "", "CARD"} {
		if m.Valid() {
			t.Fatalf("%q should be invalid", m)
		}
	}
}

func TestSubscriptionCadenceValid(t *testing.T) {
	for _, c := range []SubscriptionCadence{CadenceDaily, CadenceWeekly, CadenceMonthly} {
		if !c.Valid() {
			t.Fatalf("%s should be valid", c)
		}
	}
	for _, c := range []SubscriptionCadence{"yearly", "", "Daily"} {
		if c.Valid() {
			t.Fatalf("%q should be invalid", c)
		}
	}
}

func TestSubscriptionCadenceAdvance(t *testing.T) {
	base := timeFromISO("2026-01-31T00:00:00Z")
	tests := []struct {
		c    SubscriptionCadence
		want string
	}{
		{CadenceDaily, "2026-02-01T00:00:00Z"},
		{CadenceWeekly, "2026-02-07T00:00:00Z"},
		// Jan 31 + 1 month -> Mar 3 (Go's calendar math rolls over, not clamps).
		{CadenceMonthly, "2026-03-03T00:00:00Z"},
	}
	for _, tt := range tests {
		got := tt.c.Advance(base).UTC().Format("2006-01-02T15:04:05Z")
		if got != tt.want {
			t.Fatalf("cadence=%s got=%s want=%s", tt.c, got, tt.want)
		}
	}
}

// timeFromISO is only used in tests; kept local to avoid a runtime dep.
func timeFromISO(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
