package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/robin-mongodb/gripe/internal/store"
)

// IdempotencyRecord is what we persist per key so retries can replay.
type IdempotencyRecord struct {
	Key        string    `bson:"_id"        json:"key"`
	ActorRole  string    `bson:"actor_role" json:"actor_role"`
	ActorID    string    `bson:"actor_id"   json:"actor_id"`
	BodyHash   string    `bson:"body_hash"  json:"body_hash"`
	StatusCode int       `bson:"status"     json:"status"`
	Response   []byte    `bson:"response"   json:"response"`
	CreatedAt  time.Time `bson:"created_at" json:"created_at"`
	ExpiresAt  time.Time `bson:"expires_at" json:"expires_at"`
}

// ErrIdempotencyExists is what PutIdempotencyRecord returns when the key is already used.
var ErrIdempotencyExists = errors.New("api: idempotency record exists")

// IdempotencyStore is the small subset of store.Store the middleware needs.
// Keeping it narrow makes the middleware trivially testable with a map-backed fake.
type IdempotencyStore interface {
	PutIdempotencyRecord(ctx context.Context, rec IdempotencyRecord) error
	GetIdempotencyRecord(ctx context.Context, key string) (IdempotencyRecord, error)
}

// idempotencyMiddleware guards write handlers.
//   - key missing on POST -> 400
//   - same key + same body -> replay cached response with Idempotency-Replayed: true
//   - same key + different body -> 409
//   - expired key -> treated as new
func idempotencyMiddleware(is IdempotencyStore, ttl time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Idempotency-Key header is required on writes",
			})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		hash := sha256Hex(body)

		actor, _ := ActorFromContext(r.Context())

		existing, err := is.GetIdempotencyRecord(r.Context(), key)
		switch {
		case err == nil && !existing.ExpiresAt.IsZero() && time.Now().After(existing.ExpiresAt):
			// Expired — fall through and re-insert below.
		case err == nil && existing.BodyHash != hash:
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "Idempotency-Key was previously used with a different request body",
			})
			return
		case err == nil:
			// Live record with matching body -> replay.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotency-Replayed", "true")
			w.WriteHeader(existing.StatusCode)
			_, _ = w.Write(existing.Response)
			return
		case errors.Is(err, store.ErrNotFound):
			// Fresh key -> continue.
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "idempotency lookup: " + err.Error()})
			return
		}

		rec := &recorder{ResponseWriter: w, status: http.StatusOK, buf: &bytes.Buffer{}}
		next.ServeHTTP(rec, r)

		// Only persist 2xx — 4xx/5xx are safe to retry naturally.
		if rec.status >= 200 && rec.status < 300 {
			now := time.Now()
			putErr := is.PutIdempotencyRecord(r.Context(), IdempotencyRecord{
				Key:        key,
				ActorRole:  string(actor.Role),
				ActorID:    actor.ID,
				BodyHash:   hash,
				StatusCode: rec.status,
				Response:   rec.buf.Bytes(),
				CreatedAt:  now,
				ExpiresAt:  now.Add(ttl),
			})
			// Race: two requests with the same key won both fresh-key checks; second write loses harmlessly.
			if putErr != nil && !errors.Is(putErr, ErrIdempotencyExists) {
				_ = putErr // response is already written; nothing to do at this layer
			}
		}
	})
}

// recorder captures the handler's response bytes so idempotency can replay.
type recorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	buf         *bytes.Buffer
}

func (r *recorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
