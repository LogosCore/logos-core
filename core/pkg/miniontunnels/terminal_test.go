package miniontunnels

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// aTerminal makes the rig's tunnel a terminal's: program with args, in a
// terminal of 30 rows and 100 columns on its raw port.
func aTerminal(program string, args ...string) func(*Opening) {
	return func(o *Opening) {
		o.Purpose, o.Program, o.Args, o.Rows, o.Cols = models.OperationalPurposeTerminal, program, args, 30, 100
	}
}

// readUntil reads c until what it read holds want, or the deadline.
func readUntil(t *testing.T, c io.Reader, want string) string {
	t.Helper()
	var got []byte
	buf := make([]byte, 4<<10)
	for !bytes.Contains(got, []byte(want)) {
		n, err := c.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			t.Fatalf("read %q, then %v; want %q", got, err, want)
		}
	}
	return string(got)
}

// Each connection to a terminal's port is a process on the minion's host,
// the task's program in a terminal of the task's size: what the tool types
// goes to it, what it says comes back, and how it ended is its last line.
// The stream ends done once both ends did.
func TestTerminalRawPort(t *testing.T) {
	r := newRig(t, 0, nil, aTerminal("bash", "-l"))
	r.tasks.ack(r.task)
	m := startMinion(t, r)
	c := dialPort(t, r.port)
	if _, err := c.Write([]byte("id\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "id\n")
	m.mu.Lock()
	open := m.opens[0]
	m.mu.Unlock()
	if open.Kind != protocol.TunnelKindProcess || open.Path != "bash" || strings.Join(open.Args, " ") != "-l" || open.Rows != 30 || open.Cols != 100 ||
		open.Host != "" || open.Validate() != nil {
		t.Errorf("the open %+v", open)
	}
	if _, err := c.Write([]byte("exit 3\n")); err != nil {
		t.Fatal(err)
	}
	if rest, _ := io.ReadAll(c); !strings.Contains(string(rest), "[the process exited: 3]") {
		t.Errorf("its end: %q", rest)
	}
	_ = c.Close()
	eventually(t, "the stream ends done", func() bool { return r.stream(1).End == models.TunnelEndDone })
	rec := r.stream(1)
	if rec.Kind != protocol.TunnelKindProcess || rec.Program != "bash" || rec.Exit == nil || *rec.Exit != 3 || rec.Terminal == nil || !*rec.Terminal ||
		!strings.Contains(rec.SearchText, "bash") {
		t.Errorf("record %+v", rec)
	}
}

// A web terminal is a stream of the task's terminal of its own size: it
// resizes, signals its process (each ctl once, in order), and says how
// the process ended.
func TestTerminalWeb(t *testing.T) {
	r := newRig(t, 0, nil, aTerminal(""))
	r.tasks.ack(r.task)
	m := startMinion(t, r)
	tm, err := r.svc.OpenTerminal(r.task, "web: alice", 40, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	if _, err := tm.Write([]byte("whoami\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, tm, "whoami\n")
	if err := tm.Resize(50, 200); err != nil {
		t.Fatal(err)
	}
	if err := tm.Signal(protocol.TunnelSignalInterrupt); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the minion has both ctls", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.ctls) == 2
	})
	m.mu.Lock()
	open, ctls := m.opens[0], m.ctls
	m.mu.Unlock()
	if open.Path != "" || open.Rows != 40 || open.Cols != 120 {
		t.Errorf("the open %+v", open)
	}
	if ctls[0].Seq != 0 || ctls[0].Rows != 50 || ctls[0].Cols != 200 || ctls[1].Seq != 1 || ctls[1].Signal != protocol.TunnelSignalInterrupt {
		t.Errorf("ctls %+v", ctls)
	}
	if _, err := tm.Write([]byte("exit 0\n")); err != nil {
		t.Fatal(err)
	}
	if rest, _ := io.ReadAll(tm); !strings.Contains(string(rest), "[the process exited: 0]") {
		t.Errorf("its end: %q", rest)
	}
	_ = tm.CloseWrite()
	select {
	case <-tm.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the stream did not end")
	}
	end, exit, terminal := tm.Ended()
	if end != models.TunnelEndDone || exit == nil || *exit != 0 || terminal == nil || !*terminal {
		t.Errorf("ended %q %v %v", end, exit, terminal)
	}
	if rec := r.stream(tm.Stream()); rec.Client != "web: alice" || !strings.Contains(rec.SearchText, "shell") {
		t.Errorf("record %+v", rec)
	}
	if err := tm.Resize(10, 10); !errors.Is(err, errTerminalEnded) {
		t.Errorf("a resize once it ended: %v", err)
	}
}

// A ctl the minion lost goes again from its ack, and is taken once.
func TestTerminalCtlGoesAgain(t *testing.T) {
	r := newRig(t, 0, nil, aTerminal("sh"))
	r.tasks.ack(r.task)
	m := startMinion(t, r, func(m *fakeMinion) { m.dropCtl = 2 })
	tm, err := r.svc.OpenTerminal(r.task, "web", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	_, _ = tm.Write([]byte("x\n"))
	readUntil(t, tm, "x\n")
	if err := tm.Resize(25, 81); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the size, again", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.ctls) == 1
	})
	time.Sleep(3 * r.svc.cfg.ResendAfter)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ctls) != 1 || m.ctls[0].Rows != 25 || m.ctls[0].Cols != 81 {
		t.Errorf("ctls %+v", m.ctls)
	}
}

// A program that does not start says why in the terminal.
func TestTerminalDoesNotStart(t *testing.T) {
	r := newRig(t, 0, nil, aTerminal("missing"))
	r.tasks.ack(r.task)
	startMinion(t, r)
	c := dialPort(t, r.port)
	if got, _ := io.ReadAll(c); !strings.Contains(string(got), "[logos: missing did not start: not_found: no such program]") {
		t.Errorf("said %q", got)
	}
	eventually(t, "not opened", func() bool { return r.stream(1).End == models.TunnelEndNotOpened })
}

// A size not yet sent gives way to a newer one; a signal keeps its place;
// a stream holds so many ctls at most.
func TestTerminalCtlQueue(t *testing.T) {
	s := &Service{}
	st := &stream{t: &tunnel{task: uuid.New()}, id: 7}
	_ = s.queueCtl(st, protocol.TunnelCtlItem{Rows: 10, Cols: 10})
	_ = s.queueCtl(st, protocol.TunnelCtlItem{Rows: 20, Cols: 30})
	_ = s.queueCtl(st, protocol.TunnelCtlItem{Signal: protocol.TunnelSignalKill})
	_ = s.queueCtl(st, protocol.TunnelCtlItem{Rows: 40, Cols: 50})
	if len(st.ctlOut) != 3 || st.ctlOut[0].Rows != 20 || st.ctlOut[0].Seq != 0 || st.ctlOut[1].Signal != protocol.TunnelSignalKill ||
		st.ctlOut[2].Seq != 2 || st.ctlOut[2].Stream != 7 || st.ctlOut[2].Validate() != nil {
		t.Errorf("queue %+v", st.ctlOut)
	}
	// Sent: a newer size is a ctl of its own.
	st.ctlSent = 3
	_ = s.queueCtl(st, protocol.TunnelCtlItem{Rows: 41, Cols: 51})
	if len(st.ctlOut) != 4 {
		t.Errorf("a size after the sent one: %+v", st.ctlOut)
	}
	if !st.ctlAcked(2) || len(st.ctlOut) != 2 || st.ctlBase != 2 {
		// Not all acked: false.
		if st.ctlBase != 2 || len(st.ctlOut) != 2 {
			t.Errorf("acked to 2: base %d, %d left", st.ctlBase, len(st.ctlOut))
		}
	}
	if st.ctlAcked(9) || st.ctlAcked(1) {
		t.Error("an ack past what went, or below the last, taken")
	}
	for i := 0; i < maxCtlOut; i++ {
		_ = s.queueCtl(st, protocol.TunnelCtlItem{Signal: 1})
	}
	if err := s.queueCtl(st, protocol.TunnelCtlItem{Signal: 1}); !errors.Is(err, errCtlFull) {
		t.Errorf("a full queue: %v", err)
	}
}

// Only a terminal's task takes a web terminal, of a size a terminal has.
func TestOpenTerminalRefuses(t *testing.T) {
	r := newRig(t, 0, nil)
	if _, err := r.svc.OpenTerminal(r.task, "web", 24, 80); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("a SOCKS proxy's: %v", err)
	}
	if _, err := r.svc.OpenTerminal(uuid.New(), "web", 24, 80); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("no tunnel: %v", err)
	}
	term := newRig(t, 0, nil, aTerminal("sh"))
	if _, err := term.svc.OpenTerminal(term.task, "web", 0, 80); err == nil {
		t.Error("no rows")
	}
}

// socks5Bind asks the SOCKS proxy on port for a BIND, the peer it expects
// to come from 127.0.0.1:9: the connection, and the first reply.
func socks5Bind(t *testing.T, port int) (net.Conn, byte, string) {
	t.Helper()
	c := dialPort(t, port)
	if _, err := c.Write([]byte{5, 1, 0, 5, 2, 0, 1, 127, 0, 0, 1, 0, 9}); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil || head[1] != 0 {
		t.Fatalf("method % x %v", head, err)
	}
	code, addr := socksReply(t, c)
	return c, code, addr
}

// socksReply reads a SOCKS5 reply: its code and address.
func socksReply(t *testing.T, c net.Conn) (byte, string) {
	t.Helper()
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("a reply: %v", err)
	}
	size := 4
	if head[3] == 4 {
		size = 16
	}
	rest := make([]byte, size+2)
	if _, err := io.ReadFull(c, rest); err != nil {
		t.Fatalf("a reply's address: %v", err)
	}
	ip := net.IP(rest[:size])
	return head[1], net.JoinHostPort(ip.String(), strconv.Itoa(int(binary.BigEndian.Uint16(rest[size:]))))
}

// SOCKS5 BIND, where the command opens accept: the first reply says where
// the minion listens, the second who came, and the stream is theirs.
func TestSOCKSBind(t *testing.T) {
	r := newRig(t, 0, nil, func(o *Opening) { o.Accept = true })
	r.tasks.ack(r.task)
	startMinion(t, r)
	c, code, where := socks5Bind(t, r.port)
	if code != socks5Succeeded {
		t.Fatalf("first reply %d", code)
	}
	server, err := net.Dial("tcp", where)
	if err != nil {
		t.Fatalf("connect back to %s: %v", where, err)
	}
	defer server.Close()
	code, came := socksReply(t, c)
	if code != socks5Succeeded || came != server.LocalAddr().String() {
		t.Fatalf("second reply %d %s, want %s", code, came, server.LocalAddr())
	}
	if _, err := server.Write([]byte("220 ready\r\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "220 ready\r\n")
	if _, err := c.Write([]byte("PORT data\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = server.SetDeadline(time.Now().Add(10 * time.Second))
	readUntil(t, server, "PORT data\r\n")
	eventually(t, "who came, kept", func() bool { return r.stream(1).Came == came })
	if rec := r.stream(1); rec.Kind != protocol.TunnelKindAccept || rec.Host != "127.0.0.1" || rec.Port != 9 || rec.Bound != where {
		t.Errorf("record %+v", rec)
	}
}

// A command that opens no accept: BIND is not supported.
func TestSOCKSBindRefused(t *testing.T) {
	r := newRig(t, 0, nil)
	r.tasks.ack(r.task)
	startMinion(t, r)
	_, code, _ := socks5Bind(t, r.port)
	if code != socks5CommandNotSupported {
		t.Errorf("reply %d", code)
	}
}
