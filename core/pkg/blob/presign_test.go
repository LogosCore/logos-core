package blob

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestPresignerSignsOffline(t *testing.T) {
	// Nothing listens there: signing must not need the store.
	p, err := NewPresigner(S3Config{Endpoint: "http://seaweedfs-s3:1", AccessKey: "ak", SecretKey: "sk", Bucket: "logos-builds"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for name, sign := range map[string]func() (string, error){
		"get": func() (string, error) { return p.GetURL(ctx, "files/op/f1", time.Minute) },
		"put": func() (string, error) { return p.PutURL(ctx, "artifacts/op/b1", time.Minute) },
	} {
		raw, err := sign()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host != "seaweedfs-s3:1" || !strings.HasPrefix(u.Path, "/logos-builds/") ||
			u.Query().Get("X-Amz-Signature") == "" || u.Query().Get("X-Amz-Expires") != "60" {
			t.Fatalf("%s: %s", name, raw)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	missing := fmt.Errorf("blob: stat %q: %w", "k", minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404})
	if !IsNotFound(missing) {
		t.Error("a wrapped NoSuchKey is not found")
	}
	if IsNotFound(fmt.Errorf("blob: stat: %w", minio.ErrorResponse{Code: "InternalError", StatusCode: 500})) ||
		IsNotFound(errors.New("connection refused")) {
		t.Error("a store failure taken for a missing object")
	}
}
