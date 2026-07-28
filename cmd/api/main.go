package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robin-mongodb/gripe/internal/api"
	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	// Backend selection. Concrete impls land in tasks 8 (Mongo) / 27 (PG).
	// ponytail: nil Store until an impl lands — /healthz still reports which backend was chosen.
	var s store.Store
	switch cfg.Backend {
	case config.BackendMongo:
		log.Info("selected backend", "backend", "mongo", "note", "Mongo Store impl pending — task 8")
	case config.BackendPostgres:
		log.Info("selected backend", "backend", "postgres", "note", "Postgres Store impl pending — task 27")
	}

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           api.New(cfg, s).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("api listening", "addr", cfg.APIAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if s != nil {
		_ = s.Close(ctx)
	}
}
