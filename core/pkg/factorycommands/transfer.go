package factorycommands

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// What a factory command's call links to (docs: factory.command): paths on
// core's address, signed as a build's links are, for LinkTTL, and good while
// the task runs. The link is the credential. The task links of composite
// tasks share the prefix and the files' paths: one controller serves both.

// linkPrefix is where the links lead, and the routes that serve them.
const linkPrefix = "/api/factory/tasks/"

// UploadPath is where the factory puts a file for the task: PUT.
func UploadPath(task uuid.UUID) string { return linkPrefix + task.String() + "/files" }

// FilePath is a file of the task: one its args name, or one its factory put
// for it: GET.
func FilePath(task, file uuid.UUID) string {
	return linkPrefix + task.String() + "/files/" + file.String()
}

// ArtifactPath is the task's session's build's artifact: GET.
func ArtifactPath(task uuid.UUID) string { return linkPrefix + task.String() + "/artifact" }

// BuildFilePath is one of the task's session's build's files: GET.
func BuildFilePath(task, file uuid.UUID) string {
	return linkPrefix + task.String() + "/build-files/" + file.String()
}

// Transfer errors.
var (
	// ErrNotFound: no such task, or no such file of it.
	ErrNotFound = errors.New("no such task or file")
	// ErrTaskEnded: a link of a task that is no factory command's running.
	ErrTaskEnded = errors.New("the task is not a factory command's that runs")
	// ErrNoFileName: a put without its file's name.
	ErrNoFileName = errors.New("the file needs a name (" + protocol.HeaderFileName + "): a file's name alone, at most 255 bytes")
	// ErrLengthRequired: a put that does not say its length.
	ErrLengthRequired = errors.New("the file's length is required (Content-Length)")
	// errNotPut: a file a result names that the factory did not put for
	// the task.
	errNotPut = errors.New("not a file the factory put for the task")
)

// FileTooLargeError is a put over the largest file core takes.
type FileTooLargeError struct{ Max int64 }

func (e *FileTooLargeError) Error() string {
	return "the file is larger than core takes for a task (" + strconv.FormatInt(e.Max, 10) + " bytes)"
}

// CheckLink checks a link's expires and sig against path.
func (s *Service) CheckLink(path, expires, sig string) error {
	return s.links.Check(path, expires, sig, s.now())
}

// IsFactoryTask reports whether task is a factory command's: its links are
// this service's.
func (s *Service) IsFactoryTask(ctx context.Context, task uuid.UUID) (bool, error) {
	t, err := s.tasks.FindByTaskID(ctx, task)
	if repository.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return t.Factory != nil, nil
}

// File is a file of a task that runs, kept whole: one its args name, or one
// its factory put for it.
func (s *Service) File(ctx context.Context, task, file uuid.UUID) (io.ReadCloser, *models.MinionFile, error) {
	t, err := s.running(ctx, task)
	if err != nil {
		return nil, nil, err
	}
	if s.files == nil {
		return nil, nil, ErrNotFound
	}
	f, err := s.files.FileByID(ctx, file)
	if err != nil {
		return nil, nil, err
	}
	if f == nil || f.Kind != models.MinionFileKindTask || f.State != models.MinionFileComplete ||
		(!slices.Contains(t.Factory.Files, file) && f.ForTask != task) {
		return nil, nil, ErrNotFound
	}
	return s.files.Open(ctx, f.FileID)
}

// PutFile keeps a file the factory puts for a task that runs, of size bytes
// from body: the task's, kept while it runs (unless its result names it as
// the task's own). It answers with the file's id, size and SHA-256, as core
// kept it.
func (s *Service) PutFile(ctx context.Context, task uuid.UUID, name string, body io.Reader, size int64) (protocol.BuildFile, error) {
	switch {
	case !fileName(name):
		return protocol.BuildFile{}, ErrNoFileName
	case size < 0:
		return protocol.BuildFile{}, ErrLengthRequired
	case size > s.cfg.MaxFileSize:
		return protocol.BuildFile{}, &FileTooLargeError{Max: s.cfg.MaxFileSize}
	case s.files == nil:
		return protocol.BuildFile{}, ErrTaskEnded
	}
	t, err := s.running(ctx, task)
	if err != nil {
		return protocol.BuildFile{}, err
	}
	f, err := s.files.PutFactoryFile(ctx, t.OperationID, t.TaskID, t.IssuedBy, name, io.LimitReader(body, size))
	if err != nil {
		return protocol.BuildFile{}, err
	}
	s.logger.Info("factory command's factory put a file", zap.String("task_id", t.TaskID.String()), zap.String("file_id", f.FileID.String()),
		zap.String("name", name), zap.Int64("size", f.Total))
	return protocol.BuildFile{ID: f.FileID.String(), Name: name, Size: f.Total, SHA256: hex.EncodeToString(f.SHA256)}, nil
}

// BuildFile is one of a running task's session's build's files.
func (s *Service) BuildFile(ctx context.Context, task, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error) {
	t, err := s.running(ctx, task)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.buildOf(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	r, f, err := s.buildFiles.OpenFile(ctx, b, file)
	if errors.Is(err, builds.ErrTransferNotFound) {
		return nil, nil, ErrNotFound
	}
	return r, f, err
}

// Artifact is a running task's session's build's artifact, while it is kept.
func (s *Service) Artifact(ctx context.Context, task uuid.UUID) (io.ReadCloser, *models.BuildArtifact, error) {
	t, err := s.running(ctx, task)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.buildOf(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	if b.Artifact == nil || b.Artifact.Deleted {
		return nil, nil, ErrNotFound
	}
	r, opened, err := s.buildFiles.OpenArtifact(ctx, b.BuildID.String())
	if err != nil {
		if opened != nil && (opened.Artifact == nil || opened.Artifact.Deleted) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	return r, b.Artifact, nil
}

// running is a factory command's task that runs, offered to an instance or
// taken by one: ErrNotFound for no task, ErrTaskEnded for another.
func (s *Service) running(ctx context.Context, task uuid.UUID) (*models.MinionTask, error) {
	t, err := s.tasks.FindByTaskID(ctx, task)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !t.InFactory() || t.Factory.Instance == "" {
		return nil, ErrTaskEnded
	}
	return t, nil
}

// buildOf is a task's session's build.
func (s *Service) buildOf(ctx context.Context, t *models.MinionTask) (*models.Build, error) {
	sess, err := s.sessions.FindBySessionID(ctx, t.SessionID)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	b, err := s.builds.FindByBuildID(ctx, sess.BuildID)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return b, err
}

// fileName reports whether name is a file's name alone: no path, no control
// character, 255 bytes at most.
func fileName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && utf8.ValidString(name) &&
		!strings.ContainsAny(name, `/\`) && strings.IndexFunc(name, unicode.IsControl) < 0
}
