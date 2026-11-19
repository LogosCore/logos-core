package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/desktops"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/guacd"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// DesktopPorts finds a desktop task's operational port (the tunnels:
// miniontunnels.Service): guacd reaches its server through it, its
// connections there named for the desktop's operator until release
// (DesktopClient), as its streams list says them.
type DesktopPorts interface {
	Port(ctx context.Context, task uuid.UUID) (*models.OperationalPort, error)
	DesktopClient(task uuid.UUID, from netip.Addr, client string) (release func())
}

// DesktopCredentials resolves a desktop's login (credentialrefs.Resolver).
type DesktopCredentials interface {
	Resolve(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error)
}

// Recordings records the desktops core relays (recordings.Service):
// nil when the recording cannot be kept, and the desktop opens unrecorded.
type Recordings interface {
	Begin(ctx context.Context, b recordings.Beginning) *recordings.Recorder
}

// DesktopConfig is where core's guacd is (GUACD_ADDRESS) and the host it
// reaches core's operational ports at (GUACD_CORE_HOST).
type DesktopConfig struct {
	Guacd    string
	CoreHost string
}

// MinionDesktopController serves the web app's desktops (docs:
// contracts/minion-messages/tunnels.md, Desktops): a WebSocket for each,
// a session in guacd with the task's server, through the task's port.
type MinionDesktopController struct {
	ports    DesktopPorts
	tasks    TerminalTasks
	creds    DesktopCredentials
	opRepo   gqlctx.OperationFinder
	recs     Recordings
	cfg      DesktopConfig
	upgrader gws.Upgrader
	logger   *zap.Logger
}

// NewMinionDesktopController takes checkOrigin from the GraphQL socket's,
// as the terminal's does: a cookie authenticates the upgrade. recs records
// each desktop (nil: none is).
func NewMinionDesktopController(ports DesktopPorts, tasks TerminalTasks, creds DesktopCredentials, opRepo gqlctx.OperationFinder,
	recs Recordings, cfg DesktopConfig, checkOrigin func(*http.Request) bool, logger *zap.Logger) *MinionDesktopController {
	return &MinionDesktopController{ports: ports, tasks: tasks, creds: creds, opRepo: opRepo, recs: recs, cfg: cfg, logger: logger,
		upgrader: gws.Upgrader{ReadBufferSize: 8 << 10, WriteBufferSize: 64 << 10, CheckOrigin: checkOrigin,
			// The web app's Guacamole client asks for it.
			Subprotocols: []string{"guacamole"}}}
}

// desktopReadLimit bounds one of the web app's messages: an instruction
// (a clipboard's blob is 8 KiB).
const desktopReadLimit = 1 << 20

var (
	// desktopWriteWait bounds a write to the web app: one that takes
	// nothing for that long is gone.
	desktopWriteWait = 10 * time.Second
	// desktopReadWait: a web app silent for that long is gone. Its
	// Guacamole client pings every half second, and keeps guacd's session
	// alive every 5 (guacd drops a user silent for 15 s).
	desktopReadWait = time.Minute
)

// A desktop's size bounds, as the web app asks for it.
const (
	desktopMinSide, desktopMaxSide = 100, 8192
	desktopMinDPI, desktopMaxDPI   = 48, 480
)

// Desktop handles GET /api/v1/minion-tasks/{id}/desktop?width=&height=&dpi=&settings=.
//
// A WebSocket for a desktop of a desktop task (front rdp, vnc, ssh or
// telnet), for an operator of its operation, speaking the Guacamole
// protocol with the web app's Guacamole client (subprotocol guacamole):
// core opens a session in guacd with the task's server, through the
// task's operational port, logging in with the task's credential and
// drawing it by the settings the operator picked as it opened (a JSON
// object: desktops.Fields, GraphQL minionDesktopSettings), then relays
// it; the client's tunnel pings are answered. Each is recorded as it goes
// (pkg/recordings: GraphQL recordings), one guacd did not open too
// (not one core refused before, its settings or its login). A desktop
// that does not open sends an error instruction (its message and status
// code) and closes with the code as the reason, as one core ends does
// (relayDesktop); one guacd ends closes with no reason.
//
//	@Summary		Open a desktop on a desktop task
//	@Tags			Minion tasks
//	@Security		BearerAuth
//	@Param			id			path	string	true	"Task ID"
//	@Param			width		query	int		false	"Width in pixels (1024)"
//	@Param			height		query	int		false	"Height in pixels (768)"
//	@Param			dpi			query	int		false	"DPI (96)"
//	@Param			settings	query	string	false	"The desktop's settings: a JSON object of its front's (minionDesktopSettings), each left out its default"
//	@Router			/minion-tasks/{id}/desktop [get]
func (dc *MinionDesktopController) Desktop(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return
	}
	t, err := dc.tasks.FindByTaskID(ctx, id)
	if err != nil {
		if repository.IsNotFound(err) {
			c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
			return
		}
		dc.logger.Error("Failed to load a minion task", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	if !callerHasOperationRole(c, dc.opRepo, t.OperationID, models.OperationRoleViewer) {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return
	}
	if !callerHasOperationRole(c, dc.opRepo, t.OperationID, models.OperationRoleOperator) {
		c.JSON(http.StatusForbidden, responses.NewErrorResponse("a desktop is an operator's"))
		return
	}
	front, _ := protocol.ArgString(t.Args, protocol.ParamFront)
	if t.Action != protocol.ActionTunnel || !protocol.IsDesktopFront(front) {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("the task is no desktop's (its front is not rdp, vnc, ssh or telnet)"))
		return
	}
	if dc.cfg.Guacd == "" {
		c.JSON(http.StatusServiceUnavailable, responses.NewErrorResponse("core draws no desktops: it has no guacd (GUACD_ADDRESS)"))
		return
	}
	width := clampInt(queryInt(c, "width", 1024), desktopMinSide, desktopMaxSide)
	height := clampInt(queryInt(c, "height", 768), desktopMinSide, desktopMaxSide)
	dpi := clampInt(queryInt(c, "dpi", 96), desktopMinDPI, desktopMaxDPI)
	conn, err := dc.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// The upgrader answered already.
		return
	}
	defer func() { _ = conn.Close() }()
	var wmu sync.Mutex
	write := func(b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(desktopWriteWait))
		return conn.WriteMessage(gws.TextMessage, b)
	}
	// The tunnel's own first instruction: its id, which opens it.
	if write([]byte(guacd.Instruction{Args: []string{uuid.NewString()}}.Encode())) != nil {
		return
	}
	fail := func(status int, message string) { sayEnd(conn, write, &wmu, status, message) }
	port, err := dc.ports.Port(ctx, id)
	if err != nil {
		if repository.IsNotFound(err) {
			fail(guacd.StatusResourceNotFound, "the task's tunnel is not open: it ended, or has not started")
			return
		}
		dc.logger.Error("Failed to read a desktop's port", zap.String("task_id", id.String()), zap.Error(err))
		fail(guacd.StatusServerError, "core could not read the task's port")
		return
	}
	values, settings, err := desktopSettings(front, c.Query("settings"), dc.cfg.CoreHost, port.Port)
	if err != nil {
		fail(guacd.StatusClientBadRequest, err.Error())
		return
	}
	login, status, err := dc.login(ctx, t)
	if err != nil {
		if status == guacd.StatusServerError {
			dc.logger.Error("Failed to read a desktop's login", zap.String("task_id", id.String()), zap.Error(err))
		}
		fail(status, err.Error())
		return
	}
	// A fixed screen (an RDP desktop's by default) is its size, not the
	// pane's: the recording's too.
	if w, err := strconv.Atoi(settings["width"]); err == nil {
		width = w
	}
	if h, err := strconv.Atoi(settings["height"]); err == nil {
		height = h
	}
	if d, err := strconv.Atoi(settings["dpi"]); err == nil {
		dpi = d
	}
	username := c.GetString("username")
	var rec *recordings.Recorder
	if dc.recs != nil {
		userID, _ := uuid.Parse(c.GetString("userID"))
		rec = dc.recs.Begin(ctx, recordings.Beginning{TaskID: id, Front: front, UserID: userID, Username: username, Client: "web: " + username,
			Settings: values, Width: width, Height: height, Keys: settings["read-only"] != "true"})
	}
	// guacd's connections to the task's port for this desktop are the
	// operator's in its streams list ("web: alice", as a web terminal's),
	// named from guacd's address before guacd connects, for as long as the
	// desktop lasts.
	var release func()
	defer func() {
		if release != nil {
			release()
		}
	}()
	cfg := guacd.Config{Protocol: front, Params: desktopParams(id, front, dc.cfg.CoreHost, port.Port, settings, login),
		Width: width, Height: height, DPI: dpi, Images: []string{"image/png", "image/jpeg", "image/webp"}, Name: username,
		Dialed: func(remote net.Addr) {
			if ta, ok := remote.(*net.TCPAddr); ok {
				release = dc.ports.DesktopClient(id, ta.AddrPort().Addr(), "web: "+username)
			}
		}}
	sess, err := guacd.Open(ctx, dc.cfg.Guacd, cfg)
	if err != nil {
		var ge *guacd.Error
		if !errors.As(err, &ge) {
			ge = &guacd.Error{Message: err.Error(), Status: guacd.StatusServerError}
		}
		dc.logger.Warn("A desktop did not open", zap.String("task_id", id.String()), zap.String("front", front), zap.Error(err))
		rec.Fail(ge.Message)
		fail(ge.Status, ge.Message)
		return
	}
	dc.logger.Info("A desktop opened", zap.String("task_id", id.String()), zap.String("front", front),
		zap.String("user", username), zap.String("guacd_connection", sess.ID), zap.String("recording_id", rec.ID().String()))
	end := relayDesktop(conn, sess, write, &wmu, rec)
	rec.End(end)
	dc.logger.Info("A desktop closed", zap.String("task_id", id.String()), zap.String("guacd_connection", sess.ID), zap.String("end", end))
}

// login is the credential a desktop task's args name, as guacd gets it;
// nil for none. One gone, or of a type the desktop does not take, is an
// error the web app shows (StatusClientForbidden); one core could not
// read, core's (StatusServerError).
func (dc *MinionDesktopController) login(ctx context.Context, t *models.MinionTask) (*protocol.Credential, int, error) {
	raw, _ := protocol.ArgString(t.Args, protocol.ParamCredential)
	if raw == "" {
		return nil, 0, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, guacd.StatusClientForbidden, errors.New("the desktop's login is no credential id")
	}
	if dc.creds == nil {
		return nil, guacd.StatusServerError, errors.New("core resolves no credentials here")
	}
	given, _, err := dc.creds.Resolve(ctx, t.OperationID, []uuid.UUID{id})
	var unusable *credentialrefs.Unusable
	switch {
	case errors.As(err, &unusable):
		return nil, guacd.StatusClientForbidden, errors.New("the desktop's login: " + unusable.Why +
			": start the tunnel again with another credential, or none")
	case err != nil:
		return nil, guacd.StatusServerError, errors.New("core could not read the desktop's login")
	}
	cred, ok := given[raw]
	if !ok {
		return nil, guacd.StatusClientForbidden, errors.New("the desktop's login is gone: start the tunnel again with another credential, or none")
	}
	if cred.Type != protocol.CredentialTypePlaintext && cred.Type != protocol.CredentialTypeKey {
		return nil, guacd.StatusClientForbidden, errors.New("the desktop's login is a " + cred.Type + ": a desktop takes a password or a key")
	}
	return &cred, 0, nil
}

// desktopSettings are the settings a desktop opens with (raw: the JSON
// object the web app sent; none, the defaults), as it sent them, numbers
// as JSON's, and their guacd params, for its server at host:port; an error
// says which are wrong.
func desktopSettings(front, raw, host string, port int) (map[string]any, map[string]string, error) {
	var values map[string]any
	if raw != "" {
		d := json.NewDecoder(strings.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&values); err != nil {
			return nil, nil, errors.New("the desktop's settings are no JSON object")
		}
	}
	params, problems := desktops.Params(front, values, host, port)
	if problems != nil {
		keys := slices.Sorted(maps.Keys(problems))
		said := make([]string, len(keys))
		for i, k := range keys {
			said[i] = k + ": " + problems[k]
		}
		return nil, nil, errors.New("the desktop's settings: " + strings.Join(said, "; "))
	}
	return values, params, nil
}

// desktopParams are a desktop session's connection params, by guacd's
// names: its settings' (desktopSettings), then what no setting changes:
// its server, core's port for the task (host, port), and its login, the
// credential's (login: none, the desktop asks). An RDP desktop's computer
// names that no setting gives are filled (desktops.ComputerNames).
func desktopParams(task uuid.UUID, front, host string, port int, settings map[string]string, login *protocol.Credential) map[string]string {
	p := maps.Clone(settings)
	if p == nil {
		p = map[string]string{}
	}
	p["hostname"], p["port"] = host, strconv.Itoa(port)
	if login != nil {
		switch front {
		case protocol.FrontRDP:
			p["username"], p["password"], p["domain"] = login.Username, login.Password, login.Realm
		case protocol.FrontVNC:
			p["username"], p["password"] = login.Username, login.Password
		case protocol.FrontSSH:
			p["username"] = login.Username
			if login.Type == protocol.CredentialTypeKey {
				p["private-key"], p["passphrase"] = login.Key, login.Passphrase
			} else {
				p["password"] = login.Password
			}
		case protocol.FrontTelnet:
			p["username"], p["password"] = login.Username, login.Password
		}
	}
	if front == protocol.FrontRDP {
		desktops.ComputerNames(task, p)
	}
	return p
}

// relayDesktop carries the session over the socket until either ends,
// recording it (rec: nil, it is not): guacd's bytes to the web app as they
// come, whole characters to a message; the web app's instructions to
// guacd, but for the tunnel's own, a ping answered. It says which ended
// it: the web app (models.RecordingEndClosed: it closed, went silent for
// desktopReadWait, sent what is not Guacamole's, or took nothing for
// desktopWriteWait) or guacd (...Desktop: it ended the session, took
// nothing for guacd.WriteWait, or said nothing for guacd.ReadWait), and
// returns once the session is closed. Where core ends it (silent, not
// Guacamole's, guacd taking nothing or silent), the web app is told why
// first (sayEnd): its Guacamole client ends a session on that word, as on
// guacd's, not on its socket's close.
func relayDesktop(conn *gws.Conn, sess *guacd.Session, write func([]byte) error, wmu *sync.Mutex, rec *recordings.Recorder) string {
	out := make(chan struct{})
	var guacdEnded atomic.Bool
	go func() {
		defer close(out)
		buf := make([]byte, 64<<10)
		pending := 0
		var readErr error
		for {
			n, err := sess.Read(buf[pending:])
			if n > 0 {
				rec.FromGuacd(buf[pending : pending+n])
				whole, rest := guacd.CutUTF8(buf[:pending+n])
				if len(whole) > 0 && write(whole) != nil {
					// The web app takes nothing: it is gone, and so is
					// the socket, which ends the read below.
					_ = conn.Close()
					return
				}
				pending = copy(buf, rest)
			}
			if err != nil {
				readErr = err
				break
			}
		}
		guacdEnded.Store(true)
		if timedOut(readErr) {
			// guacd said nothing past guacd.ReadWait, not even its
			// keep-alive: it is stuck, or unreachable.
			sayEnd(conn, write, wmu, guacd.StatusUpstreamTimeout,
				fmt.Sprintf("guacd said nothing for %s (it may be stuck): core ended the desktop", guacd.ReadWait))
			return
		}
		// guacd ended the session: so does the socket.
		wmu.Lock()
		_ = conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, ""), time.Now().Add(desktopWriteWait))
		wmu.Unlock()
	}()
	// guacd's socket closes before its reader is waited for: a guacd that
	// says nothing more (stuck) would hold the reader, and the session,
	// forever.
	defer func() {
		_ = sess.Close()
		<-out
	}()
	ended := func() string {
		if guacdEnded.Load() {
			return models.RecordingEndDesktop
		}
		return models.RecordingEndClosed
	}
	conn.SetReadLimit(desktopReadLimit)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(desktopReadWait))
		kind, b, err := conn.ReadMessage()
		if err != nil {
			end := ended()
			if end == models.RecordingEndClosed && timedOut(err) {
				sayEnd(conn, write, wmu, guacd.StatusClientTimeout,
					fmt.Sprintf("core heard nothing from the web app for %s: it ended the desktop", desktopReadWait))
			}
			return end
		}
		if kind != gws.TextMessage {
			continue
		}
		ins, err := guacd.Parse(string(b))
		if err != nil {
			// Not Guacamole's: the session ends.
			sayEnd(conn, write, wmu, guacd.StatusClientBadRequest, "the web app sent what is not Guacamole's: core ended the desktop")
			return models.RecordingEndClosed
		}
		var toGuacd []byte
		for _, i := range ins {
			if i.Opcode == "" {
				if len(i.Args) > 0 && i.Args[0] == "ping" {
					_ = write([]byte(i.Encode()))
				}
				continue
			}
			rec.FromWeb(i)
			toGuacd = append(toGuacd, i.Encode()...)
		}
		if len(toGuacd) > 0 {
			if _, err := sess.Write(toGuacd); err != nil {
				// guacd takes nothing (stuck), or its socket failed.
				status, msg := guacd.StatusServerError, "core lost its connection to guacd: "+err.Error()
				if timedOut(err) {
					status, msg = guacd.StatusUpstreamTimeout,
						fmt.Sprintf("guacd took none of the desktop's input for %s (it may be stuck): core ended the desktop", guacd.WriteWait)
				}
				sayEnd(conn, write, wmu, status, msg)
				return models.RecordingEndDesktop
			}
		}
	}
}

// sayEnd ends a desktop from core's side, saying why: an error instruction
// (message, and status: a Guacamole status code), which the web app's
// Guacamole client shows as it ends the session, then the socket's close,
// the code its reason (where Guacamole's tunnel reads its status).
func sayEnd(conn *gws.Conn, write func([]byte) error, wmu *sync.Mutex, status int, message string) {
	_ = write([]byte(guacd.Instruction{Opcode: "error", Args: []string{message, strconv.Itoa(status)}}.Encode()))
	wmu.Lock()
	defer wmu.Unlock()
	_ = conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, strconv.Itoa(status)),
		time.Now().Add(desktopWriteWait))
}

// timedOut reports whether err is a deadline's: a net.Error that timed out.
func timedOut(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }
