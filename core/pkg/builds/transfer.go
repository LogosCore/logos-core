package builds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// The endpoints a factory.build's links lead to (links.go): core streams a
// build's files from its storage and the artifact into it, so a factory
// needs to reach core alone.

// Errors of the transfer endpoints.
var (
	// ErrTransferNotFound: no such build, or the file is not one of its.
	ErrTransferNotFound = errors.New("no such build file")
	// ErrBuildEnded: the build has finished, and its links with it.
	ErrBuildEnded = errors.New("the build has ended")
	// ErrLengthRequired: an artifact's upload says its length.
	ErrLengthRequired = errors.New("the artifact's length is required (Content-Length)")
)

// ArtifactTooLargeError is an artifact over its build's limit, refused
// before a byte of it is read.
type ArtifactTooLargeError struct {
	Size, Limit int64
}

func (e *ArtifactTooLargeError) Error() string {
	return fmt.Sprintf("the artifact is %d bytes; the build takes at most %d", e.Size, e.Limit)
}

// CheckLink checks a link a factory followed: path (FilePath, ArtifactPath)
// and the link's query.
func (s *Service) CheckLink(path, expires, sig string) error {
	return s.links.Check(path, expires, sig, s.now())
}

// FactoryFile opens one of build's files for its factory, until the build
// ends. The caller closes it.
func (s *Service) FactoryFile(ctx context.Context, build, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error) {
	b, err := s.transferBuild(ctx, build)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(b.FileIDs, file) {
		return nil, nil, ErrTransferNotFound
	}
	f, err := s.file(ctx, b.OperationID, file)
	if errors.Is(err, ErrFileNotFound) {
		return nil, nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	r, _, err := s.store.Get(ctx, f.ObjectKey)
	if err != nil {
		return nil, nil, fmt.Errorf("open build file: %w", err)
	}
	return r, f, nil
}

// FactoryArtifact stores build's artifact as its factory uploads it: size
// bytes of body, at most the build's limit, until the build ends. Another
// upload replaces it; the factory's result says which bytes it meant, and
// HandleResult checks them.
func (s *Service) FactoryArtifact(ctx context.Context, build uuid.UUID, body io.Reader, size int64, contentType string) error {
	b, err := s.transferBuild(ctx, build)
	if err != nil {
		return err
	}
	if size < 0 {
		return ErrLengthRequired
	}
	limit := b.ArtifactMaxSize
	if limit <= 0 {
		limit = s.cfg.ArtifactMaxSize // offered, and not yet marked building
	}
	if size > limit {
		return &ArtifactTooLargeError{Size: size, Limit: limit}
	}
	if err := s.store.Put(ctx, artifactKey(b.OperationID, b.BuildID), body, size, contentType); err != nil {
		return fmt.Errorf("store artifact: %w", err)
	}
	return nil
}

// transferBuild is the build a link names, while its links hold: not yet
// finished. Not "building" alone: a factory may follow them the moment it
// accepts, before core has marked the build.
func (s *Service) transferBuild(ctx context.Context, build uuid.UUID) (*models.Build, error) {
	b, err := s.repos.Builds.FindByBuildID(ctx, build)
	if repository.IsNotFound(err) {
		return nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load build: %w", err)
	}
	if b.Finished() {
		return nil, ErrBuildEnded
	}
	return b, nil
}
