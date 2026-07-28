package domain

import "testing"

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
