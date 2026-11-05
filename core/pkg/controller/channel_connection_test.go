package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/minionsync"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// slowMinions takes its first message slowly (a slow store), then the rest
// at once; each gets a reply.
type slowMinions struct {
	fakeMinions
	slow    time.Duration
	taken   int
	modules []string
}

func (f *slowMinions) Exchange(ctx context.Context, _ *minionsync.Conn, _, _ string, _ []byte) ([]byte, error) {
	f.modules = append(f.modules, minionsync.ChannelOf(ctx))
	if _, ok := ctx.Deadline(); !ok {
		f.modules = append(f.modules, "no deadline")
	}
	f.taken++
	if f.taken == 1 {
		time.Sleep(f.slow)
	}
	return []byte{byte(f.taken)}, nil
}

// A message core takes longer to handle than connSilentFor is not the
// socket's silence: the next one is read, and answered.
func TestConnectionSlowHandlingIsNotSilence(t *testing.T) {
	was := connSilentFor
	connSilentFor = 200 * time.Millisecond
	t.Cleanup(func() { connSilentFor = was })
	gin.SetMode(gin.TestMode)
	minions := &slowMinions{slow: 500 * time.Millisecond}
	ctrl := NewChannelController(fakeGate{registered: true, module: "http"}, minions, zap.NewNop(), 1<<20)
	r := gin.New()
	r.GET("/channel/connection", ctrl.Connection)
	srv := httptest.NewServer(r)
	defer srv.Close()

	h := http.Header{}
	h.Set(protocol.HeaderChannelInstance, "http-1")
	h.Set(protocol.HeaderConnectionKind, protocol.KindReversePersistent)
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/channel/connection", h)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	defer func() { _ = ws.Close() }()
	id := uuid.New()
	for i := 1; i <= 2; i++ {
		frame, err := protocol.EncodeConnFrame(protocol.ConnFrame{ID: id[:], Data: []byte{0xa0}})
		if err != nil {
			t.Fatal(err)
		}
		if err := ws.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
		f, err := protocol.DecodeConnFrame(data)
		if err != nil || len(f.Data) != 1 || int(f.Data[0]) != i {
			t.Fatalf("reply %d: %+v %v", i, f, err)
		}
	}
	// Each message says the channel that carried it.
	if len(minions.modules) != 2 || minions.modules[0] != "http" {
		t.Fatalf("the channel's module: %v", minions.modules)
	}
}
