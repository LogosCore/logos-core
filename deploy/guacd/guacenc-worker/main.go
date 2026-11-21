// guacenc-worker makes a video of a desktop's recording for core (docs:
// Core Infrastructure, guacenc): core sends it a recording in Guacamole's
// format and gets back H.264 in MP4. guacenc, guacamole-server's own,
// draws the recording (as MPEG-4 Part 2, which browsers do not play), and
// ffmpeg encodes that again for them. It runs from logos-guacd's image,
// beside guacd, so both are the same guacamole-server. One recording at a
// time: another while one encodes is answered 503, and core asks again.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Settings, from the environment.
type config struct {
	// Listen is where it serves (GUACENC_LISTEN).
	Listen string
	// Guacenc and FFmpeg are the programs it runs.
	Guacenc string
	FFmpeg  string
	// TempDir holds a recording and its videos while it encodes.
	TempDir string
	// CRF is x264's quality, lower is better (GUACENC_CRF); Preset its
	// speed (GUACENC_PRESET).
	CRF    string
	Preset string
}

func configFromEnv() config {
	return config{
		Listen:  env("GUACENC_LISTEN", ":4823"),
		Guacenc: env("GUACENC_BIN", "/opt/guacamole/bin/guacenc"),
		FFmpeg:  env("FFMPEG_BIN", "ffmpeg"),
		TempDir: env("GUACENC_TMP", os.TempDir()),
		CRF:     env("GUACENC_CRF", "28"),
		Preset:  env("GUACENC_PRESET", "veryfast"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := configFromEnv()
	srv := &http.Server{Addr: cfg.Listen, Handler: newHandler(cfg, run), ReadHeaderTimeout: 30 * time.Second}
	slog.Info("guacenc-worker listening", "addr", cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("guacenc-worker stopped", "err", err)
		os.Exit(1)
	}
}

// runner runs a program to its end, its output (both streams) for the
// error it fails with.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// handler serves /encode and /healthz.
type handler struct {
	cfg  config
	run  runner
	busy sync.Mutex
}

func newHandler(cfg config, r runner) http.Handler {
	h := &handler{cfg: cfg, run: r}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /encode", h.encode)
	return mux
}

// maxSide bounds a desktop's width and height: guacenc's, and x264's sane.
const maxSide = 8192

// encode takes a recording as the body (?width=&height=: the desktop's
// size in pixels) and answers with its video: 200 video/mp4; 400 for a
// size it cannot take; 422 when guacenc or ffmpeg fails on it (their
// output, the end of it, as the text); 503 while another encodes.
func (h *handler) encode(w http.ResponseWriter, r *http.Request) {
	width, werr := strconv.Atoi(r.URL.Query().Get("width"))
	height, herr := strconv.Atoi(r.URL.Query().Get("height"))
	if werr != nil || herr != nil || width < 2 || height < 2 || width > maxSide || height > maxSide {
		http.Error(w, fmt.Sprintf("width and height are 2 to %d pixels", maxSide), http.StatusBadRequest)
		return
	}
	if !h.busy.TryLock() {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "another recording is encoding", http.StatusServiceUnavailable)
		return
	}
	defer h.busy.Unlock()

	dir, err := os.MkdirTemp(h.cfg.TempDir, "guacenc-")
	if err != nil {
		http.Error(w, "no room to work: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in := filepath.Join(dir, "recording.guac")
	if err := save(in, r.Body); err != nil {
		http.Error(w, "the recording did not arrive: "+err.Error(), http.StatusBadRequest)
		return
	}
	started := time.Now()
	ctx := r.Context()
	// guacenc writes <input>.m4v, at the size asked; its bit rate high, as
	// ffmpeg encodes it again.
	size := fmt.Sprintf("%dx%d", width, height)
	if out, err := h.run(ctx, h.cfg.Guacenc, "-s", size, "-r", "20000000", "-f", in); err != nil {
		failed(w, "guacenc", out, err)
		return
	}
	mp4 := filepath.Join(dir, "recording.mp4")
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", in + ".m4v",
		"-an", "-c:v", "libx264", "-preset", h.cfg.Preset, "-crf", h.cfg.CRF, "-pix_fmt", "yuv420p",
		// yuv420p needs an even width and height.
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		// Its index first, so the browser plays it before it all came.
		"-movflags", "+faststart", mp4}
	if out, err := h.run(ctx, h.cfg.FFmpeg, args...); err != nil {
		failed(w, "ffmpeg", out, err)
		return
	}
	f, err := os.Open(mp4) //nolint:gosec // its own temporary file
	if err != nil {
		http.Error(w, "the video is not there: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("encoded a recording", "size", size, "video_bytes", st.Size(), "took", time.Since(started).Round(time.Millisecond))
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	_, _ = io.Copy(w, f)
}

// save writes body to path.
func save(path string, body io.Reader) error {
	f, err := os.Create(path) //nolint:gosec // its own temporary file
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// failed answers 422 with what a program said, its last 2 KiB.
func failed(w http.ResponseWriter, what string, out []byte, err error) {
	msg := strings.TrimSpace(string(out))
	if len(msg) > 2048 {
		msg = "…" + msg[len(msg)-2048:]
	}
	slog.Warn("encoding failed", "step", what, "err", err, "output", msg)
	http.Error(w, fmt.Sprintf("%s failed (%v): %s", what, err, msg), http.StatusUnprocessableEntity)
}
