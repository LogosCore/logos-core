package minionsync

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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

func (f lateSocket) Open(_ context.Context, _ string, _ map[string]any, _ [16]byte, _ *protocol.LinkKeys, _ time.Duration, sent func(string, uint64)) (protocol.LinkFrame, error) {
	sent("ssh-1", 88)
	target, policy, ok := f.svc.OpenedFor("ssh-1", 88)
	if !ok {
		f.t.Error("the socket's open is not waited for")
	}
	if f.delay >= 0 {
		go func() {
			time.Sleep(f.delay)
			var c *Conn
			c = NewConn(uuid.NewString(), protocol.KindBindPersistent, "ssh-1", "203.0.113.9",
				func(_ context.Context, fr protocol.ConnFrame) error {
					f.mu.Lock()
					f.sent = append(f.sent, fr)
					f.mu.Unlock()
					return nil
				},
				func() { go f.svc.Closed(context.Background(), c) })
			f.svc.ForTarget(c, target, policy)
			f.mu.Lock()
			f.conns = append(f.conns, c)
			f.mu.Unlock()
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
	b.svc.WithTargets(b.targets, lateSocket{ch, 50 * time.Millisecond})
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
	b.svc.WithTargets(b.targets, lateSocket{ch, -1})
	tg, err := b.svc.ConnectTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetUnreachable || tg.StateDetail != "the connection closed as soon as it opened" {
		t.Fatalf("target %s %q", tg.State, tg.StateDetail)
	}
}

// A target an open gave up on while its connection came is connected once
// its minion speaks over that connection.
func TestTargetConnectedAsItsMinionSpeaks(t *testing.T) {
	shortSocketWait(t, 20*time.Millisecond)
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	b.svc.WithTargets(b.targets, lateSocket{ch, 150 * time.Millisecond})
	tg, err := b.svc.ConnectTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetUnreachable {
		t.Fatalf("the open did not give up: %s", tg.State)
	}
	waitFor(t, "its connection", func() bool { ch.mu.Lock(); defer ch.mu.Unlock(); return len(ch.conns) == 1 })
	if _, err := b.svc.Exchange(context.Background(), ch.connAt(0), b.build.BuildID.String(), "203.0.113.9",
		sealedMinionMessage(t, b.keys, b.build.BuildID, nil, agentlessHello("server01"))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "connected", func() bool { return b.get(t).State == models.TargetConnected })
	if tg := b.get(t); tg.SessionID == nil || tg.Failures != 0 || tg.NextCallAt != nil {
		t.Fatalf("target %+v", tg)
	}
}
