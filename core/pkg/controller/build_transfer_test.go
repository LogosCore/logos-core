package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

// transferBuilds is one building build with one file, behind real links.
type transferBuilds struct {
	links       *builds.Links
	build, file uuid.UUID
	ended       bool
	limit       int64
	artifact    []byte
	mediaType   string
}

func (f *transferBuilds) UploadFile(context.Context, uuid.UUID, string, string, io.Reader, int64, models.ModuleActor) (*models.BuildFile, error) {
	return nil, errors.New("not here")
}
func (f *transferBuilds) Build(context.Context, string) (*models.Build, error) {
	return nil, errors.New("not here")
}
func (f *transferBuilds) OpenArtifact(context.Context, string) (io.ReadCloser, *models.Build, error) {
	return nil, nil, errors.New("not here")
}

func (f *transferBuilds) CheckLink(path, expires, sig string) error {
	return f.links.Check(path, expires, sig, time.Now())
}

func (f *transferBuilds) FactoryFile(_ context.Context, build, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error) {
	switch {
	case build != f.build || file != f.file:
		return nil, nil, builds.ErrTransferNotFound
	case f.ended:
		return nil, nil, builds.ErrBuildEnded
	}
	return io.NopCloser(strings.NewReader("png!")), &models.BuildFile{FileID: file, Size: 4, MediaType: "image/png"}, nil
}

func (f *transferBuilds) FactoryArtifact(_ context.Context, build uuid.UUID, body io.Reader, size int64, mediaType string) error {
	switch {
	case build != f.build:
		return builds.ErrTransferNotFound
	case f.ended:
		return builds.ErrBuildEnded
	case size < 0:
		return builds.ErrLengthRequired
	case size > f.limit:
		return &builds.ArtifactTooLargeError{Size: size, Limit: f.limit}
	}
	b, err := io.ReadAll(body)
	f.artifact, f.mediaType = b, mediaType
	return err
}

// A factory follows its build's links: the file and the upload go through
// with a link of core's, and nothing goes without one (no signature,
// another build's, expired); the service's refusals keep their statuses.
func TestFactoryTransferRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	links, err := builds.NewLinks("secret")
	if err != nil {
		t.Fatal(err)
	}
	f := &transferBuilds{links: links, build: uuid.New(), file: uuid.New(), limit: 8}
	bc := NewBuildController(f, nil, zap.NewNop(), 1<<20)
	r := gin.New()
	r.GET("/api/factory/builds/:build/files/:file", bc.FactoryFile)
	r.PUT("/api/factory/builds/:build/artifact", bc.FactoryArtifact)
	srv := httptest.NewServer(r)
	defer srv.Close()
	soon := time.Now().Add(time.Hour)
	fileLink := srv.URL + links.Sign(builds.FilePath(f.build, f.file), soon)
	artifactLink := srv.URL + links.Sign(builds.ArtifactPath(f.build), soon)

	do := func(method, url string, body []byte) (int, string, http.Header) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, url, rd)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "Application/Octet-Stream; x=1")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		got, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(got), resp.Header
	}

	if code, body, h := do(http.MethodGet, fileLink, nil); code != http.StatusOK || body != "png!" ||
		h.Get("Content-Type") != "image/png" || h.Get("Content-Length") != "4" {
		t.Fatalf("file: %d %q %v", code, body, h)
	}
	if code, body, _ := do(http.MethodPut, artifactLink, []byte("mz")); code != http.StatusNoContent ||
		string(f.artifact) != "mz" || f.mediaType != "application/octet-stream" {
		t.Fatalf("artifact: %d %q, stored %q as %q", code, body, f.artifact, f.mediaType)
	}

	other := uuid.New()
	refused := map[string][2]string{
		"no signature":       {http.MethodGet, srv.URL + builds.FilePath(f.build, f.file)},
		"another file":       {http.MethodGet, strings.Replace(fileLink, f.file.String(), other.String(), 1)},
		"another build":      {http.MethodPut, strings.Replace(artifactLink, f.build.String(), other.String(), 1)},
		"the file's link":    {http.MethodPut, srv.URL + builds.ArtifactPath(f.build) + fileLink[strings.Index(fileLink, "?"):]},
		"expired":            {http.MethodGet, srv.URL + links.Sign(builds.FilePath(f.build, f.file), time.Now().Add(-time.Minute))},
		"a changed expiry":   {http.MethodGet, strings.Replace(fileLink, "expires=", "expires=9", 1)},
		"a changed artifact": {http.MethodPut, strings.Replace(artifactLink, "sig=", "sig=0", 1)},
	}
	for name, c := range refused {
		f.artifact = nil
		if code, body, _ := do(c[0], c[1], []byte("mz")); code != http.StatusForbidden || f.artifact != nil {
			t.Errorf("%s: %d %q", name, code, body)
		}
	}
	if code, _, _ := do(http.MethodGet, srv.URL+"/api/factory/builds/x/files/y", nil); code != http.StatusNotFound {
		t.Errorf("a path naming no build: %d", code)
	}

	// The service's refusals, each with its status.
	if code, _, _ := do(http.MethodPut, artifactLink, []byte("123456789")); code != http.StatusRequestEntityTooLarge {
		t.Errorf("over the limit: %d", code)
	}
	req, _ := http.NewRequest(http.MethodPut, artifactLink, io.MultiReader(strings.NewReader("mz"))) // no length: chunked
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusLengthRequired {
		t.Errorf("no length: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	f.ended = true
	if code, _, _ := do(http.MethodGet, fileLink, nil); code != http.StatusConflict {
		t.Errorf("file of an ended build: %d", code)
	}
	if code, _, _ := do(http.MethodPut, artifactLink, []byte("mz")); code != http.StatusConflict {
		t.Errorf("artifact of an ended build: %d", code)
	}
}
