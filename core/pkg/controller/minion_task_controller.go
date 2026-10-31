package controller

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"go.uber.org/zap"
)

// MinionTaskController serves a task's output as the minion sent it, bytes
// and all. Everything else about tasks is GraphQL (minion_tasks.graphql),
// where the output is text.
type MinionTaskController struct {
	tasks  repository.IMinionTaskRepository
	opRepo repository.IOperationRepository
	logger *zap.Logger
}

func NewMinionTaskController(tasks repository.IMinionTaskRepository, opRepo repository.IOperationRepository, logger *zap.Logger) *MinionTaskController {
	return &MinionTaskController{tasks: tasks, opRepo: opRepo, logger: logger}
}

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
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return
	}
	t, err := mc.tasks.FindByTaskID(ctx, id)
	if err != nil {
		if repository.IsNotFound(err) {
			c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
			return
		}
		mc.logger.Error("Failed to load a minion task", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	if !callerHasOperationRole(c, mc.opRepo, t.OperationID, models.OperationRoleViewer) {
		// Not found rather than forbidden: a task id says nothing to someone
		// outside its operation.
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("task not found"))
		return
	}
	parts, err := mc.tasks.Parts(ctx, id)
	if err != nil {
		mc.logger.Error("Failed to load a minion task's output", zap.String("task_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	var size int
	for _, p := range parts {
		size += len(p.Out)
	}

	name := unsafeFilename.ReplaceAllString(t.Command, "_") + "-" + id.String()[:8] + ".out"
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", contentDispositionFor(name, "application/octet-stream", false))
	c.Header("Content-Length", strconv.Itoa(size))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Status(http.StatusOK)
	for _, p := range parts {
		if _, err := c.Writer.Write(p.Out); err != nil {
			mc.logger.Debug("Task output stream aborted", zap.Error(err))
			return
		}
	}
}
