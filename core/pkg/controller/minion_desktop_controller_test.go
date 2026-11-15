package controller

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/guacd"
	"github.com/logoscore/logos-core/core/pkg/models"
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

// credsByID are the operation's credentials.
type credsByID map[string]protocol.Credential

func (f credsByID) Resolve(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error) {
	out := map[string]protocol.Credential{}
	for _, id := range ids {
		if c, ok := f[id.String()]; ok {
			out[id.String()] = c
		}
	}
	return out, nil, nil
}

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
		args := []string{"VERSION_1_5_0", "hostname", "port", "domain", "username", "password", "security", "ignore-cert", "resize-method",
			"color-depth", "disable-gfx", "server-layout", "enable-font-smoothing", "read-only"}
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
	dc := NewMinionDesktopController(portsByTask{task: 24005}, tasks, creds, ops, DesktopConfig{Guacd: addr, CoreHost: "logos-core"},
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
		return "ws" + strings.TrimPrefix(srv.URL, "http") + "/minion-tasks/" + id.String() + "/desktop?width=1280&height=720&dpi=96&timezone=Europe/Kyiv" +
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
	values := <-connect
	if want := []string{"VERSION_1_5_0", "logos-core", "24005", "CORP", "admin", "S3cret;,", "nla", "false", "display-update",
		"16", "true", "en-us-qwerty", "", ""}; !slices.Equal(values, want) {
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
}

// A desktop that does not open says why: an error instruction, and a close
// whose reason is its status code, as Guacamole's tunnel reads it.
func TestDesktopRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	operator := uuid.New()
	opID, task, gone := uuid.New(), uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{{UserID: operator, Role: models.OperationRoleOperator}}}}
	tasks := tasksByID{
		task: {TaskID: task, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "vnc", "host": "h", "to": 5900}},
		gone: {TaskID: gone, OperationID: opID, Action: protocol.ActionTunnel, Args: models.JSONMap{"front": "ssh", "host": "h", "to": 22,
			"credential": uuid.NewString()}},
	}
	// A setting another front's, or a value it does not take: nothing
	// opens (the login is not read).
	settings := neturl.QueryEscape(`{"quality":"speed","font_size":100}`)
	// No guacd answers there.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	_ = l.Close()
	dc := NewMinionDesktopController(portsByTask{task: 24001, gone: 24002}, tasks, credsByID{}, ops, DesktopConfig{Guacd: addr, CoreHost: "core"},
		func(*http.Request) bool { return true }, zap.NewNop())
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
		{gone, "", "771", "the desktop's login is gone"},
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
}
