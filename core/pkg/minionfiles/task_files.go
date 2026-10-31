package minionfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// Task files (docs: contracts/task-results-and-actions.md, Task files): a
// file an operator uploads for a task's file param, which core sends the
// minion in chunks the minion asks for (minionsync, uploads.go). Kept in
// the minion files bucket as a file a minion sent is, while a task that
// names it has not ended; dropped when the last one ends, or a day after
// its upload when no task names it.

// ErrTooLarge is a task file over the largest taken.
var ErrTooLarge = errors.New("the file is too large")

// ErrNoTaskFile is a task file that is not the operation's, or is gone.
var ErrNoTaskFile = errors.New("no such task file in this operation: upload it again")

// PutTaskFile keeps a file an operator uploads, streamed from body, of at
// most MaxSize bytes.
func (s *Service) PutTaskFile(ctx context.Context, operationID, by uuid.UUID, name string, body io.Reader) (*models.MinionFile, error) {
	id := uuid.New()
	sum := sha256.New()
	head := &prefix{max: sniffLen}
	// One byte past the max tells a file over it.
	limited := &io.LimitedReader{R: body, N: s.cfg.MaxSize + 1}
	size, err := s.store.PutStream(ctx, objectKey(id), io.TeeReader(limited, io.MultiWriter(sum, head)), "application/octet-stream")
	if err == nil && size > s.cfg.MaxSize {
		err = ErrTooLarge
	}
	if err != nil {
		_ = s.store.Delete(ctx, objectKey(id))
		return nil, err
	}
	// A task file has no session; its own random xfer keeps it apart from
	// every other under the unique (session_id, xfer).
	xfer := make([]byte, 16)
	_, _ = rand.Read(xfer)
	now := s.now()
	f := &models.MinionFile{FileID: id, Kind: models.MinionFileKindTask, UploadedBy: by, OperationID: operationID, Xfer: xfer,
		Name: name, Total: size, Received: size, SHA256: sum.Sum(nil), ContentType: mimetype.Detect(head.b).String(),
		State: models.MinionFileComplete, LastChunkAt: now, DoneAt: &now}
	if err := s.files.Insert(ctx, f); err != nil {
		_ = s.store.Delete(ctx, objectKey(id))
		return nil, fmt.Errorf("keep the task file: %w", err)
	}
	s.logger.Info("task file uploaded", zap.String("file_id", id.String()), zap.String("name", name), zap.Int64("size", size))
	return f, nil
}

// TaskFile is an operation's task file by its id, while it is kept.
func (s *Service) TaskFile(ctx context.Context, operationID uuid.UUID, id string) (*models.MinionFile, error) {
	fid, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrNoTaskFile
	}
	f, err := s.files.FindByID(ctx, fid)
	if repository.IsNotFound(err) || (err == nil && (f.Kind != models.MinionFileKindTask || f.OperationID != operationID)) {
		return nil, ErrNoTaskFile
	}
	return f, err
}

// ReadChunk is n bytes of a kept file from off (fewer at its end).
func (s *Service) ReadChunk(ctx context.Context, fileID uuid.UUID, off int64, n int) ([]byte, error) {
	body, _, err := s.store.Get(ctx, objectKey(fileID))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	if sk, ok := body.(io.Seeker); ok {
		_, err = sk.Seek(off, io.SeekStart)
	} else {
		_, err = io.CopyN(io.Discard, body, off)
	}
	if err != nil {
		return nil, fmt.Errorf("read from %d: %w", off, err)
	}
	buf := make([]byte, n)
	got, err := io.ReadFull(body, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}

// ReleaseTaskFile drops a task file no task that has not ended names: one
// some task named, or one older than IdleAfter that none did. Nothing for
// a file in use, or not a task file.
func (s *Service) ReleaseTaskFile(ctx context.Context, fileID uuid.UUID) error {
	f, err := s.files.FindByID(ctx, fileID)
	if repository.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.release(ctx, *f)
}

func (s *Service) release(ctx context.Context, f models.MinionFile) error {
	if f.Kind != models.MinionFileKindTask {
		return nil
	}
	open, named, err := s.tasks.UploadFileUse(ctx, f.FileID)
	if err != nil {
		return err
	}
	if open || (!named && f.CreateAt.After(s.now().Add(-s.cfg.IdleAfter))) {
		return nil
	}
	if err := s.store.Delete(ctx, objectKey(f.FileID)); err != nil {
		return fmt.Errorf("drop the task file's bytes: %w", err)
	}
	if err := s.files.Delete(ctx, f.FileID); err != nil {
		return err
	}
	s.logger.Info("task file dropped", zap.String("file_id", f.FileID.String()), zap.String("name", f.Name), zap.Bool("named", named))
	return nil
}

// sweepTaskFiles drops the task files no task needs any more: the backstop
// of ReleaseTaskFile, for tasks that ended where no release is called (one
// cancelled before it was sent), and uploads no task named.
func (s *Service) sweepTaskFiles(ctx context.Context) {
	files, err := s.files.TaskFiles(ctx, s.now())
	if err != nil {
		s.logger.Error("minion files: list task files", zap.Error(err))
		return
	}
	for _, f := range files {
		if err := s.release(ctx, f); err != nil {
			s.logger.Error("minion files: release a task file", zap.String("file_id", f.FileID.String()), zap.Error(err))
		}
	}
}
