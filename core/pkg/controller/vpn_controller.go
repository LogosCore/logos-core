package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// VPNTunnels is what the VPN controller needs (miniontunnels.Service).
type VPNTunnels interface {
	ConnectVPN(ctx context.Context, raw string, hello protocol.VPNHello, addr string) (*miniontunnels.VPNClient, protocol.VPNWelcome, error)
}

// VPNController serves logos-vpn (docs: contracts/vpn-front): the link
// each client connects over, its key its sign-in, and the client's builds
// for each system.
type VPNController struct {
	vpn       VPNTunnels
	clientDir string
	upgrader  gws.Upgrader
	logger    *zap.Logger
}

// NewVPNController serves the link and the builds in clientDir
// (VPN_CLIENT_DIR: logos-vpn-{os}-{arch}, .exe on Windows).
func NewVPNController(vpn VPNTunnels, clientDir string, logger *zap.Logger) *VPNController {
	return &VPNController{vpn: vpn, clientDir: clientDir, logger: logger,
		upgrader: gws.Upgrader{ReadBufferSize: 32 << 10, WriteBufferSize: 64 << 10,
			// A key signs the link in, never a cookie: a browser's page, which
			// sends an Origin, has no business here.
			CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}}
}

const (
	// vpnHelloWait bounds how long a link waits for the client's hello.
	vpnHelloWait = 10 * time.Second
	// vpnPing is how often core pings a link; vpnReadWait how long it waits
	// to hear anything (a pong among it) before it takes the link for dead.
	vpnPing     = 30 * time.Second
	vpnReadWait = 90 * time.Second
	vpnWrite    = 10 * time.Second
)

// Link handles GET /api/v1/vpn: a client's link, its key its bearer
// (Authorization: Bearer lgv_…). The client's first frame is its hello
// (text: {"hello":{...}}); core answers its welcome, then carries the
// client (miniontunnels.VPNClient.Serve). A client core does not take is
// closed with protocol.VPNCloseRefused, or VPNCloseVersion for a hello of
// another link, and why.
//
//	@Summary	A logos-vpn client's link (WebSocket)
//	@Tags		VPN
//	@Router		/vpn [get]
func (vc *VPNController) Link(c *gin.Context) {
	if !gws.IsWebSocketUpgrade(c.Request) {
		c.JSON(http.StatusBadRequest, responses.NewErrorResponse("a WebSocket for logos-vpn"))
		return
	}
	raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(raw, protocol.VPNKeyPrefix) {
		c.JSON(http.StatusUnauthorized, responses.NewErrorResponse("a VPN key: Authorization: Bearer lgv_…"))
		return
	}
	conn, err := vc.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(protocol.MaxVPNFrame + 4<<10)
	l := &wsLink{conn: conn, done: make(chan struct{})}
	_ = conn.SetReadDeadline(time.Now().Add(vpnHelloWait))
	kind, b, err := conn.ReadMessage()
	var ctl protocol.VPNControl
	if err != nil || kind != gws.TextMessage || len(b) > protocol.MaxVPNControl || json.Unmarshal(b, &ctl) != nil || ctl.Hello == nil {
		l.Close(protocol.VPNCloseVersion, "the client's hello first: {\"hello\":{...}}")
		return
	}
	client, welcome, err := vc.vpn.ConnectVPN(c.Request.Context(), raw, *ctl.Hello, c.ClientIP())
	switch {
	case errors.Is(err, miniontunnels.ErrVPNHello):
		l.Close(protocol.VPNCloseVersion, closeReason("this core's client: GET /api/v1/vpn/client/{os}-{arch}: "+err.Error()))
		return
	case errors.Is(err, miniontunnels.ErrVPNRefused):
		l.Close(protocol.VPNCloseRefused, closeReason(strings.TrimPrefix(err.Error(), miniontunnels.ErrVPNRefused.Error()+": ")))
		return
	case err != nil:
		vc.logger.Error("A VPN client's connect failed", zap.Error(err))
		l.Close(gws.CloseInternalServerErr, "core could not take the client")
		return
	}
	w, _ := json.Marshal(protocol.VPNControl{Welcome: &welcome})
	if l.Write(true, w) != nil {
		l.Close(gws.CloseInternalServerErr, "")
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(vpnReadWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(vpnReadWait)) })
	go l.ping()
	client.Serve(l)
}

// vpnPlatforms are the systems logos-vpn is built for.
var vpnPlatforms = []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64"}

// Client handles GET /api/v1/vpn/client/{os}-{arch}: this core's logos-vpn
// for a system, for anyone signed in.
//
//	@Summary	Download logos-vpn for a system
//	@Tags		VPN
//	@Security	BearerAuth
//	@Param		platform	path	string	true	"linux-amd64, darwin-arm64, windows-amd64, ..."
//	@Router		/vpn/client/{platform} [get]
func (vc *VPNController) Client(c *gin.Context) {
	platform := c.Param("platform")
	if !slices.Contains(vpnPlatforms, platform) {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("logos-vpn is built for %s", strings.Join(vpnPlatforms, ", ")))
		return
	}
	name, saved := "logos-vpn-"+platform, "logos-vpn"
	if strings.HasPrefix(platform, "windows-") {
		name, saved = name+".exe", saved+".exe"
	}
	path := filepath.Join(vc.clientDir, name)
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, responses.NewErrorResponse("this core carries no logos-vpn for %s", platform))
		return
	}
	c.FileAttachment(path, saved)
}

// wsLink is a client's link over a WebSocket (miniontunnels.VPNLink): one
// writer at a time, pings while it lasts.
type wsLink struct {
	conn *gws.Conn
	wmu  sync.Mutex
	done chan struct{}
	once sync.Once
}

func (l *wsLink) Read() (bool, []byte, error) {
	for {
		kind, b, err := l.conn.ReadMessage()
		if err != nil {
			return false, nil, err
		}
		_ = l.conn.SetReadDeadline(time.Now().Add(vpnReadWait))
		switch kind {
		case gws.TextMessage:
			return true, b, nil
		case gws.BinaryMessage:
			return false, b, nil
		}
	}
}

func (l *wsLink) Write(text bool, b []byte) error {
	kind := gws.BinaryMessage
	if text {
		kind = gws.TextMessage
	}
	l.wmu.Lock()
	defer l.wmu.Unlock()
	_ = l.conn.SetWriteDeadline(time.Now().Add(vpnWrite))
	return l.conn.WriteMessage(kind, b)
}

func (l *wsLink) Close(code int, why string) {
	l.once.Do(func() {
		close(l.done)
		l.wmu.Lock()
		_ = l.conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(code, closeReason(why)), time.Now().Add(vpnWrite))
		l.wmu.Unlock()
		_ = l.conn.Close()
	})
}

// ping keeps proxies from taking a quiet link for a dead one, and has the
// client's pongs show it is there.
func (l *wsLink) ping() {
	tick := time.NewTicker(vpnPing)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			l.wmu.Lock()
			err := l.conn.WriteControl(gws.PingMessage, nil, time.Now().Add(vpnWrite))
			l.wmu.Unlock()
			if err != nil {
				return
			}
		case <-l.done:
			return
		}
	}
}
