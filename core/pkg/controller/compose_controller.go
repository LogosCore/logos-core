package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/composites"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// ComposeTransfers is what the factory reaches of a composite task through
// the links its calls carry (composites.Service).
type ComposeTransfers interface {
	CheckLink(path, expires, sig string) error
	Data(ctx context.Context, task uuid.UUID) ([]byte, error)
	Out(ctx context.Context, task uuid.UUID) ([]byte, error)
	File(ctx context.Context, task, file uuid.UUID) (io.ReadCloser, *models.MinionFile, error)
	PutFile(ctx context.Context, task uuid.UUID, name string, body io.Reader, size int64) (protocol.BuildFile, error)
}

// ComposeController serves the links a composite task's calls carry (docs:
// contracts/task-outsourcing, What travels, and how; Files): what an end
// returned, larger than a call carries, and the files tasks got, to the
// factory; the files the factory puts for the task, from it. The link is
// the credential, signed for one call, as a build's are.
type ComposeController struct {
	transfers ComposeTransfers
	logger    *zap.Logger
}

func NewComposeController(transfers ComposeTransfers, logger *zap.Logger) *ComposeController {
	return &ComposeController{transfers: transfers, logger: logger}
}

// Data handles GET /api/factory/tasks/{task}/data: a task's data, as JSON,
// as an end gives it.
//
//	@Summary		A task's data, for a composite task's factory
//	@Tags			Factories
//	@Produce		json
//	@Param			task	path	string	true	"Task ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Router			/factory/tasks/{task}/data [get]
func (cc *ComposeController) Data(c *gin.Context) {
	task, ok := cc.task(c, composites.DataPath)
	if !ok {
		return
	}
	data, err := cc.transfers.Data(c.Request.Context(), task)
	if !cc.transferOK(c, task, err) {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(http.StatusOK, "application/json", data)
}

// Out handles GET /api/factory/tasks/{task}/out: a task's output, as its
// minion sent it.
//
//	@Summary		A task's output, for a composite task's factory
//	@Tags			Factories
//	@Produce		octet-stream
//	@Param			task	path	string	true	"Task ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Router			/factory/tasks/{task}/out [get]
func (cc *ComposeController) Out(c *gin.Context) {
	task, ok := cc.task(c, composites.OutPath)
	if !ok {
		return
	}
	out, err := cc.transfers.Out(c.Request.Context(), task)
	if !cc.transferOK(c, task, err) {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(http.StatusOK, "application/octet-stream", out)
}

// File handles GET /api/factory/tasks/{task}/files/{file}: a file a task
// got (an end lists it), whole.
//
//	@Summary		A task's file, for a composite task's factory
//	@Tags			Factories
//	@Produce		octet-stream
//	@Param			task	path	string	true	"Task ID"
//	@Param			file	path	string	true	"File ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Router			/factory/tasks/{task}/files/{file} [get]
func (cc *ComposeController) File(c *gin.Context) {
	file, err := uuid.Parse(c.Param("file"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", composites.ErrNotFound.Error()))
		return
	}
	task, ok := cc.task(c, func(task uuid.UUID) string { return composites.FilePath(task, file) })
	if !ok {
		return
	}
	r, f, err := cc.transfers.File(c.Request.Context(), task, file)
	if !cc.transferOK(c, task, err) {
		return
	}
	defer func() { _ = r.Close() }()
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(f.Total, 10))
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, r); err != nil {
		cc.logger.Debug("Composite task file stream aborted", zap.String("task_id", task.String()), zap.Error(err))
	}
}

// PutFile handles PUT /api/factory/tasks/{task}/files: a file the factory
// puts for a composite task that runs, whole, with its length and its name
// (Logos-File-Name). Core streams it into storage, refusing one that does
// not say its length or is over core's limit before reading a byte, and
// answers with the file's id, size and SHA-256 as it kept it.
//
//	@Summary		A file a composite task's factory puts for it
//	@Tags			Factories
//	@Accept			octet-stream
//	@Produce		json
//	@Param			task			path	string	true	"Task ID"
//	@Param			Logos-File-Name	header	string	true	"The file's name"
//	@Param			expires			query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig				query	string	true	"The link's signature"
//	@Param			request			body	string	true	"The file"	format(binary)
//	@Success		201	{object}	protocol.BuildFile
//	@Failure		400	{object}	responses.ErrorResponse	"no file name"
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		409	{object}	responses.ErrorResponse	"the task does not run"
//	@Failure		411	{object}	responses.ErrorResponse
//	@Failure		413	{object}	responses.ErrorResponse
//	@Router			/factory/tasks/{task}/files [put]
func (cc *ComposeController) PutFile(c *gin.Context) {
	task, ok := cc.task(c, composites.UploadPath)
	if !ok {
		return
	}
	f, err := cc.transfers.PutFile(c.Request.Context(), task, c.GetHeader(protocol.HeaderFileName), c.Request.Body, c.Request.ContentLength)
	if !cc.transferOK(c, task, err) {
		return
	}
	c.JSON(http.StatusCreated, f)
}

// task reads the path's task and checks the link followed to it: false
// once it has answered.
func (cc *ComposeController) task(c *gin.Context, path func(uuid.UUID) string) (uuid.UUID, bool) {
	task, err := uuid.Parse(c.Param("task"))
	if err != nil || cc.transfers == nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", composites.ErrNotFound.Error()))
		return uuid.Nil, false
	}
	if err := cc.transfers.CheckLink(path(task), c.Query("expires"), c.Query("sig")); err != nil {
		c.JSON(http.StatusForbidden, responses.NewErrorResponse("%s", err.Error()))
		return uuid.Nil, false
	}
	return task, true
}

// transferOK answers a transfer's error: false once it has.
func (cc *ComposeController) transferOK(c *gin.Context, task uuid.UUID, err error) bool {
	var tooLarge *composites.FileTooLargeError
	switch {
	case err == nil:
		return true
	case errors.Is(err, composites.ErrNotFound):
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", err.Error()))
	case errors.Is(err, composites.ErrTaskEnded):
		c.JSON(http.StatusConflict, responses.NewErrorResponse("%s", err.Error()))
	case errors.Is(err, composites.ErrNoFileName):
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("%s", err.Error()))
	case errors.Is(err, composites.ErrLengthRequired):
		c.JSON(http.StatusLengthRequired, responses.NewErrorResponse("%s", err.Error()))
	case errors.As(err, &tooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, responses.NewErrorResponse("%s", err.Error()))
	default:
		cc.logger.Error("Composite task transfer failed", zap.String("task_id", task.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
	}
	return false
}
