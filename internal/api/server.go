// Package api wires the HTTP surface. Handlers land later — v1 exposes /healthz and
// echoes which backend is live.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/store"
)

type Server struct {
	cfg   config.Config
	store store.Store
	mux   *http.ServeMux
}

func New(cfg config.Config, s store.Store) *Server {
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /v1/healthz", s.healthz)
	// Real handlers land as tasks 5/7/8/12–17 ship.
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
