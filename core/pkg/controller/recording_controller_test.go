package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"go.uber.org/zap"
)

// A recording is its operation's to read, viewers too: its file as it was
// recorded, an attachment named for its format; one whose desktop did not
// open has none. A desktop's is never cached: it may become a video at the
// same URL; a terminal's that ended may be.
func TestRecordingFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	viewer := uuid.New()
	opID, task := uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{{UserID: viewer, Role: models.OperationRoleViewer}}}}
	recs := testRecordings(task, opID)
	ctx := context.Background()
	saved := recs.Begin(ctx, recordings.Beginning{TaskID: task, Front: "vnc", Username: "ann"})
	stream := "4.size,1.0,4.1024,3.768;4.sync,3.100;"
	saved.FromGuacd([]byte(stream))
	saved.End(models.RecordingEndClosed)
	failed := recs.Begin(ctx, recordings.Beginning{TaskID: task, Front: "vnc"})
	failed.Fail("core cannot reach guacd")

	dc := NewRecordingController(recs, ops, zap.NewNop())
	who := viewer
	r := gin.New()
	r.GET("/recordings/:id", func(c *gin.Context) {
		c.Set("userID", who.String())
		c.Set("roles", []string{"user"})
	}, dc.File)
	get := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/recordings/"+id, nil))
		return w
	}
	w := get(saved.ID().String())
	body, _ := io.ReadAll(w.Body)
	if w.Code != http.StatusOK || string(body) != stream || w.Header().Get("Content-Length") != strconv.Itoa(len(stream)) ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "desktop-vnc-"+task.String()[:8]+"-1.guac") ||
		w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("file: %d %q %v", w.Code, body, w.Header())
	}
	if w := get(failed.ID().String()); w.Code != http.StatusConflict {
		t.Errorf("one not opened: %d", w.Code)
	}
	if w := get(uuid.NewString()); w.Code != http.StatusNotFound {
		t.Errorf("none: %d", w.Code)
	}
	// A terminal's: an asciicast, named for the terminal.
	term := recs.Begin(ctx, recordings.Beginning{TaskID: task, Front: "terminal", Client: "10.0.0.5:41022", Width: 80, Height: 24, Keys: true})
	term.Output([]byte("$ "))
	term.End(models.RecordingEndClosed)
	w = get(term.ID().String())
	body, _ = io.ReadAll(w.Body)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/x-asciicast" ||
		!strings.HasPrefix(string(body), `{"version":2,"width":80,"height":24,`) || !strings.HasSuffix(string(body), `"o","$ "]`+"\n") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "terminal-"+task.String()[:8]+"-3.cast") ||
		w.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Errorf("a terminal's: %d %q %v", w.Code, body, w.Header())
	}
	who = uuid.New()
	if w := get(saved.ID().String()); w.Code != http.StatusNotFound {
		t.Errorf("one not in the operation: %d", w.Code)
	}
}
