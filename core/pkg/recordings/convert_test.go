package recordings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// fakeEncoder answers as guacenc-worker would, keeping what it was sent.
type fakeEncoder struct {
	err  error
	got  string
	w, h int
}

func (e *fakeEncoder) Encode(_ context.Context, width, height int, recording io.Reader) ([]byte, error) {
	b, err := io.ReadAll(recording)
	if err != nil {
		return nil, err
	}
	e.got, e.w, e.h = string(b), width, height
	if e.err != nil {
		return nil, e.err
	}
	return []byte("MP4"), nil
}

// A recording of a desktop, saved: its parts in the store, as a Recorder
// leaves them.
const recorded = "4.sync,3.100;3.img,1.1,2.14,1.0,9.image/png,1.0,1.0;" +
	"3.key,5.65307,1.1,3.150;4.blob,1.1,4.QUJD;3.key,5.65307,1.0,3.180;4.sync,3.200;"

func (g *rig) saved(t *testing.T, format string, startedAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	half := len(recorded) / 2
	for n, part := range []string{recorded[:half], recorded[half:]} {
		if err := g.store.Put(context.Background(), partKey(id, n), strings.NewReader(part), int64(len(part)), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.repo.Insert(context.Background(), &models.Recording{RecordingID: id, TaskID: g.task.TaskID, Format: format,
		State: models.RecordingStateSaved, Width: 1280, Height: 720, Parts: 2, Size: int64(len(recorded)), StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (g *rig) rec(t *testing.T, id uuid.UUID) *models.Recording {
	t.Helper()
	r, err := g.repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A recording's keys as they go by: guacd's first sync and each key
// instruction; a value counts characters, not bytes; a tail cut short is
// read as far as it is whole.
func TestReadKeys(t *testing.T) {
	k := readKeys(strings.NewReader(recorded + "4.sync,3.300;3.key,1.ü,1.1,3.31"))
	if k.Start != "100" || !slices.Equal(k.Keys, [][3]string{{"65307", "1", "150"}, {"65307", "0", "180"}}) {
		t.Fatalf("%+v", k)
	}
	k = readKeys(strings.NewReader("3.key,2.éa,1.1,1.5;3.key,1.a,1.0,1.6;"))
	if len(k.Keys) != 2 || k.Keys[0][0] != "éa" {
		t.Fatalf("characters, not bytes: %+v", k)
	}
	if k := readKeys(strings.NewReader("not guacamole")); len(k.Keys) != 0 || k.Start != "" {
		t.Fatalf("not a recording: %+v", k)
	}
}

// A desktop's recording becomes a video: the encoder gets it whole at its
// size, its video and keys are kept, the record says so, its parts go;
// the oldest first, and a terminal's is never taken.
func TestConvertNext(t *testing.T) {
	g := newRig(testConfig())
	enc := &fakeEncoder{}
	g.svc.WithEncoder(enc)
	ctx := context.Background()
	at := g.clock.now()
	newer := g.saved(t, models.RecordingFormatGuac, at)
	older := g.saved(t, "", at.Add(-time.Hour)) // before recordings had a format: guac
	cast := g.saved(t, models.RecordingFormatAsciicast, at.Add(-2*time.Hour))

	if err := g.svc.ConvertNext(ctx); err != nil {
		t.Fatal(err)
	}
	r := g.rec(t, older)
	if r.Video != models.RecordingVideoConverted || r.VideoSize != 3 || r.Parts != 0 || r.VideoLease != nil || enc.got != recorded ||
		enc.w != 1280 || enc.h != 720 {
		t.Fatalf("the oldest: %+v, encoder got %q at %dx%d", r, enc.got, enc.w, enc.h)
	}
	keys := g.store.Keys()
	if !slices.Contains(keys, videoKey(older)) || !slices.Contains(keys, keysKey(older)) ||
		slices.Contains(keys, partKey(older, 0)) || !slices.Contains(keys, partKey(newer, 0)) {
		t.Fatalf("kept: %v", keys)
	}
	body, _, err := g.svc.Keys(ctx, older)
	if err != nil {
		t.Fatal(err)
	}
	var k RecordingKeys
	if err := json.NewDecoder(body).Decode(&k); err != nil || k.Start != "100" || len(k.Keys) != 2 {
		t.Fatalf("its keys: %+v %v", k, err)
	}
	if v, _, err := g.svc.Video(ctx, older); err != nil || string(v) != "MP4" {
		t.Fatalf("its video: %q %v", v, err)
	}
	if _, _, err := g.svc.Video(ctx, newer); !errors.Is(err, ErrNotVideo) {
		t.Fatalf("one not converted: %v", err)
	}

	if err := g.svc.ConvertNext(ctx); err != nil || g.rec(t, newer).Video != models.RecordingVideoConverted {
		t.Fatalf("the next: %v %+v", err, g.rec(t, newer))
	}
	if err := g.svc.ConvertNext(ctx); !errors.Is(err, errNoneToConvert) || g.rec(t, cast).Video != "" {
		t.Fatalf("a terminal's taken: %v %+v", err, g.rec(t, cast))
	}

	// Deleted, it takes its video and keys along.
	if _, err := g.svc.Delete(ctx, older); err != nil {
		t.Fatal(err)
	}
	if keys := g.store.Keys(); slices.Contains(keys, videoKey(older)) || slices.Contains(keys, keysKey(older)) {
		t.Fatalf("left behind: %v", keys)
	}
}

// An encoder away: the recording waits as it was, asked again later, its
// tries not counted. One that fails on it: counted, tried again later, and
// past convertTries left as it was, its parts kept.
func TestConvertFailures(t *testing.T) {
	g := newRig(testConfig())
	enc := &fakeEncoder{err: ErrEncoderAway}
	g.svc.WithEncoder(enc)
	ctx := context.Background()
	id := g.saved(t, models.RecordingFormatGuac, g.clock.now())

	if err := g.svc.ConvertNext(ctx); !errors.Is(err, ErrEncoderAway) {
		t.Fatalf("away: %v", err)
	}
	if r := g.rec(t, id); r.Video != "" || r.VideoTries != 0 || r.VideoNextAt == nil || r.Parts != 2 {
		t.Fatalf("after the encoder was away: %+v", r)
	}
	if err := g.svc.ConvertNext(ctx); !errors.Is(err, errNoneToConvert) {
		t.Fatalf("before it is due: %v", err)
	}

	enc.err = &Refusal{Msg: "guacenc failed (exit status 1): bad"}
	for try := 1; try <= convertTries; try++ {
		g.clock.add(2 * time.Hour)
		if err := g.svc.ConvertNext(ctx); err != nil {
			t.Fatalf("try %d: %v", try, err)
		}
		r := g.rec(t, id)
		if r.VideoTries != try || r.VideoError != enc.err.Error() {
			t.Fatalf("try %d: %+v", try, r)
		}
		if try < convertTries && r.Video != "" {
			t.Fatalf("try %d: given up early: %+v", try, r)
		}
	}
	r := g.rec(t, id)
	if r.Video != models.RecordingVideoFailed || r.Parts != 2 || !slices.Contains(g.store.Keys(), partKey(id, 1)) {
		t.Fatalf("given up: %+v", r)
	}
	g.clock.add(24 * time.Hour)
	if err := g.svc.ConvertNext(ctx); !errors.Is(err, errNoneToConvert) {
		t.Fatalf("one given up taken again: %v", err)
	}

	// Without an encoder, none is converted.
	g2 := newRig(testConfig())
	g2.saved(t, models.RecordingFormatGuac, g2.clock.now())
	if err := g2.svc.ConvertNext(ctx); !errors.Is(err, errNoneToConvert) {
		t.Fatalf("no encoder: %v", err)
	}
}
