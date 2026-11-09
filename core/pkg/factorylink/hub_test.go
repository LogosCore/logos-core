package factorylink

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// serve is core's end: every handshake becomes a link of instance "acme-1"
// of module "acme".
func serve(h *Hub) *httptest.Server {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.Serve(ws, "acme-1", "acme")
	}))
}

// factory is the factory's end: it answers an inbound or outbound call
// with "re:" before its bytes (opened by key 0), keys with a material,
// and stays silent for a call whose bytes are "silent", fails one whose
// bytes are "fail", and closes on "close".
func factory(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http"), nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, b, err := ws.ReadMessage()
			if err != nil {
				return
			}
			c, err := protocol.DecodeFactoryCall(b)
			if err != nil {
				t.Errorf("a call: %v", err)
				return
			}
			a := protocol.FactoryAnswerFrame{Type: protocol.FactoryAnswer, Request: c.Request}
			switch {
			case string(c.Data) == "close":
				_ = ws.Close()
				return
			case string(c.Data) == "silent":
				continue
			case string(c.Data) == "fail":
				a.Failure = &protocol.CodecFailure{Kind: protocol.CodecFailNotOpened, Msg: "no key fits"}
			case c.Type == protocol.FactoryKeys:
				a.Material = []byte("material")
			default:
				a.Data = append([]byte("re:"), c.Data...)
				if c.Type == protocol.FactoryInbound && c.Suite() {
					zero := uint64(0)
					a.Opened = &zero
				}
			}
			out, _ := protocol.EncodeFactoryAnswer(a)
			if err := ws.WriteMessage(websocket.BinaryMessage, out); err != nil {
				return
			}
		}
	}()
	return ws
}

func waitUp(t *testing.T, h *Hub, module string, up bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.Up(module) != up {
		if time.Now().After(deadline) {
			t.Fatalf("%s up: want %v", module, up)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func inbound(data string) protocol.FactoryCall {
	build := uuid.New()
	return protocol.FactoryCall{Type: protocol.FactoryInbound, Build: build[:], ID: build[:], Data: []byte(data),
		Layers: []int{protocol.CodecLayerSuite}, Keys: [][]byte{[]byte("k")}}
}

func TestCall(t *testing.T) {
	h := New(zap.NewNop())
	ctx := context.Background()
	if _, err := h.Call(ctx, "acme", inbound("x"), time.Second); !errors.Is(err, ErrNoLink) || !Down(err) {
		t.Fatalf("no link: %v", err)
	}
	srv := serve(h)
	defer srv.Close()
	ws := factory(t, srv.URL)
	defer func() { _ = ws.Close() }()
	waitUp(t, h, "acme", true)

	a, err := h.Call(ctx, "acme", inbound("hello"), time.Second)
	if err != nil || !bytes.Equal(a.Data, []byte("re:hello")) || a.Opened == nil {
		t.Fatalf("answer %+v: %v", a, err)
	}
	build := uuid.New()
	if m, err := h.BuildKeys(ctx, "acme", build, time.Second); err != nil || string(m) != "material" {
		t.Fatalf("keys %q: %v", m, err)
	}
	_, err = h.Call(ctx, "acme", inbound("fail"), time.Second)
	var failed *Failed
	if !errors.As(err, &failed) || failed.Kind != protocol.CodecFailNotOpened || Down(err) {
		t.Fatalf("a failure: %v", err)
	}
	if _, err := h.Call(ctx, "acme", inbound("silent"), 50*time.Millisecond); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("silent: %v", err)
	}
	// An answer that does not fit its call (no opened for a suite) is not
	// taken.
	c := inbound("x")
	c.Layers, c.Keys = []int{protocol.CodecLayerEnvelope}, nil
	if a, err := h.Call(ctx, "acme", c, time.Second); err != nil || a.Opened != nil {
		t.Fatalf("envelope only: %+v %v", a, err)
	}
	if _, err := h.Call(ctx, "acme", inbound("close"), time.Second); !errors.Is(err, ErrLinkDropped) {
		t.Fatalf("dropped: %v", err)
	}
	waitUp(t, h, "acme", false)
}
