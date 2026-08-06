package api

import (
	"net/http"

	"github.com/robin-mongodb/gripe/internal/domain"
)

// Settlement is a platform operation — admin only (tasks 51/52). The store's
// state machine is the idempotency guard: a second settle is ErrInvalidState,
// so these don't need the Idempotency-Key middleware.

func (s *Server) settlePayment(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p, err := s.store.SettlePayment(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) settleRefund(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ref, err := s.store.SettleRefund(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ref)
}

// getBalances — task 56/67. Merchant sees own; admin passes ?merchant_id=.
func (s *Server) getBalances(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	var merchantID string
	switch actor.Role {
	case domain.RoleMerchant:
		merchantID = actor.ID
	case domain.RoleAdmin:
		merchantID = r.URL.Query().Get("merchant_id")
		if merchantID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "merchant_id required for admin"})
			return
		}
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "customers have no balances"})
		return
	}
	bals, err := s.store.GetMerchantBalances(r.Context(), merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if bals == nil {
		bals = []domain.Balance{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": bals})
}

// requireAdmin writes the error response and returns false unless the actor is admin
// and the store is configured.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return false
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return false
	}
	if actor.Role != domain.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin only"})
		return false
	}
	return true
}
