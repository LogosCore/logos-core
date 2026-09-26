package auth

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

// TestIsLuaError covers the spellings a Lua error_reply can reach go-redis
// in. Redis 7 prefixes a code-less reply with "ERR ", which the old exact
// match missed, turning a stale refresh token into a 500.
func TestIsLuaError(t *testing.T) {
	cases := []struct {
		err  error
		name string
		want bool
	}{
		{errors.New("NOTFOUND"), "NOTFOUND", true},
		{errors.New("ERR NOTFOUND"), "NOTFOUND", true},
		{errors.New("NOTFOUND old key gone"), "NOTFOUND", true},
		{errors.New("ERR CORRUPT"), "CORRUPT", true},
		{errors.New("ERR NOTFOUND"), "CORRUPT", false},
		{errors.New("NOTFOUNDX"), "NOTFOUND", false},
		{errors.New("dial tcp: connection refused"), "NOTFOUND", false},
		{nil, "NOTFOUND", false},
	}
	for _, c := range cases {
		if got := isLuaError(c.err, c.name); got != c.want {
			t.Errorf("isLuaError(%v, %q) = %v, want %v", c.err, c.name, got, c.want)
		}
	}
}

// TestIntegrationRotateUnknownToken runs the rotate script against a real
// Redis: an unknown refresh token must come back as ErrTokenInvalid, not as
// a generic error, whatever prefix the server puts on the Lua error reply.
//
// Opt-in: set INTEGRATION_REDIS_ADDR (host:port) to a Redis you can write
// to; `make test-integration` points it at the dev stack. It only touches
// keys under a fresh random user id and deletes them afterwards.
func TestIntegrationRotateUnknownToken(t *testing.T) {
	addr := os.Getenv("INTEGRATION_REDIS_ADDR")
	if addr == "" {
		t.Skip("INTEGRATION_REDIS_ADDR not set; skipping Redis integration test")
	}
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("INTEGRATION_REDIS_ADDR %q is not host:port", addr)
	}

	ctx := context.Background()
	store, err := NewRedisTokenStore(ctx, RedisTokenStoreConfig{
		Host:     host,
		Port:     port,
		Password: os.Getenv("INTEGRATION_REDIS_PASSWORD"),
		Logger:   zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer store.Close()

	userID := uuid.New()
	defer func() { _ = store.DeleteAllForUser(ctx, userID) }()

	_, err = store.Rotate(ctx, userID, "missing-hash", "new-hash", time.Minute)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Rotate(unknown hash) error = %v, want ErrTokenInvalid", err)
	}

	// A real rotation still works, and replaying the old hash is rejected
	// the same way.
	sessionID := uuid.New()
	if err := store.Create(ctx, userID, sessionID, "old-hash", time.Minute); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Rotate(ctx, userID, "old-hash", "new-hash", time.Minute)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if got != sessionID {
		t.Fatalf("Rotate session = %s, want %s", got, sessionID)
	}
	if _, err := store.Rotate(ctx, userID, "old-hash", "newer-hash", time.Minute); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Rotate(replayed hash) error = %v, want ErrTokenInvalid", err)
	}
}
