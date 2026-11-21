package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fake stands in for guacenc and ffmpeg: guacenc writes <input>.m4v, ffmpeg
// its last argument; fail names a program that fails instead.
type fake struct {
	fail  string
	calls [][]string
	// started is closed as the first program runs; block holds it until
	// closed.
	started chan struct{}
	once    sync.Once
	block   chan struct{}
}

func (f *fake) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.block != nil {
		<-f.block
	}
	if name == f.fail {
		return []byte("it broke"), errors.New("exit status 1")
	}
	out := args[len(args)-1]
	if name == "guacenc" {
		out += ".m4v"
	}
	return nil, os.WriteFile(out, []byte("video of "+name), 0o600)
}

func serve(t *testing.T, f *fake) *httptest.Server {
	t.Helper()
	cfg := config{Guacenc: "guacenc", FFmpeg: "ffmpeg", TempDir: t.TempDir(), CRF: "28", Preset: "veryfast"}
	srv := httptest.NewServer(newHandler(cfg, f.run))
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, query string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/encode?"+query, "application/octet-stream", strings.NewReader("4.sync,1.0;"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// A recording comes back as ffmpeg's video, guacenc having drawn it at the
// desktop's size first.
func TestEncode(t *testing.T) {
	f := &fake{}
	srv := serve(t, f)
	code, body := post(t, srv, "width=1920&height=1080")
	if code != http.StatusOK || body != "video of ffmpeg" {
		t.Fatalf("%d %q", code, body)
	}
	if len(f.calls) != 2 || f.calls[0][0] != "guacenc" || !slices.Contains(f.calls[0], "1920x1080") || f.calls[1][0] != "ffmpeg" ||
		!slices.Contains(f.calls[1], "libx264") || !strings.HasSuffix(f.calls[1][slices.Index(f.calls[1], "-i")+1], ".guac.m4v") {
		t.Fatalf("calls %v", f.calls)
	}
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %v %v", resp, err)
	}
	_ = resp.Body.Close()
}

// A size it cannot take is 400; guacenc or ffmpeg failing is 422, with
// what it said.
func TestEncodeRefusals(t *testing.T) {
	for _, q := range []string{"", "width=0&height=10", "width=10&height=99999", "width=a&height=10"} {
		if code, _ := post(t, serve(t, &fake{}), q); code != http.StatusBadRequest {
			t.Errorf("%q: %d", q, code)
		}
	}
	for _, prog := range []string{"guacenc", "ffmpeg"} {
		code, body := post(t, serve(t, &fake{fail: prog}), "width=800&height=600")
		if code != http.StatusUnprocessableEntity || !strings.Contains(body, prog+" failed") || !strings.Contains(body, "it broke") {
			t.Errorf("%s failing: %d %q", prog, code, body)
		}
	}
}

// One at a time: another while one encodes is 503, to be asked again.
func TestEncodeBusy(t *testing.T) {
	f := &fake{started: make(chan struct{}), block: make(chan struct{})}
	srv := serve(t, f)
	done := make(chan int)
	go func() {
		code, _ := post(t, srv, "width=800&height=600")
		done <- code
	}()
	// The first holds the worker once guacenc runs.
	<-f.started
	if code, _ := post(t, srv, "width=800&height=600"); code != http.StatusServiceUnavailable {
		t.Fatalf("a second meanwhile: %d", code)
	}
	close(f.block)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("the first: %d", code)
	}
}
