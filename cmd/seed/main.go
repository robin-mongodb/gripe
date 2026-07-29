// cmd/seed populates the configured backend with demo data.
//
// Usage: GRIPE_BACKEND=mongo MONGO_URI=... ./seed [-m 5] [-p 40] [-s 3]
// Runs locally against Atlas, or on the EC2 via `docker compose run --rm seed`.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/seed"
	"github.com/robin-mongodb/gripe/internal/store"
	mongostore "github.com/robin-mongodb/gripe/store/mongo"
	pgstore "github.com/robin-mongodb/gripe/store/postgres"
)

func main() {
	opt := seed.DefaultOptions()
	flag.IntVar(&opt.Merchants, "m", opt.Merchants, "number of merchants")
	flag.IntVar(&opt.CustomersPer, "c", opt.CustomersPer, "customer pool size per merchant")
	flag.IntVar(&opt.PaymentsPer, "p", opt.PaymentsPer, "payments per merchant")
	flag.IntVar(&opt.Subscriptions, "s", opt.Subscriptions, "subscriptions per merchant")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var s store.Store
	switch cfg.Backend {
	case config.BackendMongo:
		ms, err := mongostore.New(ctx, cfg.MongoURI, cfg.MongoDB)
		if err != nil {
			log.Error("mongo", "err", err)
			os.Exit(1)
		}
		defer ms.Close(context.Background())
		s = ms
	case config.BackendPostgres:
		ps, err := pgstore.New(ctx, cfg.PGWriterDSN)
		if err != nil {
			log.Error("postgres", "err", err)
			os.Exit(1)
		}
		defer ps.Close(context.Background())
		s = ps
	}

	rep, err := seed.Run(ctx, s, opt)
	if err != nil {
		log.Error("seed", "err", err, "report", rep)
		os.Exit(1)
	}
	log.Info("seed done",
		"merchants", rep.Merchants,
		"payments", rep.Payments,
		"subscriptions", rep.Subscriptions,
		"duration", rep.Duration)
}
