package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/robin-mongodb/gripe/internal/config"
)

// fee-worker: consumes SQS payment.created, mocks a network fee, updates the payment.
// Real logic lands with UC-13 un-park. For now, boot + log + park.
func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if _, err := config.FromEnv(); err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	log.Info("fee-worker up (idle — UC-13 pending)")
	<-await()
	log.Info("fee-worker shutting down")
}

func await() <-chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	return c
}
