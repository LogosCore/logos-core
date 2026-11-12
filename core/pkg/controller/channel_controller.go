package controller

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/minionsync"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// handleWithin bounds how long core takes over one minion message. The
// handling is core's own: a channel that stops waiting (its request timed
// out, its socket closed) does not cut it short, and a message sent again
// gets the reply it got (minionsync's replies kept).
const handleWithin = 2 * time.Minute

// RegistrationGate reports whether a module instance is currently registered.
// Satisfied by *modulegate.Gate; the data-plane endpoint rejects sync messages
// from instances that have not registered over the AMQP control plane.
type RegistrationGate interface {
	IsRegistered(ctx context.Context, instance string) (bool, error)
	// Registration is whether instance is registered, and its module's
	// name ("" when not known).
	Registration(ctx context.Context, instance string) (module string, registered bool, err error)
}

type IChannelController interface {
	Sync(c *gin.Context)
	Connection(c *gin.Context)
}

// MinionHandler opens a minion's sealed message sent under id and returns
// core's sealed answer, nil for none. Satisfied by *minionsync.Service; an
// error wrapping minionsync.ErrUnopened is the sync call's 422.
type MinionHandler interface {
	// Handle takes a frame sent under id, from peer: the IP address the
	// channel got it from, "" when it has none.
	Handle(ctx context.Context, id, peer string, frame []byte) ([]byte, error)
	// Exchange takes a frame that came over a persistent connection.
	Exchange(ctx context.Context, via *minionsync.Conn, id, peer string, frame []byte) ([]byte, error)
	// KeepAlive and Closed tell the sessions a connection serves that it is
	// open, and that it closed.
	KeepAlive(ctx context.Context, c *minionsync.Conn)
	Closed(ctx context.Context, c *minionsync.Conn)
	// OpenedFor is the target an open a bind-persistent connection's socket
	// names is for (taken: ok false for one core is not waiting for), and
	// ForTarget makes a connection that open's target's once its socket is
	// up (false: the open no longer waits, and the socket closes).
	OpenedFor(instance string, request uint64) (target uuid.UUID, policy string, ok bool)
	ForTarget(c *minionsync.Conn, instance string, request uint64) bool
}

type channelController struct {
	gate    RegistrationGate
	minions MinionHandler
	log     *zap.Logger
	maxSize int64
}

// NewChannelController serves the sync endpoint. maxSize caps a request's
// body: the minion's ciphertext. minions answers what the minion sent.
func NewChannelController(gate RegistrationGate, minions MinionHandler, log *zap.Logger, maxSize int64) IChannelController {
	return &channelController{
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
// body, and hands the message to minions: 200 with core's sealed answer, 204
// when it has none, 409 for a replay, 422 when the message does not open,
// 500 when core fails handling it (docs: contracts/channel-core-sync.md).
// A message sent again is known by the minion's own message id, sealed
// inside it, never by Logos-Message-Id.
//
//	@Summary		Channel sync (data plane)
//	@Description	A channel relays one minion message: the ciphertext as an application/octet-stream body, its ids in Logos-* headers. Core answers 200 with ciphertext for the minion, or 204 when it has nothing for it. A message the minion sends again (by its own message id, sealed inside it, not Logos-Message-Id) gets the reply it got while core keeps it (10 minutes, a reply of at most 64 KiB), and is a replay (409) after that.
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
//	@Failure		409	{object}	responses.ErrorResponse	"a replay: the message was seen before and its reply is no longer kept"
//	@Failure		413	{object}	responses.ErrorResponse
//	@Failure		415	{object}	responses.ErrorResponse
//	@Failure		422	{object}	responses.ErrorResponse	"the message does not open"
//	@Failure		500	{object}	responses.ErrorResponse	"core failed handling a message that opened"
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
	// What a channel says the peer was is kept only as an IP address.
	if a, err := netip.ParseAddr(in.PeerAddress); err == nil {
		in.PeerAddress = a.Unmap().WithZone("").String()
	} else {
		in.PeerAddress = ""
	}
	if err := protocol.ValidateInboundHeaders(in); err != nil {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s", validationMessage(err)))
		return
	}

	// Registration gate: reject data-plane traffic from channels that have not
	// registered over the AMQP control plane. Checked before the body is read,
	// so an unregistered sender never costs an upload. This is a registration
	// check, not authentication — the instance id is self-asserted;
	// cryptographic channel auth is a separate follow-up. A registry read
	// failure fails closed (503) rather than admitting unverified senders.
	module, registered, err := ctrl.gate.Registration(ctx, in.ChannelInstance)
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

	// A request sent again (the channel retrying one it is unsure of) is the
	// minion's message sent again: minionsync answers it with the reply it
	// got, or refuses it as a replay, by the minion's own message id.
	out := protocol.OutboundMinionMessage{
		MessageID:   protocol.NewID(),
		BuildID:     in.BuildID,
		TraceParent: in.TraceParent,
	}
	// Core's own work, within handleWithin, through the channel that
	// carried it: a hello naming no connection came through the build's of
	// that channel.
	hctx, cancel := context.WithTimeout(context.WithoutCancel(minionsync.ViaChannel(ctx, module)), handleWithin)
	reply, err := ctrl.minions.Handle(hctx, in.BuildID, in.PeerAddress, in.Data)
	cancel()
	switch {
	case errors.Is(err, minionsync.ErrUnopened):
		c.JSON(http.StatusUnprocessableEntity, responses.NewErrorResponse("the message does not open"))
		return
	case errors.Is(err, minionsync.ErrReplayed):
		c.JSON(http.StatusConflict, responses.NewErrorResponse("the message was seen before"))
		return
	case err != nil:
		log.Error("channel sync: handle the minion's message", zap.String("build_id", in.BuildID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, responses.ErrInternalError)
		return
	}
	out.Data = reply
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
