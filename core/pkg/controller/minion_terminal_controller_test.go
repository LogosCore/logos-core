package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

type tasksByID map[uuid.UUID]models.MinionTask

func (f tasksByID) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	t, ok := f[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &t, nil
}

// noTerminals runs no terminal: each open is refused, as asked.
type noTerminals struct {
	asked []string
}

func (n *noTerminals) OpenTerminal(_ uuid.UUID, client string, rows, cols int) (*miniontunnels.Terminal, error) {
	n.asked = append(n.asked, client+" "+itoa(rows)+"x"+itoa(cols))
	return nil, miniontunnels.ErrNoTerminal
}

func itoa(n int) string { return strconv.Itoa(n) }

// A terminal is an operator's: someone outside the operation is told the
// task is not found, a viewer is refused; an operator's socket opens, and
// a terminal core does not run closes with 4000 and why.
func TestTerminalSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	operator, viewer, stranger := uuid.New(), uuid.New(), uuid.New()
	opID, task := uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{
		{UserID: operator, Role: models.OperationRoleOperator}, {UserID: viewer, Role: models.OperationRoleViewer}}}}
	terms := &noTerminals{}
	tc := NewMinionTerminalController(terms, tasksByID{task: {TaskID: task, OperationID: opID}}, ops,
		func(*http.Request) bool { return true }, zap.NewNop())
	var who uuid.UUID
	r := gin.New()
	r.GET("/minion-tasks/:id/terminal", func(c *gin.Context) {
		c.Set("userID", who.String())
		c.Set("username", "alice")
		c.Set("roles", []string{"user"})
	}, tc.Terminal)
	srv := httptest.NewServer(r)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/minion-tasks/" + task.String() + "/terminal?rows=40&cols=120"

	for _, c := range []struct {
		who  uuid.UUID
		url  string
		want int
	}{
		{stranger, url, http.StatusNotFound},
		{viewer, url, http.StatusForbidden},
		{operator, strings.Replace(url, task.String(), uuid.New().String(), 1), http.StatusNotFound},
	} {
		who = c.who
		_, resp, err := gws.DefaultDialer.Dial(c.url, nil)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != c.want {
			t.Errorf("%v: %v %v, want %d", c.who, resp, err, c.want)
		}
	}
	who = operator
	conn, resp, err := gws.DefaultDialer.Dial(url, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	ce, ok := err.(*gws.CloseError)
	if !ok || ce.Code != terminalCloseRefused || !strings.Contains(ce.Text, "no terminal") {
		t.Errorf("closed %v", err)
	}
	if len(terms.asked) != 1 || terms.asked[0] != "web: alice 40x120" {
		t.Errorf("asked %v", terms.asked)
	}
}
