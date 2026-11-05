package minionsync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// sock is a reverse-persistent connection's socket: what core sends on it
// kept, its close recorded.
type sock struct {
	mu     sync.Mutex
	frames []protocol.ConnFrame
	closed bool
}

func newSock(id string) (*sock, *Conn) {
	s := &sock{}
	c := NewConn(id, protocol.KindReversePersistent, "http-1", "10.0.0.9",
		func(_ context.Context, f protocol.ConnFrame) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed {
				return errors.New("closed")
			}
			s.frames = append(s.frames, f)
			return nil
		}, func() { s.mu.Lock(); s.closed = true; s.mu.Unlock() })
	return s, c
}

func (s *sock) taken() ([]protocol.ConnFrame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.frames
	s.frames = nil
	return f, s.closed
}

// say sends items over c under id, sealed with the rig's build's keys (a
// session a hello under the build id opened keeps them), through the
// channel module ctx says.
func (r *rig) say(t *testing.T, ctx context.Context, c *Conn, id uuid.UUID, items ...any) []byte {
	t.Helper()
	reply, err := r.svc.Exchange(ctx, c, id.String(), "10.0.0.9", sealedMinionMessage(t, r.keys, id, nil, items...))
	if err != nil {
		t.Fatalf("a message under %s: %v", id, err)
	}
	return reply
}

// helloOver opens a session with a hello over c, and returns it.
func (r *rig) helloOver(t *testing.T, ctx context.Context, c *Conn, h protocol.HelloItem) uuid.UUID {
	t.Helper()
	return sessionID(t, r.open(t, r.say(t, ctx, c, r.build.BuildID, h)))
}

func endpoint(kind, module string) protocol.BuildConnection {
	return protocol.BuildConnection{Endpoint: protocol.ChannelEndpointExport{Format: protocol.ChannelEndpointExportV2,
		Name: module + "-" + kind, Kind: kind, Channel: protocol.ExportChannel{ModuleName: module, SchemaVersion: 1}}}
}

// A hello that names no connection came through the one that carried it,
// when only one of the build's fits: the build's only connection, or its
// only one of the socket's kind through the socket's channel. Its size
// limit is that connection's.
func TestHelloTakesItsCarrier(t *testing.T) {
	r := newRig(t)
	small := endpoint(protocol.KindReversePersistent, "http")
	small.MaxMessage = 4096
	r.build.Connections.Items = []protocol.BuildConnection{endpoint(protocol.KindReverseCall, "http"), small,
		endpoint(protocol.KindReversePersistent, "dns")}
	ctx := ViaChannel(context.Background(), "http")
	_, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, hello("ws01"))
	s := r.sessionRow(t, sid)
	if s.Conn != 2 || s.ConnKind != protocol.KindReversePersistent || s.MessageLimit() != 4096 {
		t.Fatalf("session conn %d %q, limit %d", s.Conn, s.ConnKind, s.MessageLimit())
	}
	// Through the sync call: the build's reverse-call connection of that
	// channel.
	reply, err := r.svc.Handle(ctx, r.build.BuildID.String(), "", sealedMinionMessage(t, r.keys, r.build.BuildID, nil, hello("web01")))
	if err != nil {
		t.Fatal(err)
	}
	if s := r.sessionRow(t, sessionID(t, r.open(t, reply))); s.Conn != 1 || s.ConnKind != protocol.KindReverseCall {
		t.Fatalf("a sync hello's conn %d %q", s.Conn, s.ConnKind)
	}
	// More than one fits (no channel said): the minion has to say.
	if p := carrierPlace(r.build, protocol.KindReversePersistent, ""); p != 0 {
		t.Fatalf("an unclear carrier: %d", p)
	}
	one := &models.Build{Connections: models.BuildConnections{Items: []protocol.BuildConnection{small}}}
	if p := carrierPlace(one, protocol.KindReverseCall, "other"); p != 1 {
		t.Fatalf("a build's only connection: %d", p)
	}
}

// A connection is its first speaker's own: that minion is connected while
// it is open, checked in by it, and seen a last time when it closes. A
// session it relays (a pivot's) is reached over it, but is not connected:
// its own messages say when it was there, and go late and lost by its
// schedule.
func TestRelayedSessionsAreReachedNotKeptAlive(t *testing.T) {
	r := newRig(t)
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	ctx := context.Background()
	sk, c := newSock("pivot")
	h := hello("pivot")
	h.Sleep = &protocol.Sleep{Interval: 60_000}
	pivot := r.helloOver(t, ctx, c, h)
	h = hello("relayed")
	h.Sleep = &protocol.Sleep{Interval: 60_000}
	relayed := r.helloOver(t, ctx, c, h)
	if !r.svc.Connected(pivot) || r.svc.Connected(relayed) || !r.svc.reachable(relayed) {
		t.Fatalf("connected: pivot %v, relayed %v (reachable %v)", r.svc.Connected(pivot), r.svc.Connected(relayed), r.svc.reachable(relayed))
	}
	if !r.sessionRow(t, pivot).Connected || r.sessionRow(t, relayed).Connected {
		t.Fatal("the recorded flags")
	}
	// The relayed minion's last word was an hour ago: KeepAlive leaves it
	// so, and checks the pivot in.
	hourAgo := time.Now().Add(-time.Hour)
	r.sessionRow(t, relayed).LastSeenAt = &hourAgo
	r.sessionRow(t, pivot).LastSeenAt = &hourAgo
	r.svc.KeepAlive(ctx, c)
	if got := r.sessionRow(t, relayed).LastSeenAt; !got.Equal(hourAgo) {
		t.Fatalf("the relayed session checked in by its relay's socket: %s", got)
	}
	if got := r.sessionRow(t, pivot).LastSeenAt; time.Since(*got) > time.Minute {
		t.Fatal("the pivot not checked in by its own socket")
	}
	// Its tasks still go over the pivot's socket.
	max := uint64(2)
	r.say(t, ctx, c, relayed, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max})
	sk.taken()
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: relayed, OperationID: r.build.OperationID,
		Command: "shell", Op: 11, State: models.MinionTaskQueued})
	r.svc.Notify(ctx, relayed)
	if frames, _ := sk.taken(); len(frames) != 1 || uuid.UUID(frames[0].ID) != relayed {
		t.Fatalf("a push to the relayed session: %d frames", len(frames))
	}
	// News for it pushes too: it is reached, though not connected.
	tasks.rows[0].State = models.MinionTaskDone
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: relayed, OperationID: r.build.OperationID,
		Command: "shell", Op: 11, State: models.MinionTaskQueued})
	r.svc.Nudge(relayed, false)
	waitFor(t, "a pushed task", func() bool { sk.mu.Lock(); defer sk.mu.Unlock(); return len(sk.frames) == 1 })
	sk.taken()
	// The socket closes: the pivot is seen then, the relayed session keeps
	// its own last word.
	before := *r.sessionRow(t, relayed).LastSeenAt
	r.svc.Closed(ctx, c)
	if got := r.sessionRow(t, relayed).LastSeenAt; !got.Equal(before) {
		t.Fatal("the relayed session seen at its relay's close")
	}
	if r.svc.Connected(pivot) || r.sessionRow(t, pivot).Connected {
		t.Fatal("the pivot still connected")
	}
}

// A socket's own session closed: its minion is told, as in a reply, the
// socket is no longer that session's, and the next session that speaks
// over it is its own.
func TestASocketsOwnerStartsOver(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, c := newSock("ws")
	first := r.helloOver(t, ctx, c, hello("ws01"))
	now := time.Now()
	r.sessionRow(t, first).ClosedAt = &now
	reply := r.say(t, ctx, c, first, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	var e protocol.ErrItem
	if m := r.openUnder(t, first, r.keys, reply); len(m.Items) != 1 || protocol.UnmarshalCBOR(m.Items[0], &e) != nil || e.Code != protocol.ErrItemSessionClosed {
		t.Fatalf("the closed session's reply %+v", m)
	}
	if c.ownerOf() != uuid.Nil || r.svc.reachable(first) {
		t.Fatal("still the closed session's")
	}
	second := r.helloOver(t, ctx, c, hello("ws01"))
	if c.ownerOf() != second || !r.svc.Connected(second) {
		t.Fatalf("the new session is not the socket's: %s", c.ownerOf())
	}
}

// shortUnused has sockets no open session uses close after d.
func shortUnused(t *testing.T, d time.Duration) {
	was := unusedFor
	unusedFor = d
	t.Cleanup(func() { unusedFor = was })
}

// A session closed while its minion is connected (its operation deleted):
// its minion is told at the next keepalive, over its socket, as a reply
// would; a socket no open session uses closes after unusedFor.
func TestClosedSessionToldOverItsSocket(t *testing.T) {
	shortUnused(t, 50*time.Millisecond)
	r := newRig(t)
	ctx := context.Background()
	sk, c := newSock("ws")
	sid := r.helloOver(t, ctx, c, hello("ws01"))
	now := time.Now()
	r.sessionRow(t, sid).ClosedAt = &now
	r.svc.KeepAlive(ctx, c)
	frames, closed := sk.taken()
	if len(frames) != 1 || uuid.UUID(frames[0].ID) != sid {
		t.Fatalf("%d frames", len(frames))
	}
	var e protocol.ErrItem
	if m := r.openUnder(t, sid, r.keys, frames[0].Data); m.ID == nil || len(m.Items) != 1 ||
		protocol.UnmarshalCBOR(m.Items[0], &e) != nil || e.Code != protocol.ErrItemSessionClosed {
		t.Fatalf("the push %+v", m)
	}
	if r.svc.reachable(sid) || r.svc.Connected(sid) {
		t.Fatal("still served")
	}
	if closed {
		t.Fatal("closed at once: its minion may start over on it")
	}
	time.Sleep(60 * time.Millisecond)
	r.svc.KeepAlive(ctx, c)
	if _, closed := sk.taken(); !closed {
		t.Fatal("a socket no open session uses stays open")
	}
}

// A socket nothing speaks over closes after unusedFor; one its minion
// speaks over stays.
func TestUnusedSocketCloses(t *testing.T) {
	shortUnused(t, 50*time.Millisecond)
	r := newRig(t)
	ctx := context.Background()
	quiet, qc := newSock("quiet")
	r.svc.KeepAlive(ctx, qc)
	if _, closed := quiet.taken(); closed {
		t.Fatal("closed before unusedFor")
	}
	time.Sleep(60 * time.Millisecond)
	r.svc.KeepAlive(ctx, qc)
	if _, closed := quiet.taken(); !closed {
		t.Fatal("a socket nothing spoke over stays open")
	}
	used, uc := newSock("used")
	r.helloOver(t, ctx, uc, hello("ws01"))
	time.Sleep(60 * time.Millisecond)
	r.svc.KeepAlive(ctx, uc)
	if _, closed := used.taken(); closed {
		t.Fatal("a socket its minion uses closed")
	}
}

// The minion's answer to a push comes over another of its session's
// sockets (it connected again): taken, once.
func TestAnswerOverAnotherSocket(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	sa, a := newSock("a")
	_, b := newSock("b")
	sid := r.helloOver(t, ctx, a, hello("ws01"))
	from, err := r.svc.sender(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := errMessage(protocol.ErrItemBadMessage, "a push to answer")
	if err := r.svc.push(ctx, a, from, items); err != nil {
		t.Fatal(err)
	}
	frames, _ := sa.taken()
	m := r.openUnder(t, sid, r.keys, frames[0].Data)
	answer := func() error {
		_, err := r.svc.Exchange(ctx, b, sid.String(), "10.0.0.9", sealedMinionMessage(t, r.keys, sid, m.ID, protocol.GetTasksItem{T: protocol.ItemGetTasks}))
		return err
	}
	if err := answer(); err != nil {
		t.Fatalf("an answer over another socket: %v", err)
	}
	if err := answer(); !errors.Is(err, ErrUnopened) {
		t.Fatalf("the answer again: %v", err)
	}
	// Past pendingTTL nothing waits.
	w := newAnswers()
	t0 := time.Now()
	w.add(sid, m.ID, t0)
	if w.take(sid, m.ID, t0.Add(pendingTTL+time.Second)) {
		t.Fatal("an answer past pendingTTL")
	}
}

// A ninth connection of a session no longer reaches it over the oldest:
// that one closes only when no one else uses it (a pivot's own socket
// relaying others stays).
func TestNinthConnectionKeepsASharedSocket(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	shared, c0 := newSock("c0")
	sid := r.helloOver(t, ctx, c0, hello("pivot"))
	other := r.helloOver(t, ctx, c0, hello("relayed"))
	for i := range maxSessionConns {
		_, c := newSock("c" + string(rune('1'+i)))
		r.say(t, ctx, c, sid, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	}
	if _, closed := shared.taken(); closed {
		t.Fatal("a socket another session uses closed")
	}
	for _, c := range r.svc.conns.of(sid) {
		if c == c0 {
			t.Fatal("the oldest still reaches the session")
		}
	}
	if !r.svc.reachable(other) || c0.ownerOf() == sid {
		t.Fatal("the relayed session lost its socket, or the old owner kept it")
	}
}

// slowFalse is a session store whose writes of connected false are slow:
// a socket's close racing the next socket's first message.
type slowFalse struct {
	*fakeSessions
}

func (s slowFalse) SetConnected(ctx context.Context, id uuid.UUID, connected bool) error {
	if !connected {
		time.Sleep(50 * time.Millisecond)
	}
	return s.fakeSessions.SetConnected(ctx, id, connected)
}

// The recorded flag says what core holds as it writes: a close racing the
// next connection's first message leaves it connected.
func TestConnectedFlagRace(t *testing.T) {
	r := newRig(t)
	r.svc.sessions = slowFalse{r.sessions}
	ctx := context.Background()
	_, old := newSock("old")
	sid := r.helloOver(t, ctx, old, hello("ws01"))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); r.svc.Closed(ctx, old) }()
	time.Sleep(10 * time.Millisecond)
	_, fresh := newSock("new")
	r.say(t, ctx, fresh, sid, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	wg.Wait()
	if !r.svc.Connected(sid) || !r.sessionRow(t, sid).Connected {
		t.Fatalf("connected %v, recorded %v", r.svc.Connected(sid), r.sessionRow(t, sid).Connected)
	}
}
