// fee-worker: consumes payment.created from SQS and records a mock network fee
// on the payment via Store.SettleNetworkFee (set-once, so at-least-once delivery
// is harmless). Mixes reads+updates into the DB workload — see architecture.md.
//
// ponytail: it reads the payment.created queue directly — it's the only consumer
// while fraud-worker is parked. When fraud un-parks, fan out via SNS (or api
// double-publish) to the per-worker queues and point this at SQS_FEE_URL.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/robin-mongodb/gripe/internal/bootstrap"
	"github.com/robin-mongodb/gripe/internal/config"
	"github.com/robin-mongodb/gripe/internal/events"
)

// mockNetworkFee: fixed 20 minor units + 25bp of the amount. Arbitrary but
// deterministic, so re-runs and demo checks are predictable.
func mockNetworkFee(amountMinor int64) int64 {
	return 20 + amountMinor*25/10_000
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.SQSPaymentCreatedURL == "" {
		// Local dev without AWS: stay up (compose expects the container) but idle.
		log.Info("fee-worker idle — SQS_PAYMENT_CREATED_URL not set")
		<-ctx.Done()
		return
	}

	s, err := bootstrap.OpenStore(ctx, cfg)
	if err != nil {
		log.Error("store", "err", err)
		os.Exit(1)
	}
	defer s.Close(context.Background())

	awscfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Error("aws config", "err", err)
		os.Exit(1)
	}
	client := sqs.NewFromConfig(awscfg)
	log.Info("fee-worker up", "backend", cfg.Backend, "queue", cfg.SQSPaymentCreatedURL)

	for ctx.Err() == nil {
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(cfg.SQSPaymentCreatedURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long poll
		})
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Error("receive", "err", err)
			continue
		}
		for _, msg := range out.Messages {
			var ev events.PaymentCreated
			if err := json.Unmarshal([]byte(aws.ToString(msg.Body)), &ev); err != nil {
				log.Error("bad message, dropping", "err", err)
			} else if _, err := s.SettleNetworkFee(ctx, ev.PaymentID, mockNetworkFee(ev.AmountMinor)); err != nil {
				// Leave the message in flight — SQS redelivers after the
				// visibility timeout, and SettleNetworkFee is set-once safe.
				log.Error("settle network fee", "payment_id", ev.PaymentID, "err", err)
				continue
			}
			_, _ = client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(cfg.SQSPaymentCreatedURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}
	log.Info("fee-worker shutting down")
}
