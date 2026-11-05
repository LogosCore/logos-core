package controller

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/minionsync"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Connection sockets (docs: contracts/channel-core-connections/): a channel
// opens one for each persistent minion connection, and its messages go over
// it both ways, core sending whenever it has something for a session the
// connection serves.
// connSilentFor closes a socket nothing came over, not even a pong, for
// that long while core waited for it.
var connSilentFor = 90 * time.Second

const (
	connPingEvery = 30 * time.Second
	connWriteWait = 10 * time.Second
	// connKeepAlive is how often the sessions a connection serves are
	// checked in while it is open.
	connKeepAlive = 30 * time.Second
	// connFrameOverhead is what a frame adds to its message.
	connFrameOverhead = 64
)

var connUpgrader = websocket.Upgrader{
	// Channels are not browsers: no origin to check.
	CheckOrigin: func(*http.Request) bool { return true },
}

// Connection is the endpoint a channel opens a connection socket on: a
// WebSocket, with the sync call's registration gate. Each binary frame is a
// protocol.ConnFrame either way.
//
//	@Summary		Channel connection socket (data plane)
//	@Description	A channel opens one per persistent minion connection: a WebSocket whose binary frames carry the connection's messages both ways.
//	@Tags			Channels
//	@Param			Logos-Channel-Instance	header	string	true	"The channel's registered instance id"
//	@Param			Logos-Connection-Kind	header	string	true	"reverse-persistent or bind-persistent"
//	@Param			Logos-Open-Request		header	string	false	"bind-persistent: the control link's open it answers"
//	@Param			Logos-Peer-Address		header	string	false	"The minion's address, as the channel saw it"
//	@Success		101
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse	"instance is not a registered module"
//	@Failure		503	{object}	responses.ErrorResponse	"module registry unavailable"
//	@Router			/channel/connection [get]
func (ctrl *channelController) Connection(c *gin.Context) {
	ctx := c.Request.Context()
	log := logger.From(ctx)
	h := c.Request.Header
	instance, kind := h.Get(protocol.HeaderChannelInstance), h.Get(protocol.HeaderConnectionKind)
	peer := ""
	if a, err := netip.ParseAddr(h.Get(protocol.HeaderPeerAddress)); err == nil {
		peer = a.Unmap().WithZone("").String()
	}
	request, _ := strconv.ParseUint(h.Get(protocol.HeaderOpenRequest), 10, 64)
	switch {
	case instance == "":
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s is required", protocol.HeaderChannelInstance))
		return
	case kind != protocol.KindReversePersistent && kind != protocol.KindBindPersistent:
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: %s must be %s or %s", protocol.HeaderConnectionKind,
			protocol.KindReversePersistent, protocol.KindBindPersistent))
		return
	case kind == protocol.KindBindPersistent && request == 0:
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("invalid input: a %s socket names its open (%s)", kind, protocol.HeaderOpenRequest))
		return
	}
	module, registered, err := ctrl.gate.Registration(ctx, instance)
	if err != nil {
		log.Warn("channel connection: registration check failed, rejecting", zap.String("instance", instance), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, responses.ErrRegistryUnavailable)
		return
	}
	if !registered {
		log.Info("channel connection: rejected unregistered instance", zap.String("instance", instance))
		c.JSON(http.StatusForbidden, responses.ErrChannelNotRegistered)
		return
	}
	// A bind-persistent socket answers an open core is waiting for: its
	// target's connection.
	var target uuid.UUID
	var policy string
	if kind == protocol.KindBindPersistent {
		var ok bool
		if target, policy, ok = ctrl.minions.OpenedFor(instance, request); !ok {
			log.Info("channel connection: an open core is not waiting for", zap.String("instance", instance), zap.Uint64("request", request))
			c.JSON(http.StatusConflict, responses.NewErrorResponse("no open %d of this instance waits for its socket", request))
			return
		}
	}
	ws, err := connUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // the upgrader answered the client
	}
	ctrl.serveConnection(ws, instance, module, kind, peer, target, policy, request)
}

// serveConnection reads the socket's frames into the sync service until it
// closes, pinging it, and keeps the sessions it serves checked in.
func (ctrl *channelController) serveConnection(ws *websocket.Conn, instance, module, kind, peer string, target uuid.UUID, policy string,
	request uint64) {
	// The socket outlives the handshake's request: its context is its own,
	// saying the channel that carries its messages.
	ctx, cancel := context.WithCancel(minionsync.ViaChannel(context.Background(), module))
	defer cancel()
	log := ctrl.log.With(zap.String("instance", instance), zap.String("peer", peer))
	var writeMu sync.Mutex
	write := func(messageType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := ws.SetWriteDeadline(time.Now().Add(connWriteWait)); err != nil {
			return err
		}
		return ws.WriteMessage(messageType, data)
	}
	conn := minionsync.NewConn(uuid.NewString(), kind, instance, peer, func(_ context.Context, f protocol.ConnFrame) error {
		b, err := protocol.EncodeConnFrame(f)
		if err != nil {
			return err
		}
		return write(websocket.BinaryMessage, b)
	}, func() { _ = ws.Close() })
	log = log.With(zap.String("conn", conn.ID))
	if target != uuid.Nil {
		log = log.With(zap.String("target_id", target.String()), zap.String("policy", policy))
		if !ctrl.minions.ForTarget(conn, instance, request) {
			// Its open gave up on it (it came too late), or its target's
			// call ended: the socket is no one's.
			log.Info("channel connection: its open no longer waits for it, closing")
			_ = ws.Close()
			return
		}
	}
	log.Info("channel connection opened", zap.String("kind", kind))
	defer func() {
		_ = ws.Close()
		ctrl.minions.Closed(context.Background(), conn)
		log.Info("channel connection closed")
	}()

	ws.SetReadLimit(ctrl.maxSize + connFrameOverhead)
	silent := connSilentFor
	alive := func() error { return ws.SetReadDeadline(time.Now().Add(silent)) }
	_ = alive()
	ws.SetPongHandler(func(string) error { return alive() })
	go func() {
		ping, keep := time.NewTicker(connPingEvery), time.NewTicker(connKeepAlive)
		defer ping.Stop()
		defer keep.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				writeMu.Lock()
				err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(connWriteWait))
				writeMu.Unlock()
				if err != nil {
					_ = ws.Close()
					return
				}
			case <-keep.C:
				ctrl.minions.KeepAlive(ctx, conn)
			}
		}
	}()

	for {
		// Silent for its time from now, not from the last frame: the
		// handling of a message (a slow store) does not count as silence.
		_ = alive()
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = alive()
		if mt != websocket.BinaryMessage {
			log.Info("channel connection: a text frame, closing")
			return
		}
		f, err := protocol.DecodeConnFrame(data)
		if err != nil {
			log.Info("channel connection: a frame that does not read, closing", zap.Error(err))
			return
		}
		id, _ := uuid.FromBytes(f.ID)
		reply, err := ctrl.minions.Exchange(ctx, conn, id.String(), peer, f.Data)
		switch {
		case errors.Is(err, minionsync.ErrUnopened), errors.Is(err, minionsync.ErrReplayed):
			// As the sync call's 422 and 409: nothing to tell the minion.
			log.Debug("channel connection: a message not taken", zap.Error(err))
			continue
		case err != nil:
			log.Error("channel connection: handle the minion's message", zap.Error(err))
			continue
		}
		if len(reply) == 0 {
			continue
		}
		b, err := protocol.EncodeConnFrame(protocol.ConnFrame{ID: f.ID, Data: reply})
		if err == nil {
			err = write(websocket.BinaryMessage, b)
		}
		if err != nil {
			log.Info("channel connection: a reply did not go out, closing", zap.Error(err))
			return
		}
	}
}
