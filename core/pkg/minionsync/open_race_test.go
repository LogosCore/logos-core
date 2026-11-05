package minionsync

import (
	"context"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// lateSocket is a channel whose open's answer outruns its socket, as the
// socket's handler allows: the socket takes the open (OpenedFor, before its
// WebSocket's upgrade) and becomes the target's (ForTarget) only after
// delay; never, for a negative one (its upgrade failed).
type lateSocket struct {
	*fakeAgentless
	delay time.Duration
}

func (f lateSocket) Open(_ context.Context, _ string, _ map[string]any, under [16]byte, keys *protocol.LinkKeys, _ time.Duration, sent func(string, uint64)) (protocol.LinkFrame, error) {
	sent("ssh-1", 88)
	if _, _, ok := f.svc.OpenedFor("ssh-1", 88); !ok {
		f.t.Error("the socket's open is not waited for")
	}
	if f.delay >= 0 {
		f.spoken.Add(1)
		go func() {
			defer f.spoken.Done()
			time.Sleep(f.delay)
			c := f.newConn()
			f.mu.Lock()
			f.conns = append(f.conns, c)
			f.refused = !f.svc.ForTarget(c, "ssh-1", 88)
			f.mu.Unlock()
			if f.refused {
				// As its handler does: the socket is no one's.
				return
			}
			f.speak(c, under, keys)
		}()
	}
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 88}, nil
}

// connAt is the i-th connection the channel opened.
func (f *fakeAgentless) connAt(i int) *Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns[i]
}

func shortSocketWait(t *testing.T, d time.Duration) {
	was := socketWait
	socketWait = d
	t.Cleanup(func() { socketWait = was })
}

// The channel's answer may come before its socket is the target's: the
// open waits for the socket that took it, and is connected.
func TestOpenAnswerBeforeSocket(t *testing.T) {
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	b.svc.WithTargets(b.targets, lateSocket{fakeAgentless: ch, delay: 50 * time.Millisecond})
	tg, err := b.svc.ConnectTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetConnected || !b.svc.TargetConnected(b.target) {
		t.Fatalf("target %s %q, a connection open: %v", tg.State, tg.StateDetail, b.svc.TargetConnected(b.target))
	}
}

// A socket that took the open and never became the target's (its upgrade
// failed) is waited for socketWait, then the open came to nothing.
func TestOpenSocketNeverArrives(t *testing.T) {
	shortSocketWait(t, 50*time.Millisecond)
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	b.svc.WithTargets(b.targets, lateSocket{fakeAgentless: ch, delay: -1})
	tg, err := b.svc.ConnectTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetUnreachable || tg.StateDetail != "the connection closed as soon as it opened" {
		t.Fatalf("target %s %q", tg.State, tg.StateDetail)
	}
}

// A socket that comes after its open gave up on it is no one's: refused
// (its handler closes it), the target left as the open said, retried.
func TestLateSocketRefused(t *testing.T) {
	shortSocketWait(t, 20*time.Millisecond)
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	b.svc.WithTargets(b.targets, lateSocket{fakeAgentless: ch, delay: 150 * time.Millisecond})
	tg, err := b.svc.ConnectTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetUnreachable || tg.NextCallAt == nil {
		t.Fatalf("the open did not give up: %+v", tg)
	}
	ch.settle()
	ch.mu.Lock()
	refused, conns := ch.refused, len(ch.conns)
	ch.mu.Unlock()
	if conns != 1 || !refused || b.svc.TargetConnected(b.target) || len(b.svc.targetConns(b.target)) != 0 {
		t.Fatalf("the late socket: %d, refused %v", conns, refused)
	}
	if got := b.get(t); got.State != models.TargetUnreachable {
		t.Fatalf("target %+v", got)
	}
}

// An open that gave up waiting for its socket forgets it at once: a socket
// ready even before the open stored what came of it is refused, not left
// open with no open to tell.
func TestOpenGivesUpAtOnce(t *testing.T) {
	shortSocketWait(t, 10*time.Millisecond)
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	key := openKey{"ssh-1", 99}
	b.svc.persist.mu.Lock()
	b.svc.persist.opens[key] = &opening{target: b.target, arrived: make(chan struct{})}
	b.svc.persist.mu.Unlock()
	if _, _, ok := b.svc.OpenedFor("ssh-1", 99); !ok {
		t.Fatal("the open is not waited for")
	}
	if c := b.svc.awaitSocket(context.Background(), key); c != nil {
		t.Fatal("a socket arrived")
	}
	if b.svc.ForTarget(ch.newConn(), "ssh-1", 99) {
		t.Fatal("a socket after its open gave up is the target's")
	}
}
