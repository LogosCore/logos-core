package controller

import (
	"context"
	"errors"
	"io"
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
	Upstream(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
	Write(ctx context.Context, taskID uuid.UUID, start, total int64, body io.Reader) (int64, error)
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
	name := downloadName(t.Stream.Name, "stream-"+id.String()[:8])
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

var partRange = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+|\*)$`)

// upstream is the stream_upload task a request names, for a caller with a
// signed link or an operator of its operation; ok false when it answered.
func (sc *MinionStreamController) upstream(c *gin.Context) (*models.MinionTask, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("stream not found"))
		return nil, false
	}
	signed := Signed(c)
	if signed && !sc.streams.Verify(id, c.Query("exp"), c.Query("sig")) {
		c.JSON(http.StatusForbidden, responses.NewErrorResponse("the link is not valid, or has expired"))
		return nil, false
	}
	t, err := sc.streams.Upstream(c.Request.Context(), id)
	if t == nil {
		if err != nil && !repository.IsNotFound(err) {
			sc.logger.Error("Failed to load a stream", zap.String("task_id", id.String()), zap.Error(err))
			c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
			return nil, false
		}
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("stream not found"))
		return nil, false
	}
	if !signed && !callerHasOperationRole(c, sc.opRepo, t.OperationID, models.OperationRoleOperator) {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("stream not found"))
		return nil, false
	}
	if errors.Is(err, minionstreams.ErrGone) {
		c.JSON(http.StatusGone, responses.NewErrorResponse("the stream is over: its task ended"))
		return nil, false
	}
	return t, true
}

// offsetHeaders say where a stream is, and its size once known.
func offsetHeaders(c *gin.Context, at int64, t *models.MinionTask) {
	c.Header("Upload-Offset", strconv.FormatInt(at, 10))
	if t.Upload.Sized {
		c.Header("Upload-Length", strconv.FormatInt(t.Upload.Size, 10))
	}
	c.Header("Cache-Control", "no-store")
}

// Offset handles HEAD /api/v1/minion-streams/{task}: where an
// upload_stream task's stream is (Upload-Offset), and its size once known
// (Upload-Length): where a resumed upload goes on from.
//
//	@Summary		Where a stream's upload is
//	@Tags			Minion tasks
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Task ID"
//	@Router			/minion-streams/{id} [head]
func (sc *MinionStreamController) Offset(c *gin.Context) {
	t, ok := sc.upstream(c)
	if !ok {
		return
	}
	offsetHeaders(c, t.Upload.Received, t)
	c.Status(http.StatusOK)
}

// Upload handles PUT /api/v1/minion-streams/{task}.
//
// The file of an upload_stream task, streamed to its minion as it comes
// (docs: contracts/task-results-and-actions.md, Uploading a stream): the
// whole (from 0, Content-Length its size), or a part (Content-Range:
// bytes N-M/total), only from where the stream is (409 and Upload-Offset
// otherwise). Core reads the body as the minion takes it. 204 with
// Upload-Offset when the body is all taken.
//
//	@Summary		Upload a file streamed to a minion
//	@Tags			Minion tasks
//	@Accept			application/octet-stream
//	@Security		BearerAuth
//	@Param			id				path	string	true	"Task ID"
//	@Param			Content-Range	header	string	false	"bytes N-M/total: a part"
//	@Router			/minion-streams/{id} [put]
func (sc *MinionStreamController) Upload(c *gin.Context) {
	t, ok := sc.upstream(c)
	if !ok {
		return
	}
	start, total := int64(0), c.Request.ContentLength
	if r := c.GetHeader("Content-Range"); r != "" {
		m := partRange.FindStringSubmatch(r)
		if m == nil {
			c.JSON(http.StatusBadRequest, responses.NewErrorResponse("Content-Range is bytes N-M/total"))
			return
		}
		start, _ = strconv.ParseInt(m[1], 10, 64)
		total = -1
		if m[3] != "*" {
			total, _ = strconv.ParseInt(m[3], 10, 64)
		}
	}
	at, err := sc.streams.Write(c.Request.Context(), t.TaskID, start, total, c.Request.Body)
	var off *minionstreams.OffsetError
	switch {
	case errors.As(err, &off):
		offsetHeaders(c, off.At, t)
		c.JSON(http.StatusConflict, responses.NewErrorResponse("the stream is at %d: go on from there", off.At))
		return
	case errors.Is(err, minionstreams.ErrNoSize):
		c.JSON(http.StatusLengthRequired, responses.NewErrorResponse("%s", err.Error()))
		return
	case errors.Is(err, minionstreams.ErrSize):
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("%s", err.Error()))
		return
	case errors.Is(err, minionstreams.ErrGone):
		c.JSON(http.StatusGone, responses.NewErrorResponse("the stream is over: its task ended"))
		return
	case err != nil:
		sc.logger.Info("minion stream upload cut", zap.String("task_id", t.TaskID.String()), zap.Int64("at", at), zap.Error(err))
		offsetHeaders(c, at, t)
		c.JSON(http.StatusInternalServerError, responses.NewErrorResponse("the upload stopped at %d", at))
		return
	}
	offsetHeaders(c, at, t)
	c.Status(http.StatusNoContent)
}
