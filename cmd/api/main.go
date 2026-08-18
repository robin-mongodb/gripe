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
	"github.com/robin-mongodb/gripe/internal/bootstrap"
	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/events"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	s, err := bootstrap.OpenStore(context.Background(), cfg)
	if err != nil {
		log.Error("store", "err", err, "backend", cfg.Backend)
		os.Exit(1)
	}
	log.Info("selected backend", "backend", cfg.Backend)

	// payment.created publisher; nil (no-op) when SQS isn't configured.
	pub, err := events.NewSQS(context.Background(), cfg.SQSPaymentCreatedURL)
	if err != nil {
		log.Error("sqs publisher", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           api.New(cfg, s, pub).Handler(),
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
