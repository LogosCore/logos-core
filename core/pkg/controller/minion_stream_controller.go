package controller

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/minionstreams"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"go.uber.org/zap"
)

// MinionStreams is what the stream controller needs (minionstreams.Service).
type MinionStreams interface {
	Stream(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
	Read(ctx context.Context, taskID uuid.UUID, from, end int64, write func([]byte) error) (int64, error)
	Verify(taskID uuid.UUID, exp, sig string) bool
}

// MinionStreamController serves the files minions stream: to a browser or
// curl, a download like any other (docs:
// contracts/task-results-and-actions.md, Downloading a stream).
type MinionStreamController struct {
	streams MinionStreams
	opRepo  repository.IOperationRepository
	logger  *zap.Logger
	// readyWait is how long a request waits for the minion to announce
	// the file.
	readyWait time.Duration
}

func NewMinionStreamController(streams MinionStreams, opRepo repository.IOperationRepository, logger *zap.Logger) *MinionStreamController {
	return &MinionStreamController{streams: streams, opRepo: opRepo, logger: logger, readyWait: time.Hour}
}

// Signed reports whether a request carries a stream's signed link
// (?exp=…&sig=…): it is then let through without a session, and Download
// checks the signature.
func Signed(c *gin.Context) bool {
	return c.Query("sig") != "" && c.Query("exp") != ""
}

var byteRange = regexp.MustCompile(`^bytes=(\d+)-(\d*)$`)

// Download handles GET /api/v1/minion-streams/{task}.
//
// The file a stream_file task's minion streams, as it comes: Content-Length
// the size the minion announced, a Range (bytes=N- or N-M) resumed from
// there (206), If-Range honoured against the ETag. The newest request
// takes the stream; one before it is cut. For anyone who can see the
// operation, or anyone with a signed link (?exp=…&sig=…) for it.
//
//	@Summary		Download a file a minion streams
//	@Tags			Minion tasks
//	@Produce		application/octet-stream
//	@Security		BearerAuth
//	@Param			id		path	string	true	"Task ID"
//	@Param			exp		query	string	false	"Signed link: until (unix seconds)"
//	@Param			sig		query	string	false	"Signed link: its signature"
//	@Param			Range	header	string	false	"bytes=N- to resume"
//	@Router			/minion-streams/{id} [get]
func (sc *MinionStreamController) Download(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("stream not found"))
		return
	}
	signed := Signed(c)
	if signed && !sc.streams.Verify(id, c.Query("exp"), c.Query("sig")) {
		c.JSON(http.StatusForbidden, responses.NewErrorResponse("the link is not valid, or has expired"))
		return
	}
	t, err := sc.streams.Stream(ctx, id)
	if t == nil && err != nil {
		if repository.IsNotFound(err) {
			c.JSON(http.StatusNotFound, responses.NewErrorResponse("stream not found"))
			return
		}
		sc.logger.Error("Failed to load a stream", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	if !signed && !callerHasOperationRole(c, sc.opRepo, t.OperationID, models.OperationRoleViewer) {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("stream not found"))
		return
	}
	// Not announced yet: wait for the minion's next check-in.
	for wait := time.Now().Add(sc.readyWait); errors.Is(err, minionstreams.ErrNotReady); {
		if time.Now().After(wait) {
			c.JSON(http.StatusServiceUnavailable, responses.NewErrorResponse("the minion has not started the stream"))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		t, err = sc.streams.Stream(ctx, id)
	}
	if errors.Is(err, minionstreams.ErrGone) {
		c.JSON(http.StatusGone, responses.NewErrorResponse("the stream is over: its task ended"))
		return
	}
	if err != nil {
		sc.logger.Error("Failed to load a stream", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}

	size := t.Stream.Size
	etag := `"` + t.TaskID.String() + "-" + strconv.FormatInt(size, 10) + `"`
	start, end := int64(0), size
	ranged := false
	if r := c.GetHeader("Range"); r != "" && (c.GetHeader("If-Range") == "" || c.GetHeader("If-Range") == etag) {
		m := byteRange.FindStringSubmatch(r)
		if m == nil {
			c.Header("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, _ = strconv.ParseInt(m[1], 10, 64)
		if m[2] != "" {
			last, _ := strconv.ParseInt(m[2], 10, 64)
			end = min(last+1, size)
		}
		if start >= size || start >= end {
			c.Header("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		ranged = true
	}
	name := unsafeFilename.ReplaceAllString(baseName(t.Stream.Name), "_")
	if name == "" || name == "_" {
		name = "stream-" + id.String()[:8]
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", contentDispositionFor(name, "application/octet-stream", false))
	c.Header("Content-Length", strconv.FormatInt(end-start, 10))
	c.Header("Accept-Ranges", "bytes")
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	// nginx: pass the bytes on as they come.
	c.Header("X-Accel-Buffering", "no")
	status := http.StatusOK
	if ranged {
		status = http.StatusPartialContent
		c.Header("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end-1, 10)+"/"+strconv.FormatInt(size, 10))
	}
	c.Status(status)
	c.Writer.Flush()
	got, err := sc.streams.Read(ctx, id, start, end, func(b []byte) error {
		if _, err := c.Writer.Write(b); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	})
	if got < end {
		// Cut short (the client went, another took the stream, the task
		// ended): the client sees fewer bytes than it was told, and
		// resumes, or not.
		sc.logger.Info("minion stream download cut", zap.String("task_id", id.String()), zap.Int64("at", got), zap.Int64("end", end), zap.Error(err))
	}
}
