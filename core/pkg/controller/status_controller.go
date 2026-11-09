package controller

import (
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"go.uber.org/zap"
)

type IStatusController interface {
	Status(c *gin.Context)
}

// LoginOptions is the static part of the /status answer: which login
// surfaces this deployment exposes. OIDCStatus is a func because provider
// availability can change at runtime (discovery retries).
type LoginOptions struct {
	LocalLoginEnabled bool
	OIDCStatus        func() responses.OIDCStatus
}

type statusController struct {
	userRepo repository.IUserRepository
	log      *zap.Logger
	opts     LoginOptions
}

func NewStatusController(
	userRepo repository.IUserRepository,
	log *zap.Logger,
	opts LoginOptions,
) IStatusController {
	if opts.OIDCStatus == nil {
		opts.OIDCStatus = func() responses.OIDCStatus { return responses.OIDCStatus{} }
	}
	return &statusController{
		userRepo: userRepo,
		log:      log,
		opts:     opts,
	}
}

// Status returns the current system state.
//
//	@Summary		System status
//	@Description	Returns system state including whether initial enrollment has been completed.
//	@Tags			Status
//	@Produce		json
//	@Success		200	{object}	responses.StatusResponse
//	@Failure		500	{object}	responses.ErrorResponse
//	@Router			/status [get]
func (ctrl *statusController) Status(c *gin.Context) {
	log := logger.From(c.Request.Context())

	count, err := ctrl.userRepo.Count(c.Request.Context(), "")
	if err != nil {
		log.Error("status: failed to count users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}

	c.JSON(http.StatusOK, responses.StatusResponse{
		Enrolled:          count > 0,
		LocalLoginEnabled: ctrl.opts.LocalLoginEnabled,
		OIDC:              ctrl.opts.OIDCStatus(),
		FrontCert:         FrontCert(c.Request),
	})
}

// The headers core's front (logos-nginx) sets on a request that came over
// HTTPS with a certificate no system trusts: its fingerprint and its
// public key's pin.
const (
	HeaderFrontCertSHA256 = "X-Logos-Cert-SHA256"
	HeaderFrontCertPin    = "X-Logos-Cert-Pin"
)

var frontPin = regexp.MustCompile(`^sha256//[A-Za-z0-9+/]{43}=$`)

// FrontCert is the certificate the front says a request came in with, both
// headers well formed (the fingerprint as openssl prints it); nil
// otherwise. A client that reaches core without the front can say any it
// likes: it is told it back, nobody else.
func FrontCert(r *http.Request) *responses.FrontCert {
	fp := strings.ReplaceAll(strings.TrimSpace(r.Header.Get(HeaderFrontCertSHA256)), ":", "")
	pin := strings.TrimSpace(r.Header.Get(HeaderFrontCertPin))
	b, err := hex.DecodeString(fp)
	if err != nil || len(b) != 32 || !frontPin.MatchString(pin) {
		return nil
	}
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = strings.ToUpper(hex.EncodeToString([]byte{x}))
	}
	return &responses.FrontCert{SHA256: strings.Join(parts, ":"), Pin: pin}
}
