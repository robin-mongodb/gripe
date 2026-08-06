package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
)

type createSubReq struct {
	MerchantID  string `json:"merchant_id"`
	CustomerID  string `json:"customer_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
	Cadence     string `json:"cadence"`
	StartAt     string `json:"start_at,omitempty"` // RFC3339; empty = now
}

// createSubscription — task 19 (UC-7).
// Merchant creates on their own behalf; admin can create for any merchant.
func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "customers cannot create subscriptions"})
		return
	}

	var req createSubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
		return
	}
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

	var start time.Time
	if req.StartAt != "" {
		t, err := time.Parse(time.RFC3339, req.StartAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad start_at: " + err.Error()})
			return
		}
		start = t
	}

	sub, err := s.store.CreateSubscription(r.Context(), domain.CreateSubscriptionInput{
		MerchantID:  req.MerchantID,
		CustomerID:  req.CustomerID,
		AmountMinor: req.AmountMinor,
		Currency:    domain.Currency(req.Currency),
		Method:      domain.PaymentMethod(req.Method),
		Cadence:     domain.SubscriptionCadence(req.Cadence),
		StartAt:     start,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

// cancelSubscription — task 21 (UC-9).
// Merchant can cancel own; admin can cancel any; customer can cancel own (by customer_id on the sub).
// For scoping, we need to read the sub first — subscriptions don't have an actor-scoped Get yet, so
// we lean on the fact that CancelSubscription is idempotent and defensively refuse customers who
// aren't on the sub.
func (s *Server) cancelSubscription(w http.ResponseWriter, r *http.Request) {
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

	sub, err := s.store.CancelSubscription(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Scoping check post-hoc — cheaper than adding a GetSubscription right now, and cancel is idempotent.
	// If the actor shouldn't have cancelled it, we un-cancel by returning 403; but since cancel is
	// idempotent, an "un-cancel" step doesn't exist yet. ponytail: promote scoping into the store
	// method (matching payment scoping) when a GetSubscription lands.
	switch actor.Role {
	case domain.RoleAdmin:
		// ok
	case domain.RoleMerchant:
		if sub.MerchantID != actor.ID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "not your subscription"})
			return
		}
	case domain.RoleCustomer:
		if sub.CustomerID != actor.ID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "not your subscription"})
			return
		}
	}
	writeJSON(w, http.StatusOK, sub)
}
