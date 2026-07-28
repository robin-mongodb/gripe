package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robin-mongodb/gripe/internal/domain"
	"github.com/robin-mongodb/gripe/internal/store"
)

// fakeIdemStore is an in-memory IdempotencyStore for tests.
type fakeIdemStore struct {
	mu sync.Mutex
	m  map[string]IdempotencyRecord
}

func newFakeIdemStore() *fakeIdemStore { return &fakeIdemStore{m: map[string]IdempotencyRecord{}} }

func (f *fakeIdemStore) PutIdempotencyRecord(_ context.Context, rec IdempotencyRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.m[rec.Key]; ok {
		return ErrIdempotencyExists
	}
	f.m[rec.Key] = rec
	return nil
}

func (f *fakeIdemStore) GetIdempotencyRecord(_ context.Context, key string) (IdempotencyRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.m[key]
	if !ok {
		return IdempotencyRecord{}, store.ErrNotFound
	}
	return rec, nil
}

// counting handler: writes an incrementing counter so we can tell a fresh call apart from a replay.
type countingHandler struct{ calls int }

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls++
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"call":%d}`, h.calls)
}

func withActor(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), actorKey, domain.Actor{Role: domain.RoleMerchant, ID: "mer_a"})
	return req.WithContext(ctx)
}

func TestIdempotency_Replay(t *testing.T) {
	fs := newFakeIdemStore()
	h := &countingHandler{}
	mw := idempotencyMiddleware(fs, time.Hour, h)

	do := func(key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Content-Type", "application/json")
		req = withActor(req)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		return rr
	}

	// First request: handler runs.
	r1 := do("k1", `{"amount":100}`)
	if r1.Code != http.StatusCreated || r1.Body.String() != `{"call":1}` {
		t.Fatalf("first call: status=%d body=%q", r1.Code, r1.Body.String())
	}
	if h.calls != 1 {
		t.Fatalf("expected handler called once, got %d", h.calls)
	}

	// Replay with same body: handler must NOT run again; response replayed.
	r2 := do("k1", `{"amount":100}`)
	if r2.Code != http.StatusCreated || r2.Body.String() != `{"call":1}` {
		t.Fatalf("replay: status=%d body=%q", r2.Code, r2.Body.String())
	}
	if h.calls != 1 {
		t.Fatalf("handler ran on replay: calls=%d", h.calls)
	}
	if r2.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("Idempotency-Replayed header missing on replay")
	}
}

func TestIdempotency_DifferentBodyConflict(t *testing.T) {
	fs := newFakeIdemStore()
	h := &countingHandler{}
	mw := idempotencyMiddleware(fs, time.Hour, h)

	do := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "same-key")
		req = withActor(req)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		return rr
	}

	_ = do(`{"amount":100}`)
	r := do(`{"amount":999}`)
	if r.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", r.Code, r.Body.String())
	}
	if h.calls != 1 {
		t.Fatalf("handler must not run on conflict: calls=%d", h.calls)
	}
}

func TestIdempotency_MissingKey(t *testing.T) {
	fs := newFakeIdemStore()
	h := &countingHandler{}
	mw := idempotencyMiddleware(fs, time.Hour, h)

	req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(`{}`))
	req = withActor(req)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if h.calls != 0 {
		t.Fatalf("handler must not run: calls=%d", h.calls)
	}
}

func TestIdempotency_NonPostPassesThrough(t *testing.T) {
	fs := newFakeIdemStore()
	h := &countingHandler{}
	mw := idempotencyMiddleware(fs, time.Hour, h)

	req := httptest.NewRequest("GET", "/v1/payments/pay_1", nil)
	req = withActor(req)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if h.calls != 1 {
		t.Fatalf("GET should pass through to handler; calls=%d", h.calls)
	}
	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", rr.Code)
	}
}

func TestIdempotency_ExpiredReplay(t *testing.T) {
	fs := newFakeIdemStore()
	h := &countingHandler{}
	// TTL=0 -> record expires immediately.
	mw := idempotencyMiddleware(fs, 0, h)

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(`{"amount":1}`))
		req.Header.Set("Idempotency-Key", "kx")
		req = withActor(req)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		return rr
	}

	_ = do()
	// second call: record is expired -> handler runs again. Fake store still holds the old key,
	// so the middleware will try to Put again and get ErrIdempotencyExists; that's silently ignored
	// (documented in idempotency.go).
	_ = do()
	if h.calls != 2 {
		t.Fatalf("expired record should not replay: calls=%d, want 2", h.calls)
	}
}

// Sanity: sentinel round-trips via errors.Is.
func TestIdempotency_SentinelIsErrNotFound(t *testing.T) {
	fs := newFakeIdemStore()
	_, err := fs.GetIdempotencyRecord(context.Background(), "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fake store must return store.ErrNotFound; got %v", err)
	}
}

// _ silences import when Body isn't read directly.
var _ = io.Discard
