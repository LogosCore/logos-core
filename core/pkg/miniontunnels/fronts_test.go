package miniontunnels

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// dialPort connects to one of the rig's ports.
func dialPort(t *testing.T, port int) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	return c
}

// readHead reads a response's head, to its blank line.
func readHead(t *testing.T, c net.Conn) string {
	t.Helper()
	var b []byte
	one := make([]byte, 1)
	for !bytes.HasSuffix(b, []byte("\r\n\r\n")) {
		if _, err := c.Read(one); err != nil {
			t.Fatalf("a head that does not end: %q (%v)", b, err)
		}
		b = append(b, one[0])
	}
	return string(b)
}

func httpFront(o *Opening) { o.Purpose = models.OperationalPurposeHTTP }

// An HTTP proxy's CONNECT is a stream to its host and port, said open with
// 200 once the minion opened it, then its bytes both ways. Its port has no
// UDP relay.
func TestHTTPConnect(t *testing.T) {
	host, port := splitAddr(t, echo(t))
	r := newRig(t, 0, nil, httpFront)
	r.tasks.ack(r.task)
	startMinion(t, r)
	c := dialPort(t, r.port)
	dest := net.JoinHostPort(host, strconv.Itoa(port))
	_, _ = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", dest, dest)
	if head := readHead(t, c); !strings.HasPrefix(head, "HTTP/1.1 200 ") {
		t.Fatalf("answered %q", head)
	}
	_ = c.SetDeadline(time.Time{})
	roundTrip(t, c, 128<<10)
	eventually(t, "the stream ends done", func() bool { return r.stream(1).End == models.TunnelEndDone })
	if rec := r.stream(1); rec.Host != host || rec.Port != port {
		t.Errorf("record %+v", rec)
	}
	r.svc.mu.Lock()
	relay := r.svc.tunnels[r.task].relay
	r.svc.mu.Unlock()
	if relay != nil {
		t.Error("an HTTP proxy with a UDP relay")
	}
}

// A plain request goes to its host with its target a path, its proxy
// headers dropped, and Connection: close; what the host answers comes back
// as it came.
func TestHTTPPlainRequest(t *testing.T) {
	type seen struct {
		uri, proxyConn, hop string
		close               bool
	}
	got := make(chan seen, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got <- seen{uri: req.RequestURI, proxyConn: req.Header.Get("Proxy-Connection"), hop: req.Header.Get("X-Hop"), close: req.Close}
		_, _ = io.WriteString(w, "hello from "+req.URL.Path)
	}))
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	r := newRig(t, 0, nil, httpFront)
	r.tasks.ack(r.task)
	startMinion(t, r)
	c := dialPort(t, r.port)
	_, _ = fmt.Fprintf(c, "GET http://%s/path?q=1 HTTP/1.1\r\nHost: %s\r\nProxy-Connection: keep-alive\r\nConnection: X-Hop\r\nX-Hop: 1\r\n\r\n",
		addr, addr)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "hello from /path" {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	s := <-got
	if s.uri != "/path?q=1" || s.proxyConn != "" || s.hop != "" || !s.close {
		t.Errorf("the host got %+v", s)
	}
}

// What the minion could not open is answered as a proxy would, and what is
// not a request a proxy takes, a bad request.
func TestHTTPRefused(t *testing.T) {
	r := newRig(t, 0, nil, httpFront)
	r.tasks.ack(r.task)
	startMinion(t, r)
	c := dialPort(t, r.port)
	_, _ = fmt.Fprintf(c, "CONNECT 127.0.0.1:%d HTTP/1.1\r\nHost: x\r\n\r\n", freePort(t))
	if head := readHead(t, c); !strings.HasPrefix(head, "HTTP/1.1 502 ") {
		t.Errorf("a refused destination: %q", head)
	}
	bad := dialPort(t, r.port)
	_, _ = io.WriteString(bad, "GET /no-host HTTP/1.1\r\nHost: x\r\n\r\n")
	if head := readHead(t, bad); !strings.HasPrefix(head, "HTTP/1.1 400 ") {
		t.Errorf("a request with no URL: %q", head)
	}
}

// A plain request's head is what its host takes: a path, a proxy's own
// headers and what Connection names dropped, its body framed as it came;
// CONNECT names a host and a port.
func TestHTTPTarget(t *testing.T) {
	read := func(s string) *http.Request {
		t.Helper()
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(s)))
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	host, port, head, err := httpTarget(read("POST http://web.corp:8080/a?b=1 HTTP/1.1\r\nHost: other\r\nProxy-Authorization: Basic x\r\n" +
		"Connection: keep-alive, X-Hop\r\nX-Hop: 1\r\nTransfer-Encoding: chunked\r\nX-Keep: 2\r\n\r\n"))
	h := string(head)
	if err != nil || host != "web.corp" || port != 8080 || !strings.HasPrefix(h, "POST /a?b=1 HTTP/1.1\r\nHost: web.corp:8080\r\n") ||
		strings.Contains(h, "Proxy-") || strings.Contains(h, "X-Hop") || !strings.Contains(h, "Connection: close\r\n") ||
		!strings.Contains(h, "Transfer-Encoding: chunked\r\n") || !strings.Contains(h, "X-Keep: 2\r\n") || !strings.HasSuffix(h, "\r\n\r\n") {
		t.Errorf("%s %d %q %v", host, port, h, err)
	}
	if _, port, _, err := httpTarget(read("GET http://web.corp/ HTTP/1.1\r\nHost: web.corp\r\n\r\n")); err != nil || port != 80 {
		t.Errorf("http's port: %d %v", port, err)
	}
	if host, port, head, err := httpTarget(read("CONNECT dc01:443 HTTP/1.1\r\nHost: dc01:443\r\n\r\n")); err != nil || host != "dc01" ||
		port != 443 || head != nil {
		t.Errorf("CONNECT: %s %d %v", host, port, err)
	}
	for _, bad := range []string{"CONNECT dc01 HTTP/1.1\r\n\r\n", "CONNECT dc01:0 HTTP/1.1\r\n\r\n",
		"GET https://web.corp/ HTTP/1.1\r\nHost: web.corp\r\n\r\n", "GET /a HTTP/1.1\r\nHost: web.corp\r\n\r\n"} {
		if _, _, _, err := httpTarget(read(bad)); err == nil {
			t.Errorf("%q taken", bad)
		}
	}
	for result, want := range map[int]int{protocol.TunnelRefused: 502, protocol.TunnelTimedOut: 504, protocol.TunnelNotAllowed: 503,
		protocol.TunnelUnsupported: 501, protocol.TunnelDenied: 403, 42: 502} {
		if got, why := httpStatus(result); got != want || why == "" {
			t.Errorf("result %d: %d %q", result, got, why)
		}
	}
}

// A forward's client may speak first: its bytes go with the stream's open,
// before the minion answers it (here no minion ever does).
func TestForwardSendsWithItsOpen(t *testing.T) {
	host, port := splitAddr(t, echo(t))
	r := newRig(t, 0, nil, forwardTo(host, port))
	r.tasks.ack(r.task)
	c := dialPort(t, r.port)
	if _, err := io.WriteString(c, "hello"); err != nil {
		t.Fatal(err)
	}
	opened, data := false, ""
	eventually(t, "the open and its bytes", func() bool {
		r.svc.Fill(context.Background(), r.session, 1<<20, protocol.MaxMinionMessageItems, func(it cbor.RawMessage) bool {
			switch protocol.ItemType(it) {
			case protocol.ItemTunnel:
				opened = true
			case protocol.ItemTunnelData:
				var d protocol.TunnelDataItem
				if protocol.UnmarshalCBOR(it, &d) == nil && d.Stream == 1 && d.Off == 0 && opened {
					data = string(d.Data)
				}
			}
			return true
		})
		return data == "hello"
	})
	if rec := r.stream(1); rec.Result != nil {
		t.Errorf("answered: %+v", rec)
	}
}

// Core numbers a port's streams from blocks of StreamBlock, one write a
// block; a restart goes on past the last block, so no number is used
// twice; and it counts the streams it opened apart, as its task shows.
func TestStreamBlocks(t *testing.T) {
	host, port := splitAddr(t, echo(t))
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	startMinion(t, r)
	for range 3 {
		c, code := socks5(t, r.port, host, port)
		if code != socks5Succeeded {
			t.Fatalf("reply %d", code)
		}
		_ = c.Close()
	}
	r.svc.mu.Lock()
	tn := r.svc.tunnels[r.task]
	last, opened := tn.last, tn.opened
	r.svc.mu.Unlock()
	r.ports.mu.Lock()
	reserved, block := r.ports.reserved, r.ports.recs[r.task].Streams
	r.ports.mu.Unlock()
	if last != 3 || opened != 3 || reserved != 1 || block != StreamBlock {
		t.Fatalf("last %d, opened %d, %d blocks to %d", last, opened, reserved, block)
	}
	r.svc.Stop()
	again := New(r.svc.cfg, r.ports, r.tasks, nil, zap.NewNop())
	again.Start(context.Background())
	defer again.Stop()
	again.mu.Lock()
	tn = again.tunnels[r.task]
	again.mu.Unlock()
	id, err := again.nextNumber(tn)
	if err != nil || id != StreamBlock+1 {
		t.Errorf("after a restart: %d %v", id, err)
	}
	if sum, _ := again.Summary(r.task); sum.Streams != 3 {
		t.Errorf("counted %d", sum.Streams)
	}
}

// A SOCKS proxy takes UDP ASSOCIATE only through a command that opens udp:
// with none, the port has no relay and an association is not supported.
func TestSOCKSUDPOnlyWhenOpened(t *testing.T) {
	r := newRig(t, 0, nil, func(o *Opening) { o.UDP = false })
	r.tasks.ack(r.task)
	startMinion(t, r)
	r.svc.mu.Lock()
	relay := r.svc.tunnels[r.task].relay
	r.svc.mu.Unlock()
	if relay != nil {
		t.Fatal("a relay with no udp opened")
	}
	c := dialPort(t, r.port)
	_, _ = c.Write([]byte{5, 1, 0})
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil || head[1] != 0 {
		t.Fatalf("method % x %v", head, err)
	}
	_, _ = c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != socks5CommandNotSupported {
		t.Errorf("UDP ASSOCIATE answered % x %v", rep, err)
	}
}

// A capped tunnel goes no faster than its cap, each way, over its streams
// together; SetCap changes it live and keeps it with its port.
func TestCapPaces(t *testing.T) {
	host, port := splitAddr(t, echo(t))
	r := newRig(t, 0, nil, func(o *Opening) { o.Cap = 256 << 10 })
	r.tasks.ack(r.task)
	startMinion(t, r)
	c, code := socks5(t, r.port, host, port)
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	start := time.Now()
	roundTrip(t, c, 768<<10)
	if took := time.Since(start); took < 1500*time.Millisecond {
		t.Errorf("768 KiB each way at 256 KiB/s in %v", took)
	}
	if err := r.svc.SetCap(context.Background(), r.task, 0); err != nil {
		t.Fatal(err)
	}
	if sum, _ := r.svc.Summary(r.task); sum.Cap != 0 {
		t.Errorf("cap after SetCap: %d", sum.Cap)
	}
	if p, _ := r.ports.FindByTaskID(context.Background(), r.task); p.Cap != 0 {
		t.Errorf("kept cap %d", p.Cap)
	}
	c, _ = socks5(t, r.port, host, port)
	start = time.Now()
	roundTrip(t, c, 768<<10)
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("uncapped, 768 KiB each way in %v", took)
	}
	if err := r.svc.SetCap(context.Background(), uuid.New(), 1); err == nil {
		t.Error("a cap set for no tunnel")
	}
}

// A capped tunnel whose minion sends, and core little, still goes at its
// cap: an ack that gave no room is followed by one that gives it once the
// cap allows, not by the minion's probe (its own resend, seconds later).
func TestCapPacesTheMinion(t *testing.T) {
	big := make([]byte, 512<<10)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = c.Write(big)
	}()
	host, port := splitAddr(t, ln.Addr().String())
	r := newRig(t, 0, nil, func(o *Opening) { o.Cap = 128 << 10 })
	r.tasks.ack(r.task)
	startMinion(t, r)
	c, code := socks5(t, r.port, host, port)
	if code != socks5Succeeded {
		t.Fatalf("reply %d", code)
	}
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	start := time.Now()
	got, err := io.ReadAll(c)
	took := time.Since(start)
	if err != nil || len(got) != len(big) {
		t.Fatalf("%d bytes of %d: %v", len(got), len(big), err)
	}
	// 512 KiB at 128 KiB/s, a second of it at once: about 3 s, not held
	// for the minion's probes.
	if took < 2*time.Second || took > 8*time.Second {
		t.Errorf("512 KiB at 128 KiB/s in %v", took)
	}
}

// A bucket holds a second of its cap (capBurst at least), fills at its
// rate, and says how long until it holds enough again.
func TestCapBucket(t *testing.T) {
	now := time.Unix(1000, 0)
	tn := &tunnel{cap: 100 << 10}
	if got := tn.outAllows(now); got != 100<<10 {
		t.Fatalf("a full bucket: %d", got)
	}
	tn.sentOut(150 << 10)
	if got := tn.outAllows(now); got != 0 {
		t.Errorf("past it: %d", got)
	}
	if wait := tn.out.until(minPiece, tn.cap); wait < 500*time.Millisecond || wait > 600*time.Millisecond {
		t.Errorf("until a piece again: %v", wait)
	}
	if got := tn.outAllows(now.Add(time.Second)); got != 50<<10 {
		t.Errorf("a second later: %d", got)
	}
	if tn.inAllowsNow(now) != 100<<10 {
		t.Fatal("a full bucket shut")
	}
	tn.tookIn(200<<10, now)
	if tn.inAllowsNow(now) != 0 || tn.inAllows(1, now) {
		t.Error("over the cap, open")
	}
	open := &tunnel{}
	if open.outAllows(now) < 1<<30 || open.inAllowsNow(now) < 1<<30 || !open.inAllows(1<<30, now) {
		t.Error("no cap, a limit")
	}
	st := &stream{t: tn, window: downWindow}
	if a := st.ackItem(now); *a.Room != 0 || !st.capHeld {
		t.Errorf("an ack over the cap: %d %v", *a.Room, st.capHeld)
	}
}

// A session's round trip, from its acks, sets how long core waits before
// it sends again: never less than ResendAfter, nor more than maxResend; a
// send again ends the round trip being measured (Karn).
func TestRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	s := New(cfg, &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}, &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}, nil, zap.NewNop())
	session := uuid.New()
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	s.mu.Lock()
	defer s.mu.Unlock()
	if got := s.resendAfter(session); got != cfg.ResendAfter {
		t.Errorf("with none measured: %v", got)
	}
	st := &stream{t: &tunnel{session: session}}
	st.measure(100, false, now)
	st.measure(200, false, now.Add(time.Second))
	if st.rttEnd != 100 {
		t.Fatalf("one at a time: %d", st.rttEnd)
	}
	now = now.Add(8 * time.Second)
	s.measured(st, 99)
	if !st.rttAt.IsZero() && s.rtt[session] != nil && s.rtt[session].have {
		t.Fatal("an ack short of it measured")
	}
	s.measured(st, 100)
	if got := s.resendAfter(session); got <= cfg.ResendAfter || !st.rttAt.IsZero() {
		t.Errorf("an 8 s round trip: wait %v", got)
	}
	st.measure(300, false, now)
	st.measure(300, true, now)
	if !st.rttAt.IsZero() {
		t.Error("a send again measured")
	}
	s.rttOf(session).sample(10 * time.Minute)
	if got := s.resendAfter(session); got != maxResend {
		t.Errorf("a slow link: %v", got)
	}
}

// A stream's window grows while the minion fills it and the client keeps
// up, to maxDownWindow and within the tunnel's maxTunnelExtra, and gives
// what it grew back as it goes.
func TestWindowGrows(t *testing.T) {
	tn := &tunnel{}
	st := &stream{t: tn, window: downWindow, downNext: 1 << 20, ackedEnd: 1<<20 + downWindow/16}
	if !st.grow() || st.window != 2*downWindow || tn.extra != downWindow || !st.ackDue {
		t.Fatalf("held back by its window: %d, extra %d", st.window, tn.extra)
	}
	st.down = []byte{1}
	if st.grow() {
		t.Error("grown with the client behind")
	}
	st.down, st.ackedEnd = nil, st.downNext+uint64(st.window)
	if st.grow() {
		t.Error("grown with room to spare")
	}
	st.ackedEnd = st.downNext
	for st.grow() {
	}
	if st.window != maxDownWindow {
		t.Errorf("grew to %d", st.window)
	}
	other := &stream{t: tn, window: downWindow, downNext: 10, ackedEnd: 10}
	tn.extra = maxTunnelExtra
	if other.grow() {
		t.Error("grown past the tunnel's extra")
	}
	tn.extra = maxDownWindow - downWindow
	st.shrink()
	if st.window != downWindow || tn.extra != 0 {
		t.Errorf("gave back: %d, extra %d", st.window, tn.extra)
	}
}

// countingWaker counts the sessions core wakes.
type countingWaker struct {
	mu sync.Mutex
	n  int
}

func (w *countingWaker) WakeTunnels(uuid.UUID) {
	w.mu.Lock()
	w.n++
	w.mu.Unlock()
}

func (w *countingWaker) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

// Core wakes a session itself when what it sent is to go again (no ack
// came): its message is made then, and sends it from the ack again. An
// earlier wake takes a later one's place.
func TestResendWakes(t *testing.T) {
	host, port := splitAddr(t, echo(t))
	w := &countingWaker{}
	ports := &fakePorts{recs: map[uuid.UUID]*models.OperationalPort{}}
	tasks := &fakeTasks{tasks: map[uuid.UUID]models.MinionTask{}}
	cfg := DefaultConfig()
	cfg.Ports, cfg.Bind, cfg.ResendAfter = []int{freePort(t)}, "127.0.0.1", 200*time.Millisecond
	svc := New(cfg, ports, tasks, nil, zap.NewNop()).WithWaker(w)
	svc.Start(context.Background())
	t.Cleanup(svc.Stop)
	task, session := uuid.New(), uuid.New()
	tasks.tasks[task] = models.MinionTask{TaskID: task, SessionID: session, State: models.MinionTaskSent, Action: protocol.ActionTunnel}
	p, err := svc.Open(context.Background(), Opening{Task: task, Session: session, Operation: uuid.New(), By: uuid.New(), Timeout: time.Second,
		Purpose: models.OperationalPurposeForward, Host: host, HostPort: port})
	if err != nil {
		t.Fatal(err)
	}
	tasks.ack(task)
	c := dialPort(t, p)
	_, _ = io.WriteString(c, "hello")
	fill := func() (sent int) {
		svc.Fill(context.Background(), session, 1<<20, protocol.MaxMinionMessageItems, func(it cbor.RawMessage) bool {
			if protocol.ItemType(it) == protocol.ItemTunnelData {
				sent++
			}
			return true
		})
		return sent
	}
	eventually(t, "its bytes go", func() bool { return fill() > 0 })
	time.Sleep(50 * time.Millisecond)
	before := w.count()
	eventually(t, "woken to send again", func() bool { return w.count() > before })
	// The open, then its bytes, go again, each once its wait passed.
	eventually(t, "its bytes sent again", func() bool { return fill() > 0 })
	other := uuid.New()
	svc.mu.Lock()
	now := svc.now()
	svc.wakeAt(other, now.Add(time.Hour))
	svc.wakeAt(other, now.Add(20*time.Millisecond))
	svc.wakeAt(other, now.Add(time.Hour))
	at := svc.rtt[other].wakeAt
	svc.mu.Unlock()
	if !at.Equal(now.Add(20 * time.Millisecond)) {
		t.Errorf("the wake set for %v", at)
	}
}

// An open of the minion's that core does not take as it is (a later
// version's key) is answered unsupported, and its number is used up: it
// never connects.
func TestReverseOpenUnsupported(t *testing.T) {
	r := newReverseRig(t, echo(t), 4)
	raw, err := protocol.MarshalCBOR(map[int]any{0: protocol.ItemTunnel, 1: r.task[:], 2: 1, 3: "10.0.0.9", 4: 50001, 7: true})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := r.svc.Take(context.Background(), r.session, raw); err != nil || len(out) != 0 {
		t.Fatalf("taken as %d items, %v", len(out), err)
	}
	if rep := r.replyFor(t, 1); rep.Result != protocol.TunnelUnsupported {
		t.Fatalf("reply %+v", rep)
	}
	if out := r.take(t, r.open(1)); len(closes(t, out)) != 1 {
		t.Errorf("its number again: %d items", len(out))
	}
	if sum, _ := r.svc.Summary(r.task); sum.Open != 0 {
		t.Errorf("open %d", sum.Open)
	}
	// One that reads as no tunnel at all, no task to answer by, is bad.
	none, _ := protocol.MarshalCBOR(map[int]any{0: protocol.ItemTunnel, 2: 2, 3: "h", 4: 1})
	if _, err := r.svc.Take(context.Background(), r.session, none); err == nil {
		t.Error("an open with no task taken")
	}
}

// unixEcho is a unix socket that sends back what it gets: its path.
func unixEcho(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo.sock")
	ln, err := net.Listen("unix", path)
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
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
				_ = c.(*net.UnixConn).CloseWrite()
			}()
		}
	}()
	return path
}

// A forward to a unix socket opens each stream to its path, its kind
// said: its bytes both ways, its record naming the path.
func TestForwardToAPath(t *testing.T) {
	path := unixEcho(t)
	r := newRig(t, 0, nil, func(o *Opening) {
		o.Purpose, o.Kind, o.Path = models.OperationalPurposeForward, protocol.TunnelKindUnix, path
	})
	r.tasks.ack(r.task)
	startMinion(t, r)
	c := dialPort(t, r.port)
	_ = c.SetDeadline(time.Time{})
	roundTrip(t, c, 64<<10)
	eventually(t, "the stream ends done", func() bool { return r.stream(1).End == models.TunnelEndDone })
	if rec := r.stream(1); rec.Path != path || rec.Host != "" || !strings.Contains(rec.SearchText, path) {
		t.Errorf("record %+v", rec)
	}
}

// A tunnel's TCP streams say its source address; a forward's, TLS when it
// says it; a path's, neither.
func TestOpensSayTheirOptions(t *testing.T) {
	r := newRig(t, 0, nil, func(o *Opening) {
		o.Purpose, o.Host, o.HostPort, o.Source, o.TLS = models.OperationalPurposeForward, "intranet.corp", 443, "10.0.0.7", true
	})
	r.tasks.ack(r.task)
	_ = dialPort(t, r.port)
	var open protocol.TunnelItem
	eventually(t, "the open", func() bool {
		r.svc.Fill(context.Background(), r.session, 1<<20, protocol.MaxMinionMessageItems, func(it cbor.RawMessage) bool {
			if protocol.ItemType(it) == protocol.ItemTunnel {
				o, err := protocol.ReadTunnel(it)
				if err != nil {
					t.Fatalf("an open that does not read: %v", err)
				}
				open = o
			}
			return true
		})
		return open.Stream != 0
	})
	if open.Host != "intranet.corp" || open.Port != 443 || open.Source != "10.0.0.7" || !open.TLS || open.Kind != 0 {
		t.Errorf("open %+v", open)
	}
	st := &stream{t: &tunnel{source: "10.0.0.7", tls: true, fwdKind: protocol.TunnelKindPipe}, id: 1, kind: protocol.TunnelKindPipe,
		rec: models.MinionTunnelStream{Path: `\\.\pipe\x`}}
	if it := st.openItem(); it.Kind != protocol.TunnelKindPipe || it.Path != `\\.\pipe\x` || it.Source != "" || it.TLS || it.Validate() != nil {
		t.Errorf("a pipe's open %+v", it)
	}
}
