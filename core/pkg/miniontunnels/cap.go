package miniontunnels

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// A tunnel's bandwidth cap (docs: contracts/minion-messages/tunnels.md,
// Bandwidth cap): bytes a second each way, over all of its streams
// together, so one tunnel does not fill a slow connection's messages or a
// scarce network. Core enforces it alone, the minion unchanged: it paces
// what it sends (its tunnel_data and datagrams toward the minion), and
// what it lets the minion send (the room its acks give, none while the
// tunnel is over); a datagram over it is dropped, as UDP drops what does
// not fit. The operator sets it as the tunnel starts and changes it live.

// capBurst is the least a capped tunnel may send at once: a piece of a
// stream fits whatever the cap.
const capBurst = 64 << 10

// bucket is what a capped tunnel may still send one way: tokens, filled at
// the cap's rate up to a second's worth (capBurst at least), as of at.
type bucket struct {
	tokens float64
	at     time.Time
}

// fill brings the bucket to now at rate bytes a second.
func (b *bucket) fill(now time.Time, rate int64) {
	burst := float64(max(rate, capBurst))
	if b.at.IsZero() {
		b.tokens, b.at = burst, now
		return
	}
	if d := now.Sub(b.at); d > 0 {
		b.tokens = min(burst, b.tokens+d.Seconds()*float64(rate))
		b.at = now
	}
}

// until is how long until the bucket holds n tokens again at rate.
func (b *bucket) until(n float64, rate int64) time.Duration {
	if b.tokens >= n || rate <= 0 {
		return 0
	}
	return time.Duration(math.Ceil((n - b.tokens) / float64(rate) * float64(time.Second)))
}

// outAllows is how many bytes the tunnel may send the minion now: all of
// them without a cap; under s.mu.
func (t *tunnel) outAllows(now time.Time) int {
	if t.cap <= 0 {
		return math.MaxInt
	}
	t.out.fill(now, t.cap)
	return max(int(t.out.tokens), 0)
}

// sentOut counts n bytes the tunnel sent the minion.
func (t *tunnel) sentOut(n int) {
	if t.cap > 0 {
		t.out.tokens -= float64(n)
	}
}

// inAllowsNow is how many bytes the tunnel lets the minion send now: all of
// them without a cap, none while it is over it; under s.mu.
func (t *tunnel) inAllowsNow(now time.Time) uint64 {
	if t.cap <= 0 {
		return math.MaxUint64
	}
	t.in.fill(now, t.cap)
	return uint64(max(t.in.tokens, 0))
}

// tookIn counts n bytes the minion sent the tunnel.
func (t *tunnel) tookIn(n int, now time.Time) {
	if t.cap > 0 {
		t.in.fill(now, t.cap)
		t.in.tokens -= float64(n)
	}
}

// inAllows: a datagram of n bytes from the minion fits the cap (taken
// from it when it does); under s.mu.
func (t *tunnel) inAllows(n int, now time.Time) bool {
	if t.cap <= 0 {
		return true
	}
	t.in.fill(now, t.cap)
	if t.in.tokens < float64(n) {
		return false
	}
	t.in.tokens -= float64(n)
	return true
}

// capLater wakes the session once the tunnel's cap lets it go on: its
// streams held for room get an ack, and its bytes waiting go. Under s.mu.
func (s *Service) capLater(t *tunnel, now time.Time) {
	if t.cap <= 0 || t.capWake {
		return
	}
	wait := max(t.out.until(minPiece, t.cap), t.in.until(1, t.cap), 10*time.Millisecond)
	t.capWake = true
	time.AfterFunc(wait, func() {
		s.mu.Lock()
		t.capWake = false
		for _, st := range t.streams {
			if st.capHeld {
				st.capHeld, st.ackDue = false, true
			}
		}
		session := t.session
		s.mu.Unlock()
		s.wake(session)
	})
}

// SetCap changes the task's tunnel's bandwidth cap, bytes a second each
// way (0: none), live, and keeps it with its port or listener; IsNotFound
// when core holds no tunnel of the task.
func (s *Service) SetCap(ctx context.Context, task uuid.UUID, limit int64) error {
	if limit < 0 {
		return fmt.Errorf("a cap is bytes a second, 0 for none")
	}
	s.mu.Lock()
	t := s.tunnels[task]
	if t == nil || t.closing {
		s.mu.Unlock()
		return fmt.Errorf("the task's tunnel: %w", repository.ErrNotFound)
	}
	t.cap, t.out, t.in, t.changed = limit, bucket{}, bucket{}, true
	for _, st := range t.streams {
		if st.capHeld {
			st.capHeld, st.ackDue = false, true
		}
	}
	reverse, vpn, session := t.reverse, t.vpn != nil, t.session
	s.mu.Unlock()
	s.wake(session)
	if vpn {
		return s.vpnTunnels.SetCap(ctx, task, limit)
	}
	if reverse {
		if s.listeners == nil {
			return nil
		}
		return s.listeners.SetCap(ctx, task, limit)
	}
	return s.ports.SetCap(ctx, task, limit)
}
