package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// opsRepo is the operation repository as far as access needs it.
type opsRepo struct {
	repository.IOperationRepository
	ops operationsByID
}

func (o opsRepo) FindByID(ctx context.Context, id uuid.UUID) (models.Operation, error) {
	return o.ops.FindByID(ctx, id)
}

// comingFiles is one file still coming: received of its bytes have come;
// a follower gets the rest, or cut, a reader without one stops at received.
type comingFiles struct {
	MinionFiles
	f        models.MinionFile
	body     []byte
	cut      bool
	followed bool
}

func (c *comingFiles) Open(_ context.Context, id uuid.UUID) (io.ReadCloser, *models.MinionFile, error) {
	if id != c.f.FileID {
		return nil, nil, nil
	}
	f := c.f
	return nil, &f, errors.New("the file is " + f.State)
}

func (c *comingFiles) ReadFrom(_ context.Context, _ uuid.UUID, off int64, follow bool) io.ReadCloser {
	c.followed = follow
	end := c.f.Received
	if follow {
		end = int64(len(c.body))
		if c.cut {
			end = c.f.Received
		}
	}
	return io.NopCloser(io.MultiReader(bytes.NewReader(c.body[off:end]), errorReader{}))
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("the transfer failed") }

// A file still coming: a range of what has come at once, none past it; with
// follow, all of it as it comes, its length the minion's, inline only for a
// type shown so. Nothing is cached; a transfer that fails cuts it short.
func TestDownloadFileComing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	viewer := uuid.New()
	opID := uuid.New()
	ops := operationsByID{opID: {OperationID: opID, Members: []models.OperationMember{{UserID: viewer, Role: models.OperationRoleViewer}}}}
	body := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("rows of pixels ", 20))
	files := &comingFiles{body: body, f: models.MinionFile{FileID: uuid.New(), OperationID: opID, Name: "screenshot-1.png",
		Total: int64(len(body)), Received: 100, State: models.MinionFileReceiving, ContentType: "image/png"}}
	mc := NewMinionTaskController(nil, files, opsRepo{ops: ops}, zap.NewNop())
	who := viewer
	r := gin.New()
	r.GET("/minion-files/:id", func(c *gin.Context) {
		c.Set("userID", who.String())
		c.Set("roles", []string{"user"})
	}, mc.DownloadFile)
	get := func(query, rng string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/minion-files/"+files.f.FileID.String()+query, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("", "bytes=10-59")
	if w.Code != http.StatusPartialContent || w.Body.String() != string(body[10:60]) || w.Header().Get("Content-Range") != "bytes 10-59/"+strconv.Itoa(len(body)) ||
		w.Header().Get("ETag") != "" || w.Header().Get("Cache-Control") != "private, no-store" || files.followed {
		t.Fatalf("a range that came: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if w := get("", "bytes=90-120"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "100 of the file's") {
		t.Fatalf("a range past what came: %d %s", w.Code, w.Body.String())
	}
	if w := get("", ""); w.Code != http.StatusConflict {
		t.Fatalf("all of it, not following: %d", w.Code)
	}
	if w := get("", "bytes=9999-"); w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("past its end: %d", w.Code)
	}

	w = get("?follow=1&preview=1", "")
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), body) || w.Header().Get("Content-Length") != strconv.Itoa(len(body)) ||
		!strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") || w.Header().Get("X-Accel-Buffering") != "no" ||
		w.Header().Get("Content-Type") != "image/png" || !files.followed {
		t.Fatalf("followed: %d %d bytes %v", w.Code, w.Body.Len(), w.Header())
	}
	files.f.ContentType = "text/html; charset=utf-8"
	if w := get("?follow=1&preview=1", ""); !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("a type not shown inline: %v", w.Header())
	}
	files.cut = true
	if w := get("?follow=1", "bytes=5-"); w.Code != http.StatusPartialContent || w.Body.Len() != 95 {
		t.Fatalf("cut short: %d, %d bytes", w.Code, w.Body.Len())
	}
	who = uuid.New()
	if w := get("?follow=1", ""); w.Code != http.StatusNotFound {
		t.Fatalf("one not in the operation: %d", w.Code)
	}
}
