package config

import (
	"os"
	"testing"
)

func TestFromEnv(t *testing.T) {
	// isolate: wipe relevant env for each subtest
	clear := func() {
		for _, k := range []string{"GRIPE_BACKEND", "MONGO_URI", "PG_WRITER_DSN", "PG_READER_DSN", "API_ADDR"} {
			_ = os.Unsetenv(k)
		}
	}

	t.Run("missing backend fails", func(t *testing.T) {
		clear()
		if _, err := FromEnv(); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("mongo requires uri", func(t *testing.T) {
		clear()
		os.Setenv("GRIPE_BACKEND", "mongo")
		if _, err := FromEnv(); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("mongo ok", func(t *testing.T) {
		clear()
		os.Setenv("GRIPE_BACKEND", "mongo")
		os.Setenv("MONGO_URI", "mongodb://example")
		c, err := FromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Backend != BackendMongo {
			t.Fatalf("backend = %q", c.Backend)
		}
		if c.APIAddr != ":8080" {
			t.Fatalf("APIAddr default failed: %q", c.APIAddr)
		}
	})

	t.Run("postgres reader defaults to writer", func(t *testing.T) {
		clear()
		os.Setenv("GRIPE_BACKEND", "postgres")
		os.Setenv("PG_WRITER_DSN", "postgres://writer")
		c, err := FromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.PGReaderDSN != "postgres://writer" {
			t.Fatalf("reader default failed: %q", c.PGReaderDSN)
		}
	})

	t.Run("unknown backend fails", func(t *testing.T) {
		clear()
		os.Setenv("GRIPE_BACKEND", "sqlite")
		if _, err := FromEnv(); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}
