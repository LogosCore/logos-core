package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"go.uber.org/zap"
)

// BuildService is what the build controller needs. Satisfied by
// *builds.Service.
type BuildService interface {
	UploadFile(ctx context.Context, op uuid.UUID, name, mediaType string, body io.Reader, size int64, actor models.ModuleActor) (*models.BuildFile, error)
	Build(ctx context.Context, id string) (*models.Build, error)
	OpenArtifact(ctx context.Context, buildID string) (io.ReadCloser, *models.Build, error)
}

// BuildController serves the byte paths of minion builds: build file uploads
// and artifact downloads. Everything else is GraphQL (builds.graphql).
type BuildController struct {
	builds  BuildService
	opRepo  repository.IOperationRepository
	logger  *zap.Logger
	maxSize int64
}

func NewBuildController(b BuildService, opRepo repository.IOperationRepository, logger *zap.Logger, maxSize int64) *BuildController {
	return &BuildController{builds: b, opRepo: opRepo, logger: logger, maxSize: maxSize}
}

// BuildFileUploadResponse is the JSON body returned from POST /build-files.
type BuildFileUploadResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType,omitempty"`
}

// UploadFile handles POST /api/v1/build-files (multipart/form-data).
//
//	@Summary		Upload a build file
//	@Description	Upload a file for a build's file fields (an icon, a signing certificate). It is kept while a build refers to it; an upload nothing refers to is removed after a day.
//	@Tags			Builds
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			operationId	formData	string	true	"Operation ID (UUID)"
//	@Param			file		formData	file	true	"File bytes (max 64MB by default)"
//	@Success		201			{object}	BuildFileUploadResponse
//	@Failure		400			{object}	responses.ErrorResponse
//	@Failure		403			{object}	responses.ErrorResponse
//	@Failure		413			{object}	responses.ErrorResponse
//	@Router			/build-files [post]
func (bc *BuildController) UploadFile(c *gin.Context) {
	opID, err := uuid.Parse(c.PostForm("operationId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid operationId"))
		return
	}
	header, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("file is required"))
		return
	}
	if header.Size > bc.maxSize {
		c.JSON(http.StatusRequestEntityTooLarge, responses.NewErrorResponse("file exceeds maximum size of %d bytes", bc.maxSize))
		return
	}
	name := SanitizeUploadFilename(header.Filename)
	if name == "" {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("filename is required"))
		return
	}
	if !callerHasOperationRole(c, bc.opRepo, opID, models.OperationRoleOperator) {
		c.JSON(http.StatusForbidden, responses.ErrForbidden)
		return
	}
	src, err := header.Open()
	if err != nil {
		bc.logger.Error("Failed to open build file upload", zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	defer src.Close()

	actor := models.ModuleActor{Type: "user", ID: c.GetString("userID"), Name: c.GetString("username")}
	mediaType := canonicalContentType(header.Header.Get("Content-Type"))
	f, err := bc.builds.UploadFile(c.Request.Context(), opID, name, mediaType, src, header.Size, actor)
	var invalid *builds.ValidationError
	switch {
	case errors.As(err, &invalid):
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("%s", invalid.Message))
		return
	case err != nil:
		bc.logger.Error("Failed to store a build file", zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	c.JSON(http.StatusCreated, BuildFileUploadResponse{
		ID: f.FileID.String(), Name: f.Name, Size: f.Size, SHA256: f.SHA256, MediaType: f.MediaType,
	})
}

// DownloadArtifact handles GET /api/v1/builds/{id}/artifact.
//
// An artifact holds the minion's keys, so it takes an operator of the
// operation, and it is always an attachment: never rendered.
//
//	@Summary		Download a build's artifact
//	@Tags			Builds
//	@Produce		application/octet-stream
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Build ID"
//	@Router			/builds/{id}/artifact [get]
func (bc *BuildController) DownloadArtifact(c *gin.Context) {
	ctx := c.Request.Context()
	b, err := bc.builds.Build(ctx, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("build not found"))
		return
	}
	if !callerHasOperationRole(c, bc.opRepo, b.OperationID, models.OperationRoleOperator) {
		c.JSON(http.StatusForbidden, responses.ErrForbidden)
		return
	}
	r, b, err := bc.builds.OpenArtifact(ctx, b.BuildID.String())
	var invalid *builds.ValidationError
	switch {
	case errors.As(err, &invalid):
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", invalid.Message))
		return
	case err != nil:
		bc.logger.Error("Failed to open a build artifact", zap.String("build_id", c.Param("id")), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	defer r.Close()

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", contentDispositionFor(b.Artifact.Filename, "application/octet-stream", false))
	c.Header("Content-Length", strconv.FormatInt(b.Artifact.Size, 10))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("ETag", `"`+strings.ToLower(b.Artifact.SHA256)+`"`)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, r); err != nil {
		bc.logger.Debug("Artifact stream aborted", zap.Error(err))
	}
}
