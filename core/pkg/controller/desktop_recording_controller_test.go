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

// A desktop's recording is its operation's to read, viewers too: its file
// as it was recorded, an attachment; one whose desktop did not open has
// none.
func TestDesktopRecordingFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	viewer := uuid.New()
	opID, task := uuid.New(), uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{{UserID: viewer, Role: models.OperationRoleViewer}}}}
	recs := testRecordings(task, opID)
	ctx := context.Background()
	saved := recs.Begin(ctx, recordings.Beginning{TaskID: task, Front: "vnc", Username: "ann"})
	stream := "4.size,1.0,4.1024,3.768;4.sync,3.100;"
	saved.FromGuacd([]byte(stream))
	saved.End(models.DesktopRecordingEndClosed)
	failed := recs.Begin(ctx, recordings.Beginning{TaskID: task, Front: "vnc"})
	failed.Fail("core cannot reach guacd")

	dc := NewDesktopRecordingController(recs, ops, zap.NewNop())
	who := viewer
	r := gin.New()
	r.GET("/desktop-recordings/:id", func(c *gin.Context) {
		c.Set("userID", who.String())
		c.Set("roles", []string{"user"})
	}, dc.File)
	get := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/desktop-recordings/"+id, nil))
		return w
	}
	w := get(saved.ID().String())
	body, _ := io.ReadAll(w.Body)
	if w.Code != http.StatusOK || string(body) != stream || w.Header().Get("Content-Length") != strconv.Itoa(len(stream)) ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "desktop-vnc-"+task.String()[:8]+"-1.guac") {
		t.Errorf("file: %d %q %v", w.Code, body, w.Header())
	}
	if w := get(failed.ID().String()); w.Code != http.StatusConflict {
		t.Errorf("one not opened: %d", w.Code)
	}
	if w := get(uuid.NewString()); w.Code != http.StatusNotFound {
		t.Errorf("none: %d", w.Code)
	}
	who = uuid.New()
	if w := get(saved.ID().String()); w.Code != http.StatusNotFound {
		t.Errorf("one not in the operation: %d", w.Code)
	}
}
