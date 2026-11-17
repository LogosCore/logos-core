package minionfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// setupShots is a service whose session has two screenshot tasks and a
// dir_listing one, taking files up to 10 MiB.
func setupShots(t *testing.T) (*Service, *memStore, *memFiles, tasks, *models.MinionSession, [3]uuid.UUID) {
	t.Helper()
	store := &memStore{objects: map[string][]byte{}}
	files := &memFiles{files: map[uuid.UUID]*models.MinionFile{}}
	session := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	ids := [3]uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	ts := tasks{}
	for i, result := range []string{protocol.ResultScreenshot, protocol.ResultScreenshot, protocol.ResultDirListing} {
		ts[ids[i]] = &models.MinionTask{TaskID: ids[i], SessionID: session.SessionID, OperationID: session.OperationID, Result: result}
	}
	s := NewService(files, ts, store, nil, Config{MaxSize: 10 << 20, IdleAfter: time.Hour, SweepEvery: time.Hour}, zap.NewNop())
	return s, store, files, ts, session, ids
}

// sendWhole sends body for task in one chunk, named name.
func sendWhole(t *testing.T, s *Service, session *models.MinionSession, task uuid.UUID, xfer []byte, name string, body []byte) *models.MinionFile {
	t.Helper()
	sum := sha256.Sum256(body)
	total := uint64(len(body))
	r, err := s.TakeChunk(context.Background(), session, protocol.ChunkItem{Xfer: xfer, Data: body, Total: &total, Task: task[:], Name: name, SHA256: sum[:]})
	if err != nil || r.Code != 0 || r.Next != int64(total) {
		t.Fatalf("chunk: %+v %v", r, err)
	}
	f, err := s.files.FindByXfer(context.Background(), session.SessionID, xfer)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeader is a PNG's signature and header alone: w×h, no pixels.
func pngHeader(w, h uint32) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 6, 0, 0, 0) // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	b := append([]byte("\x89PNG\r\n\x1a\n"), binary.BigEndian.AppendUint32(nil, uint32(len(ihdr)))...)
	b = append(b, chunk...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(chunk))
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 400 {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waited for %s", what)
}

// A screenshot's picture comes as a download's file does, its name alone:
// marked a screenshot, kept, and its thumbnail made, fitted into ThumbSide.
// Its task's data then puts its size in pixels and its screen onto it, and
// data that is not the picture is a problem. The operation's screenshots
// list by kind, and go with it, thumbnails too.
func TestScreenshot(t *testing.T) {
	s, store, files, _, session, ids := setupShots(t)
	ctx := context.Background()
	body := pngOf(t, 1200, 600)
	xfer := bytes.Repeat([]byte{5}, 16)
	f := sendWhole(t, s, session, ids[0], xfer, "screen.png", body)
	if f.Kind != models.MinionFileKindScreenshot || f.Name != "screen.png" {
		t.Fatalf("marked: %+v", f)
	}
	waitFor(t, "its thumbnail", func() bool {
		g, _ := files.FindByID(ctx, f.FileID)
		return g.Thumb
	})
	rc, size, g, err := s.OpenThumb(ctx, f.FileID)
	if err != nil {
		t.Fatal(err)
	}
	thumb, _ := io.ReadAll(rc)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(thumb))
	if err != nil || format != "jpeg" || cfg.Width != ThumbSide || cfg.Height != ThumbSide/2 || size != int64(len(thumb)) || g.ContentType != "image/png" {
		t.Fatalf("thumbnail: %s %dx%d, %d bytes (%d), %q, %v", format, cfg.Width, cfg.Height, len(thumb), size, g.ContentType, err)
	}

	sum := sha256.Sum256(body)
	shot := protocol.Screenshot{Xfer: xfer, Size: uint64(len(body)), SHA256: sum[:], Width: 1200, Height: 600, Display: "HDMI-1"}
	if p, err := s.TakeScreenshot(ctx, ids[0], shot); p != "" || err != nil {
		t.Fatalf("its data: %q %v", p, err)
	}
	if g, _ := files.FindByID(ctx, f.FileID); g.Width != 1200 || g.Height != 600 || g.Display != "HDMI-1" {
		t.Fatalf("on the file: %+v", g)
	}
	bigger := shot
	bigger.Size++
	if p, _ := s.TakeScreenshot(ctx, ids[0], bigger); !strings.Contains(p, "bytes") {
		t.Fatalf("another size: %q", p)
	}
	other := shot
	other.Xfer = make([]byte, 16)
	if p, _ := s.TakeScreenshot(ctx, ids[0], other); p != "the data names another transfer" {
		t.Fatalf("another transfer: %q", p)
	}
	if p, _ := s.TakeScreenshot(ctx, ids[1], shot); p != "no picture came for it" {
		t.Fatalf("no picture: %q", p)
	}

	if list, err := s.Files(ctx, session.OperationID, models.MinionFileKindScreenshot, nil, 10); err != nil || len(list) != 1 || list[0].FileID != f.FileID {
		t.Fatalf("listed: %+v %v", list, err)
	}
	if err := s.DeleteByOperation(ctx, session.OperationID); err != nil || store.keys("") != 0 {
		t.Fatalf("deleted: %v, %d objects left", err, store.keys(""))
	}
}

// A screenshot that is not a PNG or a JPEG is kept, without a thumbnail,
// and is a problem of its task's data; a task of another result sends no
// file.
func TestScreenshotNotAPicture(t *testing.T) {
	s, _, files, ts, session, ids := setupShots(t)
	ctx := context.Background()
	f := sendWhole(t, s, session, ids[0], bytes.Repeat([]byte{6}, 16), "screen.png", []byte("not a picture at all"))
	waitFor(t, "the problem", func() bool { return problemOf(ts, ids[0]) != "" })
	if p := problemOf(ts, ids[0]); p != "the picture is text/plain; charset=utf-8, not a PNG or a JPEG" {
		t.Fatalf("problem: %q", p)
	}
	if g, _ := files.FindByID(ctx, f.FileID); g.State != models.MinionFileComplete || g.Thumb {
		t.Fatalf("file: %+v", g)
	}
	if _, _, _, err := s.OpenThumb(ctx, f.FileID); err == nil {
		t.Fatal("a thumbnail of no picture")
	}

	total := uint64(1)
	r, _ := s.TakeChunk(ctx, session, protocol.ChunkItem{Xfer: bytes.Repeat([]byte{8}, 16), Data: []byte("x"), Total: &total, Task: ids[2][:]})
	if r.Code != protocol.ErrItemBadMessage || !strings.Contains(r.Msg, "file_download or screenshot") {
		t.Fatalf("another result: %+v", r)
	}
}

// A thumbnail fits ThumbSide, its proportions kept; a picture of more than
// thumbMaxPixels gets none, and one that does not decode is unreadable.
func TestMakeThumb(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{
		{3840, 2160, 400, 225}, {1080, 1920, 225, 400}, {300, 200, 300, 200}, {10000, 1, 400, 1}, {400, 400, 400, 400},
	} {
		if w, h := fit(c.w, c.h, 400); w != c.ww || h != c.wh {
			t.Errorf("%dx%d: %dx%d, want %dx%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
	if thumb, err := makeThumb(pngHeader(7000, 6000)); thumb != nil || err != nil {
		t.Fatalf("too big: %d bytes, %v", len(thumb), err)
	}
	if _, err := makeThumb(pngHeader(10, 10)); !errors.Is(err, errUnreadable) {
		t.Fatalf("no pixels: %v", err)
	}
	thumb, err := makeThumb(pngOf(t, 120, 90))
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(thumb)); err != nil || cfg.Width != 120 || cfg.Height != 90 {
		t.Fatalf("a small one keeps its size: %+v %v", cfg, err)
	}
}
