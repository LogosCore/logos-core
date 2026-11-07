package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// MinionTerminals is what the terminal controller needs
// (miniontunnels.Service).
type MinionTerminals interface {
	OpenTerminal(task uuid.UUID, client string, rows, cols int) (*miniontunnels.Terminal, error)
}

// TerminalTasks finds a terminal's task (repository.IMinionTaskRepository).
type TerminalTasks interface {
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
}

// MinionTerminalController serves the web app's terminals (docs:
// contracts/minion-messages/tunnels.md, Terminal): a WebSocket for each,
// one stream of a terminal task.
type MinionTerminalController struct {
	terminals MinionTerminals
	tasks     TerminalTasks
	opRepo    gqlctx.OperationFinder
	upgrader  gws.Upgrader
	logger    *zap.Logger
}

// NewMinionTerminalController takes checkOrigin from the GraphQL socket's
// (CORS_ALLOWED_ORIGINS and same-origin): a cookie authenticates the
// upgrade, so the origin is what stops another site's page opening one.
func NewMinionTerminalController(terminals MinionTerminals, tasks TerminalTasks, opRepo gqlctx.OperationFinder,
	checkOrigin func(*http.Request) bool, logger *zap.Logger) *MinionTerminalController {
	return &MinionTerminalController{terminals: terminals, tasks: tasks, opRepo: opRepo, logger: logger,
		upgrader: gws.Upgrader{ReadBufferSize: 4 << 10, WriteBufferSize: 32 << 10, CheckOrigin: checkOrigin}}
}

// The terminal socket's closes past the WebSocket's own: why core did not
// open the terminal, in the close's reason.
const (
	terminalCloseRefused = 4000
	terminalCloseUnknown = 4001
)

const (
	terminalPing      = 30 * time.Second
	terminalWriteWait = 10 * time.Second
	// terminalEndWait is how long a terminal whose process ended waits for
	// its stream to end done before it says how.
	terminalEndWait = 10 * time.Second
)

// terminalControl is a text frame from the web app: a new size, a signal
// (interrupt, terminate, kill), or the end of its input.
type terminalControl struct {
	Rows   int    `json:"rows,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Signal string `json:"signal,omitempty"`
	EOF    bool   `json:"eof,omitempty"`
}

// terminalStatus is a text frame of core's: the stream's number first, how
// it ended last.
type terminalStatus struct {
	Stream   uint64 `json:"stream,omitempty"`
	End      string `json:"end,omitempty"`
	Exit     *int64 `json:"exit,omitempty"`
	Terminal *bool  `json:"terminal,omitempty"`
}

var terminalSignals = map[string]int{
	"interrupt": protocol.TunnelSignalInterrupt,
	"terminate": protocol.TunnelSignalTerminate,
	"kill":      protocol.TunnelSignalKill,
}

// Terminal handles GET /api/v1/minion-tasks/{id}/terminal?rows=&cols=.
//
// A WebSocket for a web terminal of a terminal task (front terminal), for
// an operator of its operation: binary frames are the process's bytes each
// way; text frames are JSON, the web app's {"rows","cols"} (a new size),
// {"signal"} (interrupt, terminate or kill) or {"eof":true}, and core's
// {"stream"} once it opened, then {"end","exit","terminal"} as it ends. A
// terminal core does not open closes with 4000 and why.
//
//	@Summary		Open a web terminal on a terminal task
//	@Tags			Minion tasks
//	@Security		BearerAuth
//	@Param			id		path	string	true	"Task ID"
//	@Param			rows	query	int		false	"Rows (24)"
//	@Param			cols	query	int		false	"Columns (80)"
//	@Router			/minion-tasks/{id}/terminal [get]
func (tc *MinionTerminalController) Terminal(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return
	}
	t, err := tc.tasks.FindByTaskID(ctx, id)
	if err != nil {
		if repository.IsNotFound(err) {
			c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
			return
		}
		tc.logger.Error("Failed to load a minion task", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	if !callerHasOperationRole(c, tc.opRepo, t.OperationID, models.OperationRoleViewer) {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return
	}
	if !callerHasOperationRole(c, tc.opRepo, t.OperationID, models.OperationRoleOperator) {
		c.JSON(http.StatusForbidden, responses.NewErrorResponse("a terminal is an operator's"))
		return
	}
	rows, cols := queryInt(c, "rows", 24), queryInt(c, "cols", 80)
	conn, err := tc.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// The upgrader answered already.
		return
	}
	defer func() { _ = conn.Close() }()
	client := "web: " + c.GetString("username")
	tm, err := tc.terminals.OpenTerminal(id, client, rows, cols)
	if err != nil {
		code := terminalCloseRefused
		if !errors.Is(err, miniontunnels.ErrNoTerminal) {
			code = terminalCloseUnknown
		}
		_ = conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(code, closeReason(err.Error())), time.Now().Add(terminalWriteWait))
		return
	}
	defer tm.Close()
	tc.run(conn, tm)
}

// run carries the terminal over the socket until either ends.
func (tc *MinionTerminalController) run(conn *gws.Conn, tm *miniontunnels.Terminal) {
	var wmu sync.Mutex
	write := func(kind int, b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
		return conn.WriteMessage(kind, b)
	}
	writeStatus := func(s terminalStatus) error {
		b, _ := json.Marshal(s)
		return write(gws.TextMessage, b)
	}
	if err := writeStatus(terminalStatus{Stream: tm.Stream()}); err != nil {
		return
	}
	// The process's bytes, then how it ended.
	out := make(chan struct{})
	go func() {
		defer close(out)
		buf := make([]byte, 32<<10)
		for {
			n, err := tm.Read(buf)
			if n > 0 {
				if write(gws.BinaryMessage, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				break
			}
		}
		// The process ended: so does what the operator sends, and the
		// stream ends done.
		_ = tm.CloseWrite()
		select {
		case <-tm.Done():
		case <-time.After(terminalEndWait):
		}
		end, exit, terminal := tm.Ended()
		_ = writeStatus(terminalStatus{End: end, Exit: exit, Terminal: terminal})
		wmu.Lock()
		_ = conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, ""), time.Now().Add(terminalWriteWait))
		wmu.Unlock()
	}()
	// Pings keep proxies from taking a quiet terminal for a dead one.
	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		tick := time.NewTicker(terminalPing)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				wmu.Lock()
				err := conn.WriteControl(gws.PingMessage, nil, time.Now().Add(terminalWriteWait))
				wmu.Unlock()
				if err != nil {
					return
				}
			case <-stopPing:
				return
			case <-out:
				return
			}
		}
	}()
	conn.SetReadLimit(1 << 20)
	for {
		kind, b, err := conn.ReadMessage()
		if err != nil {
			// The operator left, or the process ended and the socket
			// closed: either way the terminal is over.
			return
		}
		switch kind {
		case gws.BinaryMessage:
			if _, err := tm.Write(b); err != nil {
				<-out
				return
			}
		case gws.TextMessage:
			var ctl terminalControl
			if json.Unmarshal(b, &ctl) != nil {
				continue
			}
			tc.control(tm, ctl)
		}
	}
}

// control does what a text frame asks; one that does not do (a bad size,
// a terminal that ended) is let go.
func (tc *MinionTerminalController) control(tm *miniontunnels.Terminal, ctl terminalControl) {
	var err error
	switch {
	case ctl.EOF:
		err = tm.CloseWrite()
	case ctl.Signal != "":
		sig, ok := terminalSignals[ctl.Signal]
		if !ok {
			return
		}
		err = tm.Signal(sig)
	case ctl.Rows != 0 || ctl.Cols != 0:
		err = tm.Resize(ctl.Rows, ctl.Cols)
	}
	if err != nil {
		tc.logger.Debug("A terminal's control was not taken", zap.Uint64("stream", tm.Stream()), zap.Error(err))
	}
}

// queryInt is the query's key as a number, def when absent or not one.
func queryInt(c *gin.Context, key string, def int) int {
	if n, err := strconv.Atoi(c.Query(key)); err == nil {
		return n
	}
	return def
}

// closeReason fits a close's reason in its 123 bytes.
func closeReason(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
