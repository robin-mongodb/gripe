// Package bootstrap holds the backend-selection boilerplate shared by every
// binary (api, cycler, seed, fee-worker) — one switch instead of four copies.
package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/store"
	mongostore "github.com/robin-mongodb/gripe/store/mongo"
	pgstore "github.com/robin-mongodb/gripe/store/postgres"
)

// OpenStore connects the Store implementation selected by cfg.Backend.
func OpenStore(ctx context.Context, cfg config.Config) (store.Store, error) {
	switch cfg.Backend {
	case config.BackendMongo:
		bctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return mongostore.New(bctx, cfg.MongoURI, cfg.MongoDB)
	case config.BackendPostgres:
		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return pgstore.New(bctx, cfg.PGWriterDSN)
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}
