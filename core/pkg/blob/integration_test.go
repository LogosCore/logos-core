package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The rest of this package's tests script a fake client and never speak S3.
// That leaves the one thing about PutStream that cannot be answered in
// Go unverified: whether the store on the other end implements the
// multipart upload API an unknown-length body requires. SeaweedFS's S3
// gateway is the intended target and it is not AWS; a missing
// CreateMultipartUpload would break every wiki export and no unit test
// would notice.
//
// Opt-in through INTEGRATION_S3_ENDPOINT, so `make test` and CI are
// unaffected:
//
//	INTEGRATION_S3_ENDPOINT=http://seaweedfs-s3:8333 \
//	INTEGRATION_S3_ACCESS_KEY=logos \
//	INTEGRATION_S3_SECRET_KEY=… \
//	go test ./pkg/blob/ -run Integration -v
func integrationStore(t *testing.T) *S3Store {
	t.Helper()
	endpoint := os.Getenv("INTEGRATION_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("INTEGRATION_S3_ENDPOINT not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewS3Store(ctx, S3Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("INTEGRATION_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("INTEGRATION_S3_SECRET_KEY"),
		Bucket:    "itest-blob",
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	return store
}

// A body larger than one part, so the upload has to go through
// CreateMultipartUpload / UploadPart / CompleteMultipartUpload rather than
// the single PUT a small object takes.
func TestIntegrationS3Store_PutStreamMultipart(t *testing.T) {
	store := integrationStore(t)
	ctx := context.Background()
	key := "itest/" + uuid.NewString() + ".bin"
	t.Cleanup(func() {
		if err := store.Delete(context.Background(), key); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// Random rather than zeroes: a store that quietly deduplicates or
	// sparse-encodes a run of identical bytes would make a truncated
	// upload look complete.
	payload := make([]byte, streamPartSize+(7<<20))
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}

	size, err := store.PutStream(ctx, key, bytes.NewReader(payload), "application/zip")
	if err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	if size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", size, len(payload))
	}

	reader, info, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer reader.Close()
	if info.ContentLength != int64(len(payload)) {
		t.Errorf("stored length = %d, want %d", info.ContentLength, len(payload))
	}
	if info.ContentType != "application/zip" {
		t.Errorf("content type = %q, want application/zip", info.ContentType)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("read back %d bytes that differ from what was written", len(got))
	}
}

// A reader that fails part-way is what an export whose writer errors looks
// like from here: the runner closes its pipe with the failure, and the read
// side hands that error to the upload. It must abort rather than complete a
// short object, which the download endpoint would then serve as the export.
//
// The error matters. minio's part loop reads with readFull and treats EOF
// and io.ErrUnexpectedEOF as "this was the last, short part" — which is
// exactly what a final part looks like, so a producer that signalled failure
// with either would get a truncated object stored as a success. Anything
// else propagates. This is why the runner passes the real error to
// CloseWithError and only ever closes cleanly on success.
func TestIntegrationS3Store_PutStreamAbortsOnReaderError(t *testing.T) {
	store := integrationStore(t)
	ctx := context.Background()
	key := "itest/" + uuid.NewString() + ".bin"
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })

	body := io.MultiReader(
		bytes.NewReader(make([]byte, 1<<20)),
		&failingReader{err: errWriterGaveUp},
	)
	if _, err := store.PutStream(ctx, key, body, "application/zip"); err == nil {
		t.Fatal("PutStream succeeded on a body that failed mid-stream")
	} else if !errors.Is(err, errWriterGaveUp) {
		t.Errorf("error = %v, want the producer's own failure", err)
	}
	if _, err := store.Head(ctx, key); err == nil {
		t.Error("a failed stream left an object behind")
	}
}

var errWriterGaveUp = errors.New("the writer gave up")

type failingReader struct{ err error }

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }
