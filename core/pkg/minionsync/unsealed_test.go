package minionsync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeAgentless is an agentless channel (docs: contracts/agentless-channels/):
// it is the minion itself, and core's messages come and go unsealed. Calls
// are answered as a page answers them; an open opens the connection's
// socket at once, the frames core sends on it kept.
type fakeAgentless struct {
	t   *testing.T
	svc *Service
	// fail answers every call and open with it.
	fail *protocol.LinkFailure

	mu    sync.Mutex
	calls []protocol.CoreMessage
	ids   []uuid.UUID
	conns []*Conn
	sent  []protocol.ConnFrame
}

func (f *fakeAgentless) Call(_ context.Context, _ string, c channellink.Call) (protocol.LinkFrame, error) {
	if f.fail != nil {
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Failure: f.fail}, nil
	}
	var msg protocol.CoreMessage
	if err := protocol.UnmarshalCBOR(c.Data, &msg); err != nil {
		f.t.Errorf("core's message is not unsealed CBOR: %v", err)
		return protocol.LinkFrame{}, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, msg)
	f.ids = append(f.ids, c.ID)
	f.mu.Unlock()
	var out []any
	for _, raw := range msg.Items {
		if protocol.ItemType(raw) == protocol.ItemRehello {
			out = append(out, agentlessHello("server01")) // the minion knows no build: no conn
		}
	}
	max := uint64(4)
	out = append(out, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max})
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, ID: c.ID[:], Data: minionMessage(f.t, msg.ID, out...), Peer: "203.0.113.9"}, nil
}

func (f *fakeAgentless) Open(_ context.Context, _ string, _ map[string]any, _ time.Duration, sent func(string, uint64)) (protocol.LinkFrame, error) {
	if f.fail != nil {
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Failure: f.fail}, nil
	}
	sent("ssh-1", 88)
	target, policy, ok := f.svc.OpenedFor("ssh-1", 88)
	if !ok {
		f.t.Error("the socket's open is not waited for")
	}
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
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 88}, nil
}

// frames are what core sent on the connections, so far.
func (f *fakeAgentless) frames() []protocol.ConnFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.ConnFrame(nil), f.sent...)
}

// agentlessHello is the hello of a minion an agentless channel acts as: no
// process of its own, pid 0.
func agentlessHello(host string) protocol.HelloItem {
	h := hello(host)
	h.PID = 0
	return h
}

// minionMessage is a minion's message with items, unsealed: answering re,
// or (re nil) one it starts.
func minionMessage(t *testing.T, re []byte, items ...any) []byte {
	t.Helper()
	msg := protocol.MinionMessage{V: protocol.MinionMessageV3, Re: re}
	if re == nil {
		msg.ID = protocol.NewMessageID()
	}
	for _, it := range items {
		b, err := protocol.MarshalCBOR(it)
		if err != nil {
			t.Fatal(err)
		}
		msg.Items = append(msg.Items, cbor.RawMessage(b))
	}
	b, err := protocol.MarshalCBOR(msg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newUnsealedRig is a build of a factory whose minions talk unsealed, one
// connection of kind through "ssh", a target for it and the channel.
func newUnsealedRig(t *testing.T, kind string) (*bindRig, *fakeAgentless) {
	t.Helper()
	b := newBindRig(t, true)
	b.build.Crypto = &protocol.BuildCrypto{Suite: protocol.CryptoNone}
	b.build.Connections.Items = []protocol.BuildConnection{{Endpoint: protocol.ChannelEndpointExport{
		Format: protocol.ChannelEndpointExportV2, Name: "servers", Kind: kind,
		Channel: protocol.ExportChannel{ModuleName: "ssh", SchemaVersion: 1}}}}
	row := b.targets.rows[b.target]
	row.ModuleName, row.Kind, row.Values, row.Name = "ssh", kind, models.JSONMap{"host": "10.0.0.7", "user": "root"}, "10.0.0.7"
	if kind == protocol.KindBindPersistent {
		row.Policy = models.PolicyOnDemand
	}
	b.targets.rows[b.target] = row
	ch := &fakeAgentless{t: t, svc: b.svc}
	b.svc.WithTargets(b.targets, ch)
	return b, ch
}

// A call to a minion its channel acts as goes unsealed: a rehello under
// the build id, whose hello (naming no connection) opens a session of the
// target's connection, with no keys.
func TestUnsealedCall(t *testing.T) {
	b, ch := newUnsealedRig(t, protocol.KindBindCall)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetConnected || tg.SessionID == nil || len(ch.calls) != 1 || ch.ids[0] != b.build.BuildID {
		t.Fatalf("target %+v after %d calls", tg, len(ch.calls))
	}
	if first := ch.calls[0]; len(first.Items) != 1 || protocol.ItemType(first.Items[0]) != protocol.ItemRehello {
		t.Fatalf("first call %+v", first)
	}
	s, err := b.sessions.FindBySessionID(context.Background(), *tg.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Conn != 1 || s.ConnKind != protocol.KindBindCall || s.Crypto.Suite != protocol.CryptoNone || s.Staged || s.Hello.Host != "server01" || s.Hello.PID != 0 {
		t.Fatalf("session %+v", s)
	}
}

// Unsealed messages come only over the connection an open of their
// build's target brought: never through the sync call, nor another
// connection; and a minion that talks unsealed does not stage.
func TestUnsealedOnlyOverItsConnection(t *testing.T) {
	b, ch := newUnsealedRig(t, protocol.KindBindPersistent)
	stage := protocol.StageItem{T: protocol.ItemStage, Key: []byte("k"), Corr: make([]byte, 16)}
	if _, err := b.svc.Handle(context.Background(), b.build.BuildID.String(), "", minionMessage(t, nil, hello("x"))); !errors.Is(err, ErrUnopened) {
		t.Fatalf("through the sync call: %v", err)
	}
	other := NewConn("other", protocol.KindReversePersistent, "http-1", "", func(context.Context, protocol.ConnFrame) error { return nil }, func() {})
	if _, err := b.svc.Exchange(context.Background(), other, b.build.BuildID.String(), "", minionMessage(t, nil, hello("x"))); !errors.Is(err, ErrUnopened) {
		t.Fatalf("over another connection: %v", err)
	}
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "core's first word", func() bool { return len(ch.frames()) == 1 })
	reply, err := b.svc.Exchange(context.Background(), ch.conns[0], b.build.BuildID.String(), "", minionMessage(t, nil, stage))
	if err != nil {
		t.Fatal(err)
	}
	var m protocol.CoreMessage
	if protocol.UnmarshalCBOR(reply, &m) != nil || len(m.Items) != 1 || protocol.ItemType(m.Items[0]) != protocol.ItemErr {
		t.Fatalf("a stage: %+v", m)
	}
}

// Core speaks first on a connection of a build that talks unsealed: a
// rehello under the build id, whose hello opens the session the target is
// linked to; on the next connection, a message under the session's id.
func TestUnsealedOpenSpeaksFirst(t *testing.T) {
	b, ch := newUnsealedRig(t, protocol.KindBindPersistent)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "core's first word", func() bool { return len(ch.frames()) == 1 })
	first := ch.frames()[0]
	var rehello protocol.CoreMessage
	if uuid.UUID(first.ID) != b.build.BuildID || protocol.UnmarshalCBOR(first.Data, &rehello) != nil || len(rehello.Items) != 1 ||
		protocol.ItemType(rehello.Items[0]) != protocol.ItemRehello || rehello.ID == nil {
		t.Fatalf("first word under %x: %+v", first.ID, rehello)
	}
	c := ch.conns[0]
	reply, err := b.svc.Exchange(context.Background(), c, b.build.BuildID.String(), "203.0.113.9", minionMessage(t, nil, agentlessHello("server01")))
	if err != nil || reply == nil {
		t.Fatalf("hello: %v", err)
	}
	var resp protocol.CoreMessage
	if protocol.UnmarshalCBOR(reply, &resp) != nil || len(resp.Items) != 1 || protocol.ItemType(resp.Items[0]) != protocol.ItemHelloResponse {
		t.Fatalf("the hello's reply: %+v", resp)
	}
	tg := b.get(t)
	if tg.SessionID == nil {
		t.Fatal("the target is not linked to the session")
	}
	s, _ := b.sessions.FindBySessionID(context.Background(), *tg.SessionID)
	if s.Conn != 1 || s.ConnKind != protocol.KindBindPersistent {
		t.Fatalf("session %+v", s)
	}

	// Closed, and opened again: core speaks first under the session's id.
	c.closeBecause("")
	waitFor(t, "not connected", func() bool { return b.get(t).State == models.TargetNotConnected })
	if _, err := b.svc.ConnectTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "core's first word again", func() bool { return len(ch.frames()) == 2 })
	if again := ch.frames()[1]; uuid.UUID(again.ID) != *tg.SessionID {
		t.Fatalf("first word under %x, not the session's", again.ID)
	}
}

// A target that refuses the channel (its login, its host key) is refused,
// not retried, whether a call or an open met it.
func TestUnsealedRefusals(t *testing.T) {
	for _, c := range []struct {
		kind string
		fail protocol.LinkFailure
	}{
		{protocol.KindBindCall, protocol.LinkFailure{Kind: protocol.LinkFailLogin, Msg: "the server refused the login"}},
		{protocol.KindBindPersistent, protocol.LinkFailure{Kind: protocol.LinkFailHostKey, Msg: "the server's host key is SHA256:x"}},
	} {
		b, ch := newUnsealedRig(t, c.kind)
		ch.fail = &c.fail
		tg, err := b.svc.CheckTarget(context.Background(), b.target)
		if err != nil {
			t.Fatal(err)
		}
		if tg.State != models.TargetRefused || tg.NextCallAt != nil || tg.StateDetail != protocol.LinkFailName(c.fail.Kind)+": "+c.fail.Msg {
			t.Errorf("%s: %+v", c.kind, tg)
		}
	}
}

// Only a minion an agentless channel acts as goes without a pid: a sealed
// minion's hello still names its process.
func TestReadHelloPID(t *testing.T) {
	raw, err := protocol.MarshalCBOR(agentlessHello("server01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readHello(raw, false); err == nil {
		t.Fatal("a sealed minion's hello without a pid was taken")
	}
	if _, err := readHello(raw, true); err != nil {
		t.Fatalf("unsealed: %v", err)
	}
}

// Under the build id the answer to core's rehello is the hello alone
// (protocol.ValidateBuildMessage): a hello with a get_tasks is refused, and
// opens no session.
func TestUnsealedHelloAlone(t *testing.T) {
	b, ch := newUnsealedRig(t, protocol.KindBindPersistent)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "core's first word", func() bool { return len(ch.frames()) == 1 })
	max := uint64(4)
	reply, err := b.svc.Exchange(context.Background(), ch.conns[0], b.build.BuildID.String(), "203.0.113.9",
		minionMessage(t, nil, agentlessHello("server01"), protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max}))
	if err != nil {
		t.Fatal(err)
	}
	var m protocol.CoreMessage
	if protocol.UnmarshalCBOR(reply, &m) != nil || len(m.Items) != 1 || protocol.ItemType(m.Items[0]) != protocol.ItemErr {
		t.Fatalf("reply %+v", m)
	}
	if tg := b.get(t); tg.SessionID != nil {
		t.Fatalf("a session opened: %+v", tg)
	}
}
