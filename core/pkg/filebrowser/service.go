// Package filebrowser keeps what core's minions have seen of each machine's
// file system (docs: contracts/file-browser.md): a snapshot per machine of
// an operation, made from its sessions' tasks: their listings as their
// pieces come (any command's), and their results as they end (a delete, a
// download, an upload, the machine's volumes); read by the file browser.
// Nothing is asked of a minion for it.
package filebrowser

import (
	"context"
	"math"
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
	listings repository.IFsListingRepository
	tasks    Tasks
	sessions Sessions
	files    Files
	bus      eventbus.IEventBus
	logger   *zap.Logger
}

func NewService(entries repository.IFsEntryRepository, listings repository.IFsListingRepository, tasks Tasks, sessions Sessions, files Files,
	bus eventbus.IEventBus, logger *zap.Logger) *Service {
	return &Service{entries: entries, listings: listings, tasks: tasks, sessions: sessions, files: files, bus: bus, logger: logger.Named("filebrowser")}
}

// Listings takes a result part's listing pieces into the snapshot as they
// come: each at its listing's time (when core took its first piece), and
// once a listing's last piece and every one before it are in, what a whole
// listing no longer has gone. A piece before its listing's first waits for
// it (a part resent late). Errors are logged: the part is kept already.
func (s *Service) Listings(ctx context.Context, t *models.MinionTask, session *models.MinionSession, pieces []protocol.ListingPiece, now time.Time) {
	if len(pieces) == 0 {
		return
	}
	machine := models.FsMachineKey(*session)
	ref := models.FsTaskRef{TaskID: t.TaskID, SessionID: session.SessionID, Number: session.Number, User: session.Hello.User}
	changed := map[string]bool{}
	for _, p := range pieces {
		folder, err := s.piece(ctx, t, machine, ref, p, now)
		if err != nil {
			s.logger.Warn("file browser: a listing piece", zap.String("task_id", t.TaskID.String()), zap.Uint64("listing", p.ID),
				zap.Uint64("part", p.Part), zap.Error(err))
			continue
		}
		if folder != "" {
			changed[folder] = true
		}
	}
	if s.bus == nil {
		return
	}
	for folder := range changed {
		s.bus.Publish(eventbus.NewFileBrowserChangedEvent(eventbus.FileBrowserEventPayload{
			OperationID: t.OperationID.String(), Machine: machine, Folder: folder}))
	}
}

// piece applies one piece: the folder it changed, "" for none yet.
func (s *Service) piece(ctx context.Context, t *models.MinionTask, machine string, ref models.FsTaskRef, p protocol.ListingPiece, now time.Time) (string, error) {
	op := t.OperationID
	l, err := s.listings.Piece(ctx, t.TaskID, p, now)
	if err != nil {
		return "", err
	}
	if l.Path == "" {
		// Its folder is named by its first piece, not here yet.
		return "", s.listings.Stash(ctx, t.TaskID, p)
	}
	folder := entryAt(l.Path, l.Sep)
	parts := []uint64{p.Part}
	if p.Part == 0 {
		if err := s.entries.Ensure(ctx, op, machine, ancestors(folder)); err != nil {
			return "", err
		}
		if l.Err != "" {
			if err := s.entries.ListingFailed(ctx, op, machine, folder, l.Err, l.At, ref); err != nil {
				return "", err
			}
		}
	}
	entries := p.Entries
	if p.Part == 0 {
		pending, err := s.listings.TakePending(ctx, t.TaskID, p.ID)
		if err != nil {
			return "", err
		}
		for _, q := range pending {
			entries = append(entries, q.Entries...)
			parts = append(parts, q.Part)
		}
	}
	if l.Err == "" {
		children := make([]models.FsEntry, 0, len(entries))
		for _, d := range entries {
			children = append(children, describedChild(folder, d))
		}
		if _, err := s.entries.ListingPiece(ctx, op, machine, folder, children, l.At, ref); err != nil {
			return "", err
		}
	}
	l, err = s.listings.Applied(ctx, t.TaskID, p.ID, parts)
	if err != nil {
		return "", err
	}
	// Whole once every piece is in, and only then: earlier ones applied
	// later would be taken for gone.
	if l.Complete() && !l.Partial && l.Err == "" {
		claimed, err := s.listings.ClaimWhole(ctx, t.TaskID, p.ID)
		if err != nil {
			return "", err
		}
		if claimed {
			if err := s.entries.ListingWhole(ctx, op, machine, folder, l.At, ref); err != nil {
				return "", err
			}
		}
	}
	return folder.Path, nil
}

// describedChild is a listing's entry in folder, as the snapshot keeps it.
func describedChild(folder models.FsEntry, d protocol.DirEntry) models.FsEntry {
	e := child(folder, d.Name)
	e.Type, e.Mode, e.Owner, e.Group, e.Target, e.Hidden = d.Type, d.Mode, d.Owner, d.Group, d.Target, d.Hidden
	if d.Size != nil {
		n := int64(*d.Size)
		e.Size = &n
	}
	e.Modified = d.Modified
	return e
}

// removed is the line BOB's rm (a delete_path) says what went with.
var removed = regexp.MustCompile(`(?m)^removed (.+) \((?:file|folder|link|other)\)$`)

// TaskEnded takes what a task that ended done says of its machine's files.
// It runs after the minion's exchange, so it neither slows nor fails it.
func (s *Service) TaskEnded(ctx context.Context, t *models.MinionTask) {
	if t.State != models.MinionTaskDone || t.DoneAt == nil {
		return
	}
	// A listing is taken as its pieces come (Listings), not here.
	switch {
	case t.Result == protocol.ResultFileDownload, t.Result == protocol.ResultFileStream,
		t.Result == protocol.ResultFileUpload, t.Action == protocol.ActionDeletePath, t.Result == protocol.ResultVolumes:
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
	var folders []string
	if t.Result == protocol.ResultVolumes {
		folders, err = s.volumes(ctx, t, machine, ref, at)
	} else {
		var folder string
		folder, err = s.take(ctx, t, machine, ref, at)
		if folder != "" {
			folders = []string{folder}
		}
	}
	if err != nil {
		s.logger.Warn("file browser: a task's result", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		return
	}
	if s.bus == nil {
		return
	}
	for _, folder := range folders {
		s.bus.Publish(eventbus.NewFileBrowserChangedEvent(eventbus.FileBrowserEventPayload{
			OperationID: t.OperationID.String(), Machine: machine, Folder: folder}))
	}
}

// volumes applies a volumes task: each volume's path (a drive's root, a
// mount point) a folder that holds it, the machine's other volumes gone.
// The folders it changed: each volume's path, and those whose volume went
// (a drive taken out).
func (s *Service) volumes(ctx context.Context, t *models.MinionTask, machine string, ref models.FsTaskRef, at time.Time) ([]string, error) {
	op := t.OperationID
	parts, err := s.tasks.Parts(ctx, t.TaskID)
	if err != nil {
		return nil, err
	}
	var entries, way []models.FsEntry
	var folders []string
	for _, p := range parts {
		if len(p.DataCBOR) == 0 {
			continue
		}
		v, err := protocol.ReadVolumes(p.Seq, p.DataCBOR)
		if err != nil {
			return nil, err
		}
		for _, vol := range v.Volumes {
			e := entryAt(vol.Path, v.Sep)
			e.Volume = volumeOf(vol, at, ref)
			entries = append(entries, e)
			way = append(way, ancestors(e)...)
			folders = append(folders, e.Path)
		}
	}
	if len(entries) == 0 {
		// None at all is no list of the machine's volumes: a minion that
		// sent nothing says nothing of the ones it had.
		return nil, nil
	}
	if err := s.entries.Ensure(ctx, op, machine, way); err != nil {
		return nil, err
	}
	went, err := s.entries.Volumes(ctx, op, machine, entries, at)
	if err != nil {
		return nil, err
	}
	return append(folders, went...), nil
}

// volumeOf is a volume as the snapshot keeps it.
func volumeOf(v protocol.Volume, at time.Time, ref models.FsTaskRef) *models.FsVolume {
	out := &models.FsVolume{Kind: v.Kind, Device: v.Device, Label: v.Label, FS: v.FS, ReadOnly: v.ReadOnly, NoMedia: v.NoMedia,
		SeenAt: at, SeenBy: ref}
	if v.Size != nil {
		n := int64(min(*v.Size, math.MaxInt64))
		out.Size = &n
	}
	if v.Free != nil {
		n := int64(min(*v.Free, math.MaxInt64))
		out.Free = &n
	}
	return out
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
	for _, p := range parts {
		if len(p.DataCBOR) == 0 {
			continue
		}
		var path string
		var tr models.FsTransfer
		list := models.FsDownloaded
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
			var kept *models.MinionFile
			if f, err := s.files.File(ctx, t.TaskID); err == nil {
				kept = f
			}
			list, tr.FileID = streamList(kept)
		case protocol.ResultFileUpload:
			d, err := protocol.ReadFileUpload(p.Seq, p.DataCBOR)
			if err != nil {
				return "", err
			}
			by := t.IssuedBy
			path, tr, list = d.Path, models.FsTransfer{Size: int64(d.Size), SHA256: d.SHA256, By: &by, Stream: t.Upload != nil && t.Upload.Stream}, models.FsUploaded
		}
		if path == "" {
			continue
		}
		tr.TaskID, tr.At = t.TaskID, at
		e := entryAt(path, sepOf(path))
		if err := s.entries.Ensure(ctx, op, machine, ancestors(e)); err != nil {
			return "", err
		}
		if err := s.entries.Transfer(ctx, op, machine, e, list, tr, ref); err != nil {
			return "", err
		}
		return e.Parent, nil
	}
	return "", nil
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

// streamList is where a stream's transfer goes: downloaded, with the file,
// when core kept its copy (all of it came: joining or complete; minionfiles,
// streams.go); streamed otherwise, a file that went to the downloader
// alone.
func streamList(kept *models.MinionFile) (string, *uuid.UUID) {
	if kept != nil && (kept.State == models.MinionFileJoining || kept.State == models.MinionFileComplete) {
		id := kept.FileID
		return models.FsDownloaded, &id
	}
	return models.FsStreamed, nil
}
