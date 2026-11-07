package miniontunnels

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
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

// fakePorts keeps the ports in use in memory, as the repository does.
type fakePorts struct {
	mu   sync.Mutex
	recs map[uuid.UUID]*models.OperationalPort
	// reserved counts the blocks of stream numbers taken (writes).
	reserved int
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

func (f *fakePorts) FindByOperationIDWithCursor(ctx context.Context, _ uuid.UUID, _ string, _ *pagination.Cursor, _ int64, _ bool) ([]models.OperationalPort, error) {
	return f.All(ctx)
}

func (f *fakePorts) CountByOperationID(ctx context.Context, _ uuid.UUID, _ string) (int64, error) {
	all, err := f.All(ctx)
	return int64(len(all)), err
}

func (f *fakePorts) FindByTaskID(_ context.Context, task uuid.UUID) (*models.OperationalPort, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.recs[task]; r != nil {
		c := *r
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakePorts) ReserveStreams(_ context.Context, task uuid.UUID, n uint64) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[task]
	if r == nil {
		return 0, repository.ErrNotFound
	}
	r.Streams += n
	f.reserved++
	return r.Streams, nil
}

func (f *fakePorts) SetCounts(_ context.Context, task uuid.UUID, up, down int64, opened uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[task]
	if r == nil {
		return repository.ErrNotFound
	}
	r.Up, r.Down, r.Opened = up, down, opened
	return nil
}

func (f *fakePorts) SetCap(_ context.Context, task uuid.UUID, limit int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[task]
	if r == nil {
		return repository.ErrNotFound
	}
	r.Cap = limit
	return nil
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

func (f *fakeTasks) RequestCancelWhy(_ context.Context, id uuid.UUID, at time.Time, why string) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	t.CancelRequestedAt, t.CancelReason = &at, why
	f.tasks[id] = t
	return &t, nil
}

func (f *fakeTasks) SetTunnel(_ context.Context, id uuid.UUID, totals models.MinionTaskTunnel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil
	}
	t.Tunnel = &totals
	f.tasks[id] = t
	return nil
}

func (f *fakeTasks) TunnelOf(_ context.Context, id uuid.UUID) (*models.MinionTaskTunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return t.Tunnel, nil
}

// tunnel is what the task keeps of its tunnel: nil before it closed.
func (f *fakeTasks) tunnel(id uuid.UUID) *models.MinionTaskTunnel {
	t, _ := f.TunnelOf(context.Background(), id)
	return t
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
	bus     *recordingBus
	svc     *Service
	ports   *fakePorts
	tasks   *fakeTasks
	task    uuid.UUID
	session uuid.UUID
	port    int
}

// stream is the tunnel's stream n as its task's Streams tab lists it: zero
// when core keeps it no longer.
func (r *rig) stream(n uint64) models.MinionTunnelStream {
	return streamOf(r.svc, r.task, n)
}

func streamOf(svc *Service, task uuid.UUID, n uint64) models.MinionTunnelStream {
	recs, _ := svc.Streams(context.Background(), task, StreamFilter{}, n+1, 1)
	if len(recs) == 1 && recs[0].Stream == n {
		return recs[0]
	}
	return models.MinionTunnelStream{}
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

func newRig(t *testing.T, maxStreams int, cfg func(*Config), opening ...func(*Opening)) *rig {
	t.Helper()
	r := &rig{ports: &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, tasks: &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}},
		task: uuid.New(), session: uuid.New()}
	c := DefaultConfig()
	c.Ports, c.Bind = []int{freePort(t)}, "127.0.0.1"
	c.ResendAfter, c.ConnectSlack, c.Tell = 200*time.Millisecond, 2*time.Second, 20*time.Millisecond
	if cfg != nil {
		cfg(&c)
	}
	r.bus = &recordingBus{}
	r.svc = New(c, r.ports, r.tasks, r.bus, zap.NewNop())
	r.svc.Start(context.Background())
	t.Cleanup(r.svc.Stop)
	r.tasks.tasks[r.task] = models.MinionTask{TaskID: r.task, SessionID: r.session, State: models.MinionTaskSent, Action: protocol.ActionTunnel}
	// Its command opens udp: its SOCKS proxy takes UDP ASSOCIATE.
	o := Opening{Task: r.task, Session: r.session, Operation: uuid.New(), By: uuid.New(), MaxStreams: maxStreams, Timeout: time.Second, UDP: true}
	for _, f := range opening {
		f(&o)
	}
	port, err := r.svc.Open(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	r.port = port
	return r
}

// recordingBus keeps what is published.
type recordingBus struct {
	eventbus.NopEventBus
	mu     sync.Mutex
	events []eventbus.Event
}

func (b *recordingBus) Publish(e eventbus.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

// ports are the port changes published, in order.
func (b *recordingBus) ports() []eventbus.OperationalPortEventPayload {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []eventbus.OperationalPortEventPayload
	for _, e := range b.events {
		if p, ok := e.Payload.(eventbus.OperationalPortEventPayload); ok {
			out = append(out, p)
		}
	}
	return out
}

// tasks counts the task changes published for task.
func (b *recordingBus) tasks(task uuid.UUID) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.events {
		if p, ok := e.Payload.(eventbus.MinionTaskEventPayload); ok && p.TaskID == task.String() {
			n++
		}
	}
	return n
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
	// closed are core's closes the minion took.
	closed []protocol.TunnelCloseItem
	msgs   int
	stop   chan struct{}
	done   chan struct{}
}

type minionStream struct {
	conn net.Conn
	// A UDP association's socket, and its datagrams for core.
	pc     net.PacketConn
	dgrams []protocol.TunnelDatagramItem
	dseq   uint64
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
		if st.pc != nil {
			for _, d := range st.dgrams {
				// Each twice: core takes it once.
				items = append(items, m.marshal(d), m.marshal(d))
			}
			st.dgrams = nil
			continue
		}
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
			var bound string
			if st.conn != nil {
				bound = st.conn.LocalAddr().String()
			} else {
				bound = st.pc.LocalAddr().String()
			}
			m.out = append(m.out, m.marshal(protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: task, Stream: it.Stream, Result: protocol.TunnelOpen,
				Bound: bound}))
			return
		}
		if it.UDP {
			pc, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				m.t.Errorf("a UDP socket: %v", err)
				return
			}
			st := &minionStream{pc: pc}
			m.streams[it.Stream] = st
			m.out = append(m.out, m.marshal(protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: task, Stream: it.Stream, Result: protocol.TunnelOpen,
				Bound: pc.LocalAddr().String()}))
			go m.readUDP(it.Stream, st)
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
			if st.conn != nil {
				_ = st.conn.Close()
			} else {
				_ = st.pc.Close()
			}
			delete(m.streams, c.Stream)
			m.closed = append(m.closed, c)
		}
	case protocol.ItemTunnelDatagram:
		var d protocol.TunnelDatagramItem
		if err := protocol.UnmarshalCBOR(raw, &d); err != nil || d.Validate() != nil {
			m.t.Errorf("a datagram that does not read: %v", err)
			return
		}
		if st := m.streams[d.Stream]; st != nil && st.pc != nil {
			to, err := net.ResolveUDPAddr("udp", net.JoinHostPort(d.Host, strconv.Itoa(int(d.Port))))
			if err == nil {
				_, _ = st.pc.WriteTo(d.Data, to)
			}
		}
	}
}

// readUDP takes what comes to an association's socket, for collect.
func (m *fakeMinion) readUDP(id uint64, st *minionStream) {
	buf := make([]byte, 64<<10)
	for {
		n, from, err := st.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		ua := from.(*net.UDPAddr)
		m.mu.Lock()
		st.dgrams = append(st.dgrams, protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: m.r.task[:], Stream: id, Seq: st.dseq,
			Host: ua.IP.String(), Port: uint64(ua.Port), Data: bytes.Clone(buf[:n])})
		st.dseq++
		m.mu.Unlock()
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
	eventually(t, "the stream ends done", func() bool { return r.stream(1).End == models.TunnelEndDone })
	rec := r.stream(1)
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
	eventually(t, "the first stream ends", func() bool { return r.stream(1).End != "" })
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
	// Its streams go with it; what it came to stays with its task.
	if recs, _ := r.svc.Streams(context.Background(), r.task, StreamFilter{}, 0, -1); len(recs) != 0 {
		t.Errorf("its streams kept: %d", len(recs))
	}
	if tot := r.tasks.tunnel(r.task); tot == nil || tot.Streams != 1 {
		t.Errorf("its totals: %+v", tot)
	}
	if sum, live, err := r.svc.View(context.Background(), r.task); err != nil || live || sum.Streams != 1 {
		t.Errorf("its view: %+v %v %v", sum, live, err)
	}
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

// A restart opens again the port of a tunnel whose task still runs, its
// bytes counted on from what stopping kept, and gives back that of one
// which ended, what it came to kept with its task.
func TestRestartReopens(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	ended := uuid.New()
	r.ports.recs[ended] = &models.OperationalPort{Port: freePort(t), TaskID: ended, Streams: 7, Opened: 7, Up: 70, Down: 700}
	r.tasks.tasks[ended] = models.MinionTask{TaskID: ended, State: models.MinionTaskDone}
	r.svc.mu.Lock()
	r.svc.tunnels[r.task].up, r.svc.tunnels[r.task].down = 100, 200
	r.svc.mu.Unlock()
	r.svc.Stop()
	if !r.ports.held(r.task) {
		t.Fatal("stopping gave the port back")
	}
	if p, _ := r.ports.FindByTaskID(context.Background(), r.task); p.Up != 100 || p.Down != 200 {
		t.Fatalf("stopping kept the bytes as %d and %d", p.Up, p.Down)
	}
	again := New(r.svc.cfg, r.ports, r.tasks, nil, zap.NewNop())
	again.Start(context.Background())
	defer again.Stop()
	r.svc = again
	if r.ports.held(ended) {
		t.Error("an ended task's port is kept")
	}
	if tot := r.tasks.tunnel(ended); tot == nil || *tot != (models.MinionTaskTunnel{Streams: 7, Up: 70, Down: 700}) {
		t.Errorf("an ended task's totals: %+v", tot)
	}
	if sum, ok := again.Summary(r.task); !ok || sum.Up != 100 || sum.Down != 200 {
		t.Errorf("after a restart: %+v %v", sum, ok)
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

// A port's opening, its streams coming and going, and its closing are said
// to the operation (the Ports page), its streams' numbers once each Tell at
// most; its task is told as its tunnel moves on.
func TestTunnelEvents(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	startMinion(t, r)
	if got := r.bus.ports(); len(got) != 1 || !got[0].Opened || got[0].TaskID != r.task.String() || got[0].OperationID == "" {
		t.Fatalf("opened: %+v", got)
	}
	host, port := splitAddr(t, echo(t))
	c, code := socks5(t, r.port, host, port)
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	roundTrip(t, c, 1000)
	_ = c.Close()
	eventually(t, "the stream's numbers said", func() bool { return len(r.bus.ports()) >= 2 })
	for _, p := range r.bus.ports()[1:] {
		if p.Opened || p.Closed {
			t.Errorf("a stream said as an opening or a closing: %+v", p)
		}
	}
	eventually(t, "its task told", func() bool { return r.bus.tasks(r.task) > 0 })
	r.svc.Close(context.Background(), r.task)
	got := r.bus.ports()
	if last := got[len(got)-1]; !last.Closed {
		t.Errorf("closed: %+v", last)
	}
}

// udpEcho is a UDP host that sends back what it gets.
func udpEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], from)
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr)
}

// socks5UDP asks core's proxy for a UDP association: its SOCKS connection,
// and the relay the reply names.
func socks5UDP(t *testing.T, port int) (net.Conn, *net.UDPAddr) {
	t.Helper()
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte{5, 1, 0, 5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2+10)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatalf("socks reply: %v", err)
	}
	if reply[3] != socks5Succeeded || reply[5] != 1 {
		t.Fatalf("reply % x", reply)
	}
	_ = c.SetDeadline(time.Time{})
	return c, &net.UDPAddr{IP: net.IP(reply[6:10]), Port: int(binary.BigEndian.Uint16(reply[10:12]))}
}

// A UDP association carries a tool's datagrams to a host through the
// minion and the host's back, each with the SOCKS header saying its source;
// one the minion sends twice reaches the tool once. Closing the tool's SOCKS
// connection ends the association: the minion is told, its socket goes.
func TestTunnelCarriesDatagrams(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	m := startMinion(t, r)
	echoAddr := udpEcho(t)
	ctl, relay := socks5UDP(t, r.port)
	if relay.Port != r.port {
		t.Fatalf("the relay on port %d, not the proxy's %d", relay.Port, r.port)
	}
	u, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u.Close() }()
	// The first datagram binds the association to the tool's socket.
	sent := []string{"one", "two", "three"}
	for _, p := range sent {
		if _, err := u.Write(datagram(echoAddr.IP.String(), echoAddr.Port, []byte(p))); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]int{}
	buf := make([]byte, 64<<10)
	_ = u.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if len(got) == len(sent) {
			// All came: a while more, for one coming twice.
			_ = u.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		}
		n, err := u.Read(buf)
		if err != nil {
			break
		}
		host, port, data, ok := readDatagram(buf[:n])
		if !ok || host != echoAddr.IP.String() || port != echoAddr.Port {
			t.Fatalf("from %q %d (%v)", host, port, ok)
		}
		got[string(data)]++
	}
	for _, p := range sent {
		if got[p] != 1 {
			t.Errorf("%q came back %d times: %v", p, got[p], got)
		}
	}
	_ = ctl.Close()
	eventually(t, "the association ends", func() bool { return r.stream(1).End == models.TunnelEndDone })
	eventually(t, "the minion told", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.closed) == 1 && m.closed[0].Reason == protocol.TunnelCloseDone
	})
	rec := r.stream(1)
	if !rec.UDP || rec.Up == 0 || rec.Down == 0 {
		t.Errorf("record %+v", rec)
	}
}

// forwardTo makes a rig's tunnel a port forward to host:port.
func forwardTo(host string, port int) func(*Opening) {
	return func(o *Opening) { o.Purpose, o.Host, o.HostPort = models.OperationalPurposeForward, host, port }
}

// A port forward takes a plain connection, no SOCKS: each one is a stream
// to the forward's destination, the same items as a proxy's, its record
// naming the destination. Its port has no UDP relay.
func TestForwardCarriesAStream(t *testing.T) {
	host, port := splitAddr(t, echo(t))
	r := newRig(t, 0, nil, forwardTo(host, port))
	r.tasks.ack(r.task)
	startMinion(t, r)
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(r.port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	roundTrip(t, c, 256<<10)
	eventually(t, "the stream ends done", func() bool { return r.stream(1).End == models.TunnelEndDone })
	if rec := r.stream(1); rec.Host != host || rec.Port != port || rec.UDP || rec.Up != 256<<10 || rec.Down != 256<<10 {
		t.Errorf("record %+v", rec)
	}
	r.svc.mu.Lock()
	relay := r.svc.tunnels[r.task].relay
	r.svc.mu.Unlock()
	if relay != nil {
		t.Error("a forward with a UDP relay")
	}
}

// A forward whose destination refuses resets the client's connection, as
// the destination would.
func TestForwardRefused(t *testing.T) {
	r := newRig(t, 0, nil, forwardTo("127.0.0.1", freePort(t)))
	r.tasks.ack(r.task)
	startMinion(t, r)
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(r.port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("the client's connection stays: %v", err)
	}
	eventually(t, "its record ends", func() bool { return r.stream(1).End == models.TunnelEndNotOpened })
	if rec := r.stream(1); rec.Result == nil || *rec.Result != protocol.TunnelRefused {
		t.Errorf("record %+v", rec)
	}
}
