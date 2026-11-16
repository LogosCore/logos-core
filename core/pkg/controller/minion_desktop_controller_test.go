package controller

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	neturl "net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/desktops"
	"github.com/logoscore/logos-core/core/pkg/guacd"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"github.com/logoscore/logos-core/core/pkg/recordings/recordingstest"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// portsByTask are the tasks' operational ports.
type portsByTask map[uuid.UUID]int

func (p portsByTask) Port(_ context.Context, task uuid.UUID) (*models.OperationalPort, error) {
	n, ok := p[task]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &models.OperationalPort{Port: n, TaskID: task}, nil
}

// DesktopClient names nothing.
func (p portsByTask) DesktopClient(uuid.UUID, netip.Addr, string) func() { return func() {} }

// claimingPorts are portsByTask that keep what each desktop names guacd's
// connections, and count the names released.
type claimingPorts struct {
	portsByTask
	mu       sync.Mutex
	claims   []string
	released int
}

func (p *claimingPorts) DesktopClient(task uuid.UUID, from netip.Addr, client string) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = append(p.claims, task.String()+" "+from.String()+" "+client)
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.released++
	}
}

func (p *claimingPorts) named() ([]string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.claims), p.released
}

// credsByID are the operation's credentials, resolved as credentialrefs
// does: an id that names none is Unusable. One of type storeDown is not
// read: the store is not reached.
type credsByID map[string]protocol.Credential

const storeDown = "STORE_DOWN"

func (f credsByID) Resolve(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error) {
	out := map[string]protocol.Credential{}
	for _, id := range ids {
		c, ok := f[id.String()]
		switch {
		case !ok:
			return nil, nil, &credentialrefs.Unusable{Why: "credential " + id.String() + " is gone, or not one of the operation's"}
		case c.Type == storeDown:
			return nil, nil, errors.New("load credentials: connection refused")
		}
		out[id.String()] = c
	}
	return out, nil, nil
}

// testRecordings records one task's desktops in memory.
func testRecordings(task, opID uuid.UUID) *recordings.Service {
	return recordings.NewService(recordingstest.NewRepo(), recordingstest.NewTasks(models.MinionTask{TaskID: task, OperationID: opID}),
		recordingstest.NewStore(), nil, recordings.DefaultConfig(), zap.NewNop())
}

// ended waits for the task's recordings to be n, each ended.
func ended(t *testing.T, recs *recordings.Service, task uuid.UUID, n int) []models.Recording {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := recs.List(context.Background(), task, 0, 10)
		done := err == nil && len(page) == n
		for _, r := range page {
			done = done && r.State != models.RecordingStateRecording
		}
		if done {
			return page
		}
		if time.Now().After(deadline) {
			t.Fatalf("recordings %+v %v", page, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// rdpGuacdArgs are the args a fake guacd asks an RDP session for: those
// core sends (some of guacd's own).
var rdpGuacdArgs = []string{"VERSION_1_5_0", "hostname", "port", "domain", "username", "password", "security", "ignore-cert", "resize-method",
	"color-depth", "disable-gfx", "server-layout", "enable-font-smoothing", "read-only", "client-name", "workstation-name",
	"width", "height", "dpi", "os-major-type", "os-minor-type", "timezone"}

// oneGuacd is guacd for one session: it asks for an RDP's args (some of
// them), keeps the connect's values, says ready and a first instruction,
// then echoes what comes.
func oneGuacd(t *testing.T) (string, <-chan []string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	connect := make(chan []string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		r := bufio.NewReader(c)
		args := rdpGuacdArgs
		ready := false
		for {
			ins, err := guacd.Read(r)
			if err != nil {
				return
			}
			switch ins.Opcode {
			case "select":
				_, _ = c.Write([]byte(guacd.Instruction{Opcode: "args", Args: args}.Encode()))
			case "connect":
				ready = true
				connect <- ins.Args
				_, _ = c.Write([]byte(guacd.Instruction{Opcode: "ready", Args: []string{"$rdp-1"}}.Encode() + "4.sync,3.100;"))
			case "disconnect":
				return
			default:
				// The session's, once it is ready (size, image and the rest
				// open it).
				if ready {
					_, _ = c.Write([]byte(ins.Encode()))
				}
			}
		}
	}()
	return l.Addr().String(), connect
}

// A desktop is an operator's; its session in guacd reaches the task's
// server through its port, logging in with its credential, drawn by the
// settings it opened with (the others their defaults); then the session's
// instructions go both ways, and the tunnel's pings are answered by core.
// It is recorded: guacd's instructions, and the operator's mouse between
// them, timed from guacd's last sync, until the web app closes it.
func TestDesktopSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	operator, viewer := uuid.New(), uuid.New()
	opID, task, plain, cred := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{
		{UserID: operator, Role: models.OperationRoleOperator}, {UserID: viewer, Role: models.OperationRoleViewer}}}}
	tasks := tasksByID{
		task: {TaskID: task, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "rdp", "host": "10.0.0.5", "to": 3389,
			"credential": cred.String()}},
		plain: {TaskID: plain, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "forward"}},
	}
	addr, connect := oneGuacd(t)
	creds := credsByID{cred.String(): {ID: cred.String(), Type: protocol.CredentialTypePlaintext, Username: "admin", Realm: "CORP", Password: "S3cret;,"}}
	recs := testRecordings(task, opID)
	ports := &claimingPorts{portsByTask: portsByTask{task: 24005}}
	dc := NewMinionDesktopController(ports, tasks, creds, ops, recs, DesktopConfig{Guacd: addr, CoreHost: "logos-core"},
		func(*http.Request) bool { return true }, zap.NewNop())
	var who uuid.UUID
	r := gin.New()
	r.GET("/minion-tasks/:id/desktop", func(c *gin.Context) {
		c.Set("userID", who.String())
		c.Set("username", "alice")
		c.Set("roles", []string{"user"})
	}, dc.Desktop)
	srv := httptest.NewServer(r)
	defer srv.Close()
	url := func(id uuid.UUID) string {
		return "ws" + strings.TrimPrefix(srv.URL, "http") + "/minion-tasks/" + id.String() + "/desktop?width=1280&height=720&dpi=96" +
			"&settings=" + neturl.QueryEscape(`{"quality":"speed","security":"nla","ignore_cert":false}`)
	}
	dialer := gws.Dialer{Subprotocols: []string{"guacamole"}}
	for _, c := range []struct {
		who  uuid.UUID
		task uuid.UUID
		want int
	}{
		{viewer, task, http.StatusForbidden},
		{operator, plain, http.StatusBadRequest},
		{operator, uuid.New(), http.StatusNotFound},
	} {
		who = c.who
		_, resp, err := dialer.Dial(url(c.task), nil)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != c.want {
			t.Errorf("%v on %v: %v %v, want %d", c.who, c.task, resp, err, c.want)
		}
	}
	who = operator
	conn, resp, err := dialer.Dial(url(task), nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if conn.Subprotocol() != "guacamole" {
		t.Errorf("subprotocol %q", conn.Subprotocol())
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	read := func() string {
		t.Helper()
		var text string
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read: %v (so far %q)", err, text)
			}
			text += string(b)
			if ins, err := guacd.Parse(text); err == nil && len(ins) > 0 {
				return text
			}
		}
	}
	// The tunnel's id first, then the session's.
	if first, _ := guacd.Parse(read()); len(first) != 1 || first[0].Opcode != "" || len(first[0].Args) != 1 {
		t.Fatalf("first %v", first)
	}
	// A param with a value guacd does not ask for would refuse the
	// session before its connect: not a wait without end.
	var values []string
	select {
	case values = <-connect:
	case <-time.After(10 * time.Second):
		t.Fatalf("no connect: %q", read())
	}
	if want := []string{"VERSION_1_5_0", "logos-core", "24005", "CORP", "admin", "S3cret;,", "nla", "false", "",
		"16", "true", "en-us-qwerty", "", "", desktops.ComputerName(task), desktops.ComputerName(task),
		"1920", "1080", "96", "1", "3", "UTC"}; !slices.Equal(values, want) {
		t.Fatalf("connect %v, want %v", values, want)
	}
	if got := read(); got != "4.sync,3.100;" {
		t.Fatalf("session %q", got)
	}
	// A ping is core's to answer; the rest goes to guacd (which echoes).
	_ = conn.WriteMessage(gws.TextMessage, []byte("0.,4.ping,13.1731600000000;"))
	if got := read(); got != "0.,4.ping,13.1731600000000;" {
		t.Fatalf("ping answered %q", got)
	}
	_ = conn.WriteMessage(gws.TextMessage, []byte("5.mouse,2.10,2.20,1.1;"))
	if got := read(); got != "5.mouse,2.10,2.20,1.1;" {
		t.Fatalf("relayed %q", got)
	}

	// guacd's connections to the port for it were named the operator's,
	// from guacd's address as core reached it, until it closed.
	if claims, _ := ports.named(); !slices.Equal(claims, []string{task.String() + " 127.0.0.1 web: alice"}) {
		t.Errorf("named %v", claims)
	}
	_ = conn.Close()
	rec := ended(t, recs, task, 1)[0]
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, released := ports.named(); released == 1 {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("released %d", released)
		}
	}
	if rec.End != models.RecordingEndClosed || rec.State != models.RecordingStateSaved || rec.Username != "alice" ||
		rec.UserID != operator || !rec.Keys || rec.Settings["quality"] != "speed" || rec.Width != 1920 || rec.Height != 1080 ||
		rec.Front != "rdp" || rec.Number != 1 {
		t.Errorf("recording %+v", rec)
	}
	body, _, err := recs.Open(context.Background(), rec.RecordingID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	_ = body.Close()
	got, err := guacd.Parse(string(b))
	if err != nil || len(got) != 3 || got[0].String() != "sync(100)" || got[1].Opcode != "mouse" || len(got[1].Args) != 4 ||
		!slices.Equal(got[1].Args[:3], []string{"10", "20", "1"}) || got[2].String() != "mouse(10, 20, 1)" {
		t.Errorf("recorded %q %v", b, err)
	}
}

// A desktop that does not open says why: an error instruction, and a close
// whose reason is its status code, as Guacamole's tunnel reads it.
func TestDesktopRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	operator := uuid.New()
	opID, task, gone, down, unread := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{{UserID: operator, Role: models.OperationRoleOperator}}}}
	tasks := tasksByID{
		task: {TaskID: task, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "vnc", "host": "h", "to": 5900}},
		gone: {TaskID: gone, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "ssh", "host": "h", "to": 22,
			"credential": uuid.NewString()}},
		down: {TaskID: down, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "ssh", "host": "h", "to": 22,
			"credential": unread.String()}},
	}
	// A setting another front's, or a value it does not take: nothing
	// opens (the login is not read).
	settings := neturl.QueryEscape(`{"quality":"speed","font_size":100}`)
	// No guacd answers there.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	_ = l.Close()
	recs := testRecordings(task, opID)
	creds := credsByID{unread.String(): {ID: unread.String(), Type: storeDown}}
	dc := NewMinionDesktopController(portsByTask{task: 24001, gone: 24002, down: 24004}, tasks, creds, ops, recs,
		DesktopConfig{Guacd: addr, CoreHost: "core"}, func(*http.Request) bool { return true }, zap.NewNop())
	r := gin.New()
	r.GET("/minion-tasks/:id/desktop", func(c *gin.Context) {
		c.Set("userID", operator.String())
		c.Set("roles", []string{"user"})
	}, dc.Desktop)
	srv := httptest.NewServer(r)
	defer srv.Close()
	for _, want := range []struct {
		id     uuid.UUID
		query  string
		status string
		text   string
	}{
		{task, "", "519", "core cannot reach guacd"},
		{gone, "", "771", "the desktop's login: credential " + tasks[gone].Args["credential"].(string) + " is gone"},
		// The credentials' store not reached: core's failure, not a
		// refusal of the login.
		{down, "", "512", "core could not read the desktop's login"},
		{task, "?settings=" + settings, "768", "the desktop's settings: font_size: not a declared field"},
		{gone, "?settings=" + settings, "768", "font_size: must be <= 48; quality: not a declared field"},
		{task, "?settings=%7B", "768", "the desktop's settings are no JSON object"},
	} {
		id := want.id
		conn, resp, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/minion-tasks/"+id.String()+"/desktop"+want.query, nil)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var said []guacd.Instruction
		var closed *gws.CloseError
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				closed, _ = err.(*gws.CloseError)
				break
			}
			ins, _ := guacd.Parse(string(b))
			said = append(said, ins...)
		}
		_ = conn.Close()
		i := slices.IndexFunc(said, func(i guacd.Instruction) bool { return i.Opcode == "error" })
		if i < 0 || !strings.Contains(said[i].Args[0], want.text) || said[i].Args[1] != want.status || closed == nil || closed.Text != want.status {
			t.Errorf("%v%s: said %v, closed %v", id, want.query, said, closed)
		}
	}
	// One got as far as guacd: its recording says why it did not open. The
	// others did not: no recording.
	if rec := ended(t, recs, task, 1)[0]; rec.State != models.RecordingStateFailed || rec.End != models.RecordingEndNotOpened ||
		!strings.Contains(rec.Msg, "core cannot reach guacd") || rec.Parts != 0 {
		t.Errorf("recording %+v", rec)
	}
}

// floodGuacd is guacd for one session that, once ready, sends its frames
// as fast as it can, until its socket fails.
func floodGuacd(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		r := bufio.NewReader(c)
		for {
			ins, err := guacd.Read(r)
			if err != nil {
				return
			}
			switch ins.Opcode {
			case "select":
				_, _ = c.Write([]byte(guacd.Instruction{Opcode: "args", Args: rdpGuacdArgs}.Encode()))
			case "connect":
				_, _ = c.Write([]byte(guacd.Instruction{Opcode: "ready", Args: []string{"$rdp-1"}}.Encode()))
				frames := []byte(strings.Repeat("4.sync,3.100;", 1<<16))
				go func() { _, _ = io.Copy(io.Discard, r) }()
				for {
					if _, err := c.Write(frames); err != nil {
						return
					}
				}
			}
		}
	}()
	return l.Addr().String()
}

// openDesktop opens an RDP desktop task's desktop in a controller on
// guacdAddr, recorded in recs: the socket, open.
func openDesktop(t *testing.T, guacdAddr string, recs func(task, opID uuid.UUID) *recordings.Service) (*gws.Conn, *recordings.Service, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	operator, opID, task := uuid.New(), uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{{UserID: operator, Role: models.OperationRoleOperator}}}}
	tasks := tasksByID{task: {TaskID: task, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "rdp", "host": "h", "to": 3389}}}
	rs := recs(task, opID)
	dc := NewMinionDesktopController(portsByTask{task: 24003}, tasks, credsByID{}, ops, rs, DesktopConfig{Guacd: guacdAddr, CoreHost: "core"},
		func(*http.Request) bool { return true }, zap.NewNop())
	r := gin.New()
	r.GET("/minion-tasks/:id/desktop", func(c *gin.Context) {
		c.Set("userID", operator.String())
		c.Set("roles", []string{"user"})
	}, dc.Desktop)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	conn, resp, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/minion-tasks/"+task.String()+"/desktop", nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, rs, task
}

// A web app gone silent (a laptop asleep: no close, no ping) ends its
// desktop after desktopReadWait, and its recording: not a socket and a
// recording held open for as long as the network keeps them.
func TestDesktopSilentWebApp(t *testing.T) {
	wait := desktopReadWait
	desktopReadWait = 300 * time.Millisecond
	t.Cleanup(func() { desktopReadWait = wait })
	addr, _ := oneGuacd(t)
	conn, recs, task := openDesktop(t, addr, testRecordings)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	// The tunnel's id, then the session's first frame: it is relayed.
	for got := ""; !strings.Contains(got, "4.sync,3.100;"); {
		_, b, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (so far %q)", err, got)
		}
		got += string(b)
	}
	// Then nothing from the web app.
	if rec := ended(t, recs, task, 1)[0]; rec.End != models.RecordingEndClosed || rec.State != models.RecordingStateSaved {
		t.Errorf("recording %+v", rec)
	}
}

// A web app that takes nothing (its network gone, guacd's frames piling
// up) ends its desktop once a write to it fails, and its recording, rather
// than wait for it to say something.
func TestDesktopWebAppTakesNothing(t *testing.T) {
	wait := desktopWriteWait
	desktopWriteWait = 200 * time.Millisecond
	t.Cleanup(func() { desktopWriteWait = wait })
	conn, recs, task := openDesktop(t, floodGuacd(t), testRecordings)
	// The web app reads nothing, and says nothing.
	_ = conn
	if rec := ended(t, recs, task, 1)[0]; rec.End != models.RecordingEndClosed || rec.State != models.RecordingStateSaved {
		t.Errorf("recording %+v", rec)
	}
}
