package builds

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A link is signed for one build's file or its artifact, until its expiry:
// another file, another path, a later expiry or another core's key is
// refused, and so is the link once it has expired.
func TestLinks(t *testing.T) {
	l, err := NewLinks("secret")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	exp := now.Add(time.Hour)
	build, file := uuid.New(), uuid.New()
	link := l.Sign(FilePath(build, file), exp)
	u, err := url.Parse(link)
	if err != nil || u.Path != FilePath(build, file) || !protocol.IsLink(link) {
		t.Fatalf("link %q: %v", link, err)
	}
	expires, sig := u.Query().Get("expires"), u.Query().Get("sig")
	for _, at := range []time.Time{now, exp} {
		if err := l.Check(FilePath(build, file), expires, sig, at); err != nil {
			t.Fatalf("at %v: %v", at, err)
		}
	}
	if err := l.Check(FilePath(build, file), expires, sig, exp.Add(time.Second)); !errors.Is(err, ErrLinkExpired) {
		t.Fatalf("past its expiry: %v", err)
	}

	other, err := NewLinks("another core's secret")
	if err != nil {
		t.Fatal(err)
	}
	later := strconv.FormatInt(exp.Add(time.Hour).Unix(), 10)
	for name, err := range map[string]error{
		"another file":   l.Check(FilePath(build, uuid.New()), expires, sig, now),
		"another build":  l.Check(FilePath(uuid.New(), file), expires, sig, now),
		"the artifact":   l.Check(ArtifactPath(build), expires, sig, now),
		"a later expiry": l.Check(FilePath(build, file), later, sig, now),
		"no signature":   l.Check(FilePath(build, file), expires, "", now),
		"no expiry":      l.Check(FilePath(build, file), "", sig, now),
		"another core":   other.Check(FilePath(build, file), expires, sig, now),
	} {
		if !errors.Is(err, ErrLinkSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := NewLinks(""); err == nil {
		t.Error("links without a secret")
	}
}

// The factory building a build fetches its files and uploads its artifact
// through core: the build's own files alone, an artifact that says its
// length and fits the build's limit, until the build ends.
func TestFactoryTransfers(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	badge := r.upload(t, "badge.png", "image/png", "png!")
	other := r.upload(t, "other.png", "image/png", "other")
	b := r.queue(t, func(in *BuildInput) { in.Values["badge"] = badge.FileID.String() })
	r.svc.Tick(ctx)
	r.get(t, b.BuildID.String(), models.BuildStateBuilding)

	req := r.rpc.recorded()[0].payload.(protocol.BuildRequest)
	for _, link := range []string{req.Artifact.UploadURL, req.Files[badge.FileID.String()].URL} {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.svc.CheckLink(u.Path, u.Query().Get("expires"), u.Query().Get("sig")); err != nil {
			t.Errorf("%s: %v", link, err)
		}
	}

	rc, f, err := r.svc.FactoryFile(ctx, b.BuildID, badge.FileID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || string(got) != "png!" || f.MediaType != "image/png" || f.Size != 4 {
		t.Fatalf("file %q %+v: %v", got, f, err)
	}
	if _, _, err := r.svc.FactoryFile(ctx, b.BuildID, other.FileID); !errors.Is(err, ErrTransferNotFound) {
		t.Fatalf("another build's file: %v", err)
	}
	if _, _, err := r.svc.FactoryFile(ctx, uuid.New(), badge.FileID); !errors.Is(err, ErrTransferNotFound) {
		t.Fatalf("no such build: %v", err)
	}

	key := artifactKey(opID, b.BuildID)
	if err := r.svc.FactoryArtifact(ctx, b.BuildID, strings.NewReader("mz"), -1, ""); !errors.Is(err, ErrLengthRequired) {
		t.Fatalf("no length: %v", err)
	}
	var tooLarge *ArtifactTooLargeError
	if err := r.svc.FactoryArtifact(ctx, b.BuildID, strings.NewReader(""), 256<<20+1, ""); !errors.As(err, &tooLarge) || tooLarge.Limit != 256<<20 {
		t.Fatalf("over the limit: %v", err)
	}
	r.builds.setBuild(b.BuildID, func(b *models.Build) { b.ArtifactMaxSize = 1 })
	if err := r.svc.FactoryArtifact(ctx, b.BuildID, strings.NewReader("mz"), 2, ""); !errors.As(err, &tooLarge) || tooLarge.Limit != 1 {
		t.Fatalf("over the build's own limit: %v", err)
	}
	if r.store.has(key) {
		t.Fatal("a refused upload was stored")
	}
	r.builds.setBuild(b.BuildID, func(b *models.Build) { b.ArtifactMaxSize = 2 })
	if err := r.svc.FactoryArtifact(ctx, b.BuildID, strings.NewReader("mz"), 2, "application/octet-stream"); err != nil || !r.store.has(key) {
		t.Fatalf("upload: %v (stored %v)", err, r.store.has(key))
	}

	// Ended, the build's links lead nowhere.
	r.builds.setBuild(b.BuildID, func(b *models.Build) { b.State = models.BuildStateCancelled })
	if _, _, err := r.svc.FactoryFile(ctx, b.BuildID, badge.FileID); !errors.Is(err, ErrBuildEnded) {
		t.Fatalf("file of an ended build: %v", err)
	}
	if err := r.svc.FactoryArtifact(ctx, b.BuildID, strings.NewReader("mz"), 2, ""); !errors.Is(err, ErrBuildEnded) {
		t.Fatalf("artifact of an ended build: %v", err)
	}
}
