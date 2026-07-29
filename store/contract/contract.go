// Package contract hosts the shared contract suite that every Store impl must pass.
// Each impl has its own _test.go (e.g. store/mongo/contract_test.go) that calls
// RunStoreContract with a live store from testcontainers.
package contract

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

	// Tasks 13/14/15: initial state per method. "Settle later" halves for direct debit
	// and bank transfer are deferred to task 51 (SettlePayment) — we only assert the
	// state a fresh CreatePayment lands in.
	t.Run("CreatePayment_apple_pay_captured_and_decline", func(t *testing.T) {
		cases := []struct {
			amt  int64
			want domain.PaymentStatus
		}{
			{5000, domain.StatusCaptured},
			{5013, domain.StatusDeclined},
		}
		for i, c := range cases {
			p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
				MerchantID: "mer_acme", CustomerID: "cus_1",
				AmountMinor: c.amt, Currency: domain.EUR, Method: domain.MethodApplePay,
			}, fmt.Sprintf("idem-ap-%d", i))
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if p.Status != c.want {
				t.Fatalf("amt=%d status=%s want=%s", c.amt, p.Status, c.want)
			}
		}
	})

	t.Run("CreatePayment_google_pay_captured_and_decline", func(t *testing.T) {
		cases := []struct {
			amt  int64
			want domain.PaymentStatus
		}{
			{5000, domain.StatusCaptured},
			{5013, domain.StatusDeclined},
		}
		for i, c := range cases {
			p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
				MerchantID: "mer_acme", CustomerID: "cus_1",
				AmountMinor: c.amt, Currency: domain.USD, Method: domain.MethodGooglePay,
			}, fmt.Sprintf("idem-gp-%d", i))
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if p.Status != c.want {
				t.Fatalf("amt=%d status=%s want=%s", c.amt, p.Status, c.want)
			}
		}
	})

	t.Run("CreatePayment_direct_debit_lands_authorized", func(t *testing.T) {
		// Direct debit: no .13 decline (bank rails don't work like cards). Cycler settles later.
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_acme", CustomerID: "cus_1",
			AmountMinor: 5013, Currency: domain.GBP, Method: domain.MethodDirectDebit,
		}, "idem-dd-1")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.Status != domain.StatusAuthorized {
			t.Fatalf("status = %s, want authorized", p.Status)
		}
	})

	t.Run("CreatePayment_bank_transfer_lands_pending", func(t *testing.T) {
		// Bank transfer: pending until an async "settled" event lands (deferred).
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_acme", CustomerID: "cus_1",
			AmountMinor: 5013, Currency: domain.USD, Method: domain.MethodBankTransfer,
		}, "idem-bt-1")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.Status != domain.StatusPending {
			t.Fatalf("status = %s, want pending", p.Status)
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

	// Task 16 (UC-3): CapturePayment. Legal only from authorized -> captured.
	t.Run("CapturePayment_direct_debit_authorized_to_captured", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_C", CustomerID: "cus_c", AmountMinor: 2500, Currency: domain.GBP, Method: domain.MethodDirectDebit,
		}, "idem-cap-1")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.Status != domain.StatusAuthorized {
			t.Fatalf("precondition failed: status = %s", p.Status)
		}
		captured, err := s.CapturePayment(ctx, p.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if captured.Status != domain.StatusCaptured {
			t.Fatalf("status = %s, want captured", captured.Status)
		}
	})

	t.Run("CapturePayment_rejects_already_captured", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_C", CustomerID: "cus_c", AmountMinor: 100, Currency: domain.USD, Method: domain.MethodCard,
		}, "idem-cap-2")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err = s.CapturePayment(ctx, p.ID)
		if !errors.Is(err, store.ErrInvalidState) {
			t.Fatalf("want ErrInvalidState on already-captured, got %v", err)
		}
	})

	t.Run("CapturePayment_rejects_declined", func(t *testing.T) {
		p, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_C", CustomerID: "cus_c", AmountMinor: 5013, Currency: domain.GBP, Method: domain.MethodCard,
		}, "idem-cap-3")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err = s.CapturePayment(ctx, p.ID)
		if !errors.Is(err, store.ErrInvalidState) {
			t.Fatalf("want ErrInvalidState on declined, got %v", err)
		}
	})

	t.Run("CapturePayment_missing_returns_ErrNotFound", func(t *testing.T) {
		_, err := s.CapturePayment(ctx, "pay_nope")
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

	// Task 22 (UC-5): ListMerchantPayments — merchant scoping + filters + cursor.
	t.Run("ListMerchantPayments_scoped_to_merchant", func(t *testing.T) {
		// Create 3 payments for mer_L and 2 for mer_M.
		for i := 0; i < 3; i++ {
			_, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
				MerchantID: "mer_L", CustomerID: "cus_l", AmountMinor: 100, Currency: domain.GBP, Method: domain.MethodCard,
			}, fmt.Sprintf("idem-l-%d", i))
			if err != nil {
				t.Fatalf("create L%d: %v", i, err)
			}
		}
		for i := 0; i < 2; i++ {
			_, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
				MerchantID: "mer_M", CustomerID: "cus_m", AmountMinor: 100, Currency: domain.GBP, Method: domain.MethodCard,
			}, fmt.Sprintf("idem-m-%d", i))
			if err != nil {
				t.Fatalf("create M%d: %v", i, err)
			}
		}
		page, err := s.ListMerchantPayments(ctx, "mer_L", domain.Filters{}, "")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, p := range page.Items {
			if p.MerchantID != "mer_L" {
				t.Fatalf("leaked merchant: %s", p.MerchantID)
			}
		}
		// At least 3 for mer_L; may be more if earlier subtests created mer_L payments.
		if len(page.Items) < 3 {
			t.Fatalf("want >=3 items for mer_L, got %d", len(page.Items))
		}
	})

	t.Run("ListMerchantPayments_status_filter", func(t *testing.T) {
		// One declined + one captured for mer_F.
		_, _ = s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_F", CustomerID: "cus_f", AmountMinor: 5013, Currency: domain.GBP, Method: domain.MethodCard,
		}, "idem-f-decline")
		_, _ = s.CreatePayment(ctx, domain.CreatePaymentInput{
			MerchantID: "mer_F", CustomerID: "cus_f", AmountMinor: 5000, Currency: domain.GBP, Method: domain.MethodCard,
		}, "idem-f-cap")
		page, err := s.ListMerchantPayments(ctx, "mer_F", domain.Filters{Status: domain.StatusDeclined}, "")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("want 1 declined, got %d", len(page.Items))
		}
		if page.Items[0].Status != domain.StatusDeclined {
			t.Fatalf("status filter failed: %s", page.Items[0].Status)
		}
	})

	t.Run("ListMerchantPayments_cursor_paginates", func(t *testing.T) {
		// listPageMax is 50 for both backends by contract; insert enough to overflow.
		const overCap = 51
		for i := 0; i < overCap; i++ {
			_, err := s.CreatePayment(ctx, domain.CreatePaymentInput{
				MerchantID: "mer_P", CustomerID: "cus_p", AmountMinor: 100, Currency: domain.GBP, Method: domain.MethodCard,
			}, fmt.Sprintf("idem-p-%d", i))
			if err != nil {
				t.Fatalf("create P%d: %v", i, err)
			}
		}
		page1, err := s.ListMerchantPayments(ctx, "mer_P", domain.Filters{}, "")
		if err != nil {
			t.Fatalf("page1: %v", err)
		}
		if page1.NextCursor == "" {
			t.Fatal("want a cursor when more results exist")
		}
		page2, err := s.ListMerchantPayments(ctx, "mer_P", domain.Filters{}, page1.NextCursor)
		if err != nil {
			t.Fatalf("page2: %v", err)
		}
		if len(page2.Items) == 0 {
			t.Fatal("page2 should have at least one item")
		}
		// No overlap between page1 and page2 ids.
		seen := map[string]bool{}
		for _, p := range page1.Items {
			seen[p.ID] = true
		}
		for _, p := range page2.Items {
			if seen[p.ID] {
				t.Fatalf("id %s appears on both pages", p.ID)
			}
		}
	})

	t.Run("ListAllPayments_admin_sees_across_merchants", func(t *testing.T) {
		// Admin filter by merchant_id must work regardless of page saturation from earlier subtests.
		for _, mid := range []string{"mer_L", "mer_M"} {
			p, err := s.ListAllPayments(ctx, domain.Filters{MerchantID: mid}, "")
			if err != nil {
				t.Fatalf("list %s: %v", mid, err)
			}
			if len(p.Items) == 0 {
				t.Fatalf("admin filter merchant_id=%s returned 0 items", mid)
			}
			for _, item := range p.Items {
				if item.MerchantID != mid {
					t.Fatalf("filter leaked: %s", item.MerchantID)
				}
			}
		}
	})

	// Task 19/20/21 (UC-7/8/9): subscription lifecycle.
	t.Run("CreateSubscription_persists_active_with_next_charge", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		sub, err := s.CreateSubscription(ctx, domain.CreateSubscriptionInput{
			MerchantID:  "mer_S",
			CustomerID:  "cus_s",
			AmountMinor: 999,
			Currency:    domain.USD,
			Method:      domain.MethodCard,
			Cadence:     domain.CadenceMonthly,
			StartAt:     start,
		})
		if err != nil {
			t.Fatalf("create sub: %v", err)
		}
		if sub.Status != domain.SubActive {
			t.Fatalf("status = %s, want active", sub.Status)
		}
		if !sub.NextChargeAt.Equal(start) {
			t.Fatalf("next_charge_at = %v, want %v", sub.NextChargeAt, start)
		}
		if sub.NextCycleIdx != 0 {
			t.Fatalf("next_cycle_index = %d, want 0", sub.NextCycleIdx)
		}
	})

	t.Run("CreateSubscription_rejects_bad_cadence", func(t *testing.T) {
		_, err := s.CreateSubscription(ctx, domain.CreateSubscriptionInput{
			MerchantID: "mer_S", CustomerID: "cus_s", AmountMinor: 100,
			Currency: domain.USD, Method: domain.MethodCard, Cadence: "yearly",
		})
		if !errors.Is(err, store.ErrInvalidState) {
			t.Fatalf("want ErrInvalidState, got %v", err)
		}
	})

	t.Run("DueSubscriptions_returns_active_at_or_before_asOf", func(t *testing.T) {
		past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		// Due (past).
		_, err := s.CreateSubscription(ctx, domain.CreateSubscriptionInput{
			MerchantID: "mer_D", CustomerID: "cus_d", AmountMinor: 100,
			Currency: domain.USD, Method: domain.MethodCard, Cadence: domain.CadenceDaily,
			StartAt: past,
		})
		if err != nil {
			t.Fatalf("create due: %v", err)
		}
		// Not due (future).
		_, err = s.CreateSubscription(ctx, domain.CreateSubscriptionInput{
			MerchantID: "mer_D", CustomerID: "cus_d", AmountMinor: 100,
			Currency: domain.USD, Method: domain.MethodCard, Cadence: domain.CadenceDaily,
			StartAt: future,
		})
		if err != nil {
			t.Fatalf("create future: %v", err)
		}
		due, err := s.DueSubscriptions(ctx, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 100)
		if err != nil {
			t.Fatalf("due: %v", err)
		}
		var sawMerD bool
		for _, sub := range due {
			if sub.MerchantID == "mer_D" {
				sawMerD = true
				if sub.NextChargeAt.After(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
					t.Fatalf("future sub included: next=%v", sub.NextChargeAt)
				}
			}
		}
		if !sawMerD {
			t.Fatal("expected due sub for mer_D")
		}
	})

	t.Run("CancelSubscription_flips_status", func(t *testing.T) {
		sub, err := s.CreateSubscription(ctx, domain.CreateSubscriptionInput{
			MerchantID: "mer_X", CustomerID: "cus_x", AmountMinor: 100,
			Currency: domain.GBP, Method: domain.MethodCard, Cadence: domain.CadenceDaily,
			StartAt: time.Now().UTC().Add(-time.Hour),
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		cancelled, err := s.CancelSubscription(ctx, sub.ID)
		if err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if cancelled.Status != domain.SubCancelled {
			t.Fatalf("status = %s, want cancelled", cancelled.Status)
		}
		// Cancelled subs must not appear in DueSubscriptions.
		due, err := s.DueSubscriptions(ctx, time.Now().UTC(), 100)
		if err != nil {
			t.Fatalf("due: %v", err)
		}
		for _, d := range due {
			if d.ID == sub.ID {
				t.Fatalf("cancelled sub %s still due", sub.ID)
			}
		}
	})

	t.Run("CancelSubscription_missing_returns_ErrNotFound", func(t *testing.T) {
		_, err := s.CancelSubscription(ctx, "sub_does_not_exist")
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
