// Package config reads env vars into a typed Config. Fail fast at boot if anything
// required for the chosen backend is missing.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Backend string

const (
	BackendMongo    Backend = "mongo"
	BackendPostgres Backend = "postgres"
)

type Config struct {
	Backend Backend
	APIAddr string

	MongoURI string
	MongoDB  string

	PGWriterDSN string
	PGReaderDSN string

	AWSRegion            string
	SQSPaymentCreatedURL string
	SQSFraudURL          string
	SQSFeeURL            string
}

func FromEnv() (Config, error) {
	c := Config{
		Backend:              Backend(strings.ToLower(strings.TrimSpace(os.Getenv("GRIPE_BACKEND")))),
		APIAddr:              getenvDefault("API_ADDR", ":8080"),
		MongoURI:             os.Getenv("MONGO_URI"),
		MongoDB:              getenvDefault("MONGO_DB", "gripe"),
		PGWriterDSN:          os.Getenv("PG_WRITER_DSN"),
		PGReaderDSN:          os.Getenv("PG_READER_DSN"),
		AWSRegion:            os.Getenv("AWS_REGION"),
		SQSPaymentCreatedURL: os.Getenv("SQS_PAYMENT_CREATED_URL"),
		SQSFraudURL:          os.Getenv("SQS_FRAUD_URL"),
		SQSFeeURL:            os.Getenv("SQS_FEE_URL"),
	}

	switch c.Backend {
	case BackendMongo:
		if c.MongoURI == "" {
			return c, errors.New("MONGO_URI required when GRIPE_BACKEND=mongo")
		}
	case BackendPostgres:
		if c.PGWriterDSN == "" {
			return c, errors.New("PG_WRITER_DSN required when GRIPE_BACKEND=postgres")
		}
		if c.PGReaderDSN == "" {
			// ponytail: default reader to writer — one endpoint is fine until the perf phase
			c.PGReaderDSN = c.PGWriterDSN
		}
	case "":
		return c, errors.New("GRIPE_BACKEND required (mongo|postgres)")
	default:
		return c, fmt.Errorf("GRIPE_BACKEND=%q unknown (mongo|postgres)", c.Backend)
	}

	return c, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
