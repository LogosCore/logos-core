package recordings

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

const day = 24 * time.Hour

// old is a saved recording that ended ago, as ToClean finds it.
func (g *rig) old(t *testing.T, ago time.Duration) models.Recording {
	t.Helper()
	at := g.clock.now().Add(-ago)
	id := g.saved(t, models.RecordingFormatGuac, at.Add(-time.Minute))
	if err := g.repo.Save(context.Background(), id, bson.M{"ended_at": at}); err != nil {
		t.Fatal(err)
	}
	return *g.rec(t, id)
}

// videoOf makes a saved recording a video, as the converter leaves one.
func (g *rig) videoOf(t *testing.T, rec models.Recording) {
	t.Helper()
	ctx := context.Background()
	for key, body := range map[string]string{videoKey(rec.RecordingID): "MP4", keysKey(rec.RecordingID): "{}"} {
		if err := g.store.Put(ctx, key, bytes.NewReader([]byte(body)), int64(len(body)), ""); err != nil {
			t.Fatal(err)
		}
	}
	_ = g.svc.dropParts(ctx, rec)
	if err := g.repo.Save(ctx, rec.RecordingID, bson.M{"video": models.RecordingVideoConverted, "video_size": int64(3), "parts": 0}); err != nil {
		t.Fatal(err)
	}
}

// A cleanup takes a recording's files, its parts or its video and keys, and
// keeps its record, marked by the cleanup: it no longer opens, nor its
// video; what it took is counted as each was when taken (one made a video
// since the batch was read, its video). One taken already is passed over;
// deleted, it drops its record alone.
func TestCleanUpTakesTheFilesKeepsTheRecord(t *testing.T) {
	g := newRig(testConfig())
	ctx := context.Background()
	parts, video := g.old(t, 40*day), g.old(t, 50*day)
	g.videoOf(t, video)
	cleanup := uuid.New()

	recs, err := g.svc.ToClean(ctx, g.clock.now().Add(-30*day), nil, 10)
	if err != nil || len(recs) != 2 || recs[0].RecordingID != video.RecordingID || recs[1].StoredBytes() != parts.Size {
		t.Fatalf("to clean %+v %v", recs, err)
	}
	g.videoOf(t, parts)
	n, b, err := g.svc.CleanUp(ctx, recs, cleanup)
	if err != nil || n != 2 || b != 3+3 {
		t.Fatalf("took %d, %d bytes: %v", n, b, err)
	}
	if keys := g.store.Keys(); len(keys) != 0 {
		t.Fatalf("files left %v", keys)
	}
	for _, id := range []uuid.UUID{parts.RecordingID, video.RecordingID} {
		rec := g.rec(t, id)
		if rec.CleanedAt == nil || !rec.CleanedAt.Equal(g.clock.now()) || rec.CleanedBy == nil || *rec.CleanedBy != cleanup {
			t.Fatalf("its record %+v", rec)
		}
	}
	if _, _, err := g.svc.Open(ctx, parts.RecordingID); !errors.Is(err, ErrCleaned) {
		t.Fatalf("open one taken: %v", err)
	}
	if _, _, err := g.svc.Video(ctx, video.RecordingID); !errors.Is(err, ErrCleaned) {
		t.Fatalf("its video: %v", err)
	}
	if _, _, err := g.svc.Keys(ctx, video.RecordingID); !errors.Is(err, ErrCleaned) {
		t.Fatalf("its keys: %v", err)
	}

	again, _ := g.svc.ToClean(ctx, g.clock.now().Add(-30*day), nil, 10)
	if n, _, err := g.svc.CleanUp(ctx, recs, uuid.New()); len(again) != 0 || n != 0 || err != nil {
		t.Fatalf("again: to clean %d, took %d: %v", len(again), n, err)
	}
	if rec := g.rec(t, parts.RecordingID); *rec.CleanedBy != cleanup {
		t.Fatalf("taken twice: by %v", rec.CleanedBy)
	}

	before := g.repo.Len()
	if _, err := g.svc.Delete(ctx, parts.RecordingID); err != nil || g.repo.Len() != before-1 {
		t.Fatalf("deleted: %v, %d records", err, g.repo.Len())
	}
}

// The store failing stops a cleanup: what it took stays taken; the
// recording it was taking is unmarked, for the next cleanup; those after it
// are not reached.
func TestCleanUpStoreFails(t *testing.T) {
	g := newRig(testConfig())
	ctx := context.Background()
	first, second, third := g.old(t, 40*day), g.old(t, 39*day), g.old(t, 38*day)
	recs := []models.Recording{first, second, third}
	n, _, err := g.svc.CleanUp(ctx, recs[:1], uuid.New())
	if err != nil || n != 1 {
		t.Fatalf("the first: %d %v", n, err)
	}
	// The second's first part does not delete.
	g.store.FailDeletes(1)
	n, _, err = g.svc.CleanUp(ctx, recs[1:], uuid.New())
	if err == nil || n != 0 {
		t.Fatalf("with the store failing: took %d, %v", n, err)
	}
	if rec := g.rec(t, second.RecordingID); rec.CleanedAt != nil || rec.CleanedBy != nil {
		t.Fatalf("the one the store failed on is marked %+v", rec)
	}
	if rec := g.rec(t, third.RecordingID); rec.CleanedAt != nil {
		t.Fatal("the one after it was taken")
	}
	if _, err := g.store.Head(ctx, partKey(third.RecordingID, 0)); err != nil {
		t.Fatalf("the one after it lost its part: %v", err)
	}
	n, _, err = g.svc.CleanUp(ctx, recs[1:], uuid.New())
	if err != nil || n != 2 {
		t.Fatalf("with the store back: took %d, %v", n, err)
	}
}

// gateEncoder holds a conversion until the test lets it go.
type gateEncoder struct {
	started chan struct{}
	release chan struct{}
}

func (e *gateEncoder) Encode(_ context.Context, _, _ int, recording io.Reader) ([]byte, error) {
	_, _ = io.ReadAll(recording)
	close(e.started)
	<-e.release
	return []byte("MP4"), nil
}

// A recording the encoder has under a lease running is not taken; once the
// lease ran out, a cleanup takes it, and the conversion that outlived it
// keeps no video. One taken is never converted.
func TestCleanUpAndTheConverter(t *testing.T) {
	g := newRig(testConfig())
	ctx := context.Background()
	rec := g.old(t, 40*day)
	enc := &gateEncoder{started: make(chan struct{}), release: make(chan struct{})}
	g.svc.WithEncoder(enc)
	converted := make(chan error, 1)
	go func() { converted <- g.svc.ConvertNext(ctx) }()
	<-enc.started

	if n, _, err := g.svc.CleanUp(ctx, []models.Recording{rec}, uuid.New()); err != nil || n != 0 {
		t.Fatalf("while the encoder has it: took %d, %v", n, err)
	}
	if recs, _ := g.svc.ToClean(ctx, g.clock.now().Add(-30*day), nil, 10); len(recs) != 0 {
		t.Fatalf("to clean while the encoder has it: %d", len(recs))
	}
	g.clock.add(convertLease + time.Minute)
	if n, _, err := g.svc.CleanUp(ctx, []models.Recording{rec}, uuid.New()); err != nil || n != 1 {
		t.Fatalf("once its lease ran out: took %d, %v", n, err)
	}
	close(enc.release)
	if err := <-converted; err == nil {
		t.Fatal("the conversion that outlived its lease said it made a video")
	}
	if keys := g.store.Keys(); len(keys) != 0 {
		t.Fatalf("files left %v", keys)
	}
	if after := g.rec(t, rec.RecordingID); after.CleanedAt == nil || after.Video == models.RecordingVideoConverted {
		t.Fatalf("after the conversion %+v", after)
	}
	if err := g.svc.ConvertNext(ctx); !errors.Is(err, errNoneToConvert) {
		t.Fatalf("one taken was converted: %v", err)
	}
}
