// Package events publishes domain events to SQS. One event type for now:
// payment.created, consumed by the fee-worker (fraud-worker is parked).
package events

import (
	"context"
	"encoding/json"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/robin-mongodb/gripe/internal/domain"
)

// PaymentCreated is the message body — deliberately small; consumers re-read
// the payment through the Store for anything else.
type PaymentCreated struct {
	PaymentID   string          `json:"payment_id"`
	MerchantID  string          `json:"merchant_id"`
	AmountMinor int64           `json:"amount_minor"`
	Currency    domain.Currency `json:"currency"`
}

// Publisher sends payment.created events. Nil *SQSPublisher is a safe no-op,
// so callers don't branch on "is SQS configured".
type SQSPublisher struct {
	client *sqs.Client
	url    string
}

// NewSQS returns nil (no-op) when queueURL is empty — local dev without AWS.
func NewSQS(ctx context.Context, queueURL string) (*SQSPublisher, error) {
	if queueURL == "" {
		return nil, nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &SQSPublisher{client: sqs.NewFromConfig(cfg), url: queueURL}, nil
}

// PublishPaymentCreated is best-effort by contract (see architecture.md § data
// flow) — the caller logs the error, the payment write is never rolled back.
func (p *SQSPublisher) PublishPaymentCreated(ctx context.Context, pay domain.Payment) error {
	if p == nil {
		return nil
	}
	body, err := json.Marshal(PaymentCreated{
		PaymentID:   pay.ID,
		MerchantID:  pay.MerchantID,
		AmountMinor: pay.AmountMinor,
		Currency:    pay.Currency,
	})
	if err != nil {
		return err
	}
	_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.url),
		MessageBody: aws.String(string(body)),
	})
	return err
}
