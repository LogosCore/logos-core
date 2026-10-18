package wiki

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// oneBlobStore serves a single object; everything else panics through the
// nil embed.
type oneBlobStore struct {
	blob.ObjectStore
	body string
}

func (s oneBlobStore) Get(context.Context, string) (io.ReadCloser, blob.ObjectInfo, error) {
	return io.NopCloser(strings.NewReader(s.body)), blob.ObjectInfo{}, nil
}

// The image budget trusts the size on the image's record, so the read must
// not go past it: a blob larger than its record was read whole.
func TestReadBlob_RefusesABlobLargerThanItsRecord(t *testing.T) {
	d := &DrawingFileRenderer{store: oneBlobStore{body: "0123456789"}}

	if _, err := d.readBlob(context.Background(), "k", 4); err == nil {
		t.Fatal("read a 10-byte blob recorded as 4 bytes")
	}
	data, err := d.readBlob(context.Background(), "k", 10)
	if err != nil || string(data) != "0123456789" {
		t.Fatalf("exact size: %q, %v", data, err)
	}
	// No size on the record: bounded by the ceiling rather than refused.
	if data, err := d.readBlob(context.Background(), "k", 0); err != nil || len(data) != 10 {
		t.Fatalf("unknown size: %q, %v", data, err)
	}
}

// countingImages answers FindByIDs and counts the calls. FindByID panics
// through the nil embed: embedding must not fall back to one query per image.
type countingImages struct {
	repository.IWikiImageRepository
	rows  []models.WikiImage
	calls int
}

func (c *countingImages) FindByIDs(_ context.Context, ids []uuid.UUID) ([]models.WikiImage, error) {
	c.calls++
	want := map[uuid.UUID]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []models.WikiImage
	for _, r := range c.rows {
		if want[r.ImageID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestEmbedImages_LooksUpTheSceneInOneQuery(t *testing.T) {
	op, other := uuid.New(), uuid.New()
	a, b, foreign, missing := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	images := &countingImages{rows: []models.WikiImage{
		{ImageID: a, OperationID: op, ContentType: "image/png", SizeBytes: 3, ObjectKey: "a"},
		{ImageID: b, OperationID: op, ContentType: "image/png", SizeBytes: 3, ObjectKey: "b"},
		{ImageID: foreign, OperationID: other, ContentType: "image/png", SizeBytes: 3, ObjectKey: "f"},
	}}
	d := &DrawingFileRenderer{images: images, store: oneBlobStore{body: "png"}}

	files, result := d.embedImages(context.Background(), op,
		[]string{a.String(), b.String(), foreign.String(), missing.String(), "local-id"}, DrawingFileOptions{})

	if images.calls != 1 {
		t.Errorf("image lookups = %d, want 1 for the whole scene", images.calls)
	}
	if len(files) != 2 || result.ImagesEmbedded != 2 {
		t.Errorf("embedded %d (%d files), want a and b", result.ImagesEmbedded, len(files))
	}
	reasons := map[string]string{}
	for _, s := range result.Skips {
		reasons[s.ImageID] = s.Reason
	}
	if reasons[foreign.String()] != "image_operation_mismatch" ||
		reasons[missing.String()] != "image_not_found" ||
		reasons["local-id"] != "drawing_image_unadopted" {
		t.Errorf("skips = %v", reasons)
	}
}
