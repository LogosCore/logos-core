package controller

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"go.uber.org/zap"
)

// MinionTaskController serves a task's output and data as the minion sent
// them, bytes and all. Everything else about tasks is GraphQL
// (minion_tasks.graphql), where the output is text and the data JSON.
type MinionTaskController struct {
	tasks  repository.IMinionTaskRepository
	files  MinionFiles
	opRepo repository.IOperationRepository
	logger *zap.Logger
}

// MinionFiles opens the files minions sent, and keeps the task files
// operators upload (minionfiles.Service).
type MinionFiles interface {
	Open(ctx context.Context, fileID uuid.UUID) (io.ReadCloser, *models.MinionFile, error)
	PutTaskFile(ctx context.Context, operationID, by uuid.UUID, name string, body io.Reader) (*models.MinionFile, error)
}

// TaskFileResponse is the JSON body POST /minion-files answers with.
type TaskFileResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"contentType"`
}

// UploadFile handles POST /api/v1/minion-files (multipart/form-data).
//
// A task file: one an operator uploads for a command's file param, which
// core sends the minion with the task (docs:
// contracts/task-results-and-actions.md, Task files). Streamed into the
// minion files bucket as it comes, never held whole, so operationId must
// come before file. It is kept while a task that names it has not ended;
// one no task names is dropped after a day.
//
//	@Summary		Upload a file to send a minion
//	@Tags			Minion tasks
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			operationId	formData	string	true	"Operation ID (UUID), before file"
//	@Param			file		formData	file	true	"File bytes (at most MINION_FILE_MAX_SIZE, 2 GiB by default)"
//	@Success		201			{object}	TaskFileResponse
//	@Failure		400			{object}	responses.ErrorResponse
//	@Failure		403			{object}	responses.ErrorResponse
//	@Failure		413			{object}	responses.ErrorResponse
//	@Router			/minion-files [post]
func (mc *MinionTaskController) UploadFile(c *gin.Context) {
	mr, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("a multipart/form-data body"))
		return
	}
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusForbidden, responses.ErrForbidden)
		return
	}
	var opID uuid.UUID
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, responses.NewErrorResponse("the body does not read: %s", err.Error()))
			return
		}
		switch part.FormName() {
		case "operationId":
			raw, _ := io.ReadAll(io.LimitReader(part, 64))
			if opID, err = uuid.Parse(strings.TrimSpace(string(raw))); err != nil {
				c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid operationId"))
				return
			}
			if !callerHasOperationRole(c, mc.opRepo, opID, models.OperationRoleOperator) {
				c.JSON(http.StatusForbidden, responses.ErrForbidden)
				return
			}
		case "file":
			if opID == uuid.Nil {
				c.JSON(http.StatusBadRequest, responses.NewErrorResponse("operationId comes before file"))
				return
			}
			name := SanitizeUploadFilename(part.FileName())
			if name == "" {
				c.JSON(http.StatusBadRequest, responses.NewErrorResponse("filename is required"))
				return
			}
			f, err := mc.files.PutTaskFile(c.Request.Context(), opID, uid, name, part)
			switch {
			case errors.Is(err, minionfiles.ErrTooLarge):
				c.JSON(http.StatusRequestEntityTooLarge, responses.NewErrorResponse("the file is over the largest taken"))
				return
			case err != nil:
				mc.logger.Error("Failed to keep a task file", zap.Error(err))
				c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
				return
			}
			c.JSON(http.StatusCreated, TaskFileResponse{ID: f.FileID.String(), Name: f.Name, Size: f.Total,
				SHA256: hex.EncodeToString(f.SHA256), ContentType: f.ContentType})
			return
		}
	}
	c.JSON(http.StatusBadRequest, responses.NewErrorResponse("file is required"))
}

func NewMinionTaskController(tasks repository.IMinionTaskRepository, files MinionFiles, opRepo repository.IOperationRepository, logger *zap.Logger) *MinionTaskController {
	return &MinionTaskController{tasks: tasks, files: files, opRepo: opRepo, logger: logger}
}

// DownloadFile handles GET /api/v1/minion-files/{id}.
//
// A file a minion sent core, as it came: for anyone who can see its
// operation, as a task's output. Its type is what its first bytes say;
// it is an attachment unless ?preview=1 asks for it inline and it is a
// type the browser shows without running anything (as a wiki file's:
// previewAllowedContentTypes). It never changes, so it is cached by its
// SHA-256, and byte ranges are answered: the console's hex view reads it a
// piece at a time, a player seeks.
//
//	@Summary		Download a file a minion sent
//	@Tags			Minion tasks
//	@Produce		application/octet-stream
//	@Security		BearerAuth
//	@Param			id		path	string	true	"File ID"
//	@Param			preview	query	bool	false	"Inline, for a type the browser shows safely"
//	@Router			/minion-files/{id} [get]
func (mc *MinionTaskController) DownloadFile(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("file not found"))
		return
	}
	body, f, err := mc.files.Open(c.Request.Context(), id)
	if f == nil {
		if err != nil && !repository.IsNotFound(err) {
			mc.logger.Error("Failed to load a minion file", zap.String("file_id", id.String()), zap.Error(err))
			c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
			return
		}
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("file not found"))
		return
	}
	if !callerHasOperationRole(c, mc.opRepo, f.OperationID, models.OperationRoleViewer) {
		if body != nil {
			_ = body.Close()
		}
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("file not found"))
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, responses.NewErrorResponse("%s", err.Error()))
		return
	}
	defer body.Close()
	name := downloadName(f.Name, "file-"+id.String()[:8])
	contentType := f.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	preview := c.Query("preview") == "1" || c.Query("preview") == "true"
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", contentDispositionFor(name, contentType, preview))
	c.Header("ETag", fileETagFor(hex.EncodeToString(f.SHA256)))
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", fileCSPFor(contentType))
	if err := writeFileBody(c.Writer, c.Request, body, f.Total); err != nil {
		mc.logger.Debug("Minion file download aborted", zap.Error(err))
	}
}

// baseName is a path's last part, by either separator: the minion's host
// may be either kind.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// downloadName is what a minion's file is saved as: the last part of its
// path on the host, as it is (Cyrillic, spaces and all), its control
// characters dropped; fallback when nothing is left. The name is the host's
// and untrusted: the path goes, and contentDispositionFor encodes the rest
// (an ASCII fallback, and the UTF-8 name percent-encoded in filename*), so
// nothing in it reaches the header raw. The web app's savedName follows it.
func downloadName(p, fallback string) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, baseName(p))
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	return name
}

// unsafeFilename is what a name made of the app's own words (a command)
// keeps out of a download's name.
var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DownloadOutput handles GET /api/v1/minion-tasks/{id}/output.
//
// Anyone who can see the operation can read its tasks' output, as in the
// console. It is always an attachment: what a command printed is never
// rendered.
//
//	@Summary		Download a minion task's raw output
//	@Tags			Minion tasks
//	@Produce		application/octet-stream
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Task ID"
//	@Router			/minion-tasks/{id}/output [get]
func (mc *MinionTaskController) DownloadOutput(c *gin.Context) {
	t, parts, ok := mc.taskParts(c)
	if !ok {
		return
	}
	var out []byte
	for _, p := range parts {
		out = append(out, p.Out...)
	}
	mc.attach(c, t, ".out", "application/octet-stream", out)
}

// DownloadData handles GET /api/v1/minion-tasks/{id}/data.
//
// The parts' data exactly as the minion sent them: a CBOR sequence
// (RFC 8742), one item per part with data, in seq order. As the output,
// for anyone who can see the operation, and always an attachment.
//
//	@Summary		Download a minion task's data as CBOR
//	@Tags			Minion tasks
//	@Produce		application/cbor-seq
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Task ID"
//	@Router			/minion-tasks/{id}/data [get]
func (mc *MinionTaskController) DownloadData(c *gin.Context) {
	t, parts, ok := mc.taskParts(c)
	if !ok {
		return
	}
	// A result kept whole on the task (a walk's progress): its newest.
	if len(t.LatestData) > 0 {
		parts = []models.MinionTaskPart{{Seq: 0, DataCBOR: t.LatestData}}
	}
	data, err := taskdata.Sequence(parts)
	if err != nil {
		mc.logger.Error("Failed to encode a minion task's data", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	mc.attach(c, t, ".cbor", "application/cbor-seq", data)
}

// taskParts is the task the request names, and its parts in seq order,
// for a caller who can see its operation; ok false when it answered.
func (mc *MinionTaskController) taskParts(c *gin.Context) (*models.MinionTask, []models.MinionTaskPart, bool) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return nil, nil, false
	}
	t, err := mc.tasks.FindByTaskID(ctx, id)
	if err != nil {
		if repository.IsNotFound(err) {
			c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
			return nil, nil, false
		}
		mc.logger.Error("Failed to load a minion task", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return nil, nil, false
	}
	if !callerHasOperationRole(c, mc.opRepo, t.OperationID, models.OperationRoleViewer) {
		// Not found rather than forbidden: a task id says nothing to someone
		// outside its operation.
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return nil, nil, false
	}
	parts, err := mc.tasks.Parts(ctx, id)
	if err != nil {
		mc.logger.Error("Failed to load a minion task's parts", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return nil, nil, false
	}
	return t, parts, true
}

// attach answers with body as an attachment named for the task.
func (mc *MinionTaskController) attach(c *gin.Context, t *models.MinionTask, ext, contentType string, body []byte) {
	name := unsafeFilename.ReplaceAllString(t.Command, "_") + "-" + t.TaskID.String()[:8] + ext
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", contentDispositionFor(name, contentType, false))
	c.Header("Content-Length", strconv.Itoa(len(body)))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(body); err != nil {
		mc.logger.Debug("Task download aborted", zap.Error(err))
	}
}
