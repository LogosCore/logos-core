package miniontunnels

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Stopping an idle tunnel (docs: contracts/minion-messages/tunnels.md, Stop
// when idle): a tunnel task whose stop_idle is set is stopped by core, as
// Stop does, once no stream has been open that long. The clock runs from
// when core sees the minion has the task, and starts again each time its
// last open stream ends; any open stream holds it, however quiet. A
// restart starts it again. The minion is unchanged: it sees a cancel.

// streamsChanged notes a stream opened or went: the tunnel idles from now
// once none is open (the clock running), and not while one is.
func (t *tunnel) streamsChanged(now time.Time) {
	switch {
	case len(t.streams) > 0 || (t.vpn != nil && len(t.vpn.clients) > 0):
		// A VPN's connected client holds the clock, as an open stream does.
		t.idleSince = time.Time{}
	case t.idleArmed && t.idleSince.IsZero():
		t.idleSince = now
		t.changed = true
	}
}

// stopsAt is when core stops the tunnel unless a stream opens first; zero
// when it will not now.
func (t *tunnel) stopsAt() time.Time {
	if t.stopIdle <= 0 || t.idleSince.IsZero() || t.closing {
		return time.Time{}
	}
	return t.idleSince.Add(t.stopIdle)
}

// idleCheck runs the clock of a task the minion has: arms it, and stops a
// tunnel idle past its stop_idle.
func (s *Service) idleCheck(ctx context.Context, id uuid.UUID) {
	s.mu.Lock()
	t := s.tunnels[id]
	if t == nil || t.closing || t.stopping {
		s.mu.Unlock()
		return
	}
	now := s.now()
	if !t.idleArmed {
		t.idleArmed = true
		t.streamsChanged(now)
	}
	at := t.stopsAt()
	if at.IsZero() || now.Before(at) {
		s.mu.Unlock()
		return
	}
	t.stopping = true
	idle := t.stopIdle
	s.mu.Unlock()
	s.logger.Info("tunnels: stopping a tunnel no stream used", zap.String("task_id", id.String()), zap.Duration("stop_idle", idle))
	s.cancel(ctx, id, fmt.Sprintf("stopped by core: no stream open for %s (stop_idle)", shortDuration(idle)))
}

// shortDuration is a duration as an operator types one: 1h, 1h30m, 45s.
func shortDuration(d time.Duration) string {
	if d >= time.Second {
		d = d.Round(time.Second)
	}
	s := d.String()
	s = strings.Replace(s, "m0s", "m", 1)
	return strings.Replace(s, "h0m", "h", 1)
}
