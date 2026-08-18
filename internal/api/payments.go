package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

type createPaymentReq struct {
	MerchantID  string `json:"merchant_id"`
	CustomerID  string `json:"customer_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
}

func (s *Server) createPayment(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	// Only merchants and customers create payments. Admin does not.
	if actor.Role != domain.RoleMerchant && actor.Role != domain.RoleCustomer {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin cannot create payments"})
		return
	}

	var req createPaymentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
		return
	}
	// A merchant creating a payment scopes it to itself. Customer must name a merchant.
	if actor.Role == domain.RoleMerchant {
		if req.MerchantID != "" && req.MerchantID != actor.ID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "merchant_id must match X-Actor-Id for merchant callers"})
			return
		}
		req.MerchantID = actor.ID
	}
	if strings.TrimSpace(req.MerchantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "merchant_id required"})
		return
	}
	// Task 58: reject unknown currencies at the boundary, before the store sees them.
	if !domain.Currency(req.Currency).Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "currency must be one of USD, GBP, EUR"})
		return
	}

	key := r.Header.Get("Idempotency-Key") // middleware already required this on POSTs
	p, err := s.store.CreatePayment(r.Context(), domain.CreatePaymentInput{
		MerchantID:  req.MerchantID,
		CustomerID:  req.CustomerID,
		AmountMinor: req.AmountMinor,
		Currency:    domain.Currency(req.Currency),
		Method:      domain.PaymentMethod(req.Method),
	}, key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Fan out payment.created (fee-worker consumes). Best-effort by design:
	// a publish failure is logged, never surfaced — the payment is already durable.
	if p.Status != domain.StatusDeclined {
		if err := s.events.PublishPaymentCreated(r.Context(), p); err != nil {
			slog.Error("publish payment.created", "payment_id", p.ID, "err", err)
		}
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getPayment(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	id := r.PathValue("id")
	p, err := s.store.GetPayment(r.Context(), id, actor)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type refundReq struct {
	AmountMinor int64 `json:"amount_minor"`
}

func (s *Server) refundPayment(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	// Only merchant (own) or admin can refund; customer cannot.
	if actor.Role == domain.RoleCustomer {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "customers cannot refund"})
		return
	}

	paymentID := r.PathValue("id")

	// Merchant scoping: pre-check that this merchant owns the payment.
	if actor.Role == domain.RoleMerchant {
		if _, err := s.store.GetPayment(r.Context(), paymentID, actor); err != nil {
			writeStoreError(w, err)
			return
		}
	}

	var req refundReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	refund, err := s.store.RefundPayment(r.Context(), paymentID, req.AmountMinor, key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, refund)
}

// capturePayment — task 16. Merchant (own) or admin only. Store enforces state machine.
func (s *Server) capturePayment(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	if actor.Role == domain.RoleCustomer {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "customers cannot capture"})
		return
	}

	paymentID := r.PathValue("id")

	// Merchant scoping: verify ownership before mutating.
	if actor.Role == domain.RoleMerchant {
		if _, err := s.store.GetPayment(r.Context(), paymentID, actor); err != nil {
			writeStoreError(w, err)
			return
		}
	}

	p, err := s.store.CapturePayment(r.Context(), paymentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// listPayments — task 22 (UC-5) and task 24 (UC-6).
//   - merchant: forced scope to their own id (filters.merchant_id ignored)
//   - admin: optional filters.merchant_id
//   - customer: forbidden (payments visible only via detail read)
//
// Query params: status, method, currency, merchant_id (admin only), from, to
// (RFC3339), min_minor, max_minor, cursor.
func (s *Server) listPayments(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	if actor.Role == domain.RoleCustomer {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "customers cannot list payments"})
		return
	}

	q := r.URL.Query()
	filters := domain.Filters{
		Status:   domain.PaymentStatus(q.Get("status")),
		Method:   domain.PaymentMethod(q.Get("method")),
		Currency: domain.Currency(q.Get("currency")),
	}
	if actor.Role == domain.RoleAdmin {
		filters.MerchantID = q.Get("merchant_id")
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad 'from': " + err.Error()})
			return
		}
		filters.FromTime = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad 'to': " + err.Error()})
			return
		}
		filters.ToTime = t
	}
	if v := q.Get("min_minor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad 'min_minor': " + err.Error()})
			return
		}
		filters.MinMinor = n
	}
	if v := q.Get("max_minor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad 'max_minor': " + err.Error()})
			return
		}
		filters.MaxMinor = n
	}
	cursor := domain.Cursor(q.Get("cursor"))

	var page domain.Page
	switch actor.Role {
	case domain.RoleMerchant:
		page, err = s.store.ListMerchantPayments(r.Context(), actor.ID, filters, cursor)
	case domain.RoleAdmin:
		page, err = s.store.ListAllPayments(r.Context(), filters, cursor)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// writeStoreError maps store sentinel errors to HTTP statuses so handlers stay tiny.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrOverRefund):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrCurrencyMismatch):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrInvalidState):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, errors.ErrUnsupported):
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented yet"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
