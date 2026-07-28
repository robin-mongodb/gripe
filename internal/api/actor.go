package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/robin-mongodb/gripe/internal/domain"
)

type ctxKey int

const actorKey ctxKey = 1

// ErrNoActor is returned by ActorFromContext if no Actor is attached.
// Handlers should treat this as a 500 (middleware should have run).
var ErrNoActor = errors.New("api: no actor in context")

// ActorFromContext pulls the Actor out. Non-actor routes (healthz) don't use this.
func ActorFromContext(ctx context.Context) (domain.Actor, error) {
	a, ok := ctx.Value(actorKey).(domain.Actor)
	if !ok {
		return domain.Actor{}, ErrNoActor
	}
	return a, nil
}

// actorMiddleware reads X-Actor-Role + X-Actor-Id, validates, attaches to context.
// Auth is skipped per CLAUDE.md — this is where the trust boundary lives.
// Rejects with 400 if the headers are missing or the role is unknown.
func actorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := strings.TrimSpace(r.Header.Get("X-Actor-Role"))
		id := strings.TrimSpace(r.Header.Get("X-Actor-Id"))

		if role == "" || id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "X-Actor-Role and X-Actor-Id headers are required on /v1 routes",
			})
			return
		}

		a := domain.Actor{Role: domain.ActorRole(role), ID: id}
		switch a.Role {
		case domain.RoleAdmin, domain.RoleMerchant, domain.RoleCustomer:
			// ok
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "X-Actor-Role must be one of: admin, merchant, customer",
			})
			return
		}

		ctx := context.WithValue(r.Context(), actorKey, a)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
