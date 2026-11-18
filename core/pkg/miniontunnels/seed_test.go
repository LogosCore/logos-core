package miniontunnels

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

// A tunnel task's push to its ack seeds the session's round trip before its
// streams measure one: the wait for an ack follows a slow link from the
// first send, never below ResendAfter; it never replaces a stream's sample,
// and the first stream's sample replaces it.
func TestSeedRTT(t *testing.T) {
	cfg := DefaultConfig()
	s := New(cfg, &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}, nil, zap.NewNop())
	resend := func(session uuid.UUID) time.Duration {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.resendAfter(session)
	}

	fast, slow := uuid.New(), uuid.New()
	s.SeedRTT(fast, 600*time.Millisecond)
	if got := resend(fast); got != cfg.ResendAfter {
		t.Fatalf("a fast link's seed: %s, not the floor", got)
	}
	s.SeedRTT(slow, 4*time.Second)
	if got := resend(slow); got != 12*time.Second {
		t.Fatalf("a slow link's seed: %s, not its round trip and four times its spread", got)
	}
	// A second seed does not move it; the first stream's sample replaces it.
	s.SeedRTT(slow, 20*time.Second)
	if got := resend(slow); got != 12*time.Second {
		t.Fatalf("seeded again: %s", got)
	}
	s.mu.Lock()
	s.rttOf(slow).sample(3 * time.Second)
	s.mu.Unlock()
	if got := resend(slow); got != 9*time.Second {
		t.Fatalf("a stream's sample after the seed: %s, not it alone", got)
	}
	s.SeedRTT(slow, 30*time.Second)
	if got := resend(slow); got != 9*time.Second {
		t.Fatalf("a seed after a stream's sample moved it: %s", got)
	}
}
