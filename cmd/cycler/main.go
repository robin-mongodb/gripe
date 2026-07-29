package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/cycler"
	mongostore "github.com/robin-mongodb/gripe/store/mongo"
	pgstore "github.com/robin-mongodb/gripe/store/postgres"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open the store. Blocking retry on startup so DB unavailability doesn't wedge deploy.
	var advancer cycler.Advancer
	switch cfg.Backend {
	case config.BackendMongo:
		var s *mongostore.Store
		for {
			bctx, bcancel := context.WithTimeout(ctx, 10*time.Second)
			s, err = mongostore.New(bctx, cfg.MongoURI, cfg.MongoDB)
			bcancel()
			if err == nil {
				break
			}
			log.Warn("cycler: mongo not ready, retrying", "err", err)
			select {
			case <-ctx.Done():
				os.Exit(0)
			case <-time.After(5 * time.Second):
			}
		}
		defer s.Close(context.Background())
		advancer = s
	case config.BackendPostgres:
		var s *pgstore.Store
		for {
			bctx, bcancel := context.WithTimeout(ctx, 15*time.Second)
			s, err = pgstore.New(bctx, cfg.PGWriterDSN)
			bcancel()
			if err == nil {
				break
			}
			log.Warn("cycler: pg not ready, retrying", "err", err)
			select {
			case <-ctx.Done():
				os.Exit(0)
			case <-time.After(5 * time.Second):
			}
		}
		defer s.Close(context.Background())
		advancer = s
	}

	c := cycler.New(advancer, log, 100, 10*time.Second)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Info("cycler shutting down")
		cancel()
	}()

	if err := c.Run(ctx); err != nil && err != context.Canceled {
		log.Error("cycler run", "err", err)
		os.Exit(1)
	}
}
