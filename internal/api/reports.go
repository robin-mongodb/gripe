package api

import (
	"net/http"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
)

// adminVolumeReport — tasks 25/31 (employee console cross-merchant aggregate).
// GET /v1/reports/volume?from=<RFC3339>&to=<RFC3339>. Defaults to the last 30 days.
func (s *Server) adminVolumeReport(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		return
	}
	actor, err := ActorFromContext(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no actor"})
		return
	}
	if actor.Role != domain.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin only"})
		return
	}

	now := time.Now().UTC()
	from, to := now.AddDate(0, 0, -30), now
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad 'from': " + err.Error()})
			return
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad 'to': " + err.Error()})
			return
		}
		to = t
	}

	rows, err := s.store.AdminVolumeReport(r.Context(), from, to)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Never null in JSON — an empty report is an empty list.
	if rows == nil {
		rows = []domain.MerchantDailyVolume{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}
