package minionsync

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeListener is a channel and the listening minion it dials: an open
// that reaches it opens the connection's socket at once (as a channel does,
// naming the open), and the minion speaks first over it (speak), as BOB's
// does.
type fakeListener struct {
	t   *testing.T
	svc *Service

	mu sync.Mutex
	// speak is the minion's first word over a new connection; nil: it
	// says nothing.
	speak func(c *Conn)
	// before runs at each open, once its socket is up, before the answer.
	before    func()
	fail      *protocol.LinkFailure
	opens     []map[string]any
	deadlines []time.Duration
	conns     []*Conn
	closed    []*Conn
	// bg are the minion's first words and the sockets' closes, under way.
	bg sync.WaitGroup
}

func (f *fakeListener) Call(context.Context, string, channellink.Call) (protocol.LinkFrame, error) {
	return protocol.LinkFrame{}, nil
}

func (f *fakeListener) Open(ctx context.Context, module string, values map[string]any, _ map[string]protocol.Credential, _ [16]byte, keys *protocol.LinkKeys, _ *protocol.LinkActing, deadline time.Duration, sent func(string, uint64)) (protocol.LinkFrame, error) {
	if keys != nil {
		f.t.Error("keys to a channel that does not act as the minion")
	}
	f.mu.Lock()
	f.opens = append(f.opens, values)
	f.deadlines = append(f.deadlines, deadline)
	request := uint64(70 + len(f.opens))
	fail, speak, before := f.fail, f.speak, f.before
	f.mu.Unlock()
	if fail != nil {
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: 1, Failure: fail}, nil
	}
	sent("tcp-1", request)
	// The channel's socket, naming the open.
	if _, _, ok := f.svc.OpenedFor("tcp-1", request); !ok {
		f.t.Error("the socket's open is not waited for")
		return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: request, Failure: &protocol.LinkFailure{Kind: protocol.LinkFailOther}}, nil
	}
	var c *Conn
	var once sync.Once
	c = NewConn(uuid.NewString(), protocol.KindBindPersistent, "tcp-1", "10.0.0.5",
		func(context.Context, protocol.ConnFrame) error { return nil },
		func() {
			// As the socket's handler: Closed once, when its socket closes.
			once.Do(func() {
				f.mu.Lock()
				f.closed = append(f.closed, c)
				f.mu.Unlock()
				f.bg.Add(1)
				go func() {
					defer f.bg.Done()
					f.svc.Closed(context.Background(), c)
				}()
			})
		})
	if !f.svc.ForTarget(c, "tcp-1", request) {
		f.t.Error("the socket is not the open's")
	}
	f.mu.Lock()
	f.conns = append(f.conns, c)
	f.mu.Unlock()
	if before != nil {
		before()
	}
	if speak != nil {
		f.bg.Add(1)
		go func() {
			defer f.bg.Done()
			speak(c)
		}()
	}
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: request}, nil
}

// settle waits until the minion's first words were taken and the closed
// sockets' handlers are done, as a socket's handler would have them.
func (f *fakeListener) settle() { f.bg.Wait() }

func (f *fakeListener) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.opens)
}

func (f *fakeListener) connAt(i int) *Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns[i]
}

func (f *fakeListener) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.closed)
}

func (f *fakeListener) setSpeak(speak func(c *Conn)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.speak = speak
}

// newPersistentRig is a build with one bind-persistent connection through
// "tcp", a target of policy for it, and the channel and listener, whose
// minion speaks as BOB's does (speaks).
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
	l.speak = b.speaks(t)
	b.svc.WithTargets(b.targets, l)
	return b, l
}

// speaks is a minion's first word over a connection, as BOB's: a check-in
// under its session when its target has one open, else a hello under its
// build's id (which opens one).
func (b *bindRig) speaks(t *testing.T) func(c *Conn) {
	return func(c *Conn) {
		ctx := context.Background()
		id, items := b.build.BuildID, []any{hello("listener01")}
		if tg, err := b.targets.FindByTargetID(ctx, b.target); err == nil && tg.SessionID != nil {
			if s, err := b.sessions.FindBySessionID(ctx, *tg.SessionID); err == nil && !s.Closed(time.Now()) {
				max := uint64(4)
				id, items = s.SessionID, []any{protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max}}
			}
		}
		b.speak(t, c, id, items...)
	}
}

// speak sends items over c under id, sealed with the build's keys, as the
// minion does.
func (b *bindRig) speak(t *testing.T, c *Conn, id uuid.UUID, items ...any) []byte {
	reply, err := b.svc.Exchange(context.Background(), c, id.String(), "10.0.0.5", b.sealed(t, id, items...))
	if err != nil {
		t.Errorf("the minion's message under %s: %v", id, err)
	}
	return reply
}

// shortSteady has connections count as steady after d: one that drops
// later is not a failed open.
func shortSteady(t *testing.T, d time.Duration) {
	was := steadyFor
	steadyFor = d
	t.Cleanup(func() { steadyFor = was })
}

// An on-demand target: added (Check now), core opens its connection with
// the endpoint's values and the target's; the target is connected once its
// minion spoke, and linked to the session its hello opened; closed, it is
// not connected, and opened again for the work waiting, the minion going
// on under its session.
func TestPersistentOpenLinksTheSession(t *testing.T) {
	shortSteady(t, 0)
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	l.settle()
	if tg.State != models.TargetConnected || l.openCount() != 1 || !b.svc.TargetConnected(b.target) {
		t.Fatalf("after the open: %+v, %d opens", tg, l.openCount())
	}
	if v := l.opens[0]; v["listen_port"] != 4444 || v["host"] != "10.0.0.5" {
		t.Fatalf("values %v", v)
	}
	if l.deadlines[0] != openDefault {
		t.Fatalf("the open's deadline %s", l.deadlines[0])
	}
	got := b.get(t)
	if got.SessionID == nil {
		t.Fatal("the target is not linked to the minion's session")
	}
	sid := *got.SessionID
	// Connected already: Check now opens no second one.
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil || l.openCount() != 1 {
		t.Fatalf("a second open: %d (%v)", l.openCount(), err)
	}
	// The minion hangs up.
	l.connAt(0).closeBecause("")
	waitFor(t, "not connected", func() bool { return b.get(t).State == models.TargetNotConnected })
	l.settle()
	if got := b.get(t); got.NextCallAt != nil || got.StateDetail == "" || got.Failures != 0 {
		t.Fatalf("closed, on-demand: %+v", got)
	}
	// A task waits: opened again for it, the minion checking in under its
	// session, which stays the target's.
	b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskQueued})
	if tg, err := b.svc.CheckTarget(context.Background(), b.target); err != nil || l.openCount() != 2 || tg.State != models.TargetConnected {
		t.Fatalf("open again: %d (%v)", l.openCount(), err)
	}
	l.settle()
	if got := b.get(t); got.SessionID == nil || *got.SessionID != sid {
		t.Fatalf("the target's session %v, not %s", got.SessionID, sid)
	}
	// Dropped while the task waits: opened again for it.
	l.connAt(1).closeBecause("")
	waitFor(t, "a reopen for the waiting task", func() bool {
		got := b.get(t)
		return got.State == models.TargetNotConnected && got.NextCallAt != nil
	})
}

// A manual target opens only on Connect, closes on Disconnect, and is not
// opened again by core; one of policy always is.
func TestPersistentPolicies(t *testing.T) {
	shortSteady(t, 0)
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
	l.settle()
	tg, err = b.svc.DisconnectTarget(context.Background(), b.target)
	if err != nil || tg.State != models.TargetNotConnected || tg.StateDetail != "an operator disconnected it" || tg.NextCallAt != nil {
		t.Fatalf("disconnect: %+v %v", tg, err)
	}

	a, la := newPersistentRig(t, models.PolicyAlways)
	if _, err := a.svc.CheckTarget(context.Background(), a.target); err != nil || la.openCount() != 1 {
		t.Fatal("always: not opened")
	}
	la.settle()
	la.connAt(0).closeBecause("")
	waitFor(t, "a reopen scheduled", func() bool { got := a.get(t); return got.State == models.TargetNotConnected && got.NextCallAt != nil })
}

// On demand, news opens the connection only when there is work: a task
// that ended, or a connect or disconnect kept, is news with nothing to send;
// a task queued, or a cancel to send, is work. Disconnect answers with the
// state the close left.
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
	l.settle()
	if l.openCount() != 1 || b.get(t).State != models.TargetConnected {
		t.Fatalf("a task queued: %d opens, %s", l.openCount(), b.get(t).State)
	}
	b.tasks.rows = nil
	tg, err := b.svc.DisconnectTarget(context.Background(), b.target)
	if err != nil || tg.State != models.TargetNotConnected {
		t.Fatalf("disconnect: %+v %v", tg, err)
	}
	l.settle()
	// A cancel of a task the minion runs is work too: it opens for it.
	now := time.Now()
	b.tasks.rows = []models.MinionTask{{TaskID: uuid.New(), SessionID: *b.get(t).SessionID, State: models.MinionTaskRunning,
		AckedAt: &now, CancelRequestedAt: &now}}
	b.svc.callTarget(context.Background(), b.target, bindNews)
	l.settle()
	if l.openCount() != 2 {
		t.Fatalf("a cancel to send: %d opens", l.openCount())
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
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil || l.openCount() != 2 {
		t.Fatal("not opened")
	}
	l.settle()
	// Idle past on-demand's ten minutes.
	l.connAt(0).touch(time.Now().Add(-11 * time.Minute))
	b.svc.closeIdle(context.Background())
	waitFor(t, "closed idle", func() bool { return b.get(t).State == models.TargetNotConnected })
	if got := b.get(t); got.StateDetail != "idle for 10m0s" || got.Failures != 0 || got.NextCallAt != nil {
		t.Fatalf("closed idle: %+v", got)
	}
}

// As core starts, targets left connected are not (their connections went
// with the process): one of policy always is opened again at once, an
// on-demand one with nothing waiting is not, one whose minion runs tasks
// is (it may hold their results).
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
	sid := uuid.New()
	row.SessionID = &sid
	d.targets.rows[d.target] = row
	d.svc.resetPersistent(context.Background())
	if got := d.get(t); got.State != models.TargetNotConnected || got.NextCallAt != nil {
		t.Fatalf("on demand: %+v", got)
	}
	row = d.targets.rows[d.target]
	row.State = models.TargetConnected
	d.targets.rows[d.target] = row
	now := time.Now()
	d.tasks.rows = []models.MinionTask{{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskRunning, AckedAt: &now}}
	d.svc.resetPersistent(context.Background())
	if got := d.get(t); got.NextCallAt == nil {
		t.Fatalf("on demand, a task running: %+v", got)
	}
}

// A minion with other connections: core dials its listening one only when
// the minion is on it, is switching to it, or has gone quiet where it is.
func TestPersistentWhereTheMinionIs(t *testing.T) {
	shortSteady(t, 0)
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	sid := uuid.New()
	seen := time.Now()
	b.sessions.rows = append(b.sessions.rows, models.MinionSession{SessionID: sid, BuildID: b.build.BuildID, OperationID: b.build.OperationID,
		Conn: 2, ConnKind: protocol.KindReverseCall, Crypto: *b.build.Crypto, CreatedAt: seen, LastSeenAt: &seen})
	row := b.targets.rows[b.target]
	row.SessionID = &sid
	b.targets.rows[b.target] = row
	b.tasks.rows = append(b.tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskQueued})
	// On connection 2, checking in there: work waits, and still no dial.
	b.svc.callTarget(context.Background(), b.target, bindNews)
	if got := b.get(t); l.openCount() != 0 || got.State != models.TargetNotConnected || got.StateDetail == "" {
		t.Fatalf("a minion on another connection was dialed: %d opens, %+v", l.openCount(), got)
	}
	// Switching to this one: dialed, though only the switch is under way.
	b.tasks.rows = []models.MinionTask{{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskRunning,
		Action: protocol.ActionSwitchConnection, Args: models.JSONMap{"conn": int64(1)}}}
	b.svc.callTarget(context.Background(), b.target, bindNews)
	l.settle()
	if l.openCount() != 1 || b.get(t).State != models.TargetConnected {
		t.Fatalf("a switch to it: %d opens", l.openCount())
	}
	l.connAt(0).closeBecause("")
	waitFor(t, "closed", func() bool { return b.get(t).State == models.TargetNotConnected })
	l.settle()
	// Quiet where it is (late): dialed for the work waiting.
	b.tasks.rows = []models.MinionTask{{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskQueued}}
	late := time.Now().Add(-time.Minute)
	b.sessions.rows[len(b.sessions.rows)-1].LateAt = &late
	b.sessions.rows[len(b.sessions.rows)-1].Conn = 2
	b.svc.callTarget(context.Background(), b.target, bindNews)
	l.settle()
	if l.openCount() != 2 {
		t.Fatalf("a late minion: %d opens", l.openCount())
	}
}

// The target is connected once its minion spoke over the connection, not
// when the channel says it opened: until then it is connecting, and
// TargetConnected says no.
func TestPersistentConnectedOnceTheMinionSpeaks(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyManual)
	speaks, release := l.speak, make(chan struct{})
	waiting := make(chan *Conn, 1)
	l.setSpeak(func(c *Conn) {
		waiting <- c
		<-release
		speaks(c)
	})
	done := make(chan *models.MinionTarget, 1)
	go func() {
		tg, err := b.svc.ConnectTarget(context.Background(), b.target)
		if err != nil {
			t.Error(err)
		}
		done <- tg
	}()
	<-waiting
	if got := b.get(t); got.State != models.TargetConnecting || b.svc.TargetConnected(b.target) {
		t.Fatalf("before its minion spoke: %s, connected %v", got.State, b.svc.TargetConnected(b.target))
	}
	close(release)
	if tg := <-done; tg.State != models.TargetConnected {
		t.Fatalf("after: %+v", tg)
	}
	l.settle()
	if got := b.get(t); got.SessionID == nil || !b.svc.TargetConnected(b.target) {
		t.Fatalf("linked %v, connected %v", got.SessionID, b.svc.TargetConnected(b.target))
	}
}

// What takes the connection and says nothing within the open's time (the
// kind's max_open) is not the minion: the connection closes, and core does
// not retry. A connection that closes before its minion spoke is an open
// that failed: unreachable, retried.
func TestPersistentSilentOrGone(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyAlways)
	b.svc.WithChannelSchemas(fakeSchemas{"tcp": {SchemaVersion: protocol.ConnectionSchemaV2, Version: 1,
		Kinds: map[string]protocol.ConnectionKind{protocol.KindBindPersistent: {MaxOpen: "1s"}}}})
	l.setSpeak(nil)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetNotTheMinion || tg.StateDetail != "nothing spoke over the connection within 1s" || tg.NextCallAt != nil {
		t.Fatalf("a silent connection: %+v", tg)
	}
	if l.deadlines[0] != time.Second {
		t.Fatalf("the open's deadline %s, not the kind's", l.deadlines[0])
	}
	waitFor(t, "the connection closed", func() bool { return l.closedCount() == 1 && !b.svc.TargetConnected(b.target) })
	time.Sleep(50 * time.Millisecond)
	if got := b.get(t); got.State != models.TargetNotTheMinion {
		t.Fatalf("its close changed the state: %+v", got)
	}

	l.setSpeak(func(c *Conn) { c.closeBecause("") })
	tg, err = b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetUnreachable || tg.StateDetail != "the connection closed before the minion spoke" || tg.NextCallAt == nil {
		t.Fatalf("a connection closed before its minion spoke: %+v", tg)
	}
}

// otherBuild is a build of another operation, with its own keys.
func (b *bindRig) otherBuild(t *testing.T) (*models.Build, protocol.AES256CBCHMACSHA256Material) {
	t.Helper()
	keys := protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}
	_, _ = rand.Read(keys.EncKey)
	_, _ = rand.Read(keys.MACKey)
	material, _ := json.Marshal(keys)
	o := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(), ModuleName: "bob",
		Crypto: &protocol.BuildCrypto{Suite: protocol.CryptoAES256CBCHMACSHA256, Material: material}}
	b.builds[o.BuildID] = o
	return o, keys
}

// A minion of another build (another operation's, listening where the
// target points) is not the target's: its message is not taken (no
// session opens), the connection closes, and the target says so, not
// retried.
func TestPersistentAnotherBuildsMinion(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyAlways)
	other, keys := b.otherBuild(t)
	l.setSpeak(func(c *Conn) {
		frame := sealedMinionMessage(t, keys, other.BuildID, nil, hello("intruder"))
		if _, err := b.svc.Exchange(context.Background(), c, other.BuildID.String(), "10.0.0.5", frame); err != ErrUnopened {
			t.Errorf("another build's hello: %v", err)
		}
	})
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	l.settle()
	if tg.State != models.TargetNotTheMinion || tg.StateDetail != "a minion of another build spoke over the connection" ||
		tg.NextCallAt != nil || tg.SessionID != nil {
		t.Fatalf("target %+v", tg)
	}
	if len(b.sessions.rows) != 0 {
		t.Fatalf("its hello opened a session: %+v", b.sessions.rows)
	}
	waitFor(t, "the connection closed", func() bool { return l.closedCount() == 1 })
}

// The minion over a target's connection is the target's build's, the
// session the target is linked to (a later build may have claimed it), or
// a later build of the operation that claims that session; any other is
// not.
func TestConnOfTarget(t *testing.T) {
	build, op, linked := uuid.New(), uuid.New(), uuid.New()
	c := &Conn{target: uuid.New(), build: build, operation: op, session: linked}
	persists := func(sid uuid.UUID, operation uuid.UUID) *models.Build {
		return &models.Build{BuildID: uuid.New(), OperationID: operation, PersistedSessionID: &sid}
	}
	for name, c2 := range map[string]struct {
		from *sender
		ok   bool
	}{
		"a session of the build":                   {&sender{session: &models.MinionSession{SessionID: uuid.New(), BuildID: build}}, true},
		"the linked session, claimed by another":   {&sender{session: &models.MinionSession{SessionID: linked, BuildID: uuid.New()}}, true},
		"the build's id":                           {&sender{build: &models.Build{BuildID: build}}, true},
		"a staging of the build":                   {&sender{staging: &models.MinionStaging{BuildID: build}, build: &models.Build{BuildID: build}}, true},
		"a later build that claims the session":    {&sender{build: persists(linked, op)}, true},
		"another operation's build that claims it": {&sender{build: persists(linked, uuid.New())}, false},
		"a later build that claims another":        {&sender{build: persists(uuid.New(), op)}, false},
		"another build":                            {&sender{build: &models.Build{BuildID: uuid.New(), OperationID: op}}, false},
		"another build's session":                  {&sender{session: &models.MinionSession{SessionID: uuid.New(), BuildID: uuid.New()}}, false},
	} {
		if got := c.ofTarget(c2.from); got != c2.ok {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// A minion whose session closed stages again over the same connection: the
// target follows it to its new session.
func TestPersistentFollowsTheMinionsSession(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyAlways)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	l.settle()
	first := *b.get(t).SessionID
	reply := b.speak(t, l.connAt(0), b.build.BuildID, hello("listener01"))
	if m := openCore(t, b.keys, b.build.BuildID, reply); len(m.Items) != 1 || protocol.ItemType(m.Items[0]) != protocol.ItemHelloResponse {
		t.Fatalf("the new hello's reply %+v", m)
	}
	waitFor(t, "the new session", func() bool { got := b.get(t); return got.SessionID != nil && *got.SessionID != first })
	if got := b.get(t); got.State != models.TargetConnected {
		t.Fatalf("target %+v", got)
	}
}

// A connection that drops soon after its minion spoke is an open that
// failed: unreachable, its failures counted on across reopens, so they back
// off (1 minute, 5, ...). One that stays up is not: a drop then reopens at
// once, and the failures start over.
func TestPersistentQuickDropsBackOff(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyAlways)
	for i, wait := range []time.Duration{time.Minute, 5 * time.Minute} {
		if tg, err := b.svc.ConnectTarget(context.Background(), b.target); err != nil || tg.State != models.TargetConnected || tg.Failures != 0 {
			t.Fatalf("open %d: %+v %v", i+1, tg, err)
		}
		l.settle()
		l.connAt(i).closeBecause("")
		waitFor(t, "the drop", func() bool { return b.get(t).State == models.TargetUnreachable })
		got := b.get(t)
		if got.Failures != i+1 || got.NextCallAt == nil || got.UnreachableSince == nil {
			t.Fatalf("drop %d: %+v", i+1, got)
		}
		if d := time.Until(*got.NextCallAt); d < wait-5*time.Second || d > wait {
			t.Fatalf("drop %d: reopened in %s, not %s", i+1, d, wait)
		}
	}
	shortSteady(t, 0)
	if _, err := b.svc.ConnectTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	l.settle()
	l.connAt(2).closeBecause("")
	waitFor(t, "a steady drop", func() bool { return b.get(t).State == models.TargetNotConnected })
	got := b.get(t)
	if got.Failures != 0 || got.NextCallAt == nil || time.Until(*got.NextCallAt) > alwaysReopen {
		t.Fatalf("a steady connection's drop: %+v", got)
	}
}

// pushedTask is core's task item for id, as a push carries it.
func pushedTask(t *testing.T, id uuid.UUID) cbor.RawMessage {
	t.Helper()
	raw, err := protocol.MarshalCBOR(protocol.TaskItem{T: protocol.ItemTask, Task: id[:], Cmd: 1})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A connection a task it carried runs on is not closed idle: the minion
// says its result over it when it is done, a quiet while from now. Dropped
// while one runs, an on-demand target opens again for it.
func TestPersistentRunningTasksKeepItOpen(t *testing.T) {
	shortSteady(t, 0)
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	l.settle()
	sid := *b.get(t).SessionID
	now := time.Now()
	task := uuid.New()
	b.tasks.rows = []models.MinionTask{{TaskID: task, SessionID: sid, State: models.MinionTaskRunning, AckedAt: &now}}
	c := l.connAt(0)
	c.carry([]cbor.RawMessage{pushedTask(t, task)})
	c.touch(time.Now().Add(-11 * time.Minute))
	b.svc.closeIdle(context.Background())
	time.Sleep(50 * time.Millisecond)
	if l.closedCount() != 0 {
		t.Fatal("closed idle while a task it carried runs")
	}
	// Dropped while it runs: opened again for it.
	c.closeBecause("")
	waitFor(t, "a reopen for the running task", func() bool {
		got := b.get(t)
		return got.State == models.TargetNotConnected && got.NextCallAt != nil
	})
	l.settle()
	// Its result came: idle closes it, and nothing reopens it.
	if _, err := b.svc.ConnectTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	l.settle()
	c = l.connAt(1)
	c.carry([]cbor.RawMessage{pushedTask(t, task)})
	b.tasks.rows[0].State = models.MinionTaskDone
	c.touch(time.Now().Add(-11 * time.Minute))
	b.svc.closeIdle(context.Background())
	waitFor(t, "closed idle", func() bool {
		got := b.get(t)
		return got.State == models.TargetNotConnected && got.StateDetail == "idle for 10m0s"
	})
	if got := b.get(t); got.NextCallAt != nil {
		t.Fatalf("reopened with nothing running: %+v", got)
	}
}

// A target removed while its open was under way: its connection, once its
// minion spoke (a stage, which links no session yet), closes, and is no
// one's.
func TestPersistentRemovedDuringTheOpen(t *testing.T) {
	b, l := newPersistentRig(t, models.PolicyAlways)
	l.before = func() { _, _ = b.targets.Delete(context.Background(), b.target) }
	stage := b.stage(t)
	l.setSpeak(func(c *Conn) { b.speak(t, c, b.build.BuildID, stage) })
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err == nil {
		t.Fatal("a removed target was found")
	}
	l.settle()
	waitFor(t, "its connection closed", func() bool { return l.closedCount() == 1 && len(b.svc.targetConns(b.target)) == 0 })
}

// An open's time is what its kind asks for, within core's cap.
func TestOpenWithin(t *testing.T) {
	for limit, want := range map[time.Duration]time.Duration{0: openDefault, 2 * time.Minute: 2 * time.Minute, time.Hour: openMax} {
		if got := openWithin(limit); got != want {
			t.Errorf("%s: %s, not %s", limit, got, want)
		}
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
