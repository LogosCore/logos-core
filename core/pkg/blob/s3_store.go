package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures the SeaweedFS-backed object store.
type S3Config struct {
	Endpoint  string // e.g. http://localhost:8333
	AccessKey string
	SecretKey string
	Bucket    string
}

// S3Store is a minio-go-backed ObjectStore. SeaweedFS's S3 gateway is the
// intended target, but any S3-compatible endpoint works.
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store connects to the endpoint and ensures the bucket exists.
func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("blob: S3 endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("blob: S3 bucket is required")
	}

	endpoint, secure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("blob: invalid endpoint: %w", err)
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("blob: new minio client: %w", err)
	}

	ensureCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := ensureBucket(ensureCtx, client, cfg.Bucket); err != nil {
		return nil, err
	}

	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

// bucketClient is the minimal subset of *minio.Client that ensureBucket
// touches. Pulled out so tests can simulate the SeaweedFS cold-start race
// without standing up a real S3 gateway.
type bucketClient interface {
	BucketExists(ctx context.Context, bucket string) (bool, error)
	MakeBucket(ctx context.Context, bucket string, opts minio.MakeBucketOptions) error
}

// ensureBucket bootstraps the bucket, tolerating a brief startup window
// where the S3 gateway hasn't rebuilt its state from the filer yet: the
// BucketExists poll gives it ~10s to warm up before we try to create.
//
// Any MakeBucket error is fatal. An "already exists" response here means
// master and filer state are out of sync (zombie cluster) — recovery
// requires operator action, not silent retry. The filer is expected to
// persist its metadata (see docker-compose.yml's seaweedfs_filer_data
// volume); without that, this zombie state recurs on every restart.
func ensureBucket(ctx context.Context, c bucketClient, bucket string) error {
	const (
		existsAttempts = 10
		existsBackoff  = 1 * time.Second
	)

	// Poll BucketExists briefly. A warm gateway answers on the first
	// attempt; a cold one may need several seconds to rebuild state
	// from the filer.
	var lastErr error
	for i := 0; i < existsAttempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		exists, err := c.BucketExists(ctx, bucket)
		if err == nil && exists {
			return nil
		}
		if err == nil {
			// Definitive "not there" — still retry so the warm-up
			// window has a chance to convert this into a yes.
			lastErr = nil
			if i+1 < existsAttempts {
				if err := sleepCtx(ctx, existsBackoff); err != nil {
					return err
				}
				continue
			}
			break
		}
		lastErr = err
		if i+1 < existsAttempts {
			if err := sleepCtx(ctx, existsBackoff); err != nil {
				return err
			}
		}
	}
	if lastErr != nil {
		return fmt.Errorf("blob: bucket exists check: %w", lastErr)
	}

	// BucketExists stayed false across the retry budget — treat as a
	// real empty install and create the bucket. Any failure here is
	// fatal, including "collection already exists" responses that
	// indicate zombie cluster state.
	if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("blob: create bucket %q: %w", bucket, err)
	}
	return nil
}

// sleepCtx sleeps for d, or returns early when ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("blob: put %q: %w", key, err)
	}
	return nil
}

// streamPartSize is how much of an unknown-length upload is buffered before
// a part goes out.
//
// It is passed explicitly because the default is a trap: asked to store an
// object of unknown size, minio-go assumes the 5 TiB maximum and picks a
// part size to match, which allocates a ~537 MiB buffer for an upload that
// may turn out to be a hundred kilobytes. 64 MiB costs a bounded 64 MiB of
// memory and still reaches ~625 GB across the 10,000 parts S3 allows —
// past any wiki, and a limit that fails loudly at the store rather than
// truncating an archive.
const streamPartSize = 64 << 20

func (s *S3Store) PutStream(ctx context.Context, key string, body io.Reader, contentType string) (int64, error) {
	info, err := s.client.PutObject(ctx, s.bucket, key, body, -1, minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    streamPartSize,
	})
	if err != nil {
		return 0, fmt.Errorf("blob: put stream %q: %w", key, err)
	}
	return info.Size, nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("blob: get %q: %w", key, err)
	}

	// Stat up front so the caller knows size/content-type before streaming.
	// minio-go lazily opens the body so Stat failures surface here.
	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close() // already returning an error; nothing to add
		return nil, ObjectInfo{}, fmt.Errorf("blob: stat %q: %w", key, err)
	}

	return obj, ObjectInfo{
		ContentType:   stat.ContentType,
		ContentLength: stat.Size,
		ETag:          stat.ETag,
	}, nil
}

func (s *S3Store) Head(ctx context.Context, key string) (ObjectInfo, error) {
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("blob: head %q: %w", key, err)
	}
	return ObjectInfo{
		ContentType:   stat.ContentType,
		ContentLength: stat.Size,
		ETag:          stat.ETag,
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("blob: delete %q: %w", key, err)
	}
	return nil
}

// parseEndpoint normalizes endpoints like "http://host:8333" or "host:8333"
// into (host:port, secure) as minio-go expects.
func parseEndpoint(endpoint string) (string, bool, error) {
	if !strings.Contains(endpoint, "://") {
		return endpoint, false, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, err
	}
	host := u.Host
	if host == "" {
		return "", false, fmt.Errorf("missing host in %q", endpoint)
	}
	return host, u.Scheme == "https", nil
}
