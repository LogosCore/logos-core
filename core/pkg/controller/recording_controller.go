package controller

import (
	"bytes"
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

// RecordingFiles reads the desktops' and the terminals' recordings
// (recordings.Service).
type RecordingFiles interface {
	Open(ctx context.Context, id uuid.UUID) (io.ReadCloser, *models.Recording, error)
	// Video is a converted desktop recording's video, whole; Keys its keys.
	Video(ctx context.Context, id uuid.UUID) ([]byte, *models.Recording, error)
	Keys(ctx context.Context, id uuid.UUID) (io.ReadCloser, *models.Recording, error)
}

// RecordingController serves the recordings' files (docs:
// contracts/minion-messages/tunnels.md, Recordings).
type RecordingController struct {
	recs   RecordingFiles
	opRepo gqlctx.OperationFinder
	logger *zap.Logger
}

func NewRecordingController(recs RecordingFiles, opRepo gqlctx.OperationFinder, logger *zap.Logger) *RecordingController {
	return &RecordingController{recs: recs, opRepo: opRepo, logger: logger}
}

// File handles GET /api/v1/recordings/{id}.
//
// A recording, for anyone who can see its operation. A desktop's:
// Guacamole's session recording, as guacd writes its own (the protocol's
// instructions the web app got, the operator's mouse and keys between
// them), which Guacamole's player plays and guacenc makes a video of,
// saved as desktop-<front>-<task>-<number>.guac. A terminal's: an
// asciicast v2 (what the process wrote, what was typed, the terminal's
// sizes), which asciinema plays, saved as terminal-<task>-<number>.cast.
// One still recording reads as far as core kept it. An attachment.
//
//	@Summary		Download a desktop's or a terminal's recording
//	@Tags			Minion tasks
//	@Produce		application/octet-stream
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Recording ID"
//	@Router			/recordings/{id} [get]
func (rc *RecordingController) File(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("recording not found"))
		return
	}
	body, rec, err := rc.recs.Open(c.Request.Context(), id)
	if rec == nil {
		if err != nil && !repository.IsNotFound(err) {
			rc.logger.Error("Failed to load a recording", zap.String("recording_id", id.String()), zap.Error(err))
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
	if rec.Video == models.RecordingVideoConverted {
		rc.video(c, id)
		return
	}
	name := fmt.Sprintf("desktop-%s-%s-%d.guac", rec.Front, rec.TaskID.String()[:8], rec.Number)
	contentType := "application/octet-stream"
	if rec.FileFormat() == models.RecordingFormatAsciicast {
		name = fmt.Sprintf("terminal-%s-%d.cast", rec.TaskID.String()[:8], rec.Number)
		contentType = "application/x-asciicast"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", contentDispositionFor(name, contentType, false))
	c.Header("X-Content-Type-Options", "nosniff")
	// One recording grows. A desktop's that ended becomes a video where core
	// has an encoder, at this same URL: kept, the browser would play these
	// bytes as the video, so it asks again each time. A terminal's that
	// ended stays as it is.
	switch {
	case rec.State == models.RecordingStateRecording:
		c.Header("Cache-Control", "no-store")
	case rec.FileFormat() != models.RecordingFormatAsciicast:
		c.Header("Cache-Control", "no-cache")
	default:
		c.Header("Cache-Control", "private, max-age=3600")
	}
	if err := writeFileBody(c.Writer, c.Request, body, rec.Size); err != nil {
		rc.logger.Debug("Recording download aborted", zap.String("recording_id", id.String()), zap.Error(err))
	}
}

// video serves a converted desktop recording's video: H.264 in MP4,
// desktop-<front>-<task>-<number>.mp4, in ranges (a player seeks in it).
func (rc *RecordingController) video(c *gin.Context, id uuid.UUID) {
	v, rec, err := rc.recs.Video(c.Request.Context(), id)
	if err != nil {
		rc.logger.Error("Failed to read a recording's video", zap.String("recording_id", id.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	name := fmt.Sprintf("desktop-%s-%s-%d.mp4", rec.Front, rec.TaskID.String()[:8], rec.Number)
	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Disposition", contentDispositionFor(name, "video/mp4", false))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=3600")
	modified := rec.UpdateAt
	http.ServeContent(c.Writer, c.Request, name, modified, bytes.NewReader(v))
}

// Keys handles GET /api/v1/recordings/{id}/keys: a converted desktop
// recording's keys (recordings.RecordingKeys: guacd's first sync, and each
// key instruction's keysym, pressed and timestamp), for anyone who can see
// its operation; 409 for one that is not a video.
//
//	@Summary		A desktop recording's keys, once it is a video
//	@Tags			Minion tasks
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Recording ID"
//	@Router			/recordings/{id}/keys [get]
func (rc *RecordingController) Keys(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("recording not found"))
		return
	}
	body, rec, err := rc.recs.Keys(c.Request.Context(), id)
	if rec == nil || !callerHasOperationRole(c, rc.opRepo, rec.OperationID, models.OperationRoleViewer) {
		if body != nil {
			_ = body.Close()
		}
		if err != nil && rec == nil && !repository.IsNotFound(err) {
			rc.logger.Error("Failed to load a recording", zap.String("recording_id", id.String()), zap.Error(err))
			c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
			return
		}
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("recording not found"))
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, recordings.ErrNotVideo) {
			status = http.StatusConflict
		}
		c.JSON(status, responses.NewErrorResponse("%s", err.Error()))
		return
	}
	defer func() { _ = body.Close() }()
	c.Header("Content-Type", "application/json")
	c.Header("Cache-Control", "private, max-age=3600")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}
