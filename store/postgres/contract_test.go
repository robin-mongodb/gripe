// Contract-suite entrypoint for the Postgres Store.
// Spins a real Postgres 16 via testcontainers-go, runs migrations, and runs the shared
// RunStoreContract. Auto-skips if Docker isn't running.
package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/robin-mongodb/gripe/store/contract"
	pgstore "github.com/robin-mongodb/gripe/store/postgres"
)

func TestPGContract(t *testing.T) {
	if os.Getenv("SKIP_TESTCONTAINERS") != "" {
		t.Skip("SKIP_TESTCONTAINERS set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("gripe_test"),
		postgres.WithUsername("gripe"),
		postgres.WithPassword("gripe"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers unavailable (is Docker running?): %v", err)
	}
	t.Cleanup(func() {
		termCtx, termCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer termCancel()
		_ = container.Terminate(termCtx)
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	s, err := pgstore.New(ctx, dsn)
	if err != nil {
		t.Fatalf("store new: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	contract.RunStoreContract(t, s)
}

// ponytail: postgres.BasicWaitStrategies() handles the initdb double-start wait for us.
var _ = time.Second // keep time import used
