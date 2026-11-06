package miniontunnels

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// fakePorts keeps the ports in use in memory, as the repository does.
type fakePorts struct {
	mu   sync.Mutex
	recs map[uuid.UUID]*models.OperationalPort
}

func (f *fakePorts) Insert(_ context.Context, p *models.OperationalPort) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recs {
		if r.Port == p.Port || r.TaskID == p.TaskID {
			return repository.ErrPortTaken
		}
	}
	c := *p
	f.recs[p.TaskID] = &c
	return nil
}

func (f *fakePorts) All(context.Context) ([]models.OperationalPort, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.OperationalPort
	for _, r := range f.recs {
		out = append(out, *r)
	}
	return out, nil
}

func (f *fakePorts) ListByOperation(ctx context.Context, _ uuid.UUID) ([]models.OperationalPort, error) {
	return f.All(ctx)
}

func (f *fakePorts) NextStream(_ context.Context, task uuid.UUID) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[task]
	if r == nil {
		return 0, repository.ErrNotFound
	}
	r.Streams++
	return r.Streams, nil
}

func (f *fakePorts) Release(_ context.Context, task uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs, task)
	return nil
}

func (f *fakePorts) held(task uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recs[task] != nil
}

// fakeRecords keeps the streams' records.
type fakeRecords struct {
	mu   sync.Mutex
	recs map[uint64]models.MinionTunnelStream
}

func (f *fakeRecords) Insert(_ context.Context, s *models.MinionTunnelStream) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs[s.Stream] = *s
	return nil
}

func (f *fakeRecords) Update(_ context.Context, s *models.MinionTunnelStream) error {
	return f.Insert(context.Background(), s)
}

func (f *fakeRecords) ListByTask(context.Context, uuid.UUID, int) ([]models.MinionTunnelStream, error) {
	return nil, nil
}

func (f *fakeRecords) DeleteByOperationID(context.Context, uuid.UUID) error { return nil }

func (f *fakeRecords) get(stream uint64) models.MinionTunnelStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recs[stream]
}

// fakeTasks is the tunnel's task.
type fakeTasks struct {
	mu    sync.Mutex
	tasks map[uuid.UUID]models.MinionTask
}

func (f *fakeTasks) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &t, nil
}

func (f *fakeTasks) RequestCancel(_ context.Context, id uuid.UUID, at time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	t.CancelRequestedAt = &at
	f.tasks[id] = t
	return &t, nil
}

func (f *fakeTasks) ack(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	now := time.Now()
	t.AckedAt, t.State = &now, models.MinionTaskRunning
	f.tasks[id] = t
}

// rig is a service with one tunnel task, its port free on 127.0.0.1.
type rig struct {
	svc     *Service
	ports   *fakePorts
	records *fakeRecords
	tasks   *fakeTasks
	task    uuid.UUID
	session uuid.UUID
	port    int
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newRig(t *testing.T, maxStreams int, cfg func(*Config)) *rig {
	t.Helper()
	r := &rig{ports: &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, records: &fakeRecords{recs: map[uint64]models.MinionTunnelStream{}},
		tasks: &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}, task: uuid.New(), session: uuid.New()}
	c := DefaultConfig()
	c.Ports, c.Bind = []int{freePort(t)}, "127.0.0.1"
	c.ResendAfter, c.ConnectSlack = 200*time.Millisecond, 2*time.Second
	if cfg != nil {
		cfg(&c)
	}
	r.svc = New(c, r.ports, r.records, r.tasks, nil, zap.NewNop())
	r.svc.Start(context.Background())
	t.Cleanup(r.svc.Stop)
	r.tasks.tasks[r.task] = models.MinionTask{TaskID: r.task, SessionID: r.session, State: models.MinionTaskSent, Action: protocol.ActionTunnel}
	port, err := r.svc.Open(context.Background(), Opening{Task: r.task, Session: r.session, Operation: uuid.New(), By: uuid.New(),
		MaxStreams: maxStreams, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	r.port = port
	return r
}

// fakeMinion relays the tunnel's streams as a minion does, checking in
// over and over as a polling one does; lose, when set, loses core's
// replies now and then, swap swaps its data items' order.
type fakeMinion struct {
	t       *testing.T
	r       *rig
	mu      sync.Mutex
	streams map[uint64]*minionStream
	out     []cbor.RawMessage
	lose    func(n int) bool
	swap    bool
	msgs    int
	stop    chan struct{}
	done    chan struct{}
}

type minionStream struct {
	conn net.Conn
	// From core: taken up to next.
	next  uint64
	eofIn bool
	// To core: read from the host from base (acked) on; sent up to sent
	// (the eof one past the bytes), last at sentAt.
	buf    []byte
	base   uint64
	sent   uint64
	sentAt time.Time
	limit  uint64
	eofOut bool
}

// minionResend is how long the fake minion waits for an ack before it
// sends again from it, or probes a shut window.
const minionResend = 200 * time.Millisecond

func startMinion(t *testing.T, r *rig, opts ...func(*fakeMinion)) *fakeMinion {
	m := &fakeMinion{t: t, r: r, streams: map[uint64]*minionStream{}, stop: make(chan struct{}), done: make(chan struct{})}
	for _, o := range opts {
		o(m)
	}
	go m.run()
	t.Cleanup(func() {
		close(m.stop)
		<-m.done
		m.mu.Lock()
		for _, st := range m.streams {
			_ = st.conn.Close()
		}
		m.mu.Unlock()
	})
	return m
}

func (m *fakeMinion) run() {
	defer close(m.done)
	ctx := context.Background()
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		m.mu.Lock()
		items := m.collect()
		m.msgs++
		n := m.msgs
		m.mu.Unlock()
		if m.swap && len(items) > 1 {
			for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
				items[i], items[j] = items[j], items[i]
			}
		}
		var answers []cbor.RawMessage
		for _, it := range items {
			a, err := m.r.svc.Take(ctx, m.r.session, it)
			if err != nil {
				m.t.Errorf("core refused an item: %v", err)
			}
			answers = append(answers, a...)
		}
		var reply []cbor.RawMessage
		_, poll, _ := m.r.svc.Fill(ctx, m.r.session, 1<<20, protocol.MaxMinionMessageItems, func(it cbor.RawMessage) bool {
			reply = append(reply, it)
			return true
		})
		if m.lose != nil && m.lose(n) {
			reply = nil
		}
		m.mu.Lock()
		for _, it := range append(answers, reply...) {
			m.handle(it)
		}
		m.mu.Unlock()
		time.Sleep(min(poll, 20*time.Millisecond) + time.Millisecond)
	}
}

// collect is the minion's message: its acks, replies, data and closes. As
// core does, it sends again from the ack what went unacked too long, and
// probes a window shut that long.
func (m *fakeMinion) collect() []cbor.RawMessage {
	items := m.out
	m.out = nil
	now := time.Now()
	for id, st := range m.streams {
		end := st.base + uint64(len(st.buf))
		quiet := now.Sub(st.sentAt) >= minionResend
		if st.sent > st.base && quiet {
			st.sent = st.base
		}
		went := false
		for {
			n := uint64(0)
			if limit := min(end, st.limit); st.sent < limit {
				n = min(limit-st.sent, protocol.MaxTunnelData)
			}
			eof := st.eofOut && st.sent <= end && st.sent+n == end
			probe := false
			if n == 0 && !eof {
				if went || !quiet || st.sent >= end {
					break
				}
				n, probe = 1, true
			}
			from := st.sent - st.base
			d := protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: m.r.task[:], Stream: id, Off: st.sent, Data: st.buf[from : from+n], EOF: eof && !probe}
			st.sent = d.End()
			items = append(items, m.marshal(d))
			went = true
			if probe {
				break
			}
		}
		if went {
			st.sentAt = now
		}
	}
	return items
}

func (m *fakeMinion) marshal(v any) cbor.RawMessage {
	b, err := protocol.MarshalCBOR(v)
	if err != nil {
		m.t.Fatal(err)
	}
	return b
}

// handle takes one of core's items.
func (m *fakeMinion) handle(raw cbor.RawMessage) {
	task := m.r.task[:]
	switch protocol.ItemType(raw) {
	case protocol.ItemTunnel:
		var it protocol.TunnelItem
		if err := protocol.UnmarshalCBOR(raw, &it); err != nil || it.Validate() != nil {
			m.t.Errorf("a tunnel item that does not read: %v", err)
			return
		}
		if st := m.streams[it.Stream]; st != nil {
			m.out = append(m.out, m.marshal(protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: task, Stream: it.Stream, Result: protocol.TunnelOpen,
				Bound: st.conn.LocalAddr().String()}))
			return
		}
		c, err := net.DialTimeout("tcp", net.JoinHostPort(it.Host, strconv.Itoa(int(it.Port))), time.Second)
		if err != nil {
			result := protocol.TunnelFailed
			if errors.Is(err, syscall.ECONNREFUSED) {
				result = protocol.TunnelRefused
			}
			m.out = append(m.out, m.marshal(protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: task, Stream: it.Stream, Result: result, Msg: err.Error()}))
			return
		}
		st := &minionStream{conn: c, limit: protocol.TunnelRoom(it.Room)}
		m.streams[it.Stream] = st
		m.out = append(m.out, m.marshal(protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: task, Stream: it.Stream, Result: protocol.TunnelOpen,
			Bound: c.LocalAddr().String()}))
		go m.read(st)
	case protocol.ItemTunnelData:
		var d protocol.TunnelDataItem
		if err := protocol.UnmarshalCBOR(raw, &d); err != nil {
			m.t.Errorf("data: %v", err)
			return
		}
		st := m.streams[d.Stream]
		if st == nil {
			return
		}
		if d.Off == st.next && !st.eofIn {
			_, _ = st.conn.Write(d.Data)
			st.next += uint64(len(d.Data))
			if d.EOF {
				st.eofIn = true
				st.next++
				_ = st.conn.(*net.TCPConn).CloseWrite()
			}
		}
		m.out = append(m.out, m.marshal(protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: task, Stream: d.Stream, Next: st.next}))
	case protocol.ItemTunnelAck:
		var a protocol.TunnelAckItem
		if err := protocol.UnmarshalCBOR(raw, &a); err != nil {
			m.t.Errorf("ack: %v", err)
			return
		}
		st := m.streams[a.Stream]
		if st == nil || a.Next < st.base {
			return
		}
		k := min(a.Next-st.base, uint64(len(st.buf)))
		if a.Next > st.base {
			st.sentAt = time.Now()
		}
		st.buf, st.base = st.buf[k:], st.base+k
		if a.Room != nil {
			st.limit = a.Next + *a.Room
		}
		if st.sent < st.base {
			st.sent = st.base
		}
	case protocol.ItemTunnelClose:
		var c protocol.TunnelCloseItem
		if err := protocol.UnmarshalCBOR(raw, &c); err != nil {
			m.t.Errorf("close: %v", err)
			return
		}
		if st := m.streams[c.Stream]; st != nil {
			_ = st.conn.Close()
			delete(m.streams, c.Stream)
		}
	}
}

// read takes what the host sends, for collect.
func (m *fakeMinion) read(st *minionStream) {
	buf := make([]byte, 16<<10)
	for {
		n, err := st.conn.Read(buf)
		m.mu.Lock()
		st.buf = append(st.buf, buf[:n]...)
		if err != nil {
			st.eofOut = true
		}
		m.mu.Unlock()
		if err != nil {
			return
		}
	}
}

// echo is a host that sends back what it gets, and closes once it has it
// all.
func echo(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
				_ = c.(*net.TCPConn).CloseWrite()
			}()
		}
	}()
	return ln.Addr().String()
}

// socks5 connects through core's proxy to addr: the connection, and the
// reply's code.
func socks5(t *testing.T, port int, host string, destPort int) (net.Conn, byte) {
	t.Helper()
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	req := []byte{5, 1, 0, 5, 1, 0}
	if ip := net.ParseIP(host).To4(); ip != nil {
		req = append(append(req, 1), ip...)
	} else {
		req = append(append(req, 3, byte(len(host))), host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(destPort))
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 2+4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("socks reply: %v", err)
	}
	if !bytes.Equal(head[:2], []byte{5, 0}) {
		t.Fatalf("method reply % x", head[:2])
	}
	addr := 4
	if head[5] == 4 {
		addr = 16
	}
	rest := make([]byte, addr+2)
	if _, err := io.ReadFull(c, rest); err != nil {
		t.Fatalf("socks reply address: %v", err)
	}
	_ = c.SetDeadline(time.Time{})
	return c, head[3]
}

func splitAddr(t *testing.T, addr string) (string, int) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(p)
	return host, port
}

// roundTrip sends size random bytes through c and reads them back.
func roundTrip(t *testing.T, c net.Conn, size int) {
	t.Helper()
	sent := make([]byte, size)
	_, _ = rand.Read(sent)
	errs := make(chan error, 1)
	go func() {
		_, err := c.Write(sent)
		if err == nil {
			err = c.(*net.TCPConn).CloseWrite()
		}
		errs <- err
	}()
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("read back: %v (%d bytes of %d)", err, len(got), size)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sent) {
		t.Fatalf("came back %d bytes, %d sent, not the same", len(got), size)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A SOCKS5 client reaches a host through the minion: what it sends comes
// back from an echo host, a megabyte both ways through 256 KiB windows,
// and the stream ends done once both ends closed, its record saying who
// reached what and how much went.
func TestTunnelCarriesAStream(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	startMinion(t, r)
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	defer c.Close()
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	roundTrip(t, c, 1<<20)
	eventually(t, "the stream ends done", func() bool { return r.records.get(1).End == models.TunnelEndDone })
	rec := r.records.get(1)
	if rec.Host != host || rec.Port != port || rec.Up != 1<<20 || rec.Down != 1<<20 || rec.Result == nil || *rec.Result != protocol.TunnelOpen || rec.Bound == "" {
		t.Errorf("record %+v", rec)
	}
}

// Replies lost on the way (a third of them) lose no byte and double none:
// what was not acked goes again from the ack.
func TestTunnelSurvivesLostReplies(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	startMinion(t, r, func(m *fakeMinion) { m.lose = func(n int) bool { return n%3 == 0 } })
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	defer c.Close()
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	roundTrip(t, c, 512<<10)
}

// The minion's data items crossing (a later one first) are kept and taken
// in order.
func TestTunnelTakesDataAhead(t *testing.T) {
	// No resend could cover for an item dropped.
	r := newRig(t, 0, func(c *Config) { c.ResendAfter = time.Hour })
	r.tasks.ack(r.task)
	startMinion(t, r, func(m *fakeMinion) { m.swap = true })
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	defer c.Close()
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	roundTrip(t, c, 1<<20)
}

// A client that connects before the minion has the task waits for it; one
// past max_streams is not allowed; a destination nothing listens on is
// refused, as the minion says.
func TestTunnelOpening(t *testing.T) {
	r := newRig(t, 1, nil)
	startMinion(t, r)
	host, port := splitAddr(t, echo(t))
	type result struct {
		c    net.Conn
		code byte
	}
	first := make(chan result, 1)
	go func() {
		c, code := socks5(t, r.port, host, port)
		first <- result{c, code}
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-first:
		t.Fatal("a stream opened before the minion had the task")
	default:
	}
	r.tasks.ack(r.task)
	res := <-first
	defer res.c.Close()
	if res.code != socks5Succeeded {
		t.Fatalf("first: reply %d", res.code)
	}
	second, code := socks5(t, r.port, host, port)
	_ = second.Close()
	if code != socks5NotAllowed {
		t.Errorf("past max_streams: reply %d, want not allowed", code)
	}
	roundTrip(t, res.c, 1000)
	eventually(t, "the first stream ends", func() bool { return r.records.get(1).End != "" })
	closed := freePort(t)
	c, code := socks5(t, r.port, "127.0.0.1", closed)
	_ = c.Close()
	if code != socks5ConnectionRefused {
		t.Errorf("nothing listening: reply %d, want refused", code)
	}
}

// Closing the tunnel (its task stopped) closes the port and its clients'
// connections, and gives the port back.
func TestTunnelClose(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	startMinion(t, r)
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	defer c.Close()
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	r.svc.Close(context.Background(), r.task)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Error("the client's connection stays open")
	}
	if r.ports.held(r.task) {
		t.Error("the port is still the task's")
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(r.port)), time.Second); err == nil {
		t.Error("the port still listens")
	}
	eventually(t, "the stream's record ends", func() bool { return r.records.get(1).End == models.TunnelEndTaskEnded })
}

// Opening takes a free port of the range, or the one asked for; one not in
// the range, or in use, is refused.
func TestOpenPorts(t *testing.T) {
	r := newRig(t, 0, nil)
	ctx := context.Background()
	if _, err := r.svc.Open(ctx, Opening{Task: uuid.New(), Session: r.session}); !errors.Is(err, ErrNoPort) {
		t.Errorf("the range used up: %v", err)
	}
	if _, err := r.svc.Open(ctx, Opening{Task: uuid.New(), Session: r.session, Port: r.port}); !errors.Is(err, ErrNoPort) {
		t.Errorf("a port in use: %v", err)
	}
	if _, err := r.svc.Open(ctx, Opening{Task: uuid.New(), Session: r.session, Port: 1}); !errors.Is(err, ErrNoPort) {
		t.Errorf("a port not in the range: %v", err)
	}
	r.svc.Close(ctx, r.task)
	task := uuid.New()
	port, err := r.svc.Open(ctx, Opening{Task: task, Session: r.session, Port: r.port})
	if err != nil || port != r.port {
		t.Errorf("the port given back, asked for: %d %v", port, err)
	}
}

// Items about a stream core does not have are answered with a close: no
// such stream (3), or, for a task core runs no tunnel of, task ended (4);
// an item that does not read is refused.
func TestTakeUnknown(t *testing.T) {
	r := newRig(t, 0, nil)
	ctx := context.Background()
	closeReason := func(answers []cbor.RawMessage) int {
		if len(answers) != 1 {
			t.Fatalf("answers %d", len(answers))
		}
		var c protocol.TunnelCloseItem
		if err := protocol.UnmarshalCBOR(answers[0], &c); err != nil {
			t.Fatal(err)
		}
		return c.Reason
	}
	data, _ := protocol.MarshalCBOR(protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: r.task[:], Stream: 9, Data: []byte("x")})
	if a, err := r.svc.Take(ctx, r.session, data); err != nil || closeReason(a) != protocol.TunnelCloseUnknown {
		t.Errorf("no such stream: %v", err)
	}
	other := uuid.New()
	ack, _ := protocol.MarshalCBOR(protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: other[:], Stream: 1, Next: 3})
	if a, err := r.svc.Take(ctx, r.session, ack); err != nil || closeReason(a) != protocol.TunnelCloseTaskEnded {
		t.Errorf("no such task: %v", err)
	}
	if a, err := r.svc.Take(ctx, uuid.New(), data); err != nil || closeReason(a) != protocol.TunnelCloseTaskEnded {
		t.Errorf("another session's task: %v", err)
	}
	gone, _ := protocol.MarshalCBOR(protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: r.task[:], Stream: 9, Reason: protocol.TunnelCloseReset})
	if a, err := r.svc.Take(ctx, r.session, gone); err != nil || len(a) != 0 {
		t.Errorf("a close for a stream gone: %v %v", a, err)
	}
	bad, _ := protocol.MarshalCBOR(protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: r.task[:], Stream: 0, Data: []byte("x")})
	if _, err := r.svc.Take(ctx, r.session, bad); err == nil {
		t.Error("stream 0 taken")
	}
}

// A restart opens again the port of a tunnel whose task still runs, and
// gives back that of one which ended.
func TestRestartReopens(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	ended := uuid.New()
	r.ports.recs[ended] = &models.OperationalPort{Port: freePort(t), TaskID: ended}
	r.tasks.tasks[ended] = models.MinionTask{TaskID: ended, State: models.MinionTaskDone}
	r.svc.Stop()
	if !r.ports.held(r.task) {
		t.Fatal("stopping gave the port back")
	}
	again := New(r.svc.cfg, r.ports, r.records, r.tasks, nil, zap.NewNop())
	again.Start(context.Background())
	defer again.Stop()
	r.svc = again
	if r.ports.held(ended) {
		t.Error("an ended task's port is kept")
	}
	startMinion(t, r)
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	defer c.Close()
	if code != socks5Succeeded {
		t.Fatalf("after a restart: reply %d", code)
	}
	roundTrip(t, c, 1000)
}
