package cache

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TestIntegrationAppendAt runs the append script against a real Redis. The
// rules it pins are the ones agent staging relies on: chunks land in order,
// a resent chunk is not duplicated, a gap is refused with the length to
// resume from, and the cap holds.
//
// Opt-in: set INTEGRATION_REDIS_ADDR (host:port); `make test-integration`
// points it at the dev stack. It touches one random key and deletes it.
func TestIntegrationAppendAt(t *testing.T) {
	addr := os.Getenv("INTEGRATION_REDIS_ADDR")
	if addr == "" {
		t.Skip("INTEGRATION_REDIS_ADDR not set; skipping Redis integration test")
	}
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("INTEGRATION_REDIS_ADDR %q is not host:port", addr)
	}

	ctx := context.Background()
	c, err := NewRedisCache(ctx, RedisConfig{
		Host:     host,
		Port:     port,
		Password: os.Getenv("INTEGRATION_REDIS_PASSWORD"),
		Logger:   zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	key := "itest:append:" + uuid.NewString()
	defer func() { _ = c.Del(ctx, key) }()

	n, err := c.AppendAt(ctx, key, 0, "héllo ", 32, time.Minute)
	if err != nil || n != 7 {
		t.Fatalf("first chunk: n=%d err=%v", n, err)
	}
	if n, err = c.AppendAt(ctx, key, 7, "world", 32, time.Minute); err != nil || n != 12 {
		t.Fatalf("second chunk: n=%d err=%v", n, err)
	}

	// A resent chunk that already landed succeeds without appending.
	if n, err = c.AppendAt(ctx, key, 7, "world", 32, time.Minute); err != nil || n != 12 {
		t.Fatalf("resent chunk: n=%d err=%v", n, err)
	}
	// Same position, different bytes: that is not a retry.
	var mismatch *OffsetMismatchError
	if _, err = c.AppendAt(ctx, key, 7, "WORLD", 32, time.Minute); !errors.As(err, &mismatch) || mismatch.Have != 12 {
		t.Fatalf("a different chunk at an old offset: err=%v", err)
	}
	// A gap is refused with the length to resume from.
	if _, err = c.AppendAt(ctx, key, 20, "x", 32, time.Minute); !errors.As(err, &mismatch) || mismatch.Have != 12 {
		t.Fatalf("a chunk past the end: err=%v", err)
	}
	if _, err = c.AppendAt(ctx, key, 12, strings.Repeat("x", 21), 32, time.Minute); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a chunk past the cap: err=%v", err)
	}

	got, err := c.Get(ctx, key)
	if err != nil || got != "héllo world" {
		t.Fatalf("value = %q, err=%v", got, err)
	}

	// An expired or never-created key reads as length zero.
	if _, err = c.AppendAt(ctx, key+":missing", 5, "x", 32, time.Minute); !errors.As(err, &mismatch) || mismatch.Have != 0 {
		t.Fatalf("continuing a missing key: err=%v", err)
	}
}
