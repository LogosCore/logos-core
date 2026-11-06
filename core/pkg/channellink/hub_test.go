package channellink

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// serve is core's end: every handshake becomes a link of instance "dumbo-1"
// of module "dumbo".
func serve(h *Hub) *httptest.Server {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.Serve(ws, "dumbo-1", "dumbo")
	}))
}

// channel is the channel's end: it answers each call with "re:" before the
// message, under the same id, until it reads a call to close on.
func channel(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http"), nil)
	if resp != nil {
		// The handshake's body says nothing.
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
			f, err := protocol.DecodeLinkFrame(b)
			if err != nil {
				t.Errorf("a call: %v", err)
				return
			}
			if f.Values["url"] == "close" {
				_ = ws.Close()
				return
			}
			if f.Values["url"] == "silent" {
				continue
			}
			a, _ := protocol.EncodeLinkFrame(protocol.LinkFrame{Type: protocol.LinkAnswer, Request: f.Request, ID: f.ID,
				Data: append([]byte("re:"), f.Data...), Status: 200})
			if err := ws.WriteMessage(websocket.BinaryMessage, a); err != nil {
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

func TestCall(t *testing.T) {
	h := New(zap.NewNop())
	ups := make(chan string, 4)
	h.OnUp(func(m string) { ups <- m })
	srv := serve(h)
	defer srv.Close()

	ctx := context.Background()
	if _, err := h.Call(ctx, "dumbo", Call{ID: [16]byte{1}, Data: []byte("x")}); !errors.Is(err, ErrNoLink) {
		t.Fatalf("no link: %v", err)
	}
	ws := channel(t, srv.URL)
	defer func() { _ = ws.Close() }()
	waitUp(t, h, "dumbo", true)
	if m := <-ups; m != "dumbo" {
		t.Fatalf("up %q", m)
	}
	if h.Up("http") {
		t.Fatal("another module up")
	}
	// Calls at once, answered by their ids.
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			id := [16]byte{byte(i)}
			a, err := h.Call(ctx, "dumbo", Call{Values: map[string]any{"url": "http://p"}, ID: id, Data: []byte{byte(i)}})
			if err == nil && (!bytes.Equal(a.ID, id[:]) || !bytes.Equal(a.Data, []byte{'r', 'e', ':', byte(i)})) {
				err = errors.New("another call's answer")
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := h.Instances()["dumbo"]; len(got) != 1 || got[0] != "dumbo-1" {
		t.Fatalf("instances %v", got)
	}
}

func TestLinkDrops(t *testing.T) {
	h := New(zap.NewNop())
	srv := serve(h)
	defer srv.Close()
	ws := channel(t, srv.URL)
	defer func() { _ = ws.Close() }()
	waitUp(t, h, "dumbo", true)

	// A call waiting when the link drops fails with ErrLinkDropped.
	done := make(chan error, 1)
	go func() {
		_, err := h.Call(context.Background(), "dumbo", Call{Values: map[string]any{"url": "silent"}, ID: [16]byte{1}, Data: []byte("x")})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := h.Call(context.Background(), "dumbo", Call{Values: map[string]any{"url": "close"}, ID: [16]byte{2}, Data: []byte("x")}); !errors.Is(err, ErrLinkDropped) {
		t.Fatalf("the call that closed it: %v", err)
	}
	if err := <-done; !errors.Is(err, ErrLinkDropped) {
		t.Fatalf("a call waiting: %v", err)
	}
	waitUp(t, h, "dumbo", false)
}

func TestCallDeadline(t *testing.T) {
	h := New(zap.NewNop())
	srv := serve(h)
	defer srv.Close()
	ws := channel(t, srv.URL)
	defer func() { _ = ws.Close() }()
	waitUp(t, h, "dumbo", true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := h.Call(ctx, "dumbo", Call{Values: map[string]any{"url": "silent"}, ID: [16]byte{1}, Data: []byte("x")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
}

// An open goes to the channel with its values; sent hears of it (the
// instance and request a socket will name) before the answer comes.
func TestOpen(t *testing.T) {
	h := New(zap.NewNop())
	srv := serve(h)
	defer srv.Close()
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()
	waitUp(t, h, "dumbo", true)
	go func() {
		_, b, err := ws.ReadMessage()
		if err != nil {
			return
		}
		f, _ := protocol.DecodeLinkFrame(b)
		if f.Type != protocol.LinkOpen || f.Values["host"] != "10.0.0.5" || f.Deadline != 30000 || len(f.Under) != 16 || f.Under[0] != 9 || f.Keys == nil {
			t.Errorf("open %+v", f)
		}
		a, _ := protocol.EncodeLinkFrame(protocol.LinkFrame{Type: protocol.LinkAnswer, Request: f.Request})
		_ = ws.WriteMessage(websocket.BinaryMessage, a)
	}()
	var gotInstance string
	var gotReq uint64
	a, err := h.Open(context.Background(), "dumbo", map[string]any{"host": "10.0.0.5"}, nil, [16]byte{9}, testKeys, nil, 0, func(instance string, req uint64) {
		gotInstance, gotReq = instance, req
	})
	if err != nil || a.Failure != nil || gotInstance != "dumbo-1" || gotReq == 0 || a.Request != gotReq {
		t.Fatalf("open: %+v %v (sent %s %d)", a, err, gotInstance, gotReq)
	}
	if _, err := h.Open(context.Background(), "nope", nil, nil, [16]byte{9}, nil, nil, 0, nil); !errors.Is(err, ErrNoLink) {
		t.Fatalf("no link: %v", err)
	}
}

// testKeys are an agentless open's keys.
var testKeys = &protocol.LinkKeys{Suite: protocol.CryptoAES256CBCHMACSHA256,
	Material: protocol.AES256CBCHMACSHA256Material{EncKey: make([]byte, 32), MACKey: make([]byte, 32)}}
