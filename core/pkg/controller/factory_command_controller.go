package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/factorycommands"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"go.uber.org/zap"
)

// FactoryCommandTransfers is what the factory reaches of a factory command's
// task through the links its call carries, beyond the files it shares with
// composite tasks (factorycommands.Service): its session's build's artifact
// and files.
type FactoryCommandTransfers interface {
	CheckLink(path, expires, sig string) error
	Artifact(ctx context.Context, task uuid.UUID) (io.ReadCloser, *models.BuildArtifact, error)
	BuildFile(ctx context.Context, task, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error)
}

// FactoryCommandController serves the links of a factory command's call
// (docs: contracts/factory-commands, factory.command) that only such a task
// has: its session's build's artifact and files, to the factory that runs it.
// The link is the credential, as a build's are; it is good while the task
// runs.
type FactoryCommandController struct {
	transfers FactoryCommandTransfers
	logger    *zap.Logger
}

func NewFactoryCommandController(transfers FactoryCommandTransfers, logger *zap.Logger) *FactoryCommandController {
	return &FactoryCommandController{transfers: transfers, logger: logger}
}

// Artifact handles GET /api/factory/tasks/{task}/artifact: the task's
// session's build's artifact, whole.
//
//	@Summary		A session's build's artifact, for a factory command's factory
//	@Tags			Factories
//	@Produce		octet-stream
//	@Param			task	path	string	true	"Task ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		409	{object}	responses.ErrorResponse	"the task does not run"
//	@Router			/factory/tasks/{task}/artifact [get]
func (fc *FactoryCommandController) Artifact(c *gin.Context) {
	task, ok := fc.task(c, factorycommands.ArtifactPath)
	if !ok {
		return
	}
	r, a, err := fc.transfers.Artifact(c.Request.Context(), task)
	if !fc.transferOK(c, task, err) {
		return
	}
	contentType := a.MediaType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	fc.stream(c, task, r, contentType, a.Size)
}

// BuildFile handles GET /api/factory/tasks/{task}/build-files/{file}: one of
// the task's session's build's files, whole.
//
//	@Summary		A session's build's file, for a factory command's factory
//	@Tags			Factories
//	@Produce		octet-stream
//	@Param			task	path	string	true	"Task ID"
//	@Param			file	path	string	true	"Build file ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		409	{object}	responses.ErrorResponse	"the task does not run"
//	@Router			/factory/tasks/{task}/build-files/{file} [get]
func (fc *FactoryCommandController) BuildFile(c *gin.Context) {
	file, err := uuid.Parse(c.Param("file"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", factorycommands.ErrNotFound.Error()))
		return
	}
	task, ok := fc.task(c, func(task uuid.UUID) string { return factorycommands.BuildFilePath(task, file) })
	if !ok {
		return
	}
	r, f, err := fc.transfers.BuildFile(c.Request.Context(), task, file)
	if !fc.transferOK(c, task, err) {
		return
	}
	fc.stream(c, task, r, "application/octet-stream", f.Size)
}

// stream writes r whole, of size bytes, and closes it.
func (fc *FactoryCommandController) stream(c *gin.Context, task uuid.UUID, r io.ReadCloser, contentType string, size int64) {
	defer func() { _ = r.Close() }()
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, r); err != nil {
		fc.logger.Debug("Factory command transfer aborted", zap.String("task_id", task.String()), zap.Error(err))
	}
}

// task reads the path's task and checks the link followed to it: false
// once it has answered.
func (fc *FactoryCommandController) task(c *gin.Context, path func(uuid.UUID) string) (uuid.UUID, bool) {
	task, err := uuid.Parse(c.Param("task"))
	if err != nil || fc.transfers == nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", factorycommands.ErrNotFound.Error()))
		return uuid.Nil, false
	}
	if err := fc.transfers.CheckLink(path(task), c.Query("expires"), c.Query("sig")); err != nil {
		c.JSON(http.StatusForbidden, responses.NewErrorResponse("%s", err.Error()))
		return uuid.Nil, false
	}
	return task, true
}

// transferOK answers a transfer's error: false once it has.
func (fc *FactoryCommandController) transferOK(c *gin.Context, task uuid.UUID, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, factorycommands.ErrNotFound):
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", err.Error()))
	case errors.Is(err, factorycommands.ErrTaskEnded):
		c.JSON(http.StatusConflict, responses.NewErrorResponse("%s", err.Error()))
	default:
		fc.logger.Error("Factory command transfer failed", zap.String("task_id", task.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
	}
	return false
}
