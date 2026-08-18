// Package api wires the HTTP surface.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/events"
	"github.com/robin-mongodb/gripe/internal/store"
)

// idempotencyTTL is how long a persisted response replays for.
// UC-2 spec: 24h on both backends.
const idempotencyTTL = 24 * time.Hour

// Server holds handler dependencies. Store may be nil pre-boot (see cmd/api/main.go).
type Server struct {
	cfg    config.Config
	store  store.Store
	events *events.SQSPublisher // nil = no-op (SQS not configured)
	mux    *http.ServeMux
}

// New builds the HTTP surface. The idempotency middleware needs an IdempotencyStore;
// impls (store/mongo.Store) satisfy it via type assertion. pub may be nil.
func New(cfg config.Config, s store.Store, pub *events.SQSPublisher) *Server {
	srv := &Server{cfg: cfg, store: s, events: pub, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	// Unprotected health probe. No actor, no idempotency.
	s.mux.HandleFunc("GET /v1/healthz", s.healthz)

	// Payment routes go through: actorMiddleware -> idempotencyMiddleware (POST-only) -> handler.
	var idem IdempotencyStore
	if is, ok := any(s.store).(IdempotencyStore); ok {
		idem = is
	}

	// Reads
	s.mux.Handle("GET /v1/payments", actorMiddleware(http.HandlerFunc(s.listPayments)))
	s.mux.Handle("GET /v1/payments/{id}", actorMiddleware(http.HandlerFunc(s.getPayment)))
	s.mux.Handle("GET /v1/reports/volume", actorMiddleware(http.HandlerFunc(s.adminVolumeReport)))
	s.mux.Handle("GET /v1/reports/balances", actorMiddleware(http.HandlerFunc(s.adminBalanceReport)))
	s.mux.Handle("GET /v1/reports/revenue", actorMiddleware(http.HandlerFunc(s.adminRevenueReport)))
	s.mux.Handle("GET /v1/balances", actorMiddleware(http.HandlerFunc(s.getBalances)))

	// Settlement — admin-only platform ops; the store state machine guards retries.
	s.mux.Handle("POST /v1/payments/{id}/settle", actorMiddleware(http.HandlerFunc(s.settlePayment)))
	s.mux.Handle("POST /v1/refunds/{id}/settle", actorMiddleware(http.HandlerFunc(s.settleRefund)))

	// Writes: idempotency is only useful when the store can persist it.
	if idem != nil {
		s.mux.Handle("POST /v1/payments",
			actorMiddleware(idempotencyMiddleware(idem, idempotencyTTL, http.HandlerFunc(s.createPayment))))
		s.mux.Handle("POST /v1/payments/{id}/capture",
			actorMiddleware(idempotencyMiddleware(idem, idempotencyTTL, http.HandlerFunc(s.capturePayment))))
		s.mux.Handle("POST /v1/payments/{id}/refunds",
			actorMiddleware(idempotencyMiddleware(idem, idempotencyTTL, http.HandlerFunc(s.refundPayment))))
		s.mux.Handle("POST /v1/subscriptions",
			actorMiddleware(idempotencyMiddleware(idem, idempotencyTTL, http.HandlerFunc(s.createSubscription))))
		s.mux.Handle("POST /v1/subscriptions/{id}/cancel",
			actorMiddleware(idempotencyMiddleware(idem, idempotencyTTL, http.HandlerFunc(s.cancelSubscription))))
	} else {
		unavail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store not configured"})
		})
		s.mux.Handle("POST /v1/payments", actorMiddleware(unavail))
		s.mux.Handle("POST /v1/payments/{id}/capture", actorMiddleware(unavail))
		s.mux.Handle("POST /v1/payments/{id}/refunds", actorMiddleware(unavail))
		s.mux.Handle("POST /v1/subscriptions", actorMiddleware(unavail))
		s.mux.Handle("POST /v1/subscriptions/{id}/cancel", actorMiddleware(unavail))
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ok := true
	dbErr := ""
	if s.store != nil {
		if err := s.store.Ping(ctx); err != nil {
			ok = false
			dbErr = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      ok,
		"backend": s.cfg.Backend,
		"db_err":  dbErr,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
