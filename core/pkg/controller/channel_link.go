package controller

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// LinkHub holds the channels' control links. Satisfied by
// *channellink.Hub.
type LinkHub interface {
	Serve(ws *websocket.Conn, instance, module string)
}

// ChannelInstances finds a registered channel instance's module name: the
// channel a control link is of. "" with no error for an instance that is
// not a registered channel.
type ChannelInstances func(ctx context.Context, instance string) (module string, err error)

// IChannelLinkController serves the control link's endpoint.
type IChannelLinkController interface {
	Link(c *gin.Context)
}

type channelLinkController struct {
	gate      RegistrationGate
	instances ChannelInstances
	hub       LinkHub
}

// NewChannelLinkController serves control links into hub, behind the sync
// call's registration gate.
func NewChannelLinkController(gate RegistrationGate, instances ChannelInstances, hub LinkHub) IChannelLinkController {
	return &channelLinkController{gate: gate, instances: instances, hub: hub}
}

// Link is the endpoint a channel instance opens its control link on: a
// WebSocket, with the sync call's registration gate. Each binary frame is a
// protocol.LinkFrame: core's calls, the channel's answers.
//
//	@Summary		Channel control link (data plane)
//	@Description	A channel instance opens one: a WebSocket over which core asks it to call bind minions, and it answers each request by its id.
//	@Tags			Channels
//	@Param			Logos-Channel-Instance	header	string	true	"The channel's registered instance id"
//	@Success		101
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse	"instance is not a registered channel"
//	@Failure		503	{object}	responses.ErrorResponse	"module registry unavailable"
//	@Router			/channel/link [get]
func (ctrl *channelLinkController) Link(c *gin.Context) {
	ctx := c.Request.Context()
	log := logger.From(ctx)
	instance := c.Request.Header.Get(protocol.HeaderChannelInstance)
	if instance == "" {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s is required", protocol.HeaderChannelInstance))
		return
	}
	registered, err := ctrl.gate.IsRegistered(ctx, instance)
	var module string
	if err == nil && registered {
		module, err = ctrl.instances(ctx, instance)
	}
	if err != nil {
		log.Warn("channel link: registration check failed, rejecting", zap.String("instance", instance), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, responses.ErrRegistryUnavailable)
		return
	}
	if !registered || module == "" {
		log.Info("channel link: rejected an instance that is not a registered channel", zap.String("instance", instance))
		c.JSON(http.StatusForbidden, responses.ErrChannelNotRegistered)
		return
	}
	ws, err := connUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // the upgrader answered the client
	}
	ctrl.hub.Serve(ws, instance, module)
}
