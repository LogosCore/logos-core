package minionsync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeListener is a channel and the listening minion it dials: an open
// that reaches it opens the connection's socket at once (as a channel does,
// naming the open), and the test drives the connection from there.
type fakeListener struct {
	t   *testing.T
	svc *Service

	mu     sync.Mutex
	opens  []map[string]any
	fail   *protocol.LinkFailure
	conns  []*Conn
	closed []*Conn
}

func (f *fakeListener) Call(context.Context, string, channellink.Call) (protocol.LinkFrame, error) {
	return protocol.LinkFrame{}, nil
}

func (f *fakeListener) Open(ctx context.Context, module string, values map[string]any, _ time.Duration, sent func(string, uint64)) (protocol.LinkFrame, error) {
	f.mu.Lock()
	f.opens = append(f.opens, values)
	fail := f.fail
	f.mu.Unlock()
	if fail != nil {
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Failure: fail}, nil
	}
	sent("tcp-1", 77)
	// The channel's socket, naming the open.
	target, policy, ok := f.svc.OpenedFor("tcp-1", 77)
	if !ok {
		f.t.Error("the socket's open is not waited for")
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 77, Failure: &protocol.LinkFailure{Kind: protocol.LinkFailOther}}, nil
	}
	var c *Conn
	c = NewConn(uuid.NewString(), protocol.KindBindPersistent, "tcp-1", "10.0.0.5",
		func(context.Context, protocol.ConnFrame) error { return nil },
		func() {
			f.mu.Lock()
			f.closed = append(f.closed, c)
			f.mu.Unlock()
			go f.svc.Closed(context.Background(), c)
		})
	f.svc.ForTarget(c, target, policy)
	f.mu.Lock()
	f.conns = append(f.conns, c)
	f.mu.Unlock()
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 77}, nil
}

func (f *fakeListener) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.opens)
}

// newPersistentRig is a build with one bind-persistent connection through
// "tcp", a target of policy for it, and the channel and listener.
func newPersistentRig(t *testing.T, policy string) (*bindRig, *fakeListener) {
	t.Helper()
	b := newBindRig(t, true)
	b.build.Connections.Items[0].Endpoint = protocol.ChannelEndpointExport{Format: protocol.ChannelEndpointExportV2, Name: "listener",
		Kind: protocol.KindBindPersistent, Channel: protocol.ExportChannel{ModuleName: "tcp", SchemaVersion: 1},
		Params: map[string]any{"listen_port": 4444}}
	row := b.targets.rows[b.target]
	row.ModuleName, row.Kind, row.Policy = "tcp", protocol.KindBindPersistent, policy
	row.Values = models.JSONMap{"host": "10.0.0.5"}
	b.targets.rows[b.target] = row
	l := &fakeListener{t: t, svc: b.svc}
	b.svc.WithTargets(b.targets, l)
	return b, l
}

// An on-demand target: added (Check now), core opens its connection with
// the endpoint's values and the target's; the minion's first message over
// it links its session to the target; closed, it is not connected.
func TestPersistentOpenLinksTheSession(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetConnected || l.openCount() != 1 || len(b.svc.targetConns(b.target)) != 1 {
		t.Fatalf("after the open: %+v, %d opens", tg, l.openCount())
	}
	if v := l.opens[0]; v["listen_port"] != 4444 || v["host"] != "10.0.0.5" {
		t.Fatalf("values %v", v)
	}
	// Connected already: Check now opens no second one.
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil || l.openCount() != 1 {
		t.Fatalf("a second open: %d (%v)", l.openCount(), err)
	}
	// The minion speaks first: a hello under the build id opens its
	// session, which the target is then linked to.
	c := l.conns[0]
	reply, err := b.svc.Exchange(context.Background(), c, b.build.BuildID.String(), "10.0.0.5", b.sealed(t, b.build.BuildID, hello("listener01")))
	if err != nil || reply == nil {
		t.Fatalf("hello: %v", err)
	}
	if got := b.get(t); got.SessionID == nil {
		t.Fatal("the target is not linked to the minion's session")
	}
	// The minion hangs up.
	c.closeBecause("")
	waitFor(t, "not connected", func() bool { return b.get(t).State == models.TargetNotConnected })
	if got := b.get(t); got.NextCallAt != nil || got.StateDetail == "" {
		t.Fatalf("closed, on-demand: %+v", got)
	}
	// Dropped while a task waits: opened again for it.
	sid := *b.get(t).SessionID
	b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskQueued})
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil || l.openCount() != 2 {
		t.Fatalf("open again: %d (%v)", l.openCount(), err)
	}
	l.conns[1].closeBecause("")
	waitFor(t, "a reopen for the waiting task", func() bool {
		got := b.get(t)
		return got.State == models.TargetNotConnected && got.NextCallAt != nil
	})
}

// A manual target opens only on Connect, closes on Disconnect, and is not
// opened again by core; one of policy always is.
func TestPersistentPolicies(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyManual)
	b.svc.callTarget(context.Background(), b.target, bindNews)
	b.svc.callTarget(context.Background(), b.target, bindScheduled)
	if l.openCount() != 0 {
		t.Fatalf("a manual target opened by core: %d", l.openCount())
	}
	tg, err := b.svc.ConnectTarget(context.Background(), b.target)
	if err != nil || tg.State != models.TargetConnected || l.openCount() != 1 {
		t.Fatalf("connect: %+v %v", tg, err)
	}
	tg, err = b.svc.DisconnectTarget(context.Background(), b.target)
	if err != nil || tg.State != models.TargetNotConnected || tg.StateDetail != "an operator disconnected it" || tg.NextCallAt != nil {
		t.Fatalf("disconnect: %+v %v", tg, err)
	}

	a, la := newPersistentRig(t, models.PolicyAlways)
	if _, err := a.svc.CheckTarget(context.Background(), a.target); err != nil || la.openCount() != 1 {
		t.Fatal("always: not opened")
	}
	la.conns[0].closeBecause("")
	waitFor(t, "a reopen scheduled", func() bool { got := a.get(t); return got.State == models.TargetNotConnected && got.NextCallAt != nil })
}

// On demand, news opens the connection only when there is work: a task
// that ended, or a connect or disconnect kept, is news with nothing to send.
// Disconnect answers with the state the close left.
func TestPersistentOnDemandNews(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	row := b.targets.rows[b.target]
	sid := uuid.New()
	row.SessionID = &sid
	b.targets.rows[b.target] = row
	b.svc.callTarget(context.Background(), b.target, bindNews)
	if l.openCount() != 0 {
		t.Fatalf("news with nothing to send opened it: %d", l.openCount())
	}
	b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskQueued})
	b.svc.callTarget(context.Background(), b.target, bindNews)
	if l.openCount() != 1 || b.get(t).State != models.TargetConnected {
		t.Fatalf("a task queued: %d opens", l.openCount())
	}
	b.tasks.rows = nil
	tg, err := b.svc.DisconnectTarget(context.Background(), b.target)
	if err != nil || tg.State != models.TargetNotConnected {
		t.Fatalf("disconnect: %+v %v", tg, err)
	}
}

// An open that fails leaves the target unreachable, retried but for a
// manual one; an idle connection closes by its policy's time.
func TestPersistentFailureAndIdle(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	l.fail = &protocol.LinkFailure{Kind: protocol.LinkFailUnreachable, Msg: "connection refused"}
	tg, _ := b.svc.CheckTarget(context.Background(), b.target)
	if tg.State != models.TargetUnreachable || tg.NextCallAt == nil || tg.StateDetail != "unreachable: connection refused" {
		t.Fatalf("a failed open: %+v", tg)
	}
	m, lm := newPersistentRig(t, models.PolicyManual)
	lm.fail = l.fail
	if tg, _ := m.svc.ConnectTarget(context.Background(), m.target); tg.State != models.TargetUnreachable || tg.NextCallAt != nil {
		t.Fatalf("a manual failed open: %+v", tg)
	}

	l.fail = nil
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil || len(l.conns) != 1 {
		t.Fatal("not opened")
	}
	// Idle past on-demand's ten minutes.
	l.conns[0].touch(time.Now().Add(-11 * time.Minute))
	b.svc.closeIdle()
	waitFor(t, "closed idle", func() bool { return b.get(t).State == models.TargetNotConnected })
	if d := b.get(t).StateDetail; d != "idle for 10m0s" {
		t.Fatalf("detail %q", d)
	}
}

// sealed is a minion message of items, sealed under id with the build's
// keys.
func (b *bindRig) sealed(t *testing.T, id uuid.UUID, items ...any) []byte {
	t.Helper()
	msg := protocol.MinionMessage{V: protocol.MinionMessageV3, ID: protocol.NewMessageID()}
	for _, it := range items {
		raw, err := protocol.MarshalCBOR(it)
		if err != nil {
			t.Fatal(err)
		}
		msg.Items = append(msg.Items, raw)
	}
	plain, _ := protocol.MarshalCBOR(msg)
	frame, err := protocol.SealFrame(b.keys, id, protocol.FrameMinionToCore, plain)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("no %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// As core starts, targets left connected are not (their connections went
// with the process): one of policy always is opened again at once, an
// on-demand one with nothing waiting is not.
func TestPersistentResetOnStart(t *testing.T) {
	a, la := newPersistentRig(t, models.PolicyAlways)
	row := a.targets.rows[a.target]
	row.State = models.TargetConnected
	a.targets.rows[a.target] = row
	a.svc.resetPersistent(context.Background())
	got := a.get(t)
	if got.State != models.TargetNotConnected || got.NextCallAt == nil || la.openCount() != 0 {
		t.Fatalf("always: %+v", got)
	}

	d, _ := newPersistentRig(t, models.PolicyOnDemand)
	row = d.targets.rows[d.target]
	row.State = models.TargetConnecting
	d.targets.rows[d.target] = row
	d.svc.resetPersistent(context.Background())
	if got := d.get(t); got.State != models.TargetNotConnected || got.NextCallAt != nil {
		t.Fatalf("on demand: %+v", got)
	}
}
