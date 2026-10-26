package blob

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// presignRegion is the region URLs are signed for. Fixing it is what lets
// signing work offline: without one, minio-go asks the server for the bucket's
// location first. SeaweedFS answers to any region.
const presignRegion = "us-east-1"

// Presigner signs URLs to one bucket's objects for someone else to fetch or
// store: a factory downloading a build's files and uploading its artifact,
// without credentials of its own and without the bytes passing through core.
type Presigner struct {
	client *minio.Client
	bucket string
}

// NewPresigner signs for cfg.Bucket at cfg.Endpoint: the address the URLs'
// users reach, which may differ from the one core itself uses. Signing a URL
// does not contact the store.
func NewPresigner(cfg S3Config) (*Presigner, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("blob: presigner needs an endpoint and a bucket")
	}
	endpoint, secure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("blob: invalid endpoint: %w", err)
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: presignRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("blob: new minio client: %w", err)
	}
	return &Presigner{client: client, bucket: cfg.Bucket}, nil
}

// GetURL is a URL that downloads key until expiry passes.
func (p *Presigner) GetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := p.client.PresignedGetObject(ctx, p.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("blob: presign get %q: %w", key, err)
	}
	return u.String(), nil
}

// PutURL is a URL that uploads key (a single PUT) until expiry passes.
func (p *Presigner) PutURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := p.client.PresignedPutObject(ctx, p.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("blob: presign put %q: %w", key, err)
	}
	return u.String(), nil
}

// IsNotFound reports whether err says the object does not exist, as opposed
// to the store failing to answer.
func IsNotFound(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return resp.Code == "NoSuchKey" || resp.StatusCode == http.StatusNotFound
	}
	return false
}
