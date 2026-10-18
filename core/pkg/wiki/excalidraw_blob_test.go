package wiki

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/blob"
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
