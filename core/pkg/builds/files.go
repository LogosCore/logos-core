package builds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

// fileKey is where a build file's bytes are stored.
func fileKey(op, fileID uuid.UUID) string {
	return path.Join("files", op.String(), fileID.String())
}

// UploadFile stores a build file for operation op. size is the length the
// upload declared; body must deliver exactly that many bytes.
func (s *Service) UploadFile(ctx context.Context, op uuid.UUID, name, mediaType string, body io.Reader, size int64, actor models.ModuleActor) (*models.BuildFile, error) {
	if name == "" {
		return nil, invalid("the file needs a name")
	}
	if size < 0 || size > s.cfg.FileMaxSize {
		return nil, invalid("a build file may be at most %d bytes", s.cfg.FileMaxSize)
	}
	f := &models.BuildFile{
		FileID: uuid.New(), OperationID: op, Name: name, Size: size, MediaType: mediaType,
		UploadedBy: actor, CreatedAt: s.now(),
	}
	f.ObjectKey = fileKey(op, f.FileID)
	h := sha256.New()
	counted := &countingReader{r: io.TeeReader(io.LimitReader(body, size), h)}
	if err := s.store.Put(ctx, f.ObjectKey, counted, size, mediaType); err != nil {
		return nil, fmt.Errorf("store build file: %w", err)
	}
	if counted.n != size {
		_ = s.store.Delete(context.WithoutCancel(ctx), f.ObjectKey)
		return nil, invalid("the upload ended after %d of %d bytes", counted.n, size)
	}
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	if err := s.repos.Files.Insert(ctx, f); err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), f.ObjectKey)
		return nil, fmt.Errorf("record build file: %w", err)
	}
	return f, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Files returns an operation's build files, newest first.
func (s *Service) Files(ctx context.Context, op uuid.UUID) ([]models.BuildFile, error) {
	return s.repos.Files.List(ctx, op)
}

// File returns one build file.
func (s *Service) File(ctx context.Context, id string) (*models.BuildFile, error) {
	fid, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrFileNotFound
	}
	f, err := s.repos.Files.FindByFileID(ctx, fid)
	if err != nil {
		return nil, notFound(err, ErrFileNotFound)
	}
	return f, nil
}

// DeleteFile removes a build file nothing refers to. A preset or a build
// naming it keeps it: a build record must stay complete.
func (s *Service) DeleteFile(ctx context.Context, id string) error {
	f, err := s.File(ctx, id)
	if err != nil {
		return err
	}
	presets, builds, err := s.fileReferences(ctx, f.FileID.String())
	if err != nil {
		return err
	}
	if presets+builds > 0 {
		return invalid("%s is used by %d preset(s) and %d build(s)", f.Name, presets, builds)
	}
	return s.removeFile(ctx, f)
}

func (s *Service) fileReferences(ctx context.Context, id string) (presets, builds int64, err error) {
	if presets, err = s.repos.Presets.CountFileReferences(ctx, id); err != nil {
		return 0, 0, fmt.Errorf("count preset references: %w", err)
	}
	if builds, err = s.repos.Builds.CountFileReferences(ctx, id); err != nil {
		return 0, 0, fmt.Errorf("count build references: %w", err)
	}
	return presets, builds, nil
}

func (s *Service) removeFile(ctx context.Context, f *models.BuildFile) error {
	if err := s.store.Delete(ctx, f.ObjectKey); err != nil {
		return fmt.Errorf("delete build file bytes: %w", err)
	}
	if _, err := s.repos.Files.Delete(ctx, f.FileID); err != nil {
		return fmt.Errorf("delete build file: %w", err)
	}
	return nil
}

// sweepFiles removes uploads nothing referred to within FileGrace.
func (s *Service) sweepFiles(ctx context.Context) {
	old, err := s.repos.Files.ListCreatedBefore(ctx, s.now().Add(-s.cfg.FileGrace), 500)
	if err != nil {
		s.logger.Warn("build file sweep: list", zap.Error(err))
		return
	}
	for i := range old {
		presets, builds, err := s.fileReferences(ctx, old[i].FileID.String())
		if err != nil {
			s.logger.Warn("build file sweep: references", zap.Error(err))
			return
		}
		if presets+builds > 0 {
			continue
		}
		if err := s.removeFile(ctx, &old[i]); err != nil {
			s.logger.Warn("build file sweep: remove", zap.String("file_id", old[i].FileID.String()), zap.Error(err))
			continue
		}
		s.logger.Info("removed an unused build file", zap.String("file_id", old[i].FileID.String()), zap.String("name", old[i].Name))
	}
}
