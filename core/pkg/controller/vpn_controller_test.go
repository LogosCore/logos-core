package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	gws "github.com/gorilla/websocket"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// refusingVPN takes no client: each connect is answered with err, the key
// and hello it got kept.
type refusingVPN struct {
	err   error
	key   string
	hello protocol.VPNHello
	addr  string
}

func (f *refusingVPN) ConnectVPN(_ context.Context, raw string, hello protocol.VPNHello, addr string) (*miniontunnels.VPNClient, protocol.VPNWelcome, error) {
	f.key, f.hello, f.addr = raw, hello, addr
	return nil, protocol.VPNWelcome{}, f.err
}

// The link: a plain GET is no WebSocket, no key is a 401, a browser's page
// is refused; the hello comes first, and a client core does not take is
// closed with the close that says why.
func TestVPNLink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := &refusingVPN{}
	vc := NewVPNController(f, t.TempDir(), zap.NewNop())
	r := gin.New()
	r.GET("/vpn", vc.Link)
	srv := httptest.NewServer(r)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/vpn"
	auth := http.Header{"Authorization": {"Bearer lgv_0123456789ab_secret"}}

	resp, err := http.Get(srv.URL + "/vpn")
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a plain GET: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	if code := dialCode(t, url, nil); code != http.StatusUnauthorized {
		t.Errorf("no key: %d", code)
	}
	browser := auth.Clone()
	browser.Set("Origin", "https://evil.example")
	if code := dialCode(t, url, browser); code != http.StatusForbidden {
		t.Errorf("a page's: %d", code)
	}

	closeOf := func(hello string) (int, string) {
		t.Helper()
		conn, resp, err := gws.DefaultDialer.Dial(url, auth)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		defer conn.Close()
		if err := conn.WriteMessage(gws.TextMessage, []byte(hello)); err != nil {
			t.Fatal(err)
		}
		_, _, err = conn.ReadMessage()
		var ce *gws.CloseError
		if !errors.As(err, &ce) {
			t.Fatalf("no close: %v", err)
		}
		return ce.Code, ce.Text
	}
	if code, _ := closeOf(`{"bye":true}`); code != protocol.VPNCloseVersion {
		t.Errorf("no hello first: %d", code)
	}
	hello := `{"hello":{"link":1,"version":"0.1.0","os":"linux","arch":"amd64","machine":"kali","instance":"00112233445566778899aabbccddeeff"}}`
	f.err = fmt.Errorf("%w: the key is unknown", miniontunnels.ErrVPNRefused)
	if code, why := closeOf(hello); code != protocol.VPNCloseRefused || why != "the key is unknown" {
		t.Errorf("refused: %d %q", code, why)
	}
	if f.key != "lgv_0123456789ab_secret" || f.hello.Machine != "kali" || f.addr == "" {
		t.Errorf("connect got %q %+v %q", f.key, f.hello, f.addr)
	}
	f.err = fmt.Errorf("%w: hello: link 2", miniontunnels.ErrVPNHello)
	if code, why := closeOf(hello); code != protocol.VPNCloseVersion || !strings.Contains(why, "/api/v1/vpn/client/") {
		t.Errorf("another link: %d %q", code, why)
	}
}

// dialCode is the status a dial that is refused gets.
func dialCode(t *testing.T, url string, h http.Header) int {
	t.Helper()
	conn, resp, err := gws.DefaultDialer.Dial(url, h)
	if err == nil {
		_ = conn.Close()
	}
	if resp == nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// The client's builds: each system's, as logos-vpn (.exe on Windows); one
// this core carries not, or no system, is not found.
func TestVPNClientDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logos-vpn-linux-amd64"), []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logos-vpn-windows-amd64.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	vc := NewVPNController(&refusingVPN{}, dir, zap.NewNop())
	r := gin.New()
	r.GET("/vpn/client/:platform", vc.Client)
	for platform, want := range map[string]string{"linux-amd64": "logos-vpn", "windows-amd64": "logos-vpn.exe", "darwin-arm64": "", "plan9-386": ""} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/vpn/client/"+platform, nil))
		switch {
		case want == "" && w.Code != http.StatusNotFound:
			t.Errorf("%s: %d", platform, w.Code)
		case want != "" && (w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="`+want+`"`)):
			t.Errorf("%s: %d %q", platform, w.Code, w.Header().Get("Content-Disposition"))
		}
	}
}
