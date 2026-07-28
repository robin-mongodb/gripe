// Package store_test hosts the shared contract suite that every Store impl must pass.
// Each impl has its own _test.go (e.g. store/mongo/store_test.go) that calls RunStoreContract
// with a live store from testcontainers.
package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

// RunStoreContract exercises the shared, backend-agnostic behaviour of a Store impl.
// Callers pass a freshly-provisioned store; test cleanup is the caller's job.
//
// Today: CreatePayment (card + decline) + GetPayment (actor scoping). Extend as
// tasks 12/14/15/17/19/20/21/22-25 land.
func RunStoreContract(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("CreatePayment_card_captured", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID:  "mer_acme",
			CustomerID:  "cus_1",
			AmountMinor: 5000, // £50.00
			Currency:    domain.GBP,
			Method:      domain.MethodCard,
		}, "idem-1")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.Status != domain.StatusCaptured {
			t.Fatalf("status = %s, want captured", p.Status)
		}
		if p.ID == "" {
			t.Fatal("ID empty")
		}
	})

	t.Run("CreatePayment_card_decline_on_dot13", func(t *testing.T) {
		// 5013 minor units = £50.13 -> mock decline.
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID:  "mer_acme",
			CustomerID:  "cus_1",
			AmountMinor: 5013,
			Currency:    domain.GBP,
			Method:      domain.MethodCard,
		}, "idem-2")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.Status != domain.StatusDeclined {
			t.Fatalf("status = %s, want declined", p.Status)
		}
	})

	t.Run("CreatePayment_rejects_bad_currency", func(t *testing.T) {
		_, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID:  "mer_acme",
			CustomerID:  "cus_1",
			AmountMinor: 1000,
			Currency:    "AUD",
			Method:      domain.MethodCard,
		}, "idem-3")
		if !errors.Is(err, store.ErrInvalidState) {
			t.Fatalf("want ErrInvalidState, got %v", err)
		}
	})

	t.Run("GetPayment_admin_sees_everything", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_A", CustomerID: "cus_x", AmountMinor: 100, Currency: domain.USD, Method: domain.MethodCard,
		}, "idem-4")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleAdmin, ID: "gripe_ops"})
		if err != nil {
			t.Fatalf("admin get: %v", err)
		}
		if got.ID != p.ID {
			t.Fatalf("wrong payment: %s vs %s", got.ID, p.ID)
		}
	})

	t.Run("GetPayment_merchant_sees_own_not_other", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_A", CustomerID: "cus_x", AmountMinor: 100, Currency: domain.USD, Method: domain.MethodCard,
		}, "idem-5")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleMerchant, ID: "mer_A"}); err != nil {
			t.Fatalf("own merchant should see own: %v", err)
		}
		_, err = s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleMerchant, ID: "mer_B"})
		if !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("other merchant must be forbidden, got %v", err)
		}
	})

	t.Run("GetPayment_customer_sees_own_not_other", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_A", CustomerID: "cus_owner", AmountMinor: 100, Currency: domain.USD, Method: domain.MethodCard,
		}, "idem-6")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleCustomer, ID: "cus_owner"}); err != nil {
			t.Fatalf("own customer should see own: %v", err)
		}
		_, err = s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleCustomer, ID: "cus_other"})
		if !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("other customer must be forbidden, got %v", err)
		}
	})

	t.Run("GetPayment_missing_returns_ErrNotFound", func(t *testing.T) {
		_, err := s.GetPayment(ctx, "pay_does_not_exist", domain.Actor{Role: domain.RoleAdmin, ID: "gripe_ops"})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("RefundPayment_full_flips_status_to_refunded", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_R", CustomerID: "cus_r", AmountMinor: 10_000, Currency: domain.EUR, Method: domain.MethodCard,
		}, "idem-r1")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.RefundPayment(ctx, p.ID, 10_000, "idem-r2"); err != nil {
			t.Fatalf("full refund: %v", err)
		}
		got, err := s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleAdmin, ID: "gripe_ops"})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status != domain.StatusRefunded {
			t.Fatalf("status = %s, want refunded", got.Status)
		}
		if got.RefundedMinor != 10_000 {
			t.Fatalf("refunded_minor = %d, want 10000", got.RefundedMinor)
		}
	})

	t.Run("RefundPayment_partial_sums_to_captured", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_R", CustomerID: "cus_r", AmountMinor: 5000, Currency: domain.USD, Method: domain.MethodCard,
		}, "idem-r3")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.RefundPayment(ctx, p.ID, 2000, "idem-r4"); err != nil {
			t.Fatalf("partial 1: %v", err)
		}
		if _, err := s.RefundPayment(ctx, p.ID, 3000, "idem-r5"); err != nil {
			t.Fatalf("partial 2: %v", err)
		}
		got, err := s.GetPayment(ctx, p.ID, domain.Actor{Role: domain.RoleAdmin, ID: "gripe_ops"})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.RefundedMinor != 5000 || got.Status != domain.StatusRefunded {
			t.Fatalf("after 2 partials: refunded=%d status=%s", got.RefundedMinor, got.Status)
		}
	})

	t.Run("RefundPayment_over_refund_rejected", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_R", CustomerID: "cus_r", AmountMinor: 1000, Currency: domain.GBP, Method: domain.MethodCard,
		}, "idem-r6")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.RefundPayment(ctx, p.ID, 600, "idem-r7"); err != nil {
			t.Fatalf("first refund: %v", err)
		}
		_, err = s.RefundPayment(ctx, p.ID, 500, "idem-r8") // 600 + 500 > 1000
		if !errors.Is(err, store.ErrOverRefund) {
			t.Fatalf("want ErrOverRefund, got %v", err)
		}
	})

	t.Run("RefundPayment_zero_or_negative_rejected", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_R", CustomerID: "cus_r", AmountMinor: 1000, Currency: domain.GBP, Method: domain.MethodCard,
		}, "idem-r9")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		for _, bad := range []int64{0, -1} {
			if _, err := s.RefundPayment(ctx, p.ID, bad, "idem-r10"); !errors.Is(err, store.ErrInvalidState) {
				t.Fatalf("amount %d: want ErrInvalidState, got %v", bad, err)
			}
		}
	})

	t.Run("RefundPayment_missing_payment_returns_ErrNotFound", func(t *testing.T) {
		_, err := s.RefundPayment(ctx, "pay_nope", 100, "idem-r11")
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("RefundPayment_declined_payment_rejected", func(t *testing.T) {
		// amount ending in .13 -> declined
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_R", CustomerID: "cus_r", AmountMinor: 5013, Currency: domain.GBP, Method: domain.MethodCard,
		}, "idem-r12")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err = s.RefundPayment(ctx, p.ID, 100, "idem-r13")
		if !errors.Is(err, store.ErrInvalidState) {
			t.Fatalf("want ErrInvalidState on declined refund, got %v", err)
		}
	})
}
