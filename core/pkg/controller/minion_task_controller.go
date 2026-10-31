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
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"go.uber.org/zap"
)

// MinionTaskController serves a task's output and data as the minion sent
// them, bytes and all. Everything else about tasks is GraphQL
// (minion_tasks.graphql), where the output is text and the data JSON.
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
