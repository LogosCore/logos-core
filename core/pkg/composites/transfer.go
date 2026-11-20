package composites

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// What a composite task's calls link to (docs: What travels, and how;
// Files): paths on core's address, signed as a build's links are, for the
// call until a minute past its deadline. The link is the credential.

// linkPrefix is where the links lead, and the routes that serve them.
const linkPrefix = "/api/factory/tasks/"

// UploadPath is where the factory puts a file for a composite task: PUT.
func UploadPath(task uuid.UUID) string { return linkPrefix + task.String() + "/files" }

// FilePath is a task's file: GET.
func FilePath(task, file uuid.UUID) string {
	return linkPrefix + task.String() + "/files/" + file.String()
}

// DataPath is a task's data, as JSON: GET.
func DataPath(task uuid.UUID) string { return linkPrefix + task.String() + "/data" }

// OutPath is a task's output: GET.
func OutPath(task uuid.UUID) string { return linkPrefix + task.String() + "/out" }

// Transfer errors.
var (
	// ErrNotFound: no such task, or no such file of it.
	ErrNotFound = errors.New("no such task or file")
	// ErrTaskEnded: a file put for a task that is no composite command's
	// running.
	ErrTaskEnded = errors.New("the task is not a composite command's that runs")
	// ErrNoFileName: a put without its file's name.
	ErrNoFileName = errors.New("the file needs a name (" + protocol.HeaderFileName + "): a file's name alone, at most 255 bytes")
	// ErrLengthRequired: a put that does not say its length.
	ErrLengthRequired = errors.New("the file's length is required (Content-Length)")
)

// FileTooLargeError is a put over the largest file core takes.
type FileTooLargeError struct{ Max int64 }

func (e *FileTooLargeError) Error() string {
	return "the file is larger than core takes for a task (" + formatSize(e.Max) + ")"
}

// CheckLink checks a link's expires and sig against path.
func (s *Service) CheckLink(path, expires, sig string) error {
	return s.links.Check(path, expires, sig, s.now())
}

// Data is a task's data as an end gives it: JSON, "{}" for none.
func (s *Service) Data(ctx context.Context, task uuid.UUID) ([]byte, error) {
	t, err := s.task(ctx, task)
	if err != nil {
		return nil, err
	}
	parts, err := s.tasks.Parts(ctx, t.TaskID)
	if err != nil {
		return nil, err
	}
	data, err := dataJSON(*t, parts)
	if err != nil || data != nil {
		return data, err
	}
	return []byte("{}"), nil
}

// Out is a task's output, its parts' joined.
func (s *Service) Out(ctx context.Context, task uuid.UUID) ([]byte, error) {
	t, err := s.task(ctx, task)
	if err != nil {
		return nil, err
	}
	parts, err := s.tasks.OutParts(ctx, t.TaskID)
	if err != nil {
		return nil, err
	}
	return joinOut(parts), nil
}

// File is a file a task got, or one the factory put for it, kept whole.
func (s *Service) File(ctx context.Context, task, file uuid.UUID) (io.ReadCloser, *models.MinionFile, error) {
	if s.files == nil {
		return nil, nil, ErrNotFound
	}
	f, err := s.files.FileByID(ctx, file)
	if err != nil {
		return nil, nil, err
	}
	if f == nil || (f.TaskID != task && f.ForTask != task && !slices.Contains(f.AlsoTasks, task)) || f.State != models.MinionFileComplete {
		return nil, nil, ErrNotFound
	}
	body, f, err := s.files.Open(ctx, f.FileID)
	if err != nil {
		return nil, nil, err
	}
	return body, f, nil
}

// PutFile keeps a file the factory puts for a composite task that runs, of
// size bytes from body: the task's, kept as an operator's task file is
// while the task, or a subtask that names it, has not ended. It answers
// with the file's id, size and SHA-256, as core kept it.
func (s *Service) PutFile(ctx context.Context, task uuid.UUID, name string, body io.Reader, size int64) (protocol.BuildFile, error) {
	if !fileName(name) {
		return protocol.BuildFile{}, ErrNoFileName
	}
	if size < 0 {
		return protocol.BuildFile{}, ErrLengthRequired
	}
	if size > s.cfg.MaxFileSize {
		return protocol.BuildFile{}, &FileTooLargeError{Max: s.cfg.MaxFileSize}
	}
	if s.files == nil {
		return protocol.BuildFile{}, ErrTaskEnded
	}
	t, err := s.tasks.FindByTaskID(ctx, task)
	if repository.IsNotFound(err) {
		return protocol.BuildFile{}, ErrNotFound
	}
	if err != nil {
		return protocol.BuildFile{}, err
	}
	if !t.Composing() {
		return protocol.BuildFile{}, ErrTaskEnded
	}
	f, err := s.files.PutFactoryFile(ctx, t.OperationID, t.TaskID, t.IssuedBy, name, io.LimitReader(body, size))
	if err != nil {
		return protocol.BuildFile{}, err
	}
	s.logger.Info("composite task's factory put a file", zap.String("task_id", t.TaskID.String()), zap.String("file_id", f.FileID.String()),
		zap.String("name", name), zap.Int64("size", f.Total))
	out := s.buildFile(t, f, s.now())
	out.Name, out.URL = name, ""
	return out, nil
}

// task is a task by its id: ErrNotFound for none.
func (s *Service) task(ctx context.Context, id uuid.UUID) (*models.MinionTask, error) {
	t, err := s.tasks.FindByTaskID(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return t, err
}

// fileName reports whether name is a file's name alone: no path, no
// control character, 255 bytes at most.
func fileName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && utf8.ValidString(name) &&
		!strings.ContainsAny(name, `/\`) && strings.IndexFunc(name, unicode.IsControl) < 0
}

func formatSize(n int64) string {
	const gib = 1 << 30
	if n%gib == 0 {
		return itoa(n/gib) + " GiB"
	}
	return itoa(n) + " bytes"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
