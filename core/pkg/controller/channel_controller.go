package controller

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/logoscore/logos-core/core/pkg/cache"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/minionsync"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// dedupKeyPrefix namespaces channel sync message_id idempotency guards in the
// shared cache.
const dedupKeyPrefix = "channel:sync:msg:"

// RegistrationGate reports whether a module instance is currently registered.
// Satisfied by *modulegate.Gate; the data-plane endpoint rejects sync messages
// from instances that have not registered over the AMQP control plane.
type RegistrationGate interface {
	IsRegistered(ctx context.Context, instance string) (bool, error)
}

type IChannelController interface {
	Sync(c *gin.Context)
}

// MinionHandler opens a minion's sealed message sent under id and returns
// core's sealed answer, nil for none. Satisfied by *minionsync.Service; an
// error wrapping minionsync.ErrUnopened is the sync call's 422.
type MinionHandler interface {
	Handle(ctx context.Context, id string, frame []byte) ([]byte, error)
}

type channelController struct {
	cache   cache.Cache
	gate    RegistrationGate
	minions MinionHandler
	log     *zap.Logger
	maxSize int64
}

// NewChannelController serves the sync endpoint. maxSize caps a request's
// body: the minion's ciphertext. minions answers what the minion sent.
func NewChannelController(c cache.Cache, gate RegistrationGate, minions MinionHandler, log *zap.Logger, maxSize int64) IChannelController {
	return &channelController{
		cache:   c,
		gate:    gate,
		minions: minions,
		log:     log,
		maxSize: maxSize,
	}
}

// traceParentPattern is a W3C traceparent value; core ignores any other.
var traceParentPattern = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// Sync is the data-plane endpoint a channel module calls per inbound minion
// message: the minion's ciphertext as the raw body, its ids in headers. It
// checks the headers, the registration gate and the size before it reads the
// body, dedups on Logos-Message-Id, and hands the message to minions: 200
// with core's sealed answer, 204 when it has none, 422 when the message does
// not open (minion-messages contract).
//
//	@Summary		Channel sync (data plane)
//	@Description	A channel relays one minion message: the ciphertext as an application/octet-stream body, its ids in Logos-* headers. Core answers 200 with ciphertext for the minion, or 204 when it has nothing for it (today, always).
//	@Tags			Channels
//	@Accept			octet-stream
//	@Produce		octet-stream
//	@Param			Logos-Message-Id		header	string	true	"Unique per request, a lowercase UUID"
//	@Param			Logos-Build-Id			header	string	true	"The build id, a lowercase UUID"
//	@Param			Logos-Channel-Instance	header	string	true	"The sending channel's registered instance id"
//	@Param			traceparent				header	string	false	"W3C Trace Context"
//	@Param			request					body	string	true	"The minion's ciphertext"	format(binary)
//	@Success		200
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse	"instance is not a registered module"
//	@Failure		413	{object}	responses.ErrorResponse
//	@Failure		415	{object}	responses.ErrorResponse
//	@Failure		422	{object}	responses.ErrorResponse	"the message does not open"
//	@Failure		503	{object}	responses.ErrorResponse	"module registry unavailable"
//	@Router			/channel/sync [post]
func (ctrl *channelController) Sync(c *gin.Context) {
	ctx := c.Request.Context()
	log := logger.From(ctx)

	if mt, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type")); mt != protocol.SyncContentType {
		c.JSON(http.StatusUnsupportedMediaType, responses.NewErrorResponse("the body must be %s", protocol.SyncContentType))
		return
	}
	in := protocol.InboundFromHeaders(c.Request.Header)
	if !traceParentPattern.MatchString(in.TraceParent) {
		in.TraceParent = ""
	}
	if err := protocol.ValidateInboundHeaders(in); err != nil {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s", validationMessage(err)))
		return
	}

	// Registration gate: reject data-plane traffic from channels that have not
	// registered over the AMQP control plane. Checked before the body is read
	// and before the dedup guard, so an unregistered sender never costs an
	// upload or consumes a dedup slot. This is a registration check, not
	// authentication — the instance id is self-asserted; cryptographic channel
	// auth is a separate follow-up. A registry read failure fails closed (503)
	// rather than admitting unverified senders.
	registered, err := ctrl.gate.IsRegistered(ctx, in.ChannelInstance)
	if err != nil {
		log.Warn("channel sync: registration check failed, rejecting",
			zap.String("instance", in.ChannelInstance), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, responses.ErrRegistryUnavailable)
		return
	}
	if !registered {
		log.Info("channel sync: rejected unregistered instance",
			zap.String("instance", in.ChannelInstance))
		c.JSON(http.StatusForbidden, responses.ErrChannelNotRegistered)
		return
	}

	tooLarge := responses.NewErrorResponse("the body is over the %d-byte limit", ctrl.maxSize)
	if c.Request.ContentLength > ctrl.maxSize {
		c.JSON(http.StatusRequestEntityTooLarge, tooLarge)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, ctrl.maxSize))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			c.JSON(http.StatusRequestEntityTooLarge, tooLarge)
			return
		}
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: reading the body: %v", err))
		return
	}
	in.Data = data
	if err := protocol.ValidateInbound(in); err != nil {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s", validationMessage(err)))
		return
	}

	// Idempotency guard. SetNX reports false when this message id was already
	// seen within the dedup window; the replay gets the same no-op answer
	// rather than an error. Fails open when the cache is disabled (noop cache
	// reports every key as new).
	fresh, err := ctrl.cache.SetNX(ctx, dedupKeyPrefix+in.MessageID, "1", cache.TTLChannelSyncDedup)
	if err != nil {
		log.Warn("channel sync: dedup guard failed, processing anyway",
			zap.String("message_id", in.MessageID), zap.Error(err))
	} else if !fresh {
		log.Debug("channel sync: duplicate message id, returning no-op",
			zap.String("message_id", in.MessageID))
	}

	out := protocol.OutboundMinionMessage{
		MessageID:   protocol.NewID(),
		BuildID:     in.BuildID,
		TraceParent: in.TraceParent,
	}
	if fresh || err != nil {
		reply, err := ctrl.minions.Handle(ctx, in.BuildID, in.Data)
		switch {
		case errors.Is(err, minionsync.ErrUnopened):
			c.JSON(http.StatusUnprocessableEntity, responses.NewErrorResponse("the message does not open"))
			return
		case err != nil:
			log.Error("channel sync: handle the minion's message", zap.String("build_id", in.BuildID), zap.Error(err))
			c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
			return
		}
		out.Data = reply
	}
	out.SetHeaders(c.Writer.Header())
	if len(out.Data) == 0 {
		c.Status(http.StatusNoContent)
		return
	}
	c.Data(http.StatusOK, protocol.SyncContentType, out.Data)
}

// validationMessage is a protocol validation error's message, without its
// code.
func validationMessage(err error) string {
	var vErr *protocol.ValidationError
	if errors.As(err, &vErr) && vErr.Message != "" {
		return vErr.Message
	}
	return err.Error()
}
