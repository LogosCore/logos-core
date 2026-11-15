package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"go.uber.org/zap"
)

// DesktopRecordingFiles reads the desktops' recordings (recordings.Service).
type DesktopRecordingFiles interface {
	Open(ctx context.Context, id uuid.UUID) (io.ReadCloser, *models.DesktopRecording, error)
}

// DesktopRecordingController serves the desktops' recordings' files (docs:
// contracts/minion-messages/tunnels.md, Recordings).
type DesktopRecordingController struct {
	recs   DesktopRecordingFiles
	opRepo gqlctx.OperationFinder
	logger *zap.Logger
}

func NewDesktopRecordingController(recs DesktopRecordingFiles, opRepo gqlctx.OperationFinder, logger *zap.Logger) *DesktopRecordingController {
	return &DesktopRecordingController{recs: recs, opRepo: opRepo, logger: logger}
}

// File handles GET /api/v1/desktop-recordings/{id}.
//
// A desktop's recording, for anyone who can see its operation: Guacamole's
// session recording, as guacd writes its own (the protocol's instructions
// the web app got, the operator's mouse and keys between them), which
// Guacamole's player plays and guacenc makes a video of. One still
// recording reads as far as core kept it. An attachment, saved as
// desktop-<front>-<task>-<number>.guac.
//
//	@Summary		Download a desktop's recording
//	@Tags			Minion tasks
//	@Produce		application/octet-stream
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Recording ID"
//	@Router			/desktop-recordings/{id} [get]
func (rc *DesktopRecordingController) File(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("recording not found"))
		return
	}
	body, rec, err := rc.recs.Open(c.Request.Context(), id)
	if rec == nil {
		if err != nil && !repository.IsNotFound(err) {
			rc.logger.Error("Failed to load a desktop recording", zap.String("recording_id", id.String()), zap.Error(err))
			c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
			return
		}
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("recording not found"))
		return
	}
	if !callerHasOperationRole(c, rc.opRepo, rec.OperationID, models.OperationRoleViewer) {
		if body != nil {
			_ = body.Close()
		}
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("recording not found"))
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, recordings.ErrNothing) {
			status = http.StatusConflict
		}
		c.JSON(status, responses.NewErrorResponse("%s", err.Error()))
		return
	}
	defer func() { _ = body.Close() }()
	name := fmt.Sprintf("desktop-%s-%s-%d.guac", rec.Front, rec.TaskID.String()[:8], rec.Number)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", contentDispositionFor(name, "application/octet-stream", false))
	c.Header("X-Content-Type-Options", "nosniff")
	// One that ended stays as it is; one recording grows.
	if rec.State == models.DesktopRecordingRecording {
		c.Header("Cache-Control", "no-store")
	} else {
		c.Header("Cache-Control", "private, max-age=3600")
	}
	if err := writeFileBody(c.Writer, c.Request, body, rec.Size); err != nil {
		rc.logger.Debug("Desktop recording download aborted", zap.String("recording_id", id.String()), zap.Error(err))
	}
}
