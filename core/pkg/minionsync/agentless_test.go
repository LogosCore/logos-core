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
// it is the minion itself, and opens core's messages and seals its own with
// the keys each call and open gives it. Calls are answered as a page
// answers them; an open opens the connection's socket at once, the frames
// core sends on it kept, and the channel speaks first over it, as the
// minion (first: its hello alone under the id the open said, with the
// open's keys), core's replies kept.
type fakeAgentless struct {
	t   *testing.T
	svc *Service
	// fail answers every call and open with it.
	fail *protocol.LinkFailure
	// first is the minion's first word over a connection; nil: a hello.
	first []any

	mu    sync.Mutex
	calls []protocol.CoreMessage
	ids   []uuid.UUID
	conns []*Conn
	sent  []protocol.ConnFrame
	// keys are what each call and open gave, in order; unders the ids the
	// opens said the minion speaks under; replies core's replies to the
	// minion's first words.
	keys    []*protocol.LinkKeys
	unders  []uuid.UUID
	replies [][]byte
	spoken  sync.WaitGroup
	// refused: a socket the channel opened late was refused (ForTarget).
	refused bool
	// creds are the credentials each call carried; logins what the
	// channel answers of them.
	creds  []map[string]protocol.Credential
	logins []protocol.LinkLogin
}

func (f *fakeAgentless) Call(_ context.Context, _ string, c channellink.Call) (protocol.LinkFrame, error) {
	f.mu.Lock()
	f.keys = append(f.keys, c.Keys)
	f.creds = append(f.creds, c.Credentials)
	logins := f.logins
	f.mu.Unlock()
	if f.fail != nil {
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Failure: f.fail, Logins: logins}, nil
	}
	if c.Keys == nil {
		f.t.Error("a call to an agentless kind without its id's keys")
		return protocol.LinkFrame{}, errors.New("no keys")
	}
	plain, err := protocol.OpenFrame(c.Keys.Material, c.ID, protocol.FrameCoreToMinion, c.Data)
	if err != nil {
		f.t.Errorf("core's message does not open with the call's keys: %v", err)
		return protocol.LinkFrame{}, err
	}
	var msg protocol.CoreMessage
	if err := protocol.UnmarshalCBOR(plain, &msg); err != nil {
		f.t.Fatal(err)
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
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, ID: c.ID[:],
		Data: sealedMinionMessage(f.t, c.Keys.Material, c.ID, msg.ID, out...), Peer: "203.0.113.9", Logins: logins}, nil
}

func (f *fakeAgentless) Open(_ context.Context, _ string, _ map[string]any, _ map[string]protocol.Credential, under [16]byte,
	keys *protocol.LinkKeys, acting *protocol.LinkActing, _ time.Duration,
	sent func(string, uint64)) (protocol.LinkFrame, error) {
	f.mu.Lock()
	f.unders = append(f.unders, uuid.UUID(under))
	f.keys = append(f.keys, keys)
	f.mu.Unlock()
	if f.fail != nil {
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Failure: f.fail}, nil
	}
	f.mu.Lock()
	request := uint64(80 + len(f.unders))
	f.mu.Unlock()
	sent("ssh-1", request)
	if _, _, ok := f.svc.OpenedFor("ssh-1", request); !ok {
		f.t.Error("the socket's open is not waited for")
	}
	c := f.newConn()
	if !f.svc.ForTarget(c, "ssh-1", request) {
		f.t.Error("the socket is not the open's")
	}
	f.mu.Lock()
	f.conns = append(f.conns, c)
	f.mu.Unlock()
	f.speak(c, under, keys)
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: request}, nil
}

// newConn is a connection's socket: what core sends on it kept, Closed once
// when it closes.
func (f *fakeAgentless) newConn() *Conn {
	var c *Conn
	var once sync.Once
	c = NewConn(uuid.NewString(), protocol.KindBindPersistent, "ssh-1", "203.0.113.9",
		func(_ context.Context, fr protocol.ConnFrame) error {
			f.mu.Lock()
			f.sent = append(f.sent, fr)
			f.mu.Unlock()
			return nil
		},
		func() {
			once.Do(func() {
				f.spoken.Add(1)
				go func() {
					defer f.spoken.Done()
					f.svc.Closed(context.Background(), c)
				}()
			})
		})
	return c
}

// speak is the minion's first word over c, as the channel says it: under
// the id the open said, sealed with the open's keys.
func (f *fakeAgentless) speak(c *Conn, under [16]byte, keys *protocol.LinkKeys) {
	if keys == nil {
		return
	}
	first := f.first
	if first == nil {
		first = []any{agentlessHello("server01")}
	}
	f.spoken.Add(1)
	go func() {
		defer f.spoken.Done()
		reply, err := f.svc.Exchange(context.Background(), c, uuid.UUID(under).String(), "203.0.113.9",
			sealedMinionMessage(f.t, keys.Material, under, nil, first...))
		if err != nil {
			f.t.Errorf("the minion's first word: %v", err)
		}
		f.mu.Lock()
		f.replies = append(f.replies, reply)
		f.mu.Unlock()
	}()
}

// settle waits until the minion's first words were taken and the closed
// sockets' handlers are done.
func (f *fakeAgentless) settle() { f.spoken.Wait() }

// frames are what core sent on the connections, so far.
func (f *fakeAgentless) frames() []protocol.ConnFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.ConnFrame(nil), f.sent...)
}

// fakeSchemas are channels' connection schemas, by module name.
type fakeSchemas map[string]*protocol.ConnectionSchema

func (f fakeSchemas) Schema(_ context.Context, module string) (*protocol.ConnectionSchema, error) {
	return f[module], nil
}

// agentlessHello is the hello of a minion an agentless channel acts as: no
// process of its own, pid 0.
func agentlessHello(host string) protocol.HelloItem {
	h := hello(host)
	h.PID = 0
	return h
}

// minionMessage is a minion's message with items, answering re, or (re
// nil) one it starts: its CBOR, to seal.
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

// sealedMinionMessage is minionMessage sealed with keys under id, as the
// minion sends it.
func sealedMinionMessage(t *testing.T, keys protocol.AES256CBCHMACSHA256Material, id [16]byte, re []byte, items ...any) []byte {
	t.Helper()
	frame, err := protocol.SealFrame(keys, id, protocol.FrameMinionToCore, minionMessage(t, re, items...))
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

// openCore opens core's message under id with keys, as the minion does.
func openCore(t *testing.T, keys protocol.AES256CBCHMACSHA256Material, id [16]byte, frame []byte) protocol.CoreMessage {
	t.Helper()
	plain, err := protocol.OpenFrame(keys, id, protocol.FrameCoreToMinion, frame)
	if err != nil {
		t.Fatalf("core's message does not open: %v", err)
	}
	var m protocol.CoreMessage
	if err := protocol.UnmarshalCBOR(plain, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// newAgentlessRig is a build (sealed, as every build) with one connection
// of kind through "ssh", whose channel acts as the minion on it, a target
// for it and the channel.
func newAgentlessRig(t *testing.T, kind string) (*bindRig, *fakeAgentless) {
	t.Helper()
	b := newBindRig(t, true)
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
	b.svc.WithChannelSchemas(fakeSchemas{"ssh": {SchemaVersion: protocol.ConnectionSchemaV2, Version: 1,
		Kinds: map[string]protocol.ConnectionKind{kind: {Agentless: true}}}})
	return b, ch
}

// A call to an agentless kind carries its id's keys: the build's under the
// build id, which the channel opens core's rehello and seals the minion's
// hello with. The hello (naming no connection) opens a session of the
// target's connection, which keeps the build's keys: it did not stage.
func TestAgentlessCall(t *testing.T) {
	b, ch := newAgentlessRig(t, protocol.KindBindCall)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetConnected || tg.SessionID == nil || len(ch.calls) != 1 || ch.ids[0] != b.build.BuildID {
		t.Fatalf("target %+v after %d calls", tg, len(ch.calls))
	}
	if k := ch.keys[0]; k == nil || k.Suite != protocol.CryptoAES256CBCHMACSHA256 || string(k.Material.EncKey) != string(b.keys.EncKey) {
		t.Fatalf("the call's keys %+v, not the build's", k)
	}
	if first := ch.calls[0]; len(first.Items) != 1 || protocol.ItemType(first.Items[0]) != protocol.ItemRehello {
		t.Fatalf("first call %+v", first)
	}
	s, err := b.sessions.FindBySessionID(context.Background(), *tg.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Conn != 1 || s.ConnKind != protocol.KindBindCall || s.Crypto.Suite != protocol.CryptoAES256CBCHMACSHA256 || s.Staged ||
		s.Hello.Host != "server01" || s.Hello.PID != 0 {
		t.Fatalf("session %+v", s)
	}
}

// keysSeen is a page channel that keeps the keys its calls carried.
type keysSeen struct {
	*fakePage
	keys []*protocol.LinkKeys
}

func (k *keysSeen) Call(ctx context.Context, module string, c channellink.Call) (protocol.LinkFrame, error) {
	k.keys = append(k.keys, c.Keys)
	return k.fakePage.Call(ctx, module, c)
}

// A channel that relays to a minion of its own (a page's) never sees a key:
// its kind is not agentless.
func TestCallKeysOnlyToAgentless(t *testing.T) {
	b := newBindRig(t, true)
	seen := &keysSeen{fakePage: b.page}
	b.svc.WithTargets(b.targets, seen)
	b.svc.WithChannelSchemas(fakeSchemas{"dumbo": {SchemaVersion: protocol.ConnectionSchemaV2, Version: 1,
		Kinds: map[string]protocol.ConnectionKind{protocol.KindBindCall: {}}}})
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetConnected || len(seen.keys) == 0 {
		t.Fatalf("target %+v after %d calls", tg, len(seen.keys))
	}
	for i, k := range seen.keys {
		if k != nil {
			t.Fatalf("call %d carried keys", i+1)
		}
	}
}

// Core never speaks first: an open says where the minion speaks, and gives
// that id's keys. Under its build's id while its target has no session (its
// hello opens one), under the session's after; a hello that names no
// connection is the target's.
func TestOpenSaysWhereToSpeak(t *testing.T) {
	shortSteady(t, 0)
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	ch.settle()
	if tg.State != models.TargetConnected {
		t.Fatalf("target %+v", tg)
	}
	if len(ch.unders) != 1 || ch.unders[0] != b.build.BuildID || ch.keys[0] == nil ||
		string(ch.keys[0].Material.EncKey) != string(b.keys.EncKey) {
		t.Fatalf("the open said %v, keys %+v", ch.unders, ch.keys)
	}
	if n := len(ch.frames()); n != 0 {
		t.Fatalf("core spoke first (%d frames)", n)
	}
	if resp := openCore(t, b.keys, b.build.BuildID, ch.replies[0]); len(resp.Items) != 1 || protocol.ItemType(resp.Items[0]) != protocol.ItemHelloResponse {
		t.Fatalf("the hello's reply: %+v", resp)
	}
	tg = b.get(t)
	if tg.SessionID == nil {
		t.Fatal("the target is not linked to the session")
	}
	s, _ := b.sessions.FindBySessionID(context.Background(), *tg.SessionID)
	if s.Conn != 1 || s.ConnKind != protocol.KindBindPersistent {
		t.Fatalf("session %+v", s)
	}

	// Closed, and opened again: under the session's id, with its keys; its
	// hello there goes on with the session.
	ch.connAt(0).closeBecause("")
	waitFor(t, "not connected", func() bool { return b.get(t).State == models.TargetNotConnected })
	if tg, err := b.svc.ConnectTarget(context.Background(), b.target); err != nil || tg.State != models.TargetConnected {
		t.Fatalf("connect again: %+v %v", tg, err)
	}
	ch.settle()
	if len(ch.unders) != 2 || ch.unders[1] != *tg.SessionID || ch.keys[1] == nil {
		t.Fatalf("the second open said %v, keys %+v", ch.unders, ch.keys)
	}
	if got := b.get(t); got.SessionID == nil || *got.SessionID != *tg.SessionID {
		t.Fatalf("the target's session %v, not %s", got.SessionID, *tg.SessionID)
	}
}

// A target that refuses the channel (its login, its host key) is refused,
// not retried, whether a call or an open met it.
func TestAgentlessRefusals(t *testing.T) {
	for _, c := range []struct {
		kind string
		fail protocol.LinkFailure
	}{
		{protocol.KindBindCall, protocol.LinkFailure{Kind: protocol.LinkFailLogin, Msg: "the server refused the login"}},
		{protocol.KindBindPersistent, protocol.LinkFailure{Kind: protocol.LinkFailHostKey, Msg: "the server's host key is SHA256:x"}},
	} {
		b, ch := newAgentlessRig(t, c.kind)
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

// Under the build id a minion's first message is its hello alone
// (protocol.ValidateBuildMessage): a hello with a get_tasks is refused, and
// opens no session.
func TestAgentlessHelloAlone(t *testing.T) {
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	max := uint64(4)
	ch.first = []any{agentlessHello("server01"), protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max}}
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	ch.settle()
	if m := openCore(t, b.keys, b.build.BuildID, ch.replies[0]); len(m.Items) != 1 || protocol.ItemType(m.Items[0]) != protocol.ItemErr {
		t.Fatalf("reply %+v", m)
	}
	if tg := b.get(t); tg.SessionID != nil {
		t.Fatalf("a session opened: %+v", tg)
	}
}
