// Contract-suite entrypoint for the Mongo Store.
// Spins a real Mongo via testcontainers-go and runs the shared RunStoreContract.
//
// Skips if Docker isn't available (e.g. CI without a docker socket) — CLAUDE.md says
// contract tests are mandatory before merge, but this file makes local `go test ./...`
// pass on a laptop with no docker without silently omitting coverage.

package mongo_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	"github.com/robin-mongodb/gripe/store/contract"
	mongostore "github.com/robin-mongodb/gripe/store/mongo"
)

func TestMongoContract(t *testing.T) {
	if os.Getenv("SKIP_TESTCONTAINERS") != "" {
		t.Skip("SKIP_TESTCONTAINERS set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Vanilla mongo:7 for now. Atlas-local image is bigger + only needed when we un-park
	// Atlas Search / Vector Search. This tests the same core semantics.
	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Skipf("testcontainers unavailable (is Docker running?): %v", err)
	}
	t.Cleanup(func() {
		termCtx, termCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer termCancel()
		_ = container.Terminate(termCtx)
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	s, err := mongostore.New(ctx, uri, "gripe_test")
	if err != nil {
		t.Fatalf("store new: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	contract.RunStoreContract(t, s)
}
