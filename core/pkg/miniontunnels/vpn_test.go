package miniontunnels

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
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

// fakeVPNTunnels keeps VPN tunnels in memory, as the repository does.
type fakeVPNTunnels struct {
	mu   sync.Mutex
	recs map[uuid.UUID]*models.VPNTunnel
}

func (f *fakeVPNTunnels) Insert(_ context.Context, v *models.VPNTunnel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *v
	f.recs[v.TaskID] = &c
	return nil
}

func (f *fakeVPNTunnels) All(context.Context) ([]models.VPNTunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.VPNTunnel
	for _, r := range f.recs {
		out = append(out, *r)
	}
	return out, nil
}

func (f *fakeVPNTunnels) FindByOperationIDWithCursor(ctx context.Context, _ uuid.UUID, _ string, _ *pagination.Cursor, _ int64, _ bool) ([]models.VPNTunnel, error) {
	return f.All(ctx)
}

func (f *fakeVPNTunnels) CountByOperationID(ctx context.Context, _ uuid.UUID, _ string) (int64, error) {
	all, err := f.All(ctx)
	return int64(len(all)), err
}

func (f *fakeVPNTunnels) FindByTaskID(_ context.Context, task uuid.UUID) (*models.VPNTunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.recs[task]; r != nil {
		c := *r
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeVPNTunnels) update(task uuid.UUID, do func(*models.VPNTunnel)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[task]
	if r == nil {
		return repository.ErrNotFound
	}
	do(r)
	return nil
}

func (f *fakeVPNTunnels) ReserveStreams(_ context.Context, task uuid.UUID, n uint64) (uint64, error) {
	var end uint64
	err := f.update(task, func(r *models.VPNTunnel) { r.Streams += n; end = r.Streams })
	return end, err
}

func (f *fakeVPNTunnels) SetCounts(_ context.Context, task uuid.UUID, up, down int64, opened uint64) error {
	return f.update(task, func(r *models.VPNTunnel) { r.Up, r.Down, r.Opened = up, down, opened })
}

func (f *fakeVPNTunnels) SetCap(_ context.Context, task uuid.UUID, limit int64) error {
	return f.update(task, func(r *models.VPNTunnel) { r.Cap = limit })
}

func (f *fakeVPNTunnels) SetRoutes(_ context.Context, task uuid.UUID, routes []string) error {
	return f.update(task, func(r *models.VPNTunnel) { r.Routes = routes })
}

func (f *fakeVPNTunnels) Release(_ context.Context, task uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs, task)
	return nil
}

// fakeVPNKeys keeps keys in memory, taking them as the repository does.
type fakeVPNKeys struct {
	mu   sync.Mutex
	keys map[string]*models.VPNKey
}

func (f *fakeVPNKeys) Insert(_ context.Context, k *models.VPNKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *k
	f.keys[k.KeyID] = &c
	return nil
}

func (f *fakeVPNKeys) FindByKeyID(_ context.Context, id string) (*models.VPNKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k := f.keys[id]; k != nil {
		c := *k
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeVPNKeys) Take(_ context.Context, id, instance string, now time.Time) (*models.VPNKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[id]
	if k == nil || !((k.Instance == "" && k.LapsesAt != nil && k.LapsesAt.After(now)) || (k.Instance != "" && k.Instance == instance)) {
		return nil, repository.ErrNotFound
	}
	k.Instance, k.TakenAt, k.LapsesAt = instance, &now, nil
	c := *k
	return &c, nil
}

func (f *fakeVPNKeys) Release(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, id)
	return nil
}

func (f *fakeVPNKeys) ReleaseTask(_ context.Context, task uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, k := range f.keys {
		if k.TaskID == task {
			delete(f.keys, id)
		}
	}
	return nil
}

func (f *fakeVPNKeys) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)
}

// fakeRoles says whether the operator still is one.
type fakeRoles struct {
	mu sync.Mutex
	no map[uuid.UUID]bool
}

func (f *fakeRoles) IsOperator(_ context.Context, _, user uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.no[user]
}

// vpnRig is a service with one VPN tunnel task, its minion fake.
type vpnRig struct {
	*rig
	tunnels *fakeVPNTunnels
	keys    *fakeVPNKeys
	roles   *fakeRoles
	user    uuid.UUID
	minion  *fakeMinion
}

func newVPNRig(t *testing.T, routes []string, udp, icmp bool, maxStreams int) *vpnRig {
	t.Helper()
	r := &rig{ports: &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, tasks: &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}},
		task: uuid.New(), session: uuid.New(), bus: &recordingBus{}}
	v := &vpnRig{rig: r, tunnels: &fakeVPNTunnels{recs: map[uuid.UUID]*models.VPNTunnel{}}, keys: &fakeVPNKeys{keys: map[string]*models.VPNKey{}},
		roles: &fakeRoles{no: map[uuid.UUID]bool{}}, user: uuid.New()}
	c := DefaultConfig()
	c.ResendAfter, c.ConnectSlack, c.Tell, c.Sweep, c.VPNGrace = 200*time.Millisecond, 2*time.Second, 20*time.Millisecond, 50*time.Millisecond, 300*time.Millisecond
	r.svc = New(c, r.ports, r.tasks, r.bus, zap.NewNop()).WithVPN(v.tunnels, v.keys, v.roles)
	r.svc.Start(context.Background())
	t.Cleanup(r.svc.Stop)
	r.tasks.tasks[r.task] = models.MinionTask{TaskID: r.task, SessionID: r.session, State: models.MinionTaskSent, Action: protocol.ActionTunnel}
	err := r.svc.OpenVPN(context.Background(), Opening{Task: r.task, Session: r.session, Operation: uuid.New(), By: v.user, MaxStreams: maxStreams,
		Timeout: time.Second, Routes: routes, UDP: udp, ICMP: icmp, SessionNumber: 12})
	if err != nil {
		t.Fatal(err)
	}
	r.tasks.ack(r.task)
	v.minion = startMinion(t, r, func(m *fakeMinion) { m.redirect = map[string]string{"10.9.0.5": "127.0.0.1"} })
	return v
}

// key makes a key for alice.
func (v *vpnRig) key(t *testing.T) string {
	t.Helper()
	raw, lapses, err := v.svc.NewVPNKey(context.Background(), v.task, v.user, "alice")
	if err != nil || !strings.HasPrefix(raw, protocol.VPNKeyPrefix) || time.Until(lapses) < 9*time.Minute {
		t.Fatalf("a key: %q %v %v", raw, lapses, err)
	}
	return raw
}

func hello(instance byte) protocol.VPNHello {
	return protocol.VPNHello{Link: protocol.VPNLink, Version: "0.1.0", OS: "linux", Arch: "amd64", Machine: "kali",
		Instance: hex.EncodeToString(bytes.Repeat([]byte{instance}, 16))}
}

// testLink is a link in memory: the client's end of it the test's.
type testLink struct {
	in     chan linkFrame
	out    chan linkFrame
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	code   int
	why    string
}

type linkFrame struct {
	text bool
	b    []byte
}

func newTestLink() *testLink {
	return &testLink{in: make(chan linkFrame, 64), out: make(chan linkFrame, 4096), closed: make(chan struct{})}
}

func (l *testLink) Read() (bool, []byte, error) {
	select {
	case f := <-l.in:
		return f.text, f.b, nil
	case <-l.closed:
		return false, nil, io.EOF
	}
}

func (l *testLink) Write(text bool, b []byte) error {
	select {
	case <-l.closed:
		return io.ErrClosedPipe
	default:
	}
	select {
	case l.out <- linkFrame{text: text, b: bytes.Clone(b)}:
		return nil
	case <-l.closed:
		return io.ErrClosedPipe
	}
}

func (l *testLink) Close(code int, why string) {
	l.once.Do(func() {
		l.mu.Lock()
		l.code, l.why = code, why
		l.mu.Unlock()
		close(l.closed)
	})
}

func (l *testLink) closedWith() (int, string, bool) {
	select {
	case <-l.closed:
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.code, l.why, true
	default:
		return 0, "", false
	}
}

// vpnTester is logos-vpn as a test plays it: it opens streams, sends
// bytes, and keeps what core sends back, acking bytes as they come
// (unless holdAcks).
type vpnTester struct {
	t    *testing.T
	v    *vpnRig
	c    *VPNClient
	link *testLink
	task []byte

	mu       sync.Mutex
	welcome  protocol.VPNWelcome
	replies  map[uint64]protocol.TunnelReplyItem
	data     map[uint64][]byte
	next     map[uint64]uint64
	eof      map[uint64]bool
	acks     map[uint64]protocol.TunnelAckItem
	closes   map[uint64]int
	dgrams   []protocol.TunnelDatagramItem
	controls []protocol.VPNControl
	holdAcks bool
	stop     chan struct{}
}

// connect connects a client with key as instance, and serves its link.
func (v *vpnRig) connect(t *testing.T, key string, instance byte) *vpnTester {
	t.Helper()
	c, w, err := v.svc.ConnectVPN(context.Background(), key, hello(instance), "203.0.113.5")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	vt := &vpnTester{t: t, v: v, c: c, task: v.task[:], welcome: w, replies: map[uint64]protocol.TunnelReplyItem{}, data: map[uint64][]byte{},
		next: map[uint64]uint64{}, eof: map[uint64]bool{}, acks: map[uint64]protocol.TunnelAckItem{}, closes: map[uint64]int{}}
	vt.attach(newTestLink())
	return vt
}

// attach serves a new link of the client's, reading what core sends.
func (vt *vpnTester) attach(l *testLink) {
	vt.mu.Lock()
	vt.link = l
	vt.stop = make(chan struct{})
	stop := vt.stop
	vt.mu.Unlock()
	go vt.c.Serve(l)
	go vt.read(l, stop)
	vt.t.Cleanup(func() { l.Close(1000, "") })
}

func (vt *vpnTester) read(l *testLink, stop chan struct{}) {
	for {
		var f linkFrame
		select {
		case f = <-l.out:
		case <-l.closed:
			// What came before the close.
			for {
				select {
				case f = <-l.out:
					vt.take(f)
					continue
				default:
				}
				return
			}
		case <-stop:
			return
		}
		vt.take(f)
	}
}

func (vt *vpnTester) take(f linkFrame) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	if f.text {
		var ctl protocol.VPNControl
		if err := json.Unmarshal(f.b, &ctl); err != nil {
			vt.t.Errorf("a control that does not read: %v", err)
		}
		vt.controls = append(vt.controls, ctl)
		return
	}
	items, err := protocol.ReadVPNFrame(f.b)
	if err != nil {
		vt.t.Errorf("a frame that does not read: %v", err)
		return
	}
	var acks []any
	for _, raw := range items {
		switch protocol.ItemType(raw) {
		case protocol.ItemTunnelReply:
			var r protocol.TunnelReplyItem
			mustRead(vt.t, raw, &r)
			vt.replies[r.Stream] = r
		case protocol.ItemTunnelData:
			var d protocol.TunnelDataItem
			mustRead(vt.t, raw, &d)
			if d.Off == vt.next[d.Stream] && !vt.eof[d.Stream] {
				vt.data[d.Stream] = append(vt.data[d.Stream], d.Data...)
				vt.next[d.Stream] += uint64(len(d.Data))
				if d.EOF {
					vt.eof[d.Stream] = true
					vt.next[d.Stream]++
				}
			}
			room := uint64(protocol.DefaultTunnelRoom)
			acks = append(acks, protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: vt.task, Stream: d.Stream, Next: vt.next[d.Stream], Room: &room})
		case protocol.ItemTunnelAck:
			var a protocol.TunnelAckItem
			mustRead(vt.t, raw, &a)
			vt.acks[a.Stream] = a
		case protocol.ItemTunnelClose:
			var c protocol.TunnelCloseItem
			mustRead(vt.t, raw, &c)
			if _, ok := vt.closes[c.Stream]; !ok {
				// A late ack of the client's gets "no such stream": the first
				// close is the stream's.
				vt.closes[c.Stream] = c.Reason
			}
		case protocol.ItemTunnelDatagram:
			var d protocol.TunnelDatagramItem
			mustRead(vt.t, raw, &d)
			vt.dgrams = append(vt.dgrams, d)
		default:
			vt.t.Errorf("core sent item %d", protocol.ItemType(raw))
		}
	}
	if len(acks) > 0 && !vt.holdAcks {
		go vt.send(acks...)
	}
}

func mustRead(t *testing.T, raw cbor.RawMessage, v interface{ Validate() error }) {
	t.Helper()
	if err := read(raw, v); err != nil {
		t.Errorf("%T does not read: %v", v, err)
	}
}

// send sends core items in a frame.
func (vt *vpnTester) send(items ...any) {
	raws := make([]cbor.RawMessage, 0, len(items))
	for _, it := range items {
		b, err := protocol.MarshalCBOR(it)
		if err != nil {
			vt.t.Error(err)
			return
		}
		raws = append(raws, b)
	}
	frame, err := protocol.VPNFrame(raws)
	if err != nil {
		vt.t.Error(err)
		return
	}
	vt.mu.Lock()
	l := vt.link
	vt.mu.Unlock()
	select {
	case l.in <- linkFrame{b: frame}:
	case <-l.closed:
	}
}

func (vt *vpnTester) open(stream uint64, host string, port int) {
	vt.send(protocol.TunnelItem{T: protocol.ItemTunnel, Task: vt.task, Stream: stream, Host: host, Port: uint64(port)})
}

func (vt *vpnTester) reply(stream uint64) protocol.TunnelReplyItem {
	vt.t.Helper()
	var r protocol.TunnelReplyItem
	eventually(vt.t, "core's reply", func() bool {
		vt.mu.Lock()
		defer vt.mu.Unlock()
		var ok bool
		r, ok = vt.replies[stream]
		return ok
	})
	return r
}

func (vt *vpnTester) got(stream uint64) ([]byte, bool) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	return bytes.Clone(vt.data[stream]), vt.eof[stream]
}

func (vt *vpnTester) closed(stream uint64) (int, bool) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	r, ok := vt.closes[stream]
	return r, ok
}

// echoAt is an echo host's port, reached at 10.9.0.5 (the minion's
// redirect).
func echoAt(t *testing.T) int {
	t.Helper()
	_, port := splitAddr(t, echo(t))
	return port
}

// A client's stream reaches a host through the minion: its bytes come back
// from an echo host, both ends close, and core's two legs end done, the
// stream listed as the client's.
func TestVPNCarriesAStream(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, true, true, 0)
	vt := v.connect(t, v.key(t), 1)
	if w := vt.welcome; w.Task != v.task.String() || len(w.Routes) != 1 || w.Routes[0] != "10.9.0.0/24" || w.Client != "vpn: alice@kali" ||
		w.MaxStreams != DefaultMaxStreams || strings.Join(w.Opens, ",") != "tcp,udp,icmp" || w.Session != 12 {
		t.Fatalf("welcome: %+v", w)
	}
	port := echoAt(t)
	vt.open(1, "10.9.0.5", port)
	if r := vt.reply(1); r.Result != protocol.TunnelOpen {
		t.Fatalf("reply: %+v", r)
	}
	sent := bytes.Repeat([]byte("vpn bytes "), 60000)
	for off := 0; off < len(sent); off += protocol.MaxTunnelData {
		end := min(off+protocol.MaxTunnelData, len(sent))
		d := protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: vt.task, Stream: 1, Off: uint64(off), Data: sent[off:end], EOF: end == len(sent)}
		// Within the room core gives: it acks as it writes.
		eventually(t, "room", func() bool {
			vt.mu.Lock()
			defer vt.mu.Unlock()
			a, ok := vt.acks[1]
			room := uint64(protocol.DefaultTunnelRoom)
			if ok && a.Room != nil {
				room = *a.Room
			}
			return uint64(end) <= a.Next+room
		})
		vt.send(d)
	}
	eventually(t, "the echo back, and its eof", func() bool {
		got, eof := vt.got(1)
		return eof && bytes.Equal(got, sent)
	})
	eventually(t, "the stream's close", func() bool {
		r, ok := vt.closed(1)
		return ok && r == protocol.TunnelCloseDone
	})
	rec := v.stream(1)
	if rec.Client != "vpn: alice@kali" || rec.End != models.TunnelEndDone || rec.Up != int64(len(sent)) || rec.Down != int64(len(sent)) {
		t.Errorf("record: %+v", rec)
	}
	_, clients, _ := v.svc.VPN(v.task)
	if len(clients) != 1 || !clients[0].Connected || clients[0].Streams != 1 || clients[0].Up != int64(len(sent)) || clients[0].From != "203.0.113.5" {
		t.Errorf("clients: %+v", clients)
	}
}

// What the tunnel does not take is answered, never opened: an address out
// of the routes, a name, UDP or pings its command does not open, another
// kind of open; a stream opened again gets its reply again, one that went a
// close.
func TestVPNRefuses(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, false, false, 0)
	vt := v.connect(t, v.key(t), 1)
	vt.open(1, "10.8.0.5", 445)
	vt.open(2, "db.local", 445)
	vt.send(protocol.TunnelItem{T: protocol.ItemTunnel, Task: vt.task, Stream: 3, UDP: true})
	vt.send(protocol.TunnelItem{T: protocol.ItemTunnel, Task: vt.task, Stream: 4, Kind: protocol.TunnelKindICMP})
	vt.send(protocol.TunnelItem{T: protocol.ItemTunnel, Task: vt.task, Stream: 5, Kind: protocol.TunnelKindUnix, Path: "/var/run/x"})
	for stream, want := range map[uint64]int{1: protocol.TunnelDenied, 2: protocol.TunnelUnsupported, 3: protocol.TunnelUnsupported,
		4: protocol.TunnelUnsupported, 5: protocol.TunnelUnsupported} {
		if r := vt.reply(stream); r.Result != want {
			t.Errorf("stream %d: %+v, want %d", stream, r, want)
		}
	}
	vt.mu.Lock()
	delete(vt.replies, 1)
	vt.mu.Unlock()
	vt.open(1, "10.8.0.5", 445)
	if r := vt.reply(1); r.Result != protocol.TunnelDenied {
		t.Errorf("asked again: %+v", r)
	}
	// Nothing reached the minion.
	v.minion.mu.Lock()
	opens := len(v.minion.opens)
	v.minion.mu.Unlock()
	if opens != 0 {
		t.Errorf("the minion opened %d", opens)
	}
	// Bytes of a stream core has not: a close.
	vt.send(protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: vt.task, Stream: 9, Data: []byte("x")})
	eventually(t, "a close for a stream core has not", func() bool {
		r, ok := vt.closed(9)
		return ok && r == protocol.TunnelCloseUnknown
	})
	// A refused connect is the minion's answer: refused.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	vt.open(10, "10.9.0.5", port)
	if r := vt.reply(10); r.Result != protocol.TunnelRefused {
		t.Errorf("a refused connect: %+v", r)
	}
}

// Keys: one a client took is that client's alone; a used one goes when its
// client disconnects; a lapsed or unknown one is refused, as a hello of
// another link; an operator no longer one is refused, and ended once
// connected.
func TestVPNKeys(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, false, false, 0)
	ctx := context.Background()
	key := v.key(t)
	vt := v.connect(t, key, 1)
	if _, _, err := v.svc.ConnectVPN(ctx, key, hello(2), "x"); !errors.Is(err, ErrVPNRefused) {
		t.Errorf("another instance with a taken key: %v", err)
	}
	// The same instance again, its link dropped: the same client.
	c2, _, err := v.svc.ConnectVPN(ctx, key, hello(1), "x")
	if err != nil || c2 != vt.c {
		t.Fatalf("the same instance again: %v", err)
	}
	if _, _, err := v.svc.ConnectVPN(ctx, "lgv_000000000000_nope", hello(3), "x"); !errors.Is(err, ErrVPNRefused) {
		t.Errorf("an unknown key: %v", err)
	}
	bad := hello(3)
	bad.Link = 9
	if _, _, err := v.svc.ConnectVPN(ctx, v.key(t), bad, "x"); !errors.Is(err, ErrVPNHello) {
		t.Errorf("another link's hello: %v", err)
	}
	lapsed := v.key(t)
	keyID, _, _ := strings.Cut(strings.TrimPrefix(lapsed, protocol.VPNKeyPrefix), "_")
	v.keys.mu.Lock()
	past := time.Now().Add(-time.Minute)
	v.keys.keys[keyID].LapsesAt = &past
	v.keys.mu.Unlock()
	if _, _, err := v.svc.ConnectVPN(ctx, lapsed, hello(4), "x"); !errors.Is(err, ErrVPNRefused) {
		t.Errorf("a lapsed key: %v", err)
	}
	// Bye: the client ends, its key goes.
	vt.mu.Lock()
	l := vt.link
	vt.mu.Unlock()
	l.in <- linkFrame{text: true, b: []byte(`{"bye":true}`)}
	eventually(t, "the client gone", func() bool {
		_, clients, _ := v.svc.VPN(v.task)
		return len(clients) == 0
	})
	if _, _, err := v.svc.ConnectVPN(ctx, key, hello(1), "x"); !errors.Is(err, ErrVPNRefused) {
		t.Errorf("a key its client disconnected: %v", err)
	}
	// An operator no longer one.
	v.roles.mu.Lock()
	v.roles.no[v.user] = true
	v.roles.mu.Unlock()
	if _, _, err := v.svc.ConnectVPN(ctx, v.key(t), hello(5), "x"); !errors.Is(err, ErrVPNRefused) || !strings.Contains(err.Error(), "no longer an operator") {
		t.Errorf("an operator no longer one: %v", err)
	}
}

// A client whose link drops keeps its streams for another link: the
// minion's bytes it did not ack go again, nothing lost; past the grace,
// core ends it.
func TestVPNReconnectLosesNothing(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, false, false, 0)
	vt := v.connect(t, v.key(t), 1)
	port := echoAt(t)
	vt.open(1, "10.9.0.5", port)
	if r := vt.reply(1); r.Result != protocol.TunnelOpen {
		t.Fatalf("reply: %+v", r)
	}
	// The client takes the echo, but its acks never reach core.
	vt.mu.Lock()
	vt.holdAcks = true
	vt.mu.Unlock()
	vt.send(protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: vt.task, Stream: 1, Data: []byte("first")})
	eventually(t, "the echo", func() bool {
		got, _ := vt.got(1)
		return string(got) == "first"
	})
	// The link drops; what the client had of it is lost too.
	vt.mu.Lock()
	old := vt.link
	close(vt.stop)
	vt.data[1], vt.next[1], vt.holdAcks = nil, 0, false
	vt.mu.Unlock()
	old.Close(1006, "")
	eventually(t, "the client kept, its link down", func() bool {
		_, clients, _ := v.svc.VPN(v.task)
		return len(clients) == 1 && !clients[0].Connected
	})
	if _, _, err := v.svc.ConnectVPN(context.Background(), "", hello(1), "x"); err == nil {
		t.Fatal("a client with no key")
	}
	vt.attach(newTestLink())
	eventually(t, "the bytes again", func() bool {
		got, _ := vt.got(1)
		return string(got) == "first"
	})
	vt.send(protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: vt.task, Stream: 1, Off: 5, Data: []byte(" second"), EOF: true})
	eventually(t, "the rest, and the eof", func() bool {
		got, eof := vt.got(1)
		return eof && string(got) == "first second"
	})
	// Past the grace with no link, it ends.
	vt.mu.Lock()
	l := vt.link
	vt.mu.Unlock()
	l.Close(1006, "")
	eventually(t, "the client ended", func() bool {
		_, clients, _ := v.svc.VPN(v.task)
		return len(clients) == 0
	})
	eventually(t, "its key dropped", func() bool { return v.keys.count() == 0 })
}

// UDP flows and pings go as datagrams: what answers comes back to the
// client's stream, a ping's echo reply its own.
func TestVPNDatagrams(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, true, true, 0)
	vt := v.connect(t, v.key(t), 1)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(append([]byte("re: "), buf[:n]...), from)
		}
	}()
	udpPort := pc.LocalAddr().(*net.UDPAddr).Port
	vt.send(protocol.TunnelItem{T: protocol.ItemTunnel, Task: vt.task, Stream: 1, UDP: true},
		protocol.TunnelItem{T: protocol.ItemTunnel, Task: vt.task, Stream: 2, Kind: protocol.TunnelKindICMP})
	if r := vt.reply(1); r.Result != protocol.TunnelOpen {
		t.Fatalf("udp: %+v", r)
	}
	if r := vt.reply(2); r.Result != protocol.TunnelOpen {
		t.Fatalf("icmp: %+v", r)
	}
	echoReq := protocol.ICMPMessage{Type: protocol.ICMPEchoRequest, ID: 7, Seq: 1, Payload: []byte("ping")}
	vt.send(protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: vt.task, Stream: 1, Seq: 0, Host: "10.9.0.5", Port: uint64(udpPort), Data: []byte("dns?")},
		// Out of the routes: dropped.
		protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: vt.task, Stream: 1, Seq: 1, Host: "10.8.0.5", Port: 53, Data: []byte("no")},
		protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: vt.task, Stream: 2, Seq: 0, Host: "10.9.0.5", Data: echoReq.Bytes(false)},
		// No echo request: dropped.
		protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: vt.task, Stream: 2, Seq: 1, Host: "10.9.0.5",
			Data: protocol.ICMPMessage{Type: protocol.ICMPEchoReply, Seq: 2}.Bytes(false)})
	eventually(t, "the UDP answer and the echo reply", func() bool {
		vt.mu.Lock()
		defer vt.mu.Unlock()
		var udp, ping bool
		for _, d := range vt.dgrams {
			switch d.Stream {
			case 1:
				udp = udp || (string(d.Data) == "re: dns?" && d.Port == uint64(udpPort))
			case 2:
				m, err := protocol.ReadICMP(d.Data, false)
				ping = ping || (err == nil && m.Type == protocol.ICMPEchoReply && m.Seq == 1 && string(m.Payload) == "ping" && d.Host == "10.9.0.5")
			}
		}
		return udp && ping && len(vt.dgrams) == 2
	})
	v.minion.mu.Lock()
	for _, o := range v.minion.opens {
		if o.Stream != 0 && o.Kind == protocol.TunnelKindICMP && (o.Host != "" || o.UDP) {
			t.Errorf("an icmp open: %+v", o)
		}
	}
	v.minion.mu.Unlock()
	// The client's close ends the minion's stream too.
	vt.send(protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: vt.task, Stream: 1, Reason: protocol.TunnelCloseDone})
	eventually(t, "the minion's association closed", func() bool {
		v.minion.mu.Lock()
		defer v.minion.mu.Unlock()
		return len(v.minion.closed) == 1
	})
}

// Routes changed go to every client at once; Disconnect and the task's end
// end clients, saying why; a connected client holds stop_idle's clock.
func TestVPNRoutesAndEnds(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, false, false, 0)
	vt := v.connect(t, v.key(t), 1)
	got, err := v.svc.SetRoutes(context.Background(), v.task, []string{"10.9.0.0/24", " 172.16.0.0/16"})
	if err != nil || strings.Join(got, ",") != "10.9.0.0/24,172.16.0.0/16" {
		t.Fatalf("routes: %v %v", got, err)
	}
	eventually(t, "the routes said", func() bool {
		vt.mu.Lock()
		defer vt.mu.Unlock()
		return len(vt.controls) == 1 && len(vt.controls[0].Routes) == 2
	})
	if _, err := v.svc.SetRoutes(context.Background(), v.task, []string{"0.0.0.0/0"}); err == nil {
		t.Error("a default route")
	}
	if rec, _ := v.tunnels.FindByTaskID(context.Background(), v.task); len(rec.Routes) != 2 {
		t.Errorf("kept: %+v", rec.Routes)
	}
	// Disconnect.
	if err := v.svc.DisconnectVPN(v.task, vt.c.hello.Instance, "bob"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the end said, and the link closed", func() bool {
		_, _, closed := vt.link.closedWith()
		vt.mu.Lock()
		defer vt.mu.Unlock()
		n := len(vt.controls)
		return closed && n > 0 && vt.controls[n-1].End == "disconnected by bob"
	})
	// The task's end.
	vt2 := v.connect(t, v.key(t), 2)
	v.svc.Close(context.Background(), v.task)
	eventually(t, "the task's end said", func() bool {
		vt2.mu.Lock()
		defer vt2.mu.Unlock()
		n := len(vt2.controls)
		return n > 0 && vt2.controls[n-1].End == "the tunnel's task ended"
	})
	if _, ok := v.tunnels.recs[v.task]; ok || v.keys.count() != 0 {
		t.Error("the tunnel or its keys stayed")
	}
	// The Ports page heard it open, its machines come and go, and close.
	var opened, updated, closed int
	v.bus.mu.Lock()
	for _, e := range v.bus.events {
		if p, ok := e.Payload.(eventbus.VPNTunnelEventPayload); ok && p.TaskID == v.task.String() {
			switch {
			case p.Opened:
				opened++
			case p.Closed:
				closed++
			default:
				updated++
			}
		}
	}
	v.bus.mu.Unlock()
	if opened != 1 || closed != 1 || updated == 0 {
		t.Errorf("VPN events: %d opened, %d updated, %d closed", opened, updated, closed)
	}
}

// A connected client holds the idle clock; with none, stop_idle stops the
// task.
func TestVPNIdle(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, false, false, 0)
	v.svc.mu.Lock()
	tn := v.svc.tunnels[v.task]
	tn.stopIdle = 150 * time.Millisecond
	v.svc.mu.Unlock()
	vt := v.connect(t, v.key(t), 1)
	time.Sleep(400 * time.Millisecond)
	if task, _ := v.tasks.FindByTaskID(context.Background(), v.task); task.CancelRequestedAt != nil {
		t.Fatal("stopped with a client connected")
	}
	vt.send()
	_ = v.svc.DisconnectVPN(v.task, vt.c.hello.Instance, "bob")
	eventually(t, "stopped once no client is", func() bool {
		task, _ := v.tasks.FindByTaskID(context.Background(), v.task)
		return task.CancelRequestedAt != nil && strings.Contains(task.CancelReason, "stop_idle")
	})
}

// A restart takes the VPN tunnel up again: its numbers on, a client
// connecting again with its key.
func TestVPNRestart(t *testing.T) {
	v := newVPNRig(t, []string{"10.9.0.0/24"}, false, false, 0)
	key := v.key(t)
	vt := v.connect(t, key, 1)
	vt.open(1, "10.9.0.5", echoAt(t))
	if r := vt.reply(1); r.Result != protocol.TunnelOpen {
		t.Fatalf("reply: %+v", r)
	}
	v.svc.Stop()
	if code, _, closed := vt.link.closedWith(); !closed || code != 1001 {
		t.Fatalf("the link at a stop: %d %v", code, closed)
	}
	c := DefaultConfig()
	svc := New(c, v.ports, v.tasks, v.bus, zap.NewNop()).WithVPN(v.tunnels, v.keys, v.roles)
	svc.Start(context.Background())
	defer svc.Stop()
	c2, w, err := svc.ConnectVPN(context.Background(), key, hello(1), "x")
	if err != nil || w.Routes[0] != "10.9.0.0/24" {
		t.Fatalf("again: %v", err)
	}
	if sum, ok := svc.Summary(v.task); !ok || sum.Streams != 1 {
		t.Errorf("its numbers: %+v", sum)
	}
	_ = c2
	_ = strconv.Itoa
}
