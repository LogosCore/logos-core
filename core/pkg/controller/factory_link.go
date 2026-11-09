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

// FactoryLinkHub holds the factories' links. Satisfied by
// *factorylink.Hub.
type FactoryLinkHub interface {
	Serve(ws *websocket.Conn, instance, module string)
}

// FactoryInstances finds a registered factory instance's module name: the
// factory a link is of. "" with no error for an instance that is not a
// registered factory.
type FactoryInstances func(ctx context.Context, instance string) (module string, err error)

// IFactoryLinkController serves the factory link's endpoint.
type IFactoryLinkController interface {
	Link(c *gin.Context)
}

type factoryLinkController struct {
	gate      RegistrationGate
	instances FactoryInstances
	hub       FactoryLinkHub
}

// NewFactoryLinkController serves factory links into hub, behind the sync
// call's registration gate.
func NewFactoryLinkController(gate RegistrationGate, instances FactoryInstances, hub FactoryLinkHub) IFactoryLinkController {
	return &factoryLinkController{gate: gate, instances: instances, hub: hub}
}

// Link is the endpoint a factory instance that translates its minions'
// codec opens its link on: a WebSocket, with the sync call's registration
// gate. Each binary frame is a protocol.FactoryCall (core's) or a
// protocol.FactoryAnswerFrame (the factory's).
//
//	@Summary		Factory link (data plane)
//	@Description	A factory instance that translates its minions' suite or envelope opens one: a WebSocket over which core asks it to read and write its minions' messages and make its suite's keys, and it answers each request by its id.
//	@Tags			Factories
//	@Param			Logos-Factory-Instance	header	string	true	"The factory's registered instance id"
//	@Success		101
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse	"instance is not a registered factory"
//	@Failure		503	{object}	responses.ErrorResponse	"module registry unavailable"
//	@Router			/factory/link [get]
func (ctrl *factoryLinkController) Link(c *gin.Context) {
	ctx := c.Request.Context()
	log := logger.From(ctx)
	instance := c.Request.Header.Get(protocol.HeaderFactoryInstance)
	if instance == "" {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s is required", protocol.HeaderFactoryInstance))
		return
	}
	registered, err := ctrl.gate.IsRegistered(ctx, instance)
	var module string
	if err == nil && registered {
		module, err = ctrl.instances(ctx, instance)
	}
	if err != nil {
		log.Warn("factory link: registration check failed, rejecting", zap.String("instance", instance), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, responses.ErrRegistryUnavailable)
		return
	}
	if !registered || module == "" {
		log.Info("factory link: rejected an instance that is not a registered factory", zap.String("instance", instance))
		c.JSON(http.StatusForbidden, responses.ErrFactoryNotRegistered)
		return
	}
	ws, err := connUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // the upgrader answered the client
	}
	ctrl.hub.Serve(ws, instance, module)
}
