package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/robin-mongodb/gripe/internal/domain"
)

func TestActorMiddleware(t *testing.T) {
	// Handler that echoes the actor role, or "no-actor" if missing.
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, err := ActorFromContext(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(string(a.Role) + ":" + a.ID))
	})
	h := actorMiddleware(echo)

	tests := []struct {
		name       string
		role       string
		id         string
		wantStatus int
		wantBody   string
	}{
		{"valid admin", "admin", "gripe_ops", 200, "admin:gripe_ops"},
		{"valid merchant", "merchant", "mer_acme", 200, "merchant:mer_acme"},
		{"valid customer", "customer", "cus_1", 200, "customer:cus_1"},
		{"missing role", "", "mer_acme", 400, ""},
		{"missing id", "merchant", "", 400, ""},
		{"unknown role", "root", "mer_acme", 400, ""},
		{"whitespace role", "  ", "mer_acme", 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.role != "" {
				req.Header.Set("X-Actor-Role", tt.role)
			}
			if tt.id != "" {
				req.Header.Set("X-Actor-Id", tt.id)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestActorFromContext_NoActor(t *testing.T) {
	_, err := ActorFromContext(t.Context())
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

// Compile-time check: the roles the middleware accepts are the ones domain declares.
var _ = []domain.ActorRole{domain.RoleAdmin, domain.RoleMerchant, domain.RoleCustomer}
