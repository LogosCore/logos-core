package miniontunnels

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// fakeListeners keeps reverse port forwards' listeners in memory.
type fakeListeners struct {
	mu   sync.Mutex
	recs map[uuid.UUID]models.MinionListener
}

func (f *fakeListeners) Insert(_ context.Context, l *models.MinionListener) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.recs[l.TaskID]; ok {
		return errors.New("duplicate task")
	}
	f.recs[l.TaskID] = *l
	return nil
}

func (f *fakeListeners) All(context.Context) ([]models.MinionListener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []models.MinionListener{}
	for _, l := range f.recs {
		out = append(out, l)
	}
	return out, nil
}

func (f *fakeListeners) FindByOperationIDWithCursor(ctx context.Context, _ uuid.UUID, _ string, _ *pagination.Cursor, _ int64, _ bool) ([]models.MinionListener, error) {
	return f.All(ctx)
}

func (f *fakeListeners) CountByOperationID(ctx context.Context, _ uuid.UUID, _ string) (int64, error) {
	all, _ := f.All(ctx)
	return int64(len(all)), nil
}

func (f *fakeListeners) FindByTaskID(_ context.Context, task uuid.UUID) (*models.MinionListener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.recs[task]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &l, nil
}

func (f *fakeListeners) SetBound(_ context.Context, task uuid.UUID, bound string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.recs[task]
	if !ok {
		return repository.ErrNotFound
	}
	l.Bound = bound
	f.recs[task] = l
	return nil
}

func (f *fakeListeners) SeenStream(_ context.Context, task uuid.UUID, n uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.recs[task]
	if !ok {
		return repository.ErrNotFound
	}
	l.Streams = max(l.Streams, n)
	f.recs[task] = l
	return nil
}

func (f *fakeListeners) SetBytes(_ context.Context, task uuid.UUID, up, down int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.recs[task]
	if !ok {
		return repository.ErrNotFound
	}
	l.Up, l.Down = up, down
	f.recs[task] = l
	return nil
}

func (f *fakeListeners) Release(_ context.Context, task uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs, task)
	return nil
}

func (f *fakeListeners) get(task uuid.UUID) (models.MinionListener, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.recs[task]
	return l, ok
}

// reverseRig is a service with a reverse port forward's task, whose streams
// core connects to dest.
type reverseRig struct {
	svc                      *Service
	listeners                *fakeListeners
	tasks                    *fakeTasks
	bus                      *recordingBus
	task, session, operation uuid.UUID
}

func newReverseService(t *testing.T, listeners *fakeListeners, tasks *fakeTasks, bus *recordingBus, cfg ...func(*Config)) *Service {
	t.Helper()
	c := DefaultConfig()
	c.ResendAfter, c.Tell = 200*time.Millisecond, 20*time.Millisecond
	for _, f := range cfg {
		f(&c)
	}
	svc := New(c, &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, tasks, bus, zap.NewNop()).WithListeners(listeners)
	svc.Start(context.Background())
	t.Cleanup(svc.Stop)
	return svc
}

// stream is the forward's stream n as its task's Streams tab lists it.
func (r *reverseRig) stream(n uint64) models.MinionTunnelStream {
	return streamOf(r.svc, r.task, n)
}

func newReverseRig(t *testing.T, dest string, maxStreams int) *reverseRig {
	return newReverseRigWith(t, dest, maxStreams, false)
}

// newReverseRigWith is newReverseRig, a UDP forward's with udp.
func newReverseRigWith(t *testing.T, dest string, maxStreams int, udp bool, cfg ...func(*Config)) *reverseRig {
	t.Helper()
	host, port := splitAddr(t, dest)
	r := &reverseRig{listeners: &fakeListeners{recs: map[uuid.UUID]models.MinionListener{}},
		tasks: &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}, bus: &recordingBus{}, task: uuid.New(), session: uuid.New(),
		operation: uuid.New()}
	r.svc = newReverseService(t, r.listeners, r.tasks, r.bus, cfg...)
	r.tasks.tasks[r.task] = models.MinionTask{TaskID: r.task, SessionID: r.session, State: models.MinionTaskRunning,
		Action: protocol.ActionReversePortForward}
	err := r.svc.OpenReverse(context.Background(), Opening{Task: r.task, Session: r.session, Operation: r.operation, By: uuid.New(),
		Purpose: models.OperationalPurposeReverse, Bind: "0.0.0.0", Listen: 8080, UDP: udp, Host: host, HostPort: port, MaxStreams: maxStreams,
		Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// take gives core one of the minion's items: what core answers at once.
func (r *reverseRig) take(t *testing.T, v any) []cbor.RawMessage {
	t.Helper()
	raw, err := protocol.MarshalCBOR(v)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.svc.Take(context.Background(), r.session, raw)
	if err != nil {
		t.Fatalf("core refused %T: %v", v, err)
	}
	return out
}

// fill is core's next message for the minion: its tunnel items.
func (r *reverseRig) fill() []cbor.RawMessage {
	var items []cbor.RawMessage
	r.svc.Fill(context.Background(), r.session, 1<<20, protocol.MaxMinionMessageItems, func(it cbor.RawMessage) bool {
		items = append(items, it)
		return true
	})
	return items
}

// open is the minion's tunnel for stream, a connection from 10.0.0.9.
func (r *reverseRig) open(stream uint64) protocol.TunnelItem {
	return protocol.TunnelItem{T: protocol.ItemTunnel, Task: r.task[:], Stream: stream, Host: "10.0.0.9", Port: 50000 + stream}
}

// replyFor waits for core's reply for stream, which says nothing of core's
// side.
func (r *reverseRig) replyFor(t *testing.T, stream uint64) protocol.TunnelReplyItem {
	t.Helper()
	var got *protocol.TunnelReplyItem
	eventually(t, "core's reply", func() bool {
		for _, it := range r.fill() {
			if protocol.ItemType(it) != protocol.ItemTunnelReply {
				continue
			}
			var rep protocol.TunnelReplyItem
			if err := protocol.UnmarshalCBOR(it, &rep); err != nil || rep.Validate() != nil {
				t.Fatalf("core's reply does not read: %v", err)
			}
			if rep.Stream == stream {
				got = &rep
				return true
			}
		}
		return false
	})
	if got.Bound != "" || got.Msg != "" || got.Room != nil {
		t.Errorf("core's reply says more than its result: %+v", *got)
	}
	return *got
}

// closes reads the closes among items.
func closes(t *testing.T, items []cbor.RawMessage) []protocol.TunnelCloseItem {
	t.Helper()
	var out []protocol.TunnelCloseItem
	for _, it := range items {
		if protocol.ItemType(it) != protocol.ItemTunnelClose {
			continue
		}
		var c protocol.TunnelCloseItem
		if err := protocol.UnmarshalCBOR(it, &c); err != nil || c.Validate() != nil {
			t.Fatalf("a close does not read: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// The minion listens and says where; a connection that came to it is a
// stream the minion opens, which core connects to the destination: core
// answers with its result alone, and the bytes go both ways. The stream's
// record says who connected at the minion, the destination, and core's own
// side, which the minion never hears.
func TestReverseCarriesAStream(t *testing.T) {
	dest := echo(t)
	r := newReverseRig(t, dest, 4)
	r.take(t, protocol.TunnelListenItem{T: protocol.ItemTunnelListen, Task: r.task[:], Bound: "0.0.0.0:8080"})
	if sum, ok := r.svc.Summary(r.task); !ok || sum.Bound != "0.0.0.0:8080" || sum.Port != 0 {
		t.Fatalf("summary %+v %v", sum, ok)
	}
	if l, _ := r.listeners.get(r.task); l.Bound != "0.0.0.0:8080" {
		t.Fatalf("listener %+v", l)
	}
	if out := r.take(t, r.open(1)); len(out) != 0 {
		t.Fatalf("answered at once: %d items", len(out))
	}
	if rep := r.replyFor(t, 1); rep.Result != protocol.TunnelOpen {
		t.Fatalf("reply %+v", rep)
	}
	r.take(t, protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: r.task[:], Stream: 1, Data: []byte("hello"), EOF: true})
	var got []byte
	eof := false
	eventually(t, "the destination's bytes back", func() bool {
		for _, it := range r.fill() {
			if protocol.ItemType(it) != protocol.ItemTunnelData {
				continue
			}
			var d protocol.TunnelDataItem
			if err := protocol.UnmarshalCBOR(it, &d); err != nil {
				t.Fatal(err)
			}
			if d.Off == uint64(len(got)) {
				got, eof = append(got, d.Data...), eof || d.EOF
			}
		}
		return eof
	})
	if string(got) != "hello" {
		t.Fatalf("back %q", got)
	}
	eventually(t, "the stream's record", func() bool {
		rec := r.stream(1)
		return rec.Result != nil && *rec.Result == protocol.TunnelOpen
	})
	rec := r.stream(1)
	host, port := splitAddr(t, dest)
	if rec.Client != "10.0.0.9:50001" || rec.Host != host || rec.Port != port || rec.Bound == "" {
		t.Errorf("record %+v", rec)
	}
	eventually(t, "the last stream kept", func() bool {
		l, _ := r.listeners.get(r.task)
		return l.Streams == 1
	})
}

// A destination that refuses is said as the result alone (core's record
// says why); a tunnel again for a stream that went is a close, never a
// connect again; past max_streams, not allowed.
func TestReverseRefusedAndLimits(t *testing.T) {
	closedPort := freePort(t)
	r := newReverseRig(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(closedPort)), 4)
	r.take(t, r.open(1))
	if rep := r.replyFor(t, 1); rep.Result != protocol.TunnelRefused {
		t.Fatalf("reply %+v", rep)
	}
	eventually(t, "the record's end", func() bool { return r.stream(1).End == models.TunnelEndNotOpened })
	if r.stream(1).Msg == "" {
		t.Error("core's record says not why")
	}
	if c := closes(t, r.take(t, r.open(1))); len(c) != 1 || c[0].Reason != protocol.TunnelCloseUnknown {
		t.Fatalf("the stream again: %+v", c)
	}

	dest := echo(t)
	one := newReverseRig(t, dest, 1)
	one.take(t, one.open(1))
	if rep := one.replyFor(t, 1); rep.Result != protocol.TunnelOpen {
		t.Fatalf("reply %+v", rep)
	}
	one.take(t, one.open(2))
	if rep := one.replyFor(t, 2); rep.Result != protocol.TunnelNotAllowed {
		t.Fatalf("past max_streams: %+v", rep)
	}
	// Stream 1's tunnel again (its reply lost): the reply again, one
	// connect.
	one.take(t, one.open(1))
	if rep := one.replyFor(t, 1); rep.Result != protocol.TunnelOpen {
		t.Fatalf("the reply again: %+v", rep)
	}
	if sum, _ := one.svc.Summary(one.task); sum.Open != 1 || sum.Streams != 2 {
		t.Fatalf("summary %+v", sum)
	}
}

// Only a reverse port forward's minion opens streams: a tunnel for any
// other task, or another session's, is closed task_ended; a tunnel_reply
// from the minion for one of its own streams is nothing; a tunnel_listen
// for a task that is not one changes nothing.
func TestReverseOnlyItsOwn(t *testing.T) {
	r := newReverseRig(t, echo(t), 4)
	other := uuid.New()
	for _, it := range []protocol.TunnelItem{
		{T: protocol.ItemTunnel, Task: other[:], Stream: 1, Host: "10.0.0.9", Port: 1},
	} {
		if c := closes(t, r.take(t, it)); len(c) != 1 || c[0].Reason != protocol.TunnelCloseTaskEnded {
			t.Fatalf("a tunnel for a task core has none of: %+v", c)
		}
	}
	raw, _ := protocol.MarshalCBOR(r.open(1))
	out, err := r.svc.Take(context.Background(), uuid.New(), raw)
	if err != nil || len(closes(t, out)) != 1 {
		t.Fatalf("another session's: %v %d", err, len(out))
	}
	r.take(t, r.open(1))
	r.replyFor(t, 1)
	if out := r.take(t, protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: r.task[:], Stream: 1, Result: protocol.TunnelFailed}); len(out) != 0 {
		t.Fatalf("a reply from the minion answered: %d", len(out))
	}
	if sum, _ := r.svc.Summary(r.task); sum.Open != 1 {
		t.Fatalf("the minion's reply changed its stream: %+v", sum)
	}
	r.take(t, protocol.TunnelListenItem{T: protocol.ItemTunnelListen, Task: other[:], Bound: "1.2.3.4:5"})
	if sum, _ := r.svc.Summary(r.task); sum.Bound != "" {
		t.Fatalf("another task's listen taken: %+v", sum)
	}
}

// The task ending forgets the listener and closes its streams; a restart
// takes up a running task's, and a tunnel for a stream at or below the
// last it kept is closed, a later one connected.
func TestReverseCloseAndRestart(t *testing.T) {
	r := newReverseRig(t, echo(t), 4)
	r.take(t, r.open(1))
	r.replyFor(t, 1)
	r.svc.Close(context.Background(), r.task)
	if _, ok := r.listeners.get(r.task); ok {
		t.Fatal("the listener kept")
	}
	if _, ok := r.svc.Summary(r.task); ok {
		t.Fatal("the tunnel kept")
	}
	if tot := r.tasks.tunnel(r.task); tot == nil || tot.Streams != 1 {
		t.Errorf("its totals: %+v", tot)
	}
	if rec := r.stream(1); rec.Stream != 0 {
		t.Errorf("its stream kept: %+v", rec)
	}
	closed := false
	r.bus.mu.Lock()
	for _, e := range r.bus.events {
		if p, ok := e.Payload.(eventbus.MinionListenerEventPayload); ok && p.Closed && p.TaskID == r.task.String() {
			closed = true
		}
	}
	r.bus.mu.Unlock()
	if !closed {
		t.Error("no closed event")
	}

	dest := echo(t)
	host, port := splitAddr(t, dest)
	listeners := &fakeListeners{recs: map[uuid.UUID]models.MinionListener{}}
	tasks := &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}
	running, ended, session := uuid.New(), uuid.New(), uuid.New()
	listeners.recs[running] = models.MinionListener{TaskID: running, SessionID: session, Host: host, HostPort: port, Streams: 5, Up: 3, Down: 4,
		Bound: "0.0.0.0:8080"}
	listeners.recs[ended] = models.MinionListener{TaskID: ended, SessionID: session, Host: host, HostPort: port, Streams: 2, Up: 20, Down: 200}
	tasks.tasks[running] = models.MinionTask{TaskID: running, SessionID: session, State: models.MinionTaskRunning}
	tasks.tasks[ended] = models.MinionTask{TaskID: ended, SessionID: session, State: models.MinionTaskDone}
	svc := newReverseService(t, listeners, tasks, &recordingBus{})
	if sum, ok := svc.Summary(running); !ok || sum.Bound != "0.0.0.0:8080" || sum.Streams != 5 || sum.Up != 3 || sum.Down != 4 {
		t.Fatalf("not taken up: %+v %v", sum, ok)
	}
	if _, ok := listeners.get(ended); ok {
		t.Fatal("an ended task's listener kept")
	}
	if tot := tasks.tunnel(ended); tot == nil || *tot != (models.MinionTaskTunnel{Streams: 2, Up: 20, Down: 200}) {
		t.Errorf("an ended task's totals: %+v", tot)
	}
	again := &reverseRig{svc: svc, listeners: listeners, task: running, session: session}
	if c := closes(t, again.take(t, again.open(5))); len(c) != 1 || c[0].Reason != protocol.TunnelCloseUnknown {
		t.Fatalf("a stream before the restart: %+v", c)
	}
	again.take(t, again.open(6))
	if rep := again.replyFor(t, 6); rep.Result != protocol.TunnelOpen {
		t.Fatalf("a stream after the restart: %+v", rep)
	}
}

// openUDP is the minion's tunnel for a UDP stream, from 10.0.0.12.
func (r *reverseRig) openUDP(stream uint64) protocol.TunnelItem {
	return protocol.TunnelItem{T: protocol.ItemTunnel, Task: r.task[:], Stream: stream, Host: "10.0.0.12", Port: 5353, UDP: true}
}

// datagramsFor waits for n of core's datagrams for stream.
func (r *reverseRig) datagramsFor(t *testing.T, stream uint64, n int) []protocol.TunnelDatagramItem {
	t.Helper()
	var got []protocol.TunnelDatagramItem
	eventually(t, "core's datagrams", func() bool {
		for _, it := range r.fill() {
			if protocol.ItemType(it) != protocol.ItemTunnelDatagram {
				continue
			}
			var d protocol.TunnelDatagramItem
			if err := protocol.UnmarshalCBOR(it, &d); err != nil || d.Validate() != nil {
				t.Fatalf("a datagram does not read: %v", err)
			}
			if d.Stream == stream {
				got = append(got, d)
			}
		}
		return len(got) >= n
	})
	return got
}

// A UDP reverse port forward's stream is a source's: core opens a socket
// of its own to the destination, sends the minion's datagrams there (each
// sequence number once), and what comes back to the minion, for the
// source, numbered from 0.
func TestReverseUDPCarriesDatagrams(t *testing.T) {
	dest := udpEcho(t)
	r := newReverseRigWith(t, dest.String(), 4, true)
	r.take(t, r.openUDP(1))
	if rep := r.replyFor(t, 1); rep.Result != protocol.TunnelOpen {
		t.Fatalf("reply %+v", rep)
	}
	ping := protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: r.task[:], Stream: 1, Seq: 0, Host: "10.0.0.12", Port: 5353,
		Data: []byte("ping")}
	r.take(t, ping)
	r.take(t, ping)
	pong := ping
	pong.Seq, pong.Data = 1, []byte("pong")
	r.take(t, pong)
	got := r.datagramsFor(t, 1, 2)
	if len(got) != 2 || string(got[0].Data) != "ping" || got[0].Seq != 0 || string(got[1].Data) != "pong" || got[1].Seq != 1 ||
		got[0].Host != "10.0.0.12" || got[0].Port != 5353 {
		t.Fatalf("back %+v", got)
	}
	time.Sleep(100 * time.Millisecond)
	if extra := r.fill(); len(extra) != 0 {
		t.Fatalf("a datagram taken twice went twice: %d more items", len(extra))
	}
	if sum, _ := r.svc.Summary(r.task); sum.Up != 8 || sum.Down != 8 {
		t.Errorf("bytes %+v", sum)
	}
	rec := r.stream(1)
	if !rec.UDP || rec.Client != "10.0.0.12:5353" || rec.Port != dest.Port {
		t.Errorf("record %+v", rec)
	}
}

// A UDP forward takes UDP streams only, each naming its source; a TCP
// forward, TCP streams only.
func TestReverseUDPOnlyItsKind(t *testing.T) {
	r := newReverseRigWith(t, udpEcho(t).String(), 4, true)
	r.take(t, r.open(1))
	if rep := r.replyFor(t, 1); rep.Result != protocol.TunnelFailed {
		t.Fatalf("a TCP stream of a UDP forward: %+v", rep)
	}
	nameless := r.openUDP(2)
	nameless.Host, nameless.Port = "", 0
	r.take(t, nameless)
	if rep := r.replyFor(t, 2); rep.Result != protocol.TunnelFailed {
		t.Fatalf("a UDP stream with no source: %+v", rep)
	}
	tcp := newReverseRig(t, echo(t), 4)
	tcp.take(t, tcp.openUDP(1))
	if rep := tcp.replyFor(t, 1); rep.Result != protocol.TunnelFailed {
		t.Fatalf("a UDP stream of a TCP forward: %+v", rep)
	}
}

// A UDP stream no datagram went through for UDPIdle ends, done, and the
// minion is told: core's backstop to the minion's own close.
func TestReverseUDPIdle(t *testing.T) {
	r := newReverseRigWith(t, udpEcho(t).String(), 4, true, func(c *Config) {
		c.UDPIdle, c.Sweep = 150*time.Millisecond, 20*time.Millisecond
	})
	r.take(t, r.openUDP(1))
	r.replyFor(t, 1)
	var closed []protocol.TunnelCloseItem
	eventually(t, "the idle close", func() bool {
		closed = append(closed, closes(t, r.fill())...)
		return len(closed) > 0
	})
	if closed[0].Stream != 1 || closed[0].Reason != protocol.TunnelCloseDone {
		t.Fatalf("close %+v", closed[0])
	}
	if sum, _ := r.svc.Summary(r.task); sum.Open != 0 {
		t.Fatalf("still open: %+v", sum)
	}
	eventually(t, "the record's end", func() bool { return r.stream(1).End == models.TunnelEndDone })
}
