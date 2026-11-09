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

// CheckVPNKey takes the one key "lgv_0123456789ab_secret".
func (f *refusingVPN) CheckVPNKey(_ context.Context, raw string) error {
	if raw == "lgv_0123456789ab_secret" {
		return nil
	}
	return fmt.Errorf("%w: the key is unknown", miniontunnels.ErrVPNRefused)
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

// The installer's fetches: SHA256SUMS beside the builds, and a VPN key
// signs them in, checked (the router lets a keyed request past sign-in).
func TestVPNClientKeyed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "logos-vpn-linux-amd64"), []byte("ELF"), 0o644)
	vc := NewVPNController(&refusingVPN{}, dir, zap.NewNop())
	r := gin.New()
	r.GET("/vpn/client/:platform", vc.Client)
	get := func(path, key string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		r.ServeHTTP(w, req)
		return w
	}
	if w := get("/vpn/client/SHA256SUMS", "lgv_0123456789ab_secret"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "no logos-vpn hashes") {
		t.Errorf("no sums: %d %s", w.Code, w.Body)
	}
	_ = os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte("abc  logos-vpn-linux-amd64\n"), 0o644)
	if w := get("/vpn/client/SHA256SUMS", "lgv_0123456789ab_secret"); w.Code != http.StatusOK || w.Body.String() != "abc  logos-vpn-linux-amd64\n" {
		t.Errorf("sums: %d %s", w.Code, w.Body)
	}
	if w := get("/vpn/client/linux-amd64", "lgv_0123456789ab_secret"); w.Code != http.StatusOK || w.Body.String() != "ELF" {
		t.Errorf("the build: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/vpn/client/linux-amd64", "/vpn/client/SHA256SUMS"} {
		if w := get(path, "lgv_000000000000_other"); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "unknown") {
			t.Errorf("%s, another key: %d %s", path, w.Code, w.Body)
		}
	}
}

// The installers: for anyone, as they are, not cached; one this core's
// release has not is not found.
func TestVPNInstaller(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\n"), 0o644)
	vc := NewVPNController(&refusingVPN{}, dir, zap.NewNop())
	r := gin.New()
	r.GET("/vpn/install.sh", vc.Installer)
	r.GET("/vpn/install.ps1", vc.Installer)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/vpn/install.sh", nil))
	if w.Code != http.StatusOK || w.Body.String() != "#!/bin/sh\n" || w.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("install.sh: %d %q %v", w.Code, w.Body, w.Header())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/vpn/install.ps1", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "before the installers") {
		t.Errorf("install.ps1 missing: %d %s", w.Code, w.Body)
	}
}
