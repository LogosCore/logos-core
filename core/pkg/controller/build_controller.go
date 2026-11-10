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
	CheckLink(path, expires, sig string) error
	FactoryFile(ctx context.Context, build, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error)
	FactoryArtifact(ctx context.Context, build uuid.UUID, body io.Reader, size int64, contentType string) error
}

// BuildController serves the byte paths of minion builds: build file uploads
// and artifact downloads, and a factory's side of them (the links its
// factory.build carries). Everything else is GraphQL (builds.graphql).
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

// FactoryFile handles GET /api/factory/builds/{build}/files/{file}: one of a
// build's files, for the factory building it, from the link its
// factory.build carries (docs: minion-build, Files). The link is the
// credential, signed for the build and the file until a minute past the
// build's deadline; the factory follows it from wherever it reaches core.
//
//	@Summary		A build file, for its factory
//	@Tags			Factories
//	@Produce		octet-stream
//	@Param			build	path	string	true	"Build ID"
//	@Param			file	path	string	true	"File ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		409	{object}	responses.ErrorResponse	"the build has ended"
//	@Router			/factory/builds/{build}/files/{file} [get]
func (bc *BuildController) FactoryFile(c *gin.Context) {
	build, err := uuid.Parse(c.Param("build"))
	file, ferr := uuid.Parse(c.Param("file"))
	if err != nil || ferr != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", builds.ErrTransferNotFound.Error()))
		return
	}
	if !bc.linkOK(c, builds.FilePath(build, file)) {
		return
	}
	r, f, err := bc.builds.FactoryFile(c.Request.Context(), build, file)
	if !bc.transferOK(c, build, err) {
		return
	}
	defer r.Close()

	mediaType := f.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	c.Header("Content-Type", mediaType)
	c.Header("Content-Length", strconv.FormatInt(f.Size, 10))
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, r); err != nil {
		bc.logger.Debug("Build file stream aborted", zap.String("build_id", build.String()), zap.Error(err))
	}
}

// FactoryArtifact handles PUT /api/factory/builds/{build}/artifact: the
// artifact, from the factory building it, through the link its
// factory.build carries. Core streams it into storage, and refuses one
// that does not say its length or is over the build's limit before reading
// a byte. Another upload replaces it.
//
//	@Summary		A build's artifact, from its factory
//	@Tags			Factories
//	@Accept			octet-stream
//	@Param			build	path	string	true	"Build ID"
//	@Param			expires	query	int		true	"The link's expiry, Unix seconds"
//	@Param			sig		query	string	true	"The link's signature"
//	@Param			request	body	string	true	"The artifact"	format(binary)
//	@Success		204
//	@Failure		403	{object}	responses.ErrorResponse	"not a link of core's, or expired"
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		409	{object}	responses.ErrorResponse	"the build has ended"
//	@Failure		411	{object}	responses.ErrorResponse
//	@Failure		413	{object}	responses.ErrorResponse
//	@Router			/factory/builds/{build}/artifact [put]
func (bc *BuildController) FactoryArtifact(c *gin.Context) {
	build, err := uuid.Parse(c.Param("build"))
	if err != nil {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", builds.ErrTransferNotFound.Error()))
		return
	}
	if !bc.linkOK(c, builds.ArtifactPath(build)) {
		return
	}
	err = bc.builds.FactoryArtifact(c.Request.Context(), build, c.Request.Body, c.Request.ContentLength,
		canonicalContentType(c.GetHeader("Content-Type")))
	if !bc.transferOK(c, build, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

// linkOK checks the link a factory followed to path: false once it has
// answered.
func (bc *BuildController) linkOK(c *gin.Context, path string) bool {
	err := bc.builds.CheckLink(path, c.Query("expires"), c.Query("sig"))
	if err == nil {
		return true
	}
	c.JSON(http.StatusForbidden, responses.NewErrorResponse("%s", err.Error()))
	return false
}

// transferOK answers a transfer's error: false once it has.
func (bc *BuildController) transferOK(c *gin.Context, build uuid.UUID, err error) bool {
	var tooLarge *builds.ArtifactTooLargeError
	switch {
	case err == nil:
		return true
	case errors.Is(err, builds.ErrTransferNotFound):
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("%s", err.Error()))
	case errors.Is(err, builds.ErrBuildEnded):
		c.JSON(http.StatusConflict, responses.NewErrorResponse("%s", err.Error()))
	case errors.Is(err, builds.ErrLengthRequired):
		c.JSON(http.StatusLengthRequired, responses.NewErrorResponse("%s", err.Error()))
	case errors.As(err, &tooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, responses.NewErrorResponse("%s", err.Error()))
	default:
		bc.logger.Error("Build transfer failed", zap.String("build_id", build.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
	}
	return false
}
