// Package filebrowser keeps what core's minions have seen of each machine's
// file system (docs: contracts/file-browser.md): a snapshot per machine of
// an operation, made from its sessions' tasks' results as they end (a
// listing, a delete, a download, an upload), and read by the file browser.
// Nothing is asked of a minion for it.
package filebrowser

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Tasks reads what a task gave.
type Tasks interface {
	Parts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error)
}

// Sessions reads a task's session, and a machine's.
type Sessions interface {
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error)
	ByMachine(ctx context.Context, operationID uuid.UUID, machineID string) ([]models.MinionSession, error)
}

// Files finds the file core keeps of a download task.
type Files interface {
	File(ctx context.Context, taskID uuid.UUID) (*models.MinionFile, error)
}

type Service struct {
	entries  repository.IFsEntryRepository
	tasks    Tasks
	sessions Sessions
	files    Files
	bus      eventbus.IEventBus
	logger   *zap.Logger
}

func NewService(entries repository.IFsEntryRepository, tasks Tasks, sessions Sessions, files Files, bus eventbus.IEventBus, logger *zap.Logger) *Service {
	return &Service{entries: entries, tasks: tasks, sessions: sessions, files: files, bus: bus, logger: logger.Named("filebrowser")}
}

// removed is the line BOB's rm (a delete_path) says what went with.
var removed = regexp.MustCompile(`(?m)^removed (.+) \((?:file|folder|link|other)\)$`)

// TaskEnded takes what a task that ended done says of its machine's files.
// It runs after the minion's exchange, so it neither slows nor fails it.
func (s *Service) TaskEnded(ctx context.Context, t *models.MinionTask) {
	if t.State != models.MinionTaskDone || t.DoneAt == nil {
		return
	}
	switch {
	case t.Result == protocol.ResultDirListing, t.Result == protocol.ResultFileDownload, t.Result == protocol.ResultFileStream,
		t.Result == protocol.ResultFileUpload, t.Action == protocol.ActionDeletePath:
	default:
		return
	}
	session, err := s.sessions.FindBySessionID(ctx, t.SessionID)
	if err != nil {
		s.logger.Warn("file browser: the task's session", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		return
	}
	machine := models.FsMachineKey(*session)
	ref := models.FsTaskRef{TaskID: t.TaskID, SessionID: session.SessionID, Number: session.Number, User: session.Hello.User}
	at := *t.DoneAt
	folder, err := s.take(ctx, t, machine, ref, at)
	if err != nil {
		s.logger.Warn("file browser: a task's result", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		return
	}
	if folder != "" && s.bus != nil {
		s.bus.Publish(eventbus.NewFileBrowserChangedEvent(eventbus.FileBrowserEventPayload{
			OperationID: t.OperationID.String(), Machine: machine, Folder: folder}))
	}
}

// take applies a task to the snapshot: the folder it changed, "" for none.
func (s *Service) take(ctx context.Context, t *models.MinionTask, machine string, ref models.FsTaskRef, at time.Time) (string, error) {
	op := t.OperationID
	if t.Action == protocol.ActionDeletePath {
		path := ""
		parts, err := s.tasks.Parts(ctx, t.TaskID)
		if err != nil {
			return "", err
		}
		for _, p := range parts {
			if m := removed.FindSubmatch(p.Out); m != nil {
				path = string(m[1])
			}
		}
		if arg, _ := t.Args["path"].(string); path == "" && absolute(arg) {
			path = arg
		}
		if path == "" {
			return "", nil
		}
		e := entryAt(path, sepOf(path))
		if err := s.entries.Ensure(ctx, op, machine, ancestors(e)); err != nil {
			return "", err
		}
		return e.Parent, s.entries.Gone(ctx, op, machine, e, models.FsGoneDeleted, at, ref)
	}

	parts, err := s.tasks.Parts(ctx, t.TaskID)
	if err != nil {
		return "", err
	}
	if t.Result == protocol.ResultDirListing {
		if t.DataProblem != "" {
			return "", nil
		}
		// A listing core kept only part of (its data past the 4 MiB a task
		// keeps, or parts lost) adds what it has, and says nothing of the rest.
		return s.listing(ctx, op, machine, parts, !t.DataTruncated && t.MissingParts() == 0, at, ref)
	}
	for _, p := range parts {
		if len(p.DataCBOR) == 0 {
			continue
		}
		var path string
		var tr models.FsTransfer
		up := false
		switch t.Result {
		case protocol.ResultFileDownload:
			d, err := protocol.ReadFileDownload(p.Seq, p.DataCBOR)
			if err != nil {
				return "", err
			}
			path, tr = d.Path, models.FsTransfer{Size: int64(d.Size), SHA256: d.SHA256}
			if f, err := s.files.File(ctx, t.TaskID); err == nil && f != nil {
				id := f.FileID
				tr.FileID = &id
			}
		case protocol.ResultFileStream:
			d, err := protocol.ReadFileStream(p.Seq, p.DataCBOR)
			if err != nil {
				return "", err
			}
			path, tr = d.Path, models.FsTransfer{Size: int64(d.Size), SHA256: d.SHA256, Stream: true}
		case protocol.ResultFileUpload:
			d, err := protocol.ReadFileUpload(p.Seq, p.DataCBOR)
			if err != nil {
				return "", err
			}
			by := t.IssuedBy
			path, tr, up = d.Path, models.FsTransfer{Size: int64(d.Size), SHA256: d.SHA256, By: &by, Stream: t.Upload != nil && t.Upload.Stream}, true
		}
		if path == "" {
			continue
		}
		tr.TaskID, tr.At = t.TaskID, at
		e := entryAt(path, sepOf(path))
		if err := s.entries.Ensure(ctx, op, machine, ancestors(e)); err != nil {
			return "", err
		}
		if err := s.entries.Transfer(ctx, op, machine, e, up, tr, ref); err != nil {
			return "", err
		}
		return e.Parent, nil
	}
	return "", nil
}

// listing applies a dir_listing's parts: the folder the first says, the
// entries all of them have.
func (s *Service) listing(ctx context.Context, op uuid.UUID, machine string, parts []models.MinionTaskPart, whole bool, at time.Time, ref models.FsTaskRef) (string, error) {
	var folder models.FsEntry
	var children []models.FsEntry
	first := true
	for _, p := range parts {
		if len(p.DataCBOR) == 0 {
			continue
		}
		seq := uint64(1)
		if first {
			seq = 0
		}
		l, err := protocol.ReadDirListing(seq, p.DataCBOR)
		if err != nil {
			return "", err
		}
		if first {
			folder, first = entryAt(l.Path, l.Sep), false
		}
		for _, d := range l.Entries {
			e := child(folder, d.Name)
			e.Type, e.Mode, e.Owner, e.Group, e.Target, e.Hidden = d.Type, d.Mode, d.Owner, d.Group, d.Target, d.Hidden
			if d.Size != nil {
				n := int64(*d.Size)
				e.Size = &n
			}
			e.Modified = d.Modified
			children = append(children, e)
		}
	}
	if first {
		return "", nil
	}
	if err := s.entries.Ensure(ctx, op, machine, ancestors(folder)); err != nil {
		return "", err
	}
	applied, err := s.entries.Listing(ctx, op, machine, folder, children, whole, at, ref)
	if err != nil || !applied {
		return "", err
	}
	return folder.Path, nil
}

// Machine is a session's machine: its key, and the operation's sessions
// that feed it, newest first.
func (s *Service) Machine(ctx context.Context, session *models.MinionSession) (string, []models.MinionSession, error) {
	key := models.FsMachineKey(*session)
	if strings.HasPrefix(key, "session:") {
		return key, []models.MinionSession{*session}, nil
	}
	sessions, err := s.sessions.ByMachine(ctx, session.OperationID, key)
	return key, sessions, err
}

// Folder is an entry of the machine by its path (as the host writes it):
// nil for one the snapshot does not have.
func (s *Service) Folder(ctx context.Context, op uuid.UUID, machine, path string) (*models.FsEntry, error) {
	sep := sepOf(path)
	e, err := s.entries.Get(ctx, op, machine, entryAt(path, sep).PathKey)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	return e, err
}

// Roots are the machine's file systems' roots it knows.
func (s *Service) Roots(ctx context.Context, op uuid.UUID, machine string, showDeleted bool) ([]models.FsEntry, error) {
	out, _, err := s.entries.Children(ctx, op, machine, "", repository.FsQuery{ShowDeleted: showDeleted, First: 500})
	return out, err
}

// Children are a folder's entries, a page at a time.
func (s *Service) Children(ctx context.Context, op uuid.UUID, machine, path string, q repository.FsQuery) ([]models.FsEntry, string, error) {
	parent := ""
	if path != "" {
		parent = entryAt(path, sepOf(path)).PathKey
	}
	return s.entries.Children(ctx, op, machine, parent, q)
}

// Search is the machine's entries whose names match query: a part of the
// name, or a glob (* and ?), any case.
func (s *Service) Search(ctx context.Context, op uuid.UUID, machine, query string, showDeleted bool, after string, first int64) ([]models.FsEntry, string, error) {
	return s.entries.Search(ctx, op, machine, pattern(query), showDeleted, after, first)
}
