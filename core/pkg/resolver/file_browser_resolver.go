package resolver

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// The file browser's reads (docs: contracts/file-browser.md): asked for by
// a session, for anyone who can see its operation; the snapshot is the
// session's machine's (filebrowser.Service).

// TaskFileBrowser reads the snapshots (filebrowser.Service).
type TaskFileBrowser interface {
	Machine(ctx context.Context, session *models.MinionSession) (string, []models.MinionSession, error)
	Folder(ctx context.Context, op uuid.UUID, machine, path string) (*models.FsEntry, error)
	Children(ctx context.Context, op uuid.UUID, machine, path string, q repository.FsQuery) ([]models.FsEntry, string, error)
	Search(ctx context.Context, op uuid.UUID, machine, query string, showDeleted bool, after string, first int64) ([]models.FsEntry, string, error)
}

// machineOf is a session's machine, for a viewer of its operation.
func (r *minionTaskResolver) machineOf(ctx context.Context, sessionID string) (*models.MinionSession, string, []models.MinionSession, error) {
	if r.fileBrowser == nil {
		return nil, "", nil, fmt.Errorf("the file browser is not running")
	}
	s, err := r.session(ctx, sessionID, models.OperationRoleViewer)
	if err != nil {
		return nil, "", nil, err
	}
	key, sessions, err := r.fileBrowser.Machine(ctx, s)
	if err != nil {
		return nil, "", nil, fmt.Errorf("the session's machine: %w", err)
	}
	return s, key, sessions, nil
}

func (r *minionTaskResolver) FileBrowser(ctx context.Context, sessionID string) (*model.FsMachine, error) {
	_, key, sessions, err := r.machineOf(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := &model.FsMachine{Key: key, Sessions: make([]*model.FsMachineSession, 0, len(sessions))}
	now := time.Now().UTC()
	for i := range sessions {
		s := &sessions[i]
		state := "OPEN"
		if s.Closed(now) {
			state = "CLOSED"
		}
		out.Sessions = append(out.Sessions, &model.FsMachineSession{ID: s.SessionID.String(), Number: int(s.Number),
			Host: strPtr(s.Hello.Host), User: strPtr(s.Hello.User), State: state, LastSeenAt: taskTime(s.LastSeenAt)})
	}
	return out, nil
}

func (r *minionTaskResolver) FileBrowserEntries(ctx context.Context, sessionID string, path, after *string, first *int, search, sort *string,
	desc, showDeleted, foldersOnly *bool) (*model.FsEntryPage, error) {
	s, key, _, err := r.machineOf(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	q := repository.FsQuery{Sort: deref(sort), Desc: derefBool(desc), Search: deref(search), ShowDeleted: derefBool(showDeleted),
		FoldersOnly: derefBool(foldersOnly), After: deref(after)}
	if first != nil {
		q.First = int64(*first)
	}
	p := deref(path)
	entries, next, err := r.fileBrowser.Children(ctx, s.OperationID, key, p, q)
	if err != nil {
		return nil, err
	}
	out := &model.FsEntryPage{Entries: fsEntriesModel(entries), Next: strPtr(next)}
	if p != "" {
		folder, err := r.fileBrowser.Folder(ctx, s.OperationID, key, p)
		if err != nil {
			return nil, err
		}
		if folder != nil {
			out.Folder = fsEntryModel(*folder)
		}
	}
	return out, nil
}

func (r *minionTaskResolver) FileBrowserSearch(ctx context.Context, sessionID, query string, after *string, first *int, showDeleted *bool) (*model.FsEntryPage, error) {
	s, key, _, err := r.machineOf(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(query) < 2 {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "search for two characters at least")
	}
	n := int64(0)
	if first != nil {
		n = int64(*first)
	}
	entries, next, err := r.fileBrowser.Search(ctx, s.OperationID, key, query, derefBool(showDeleted), deref(after), n)
	if err != nil {
		return nil, err
	}
	return &model.FsEntryPage{Entries: fsEntriesModel(entries), Next: strPtr(next)}, nil
}

// MachineOfSession is a subscriber's session's operation and machine.
func (r *minionTaskResolver) MachineOfSession(ctx context.Context, sessionID string) (string, string, error) {
	s, key, _, err := r.machineOf(ctx, sessionID)
	if err != nil {
		return "", "", err
	}
	return s.OperationID.String(), key, nil
}

func derefBool(b *bool) bool { return b != nil && *b }

func fsEntriesModel(es []models.FsEntry) []*model.FsEntry {
	out := make([]*model.FsEntry, 0, len(es))
	for _, e := range es {
		out = append(out, fsEntryModel(e))
	}
	return out
}

func fsEntryModel(e models.FsEntry) *model.FsEntry {
	m := &model.FsEntry{Path: e.Path, Name: e.Name, Parent: strPtr(e.Parent), Sep: e.Sep, Type: e.Type, Mode: strPtr(e.Mode),
		Owner: strPtr(e.Owner), Group: strPtr(e.Group), Target: strPtr(e.Target), Hidden: e.Hidden, Known: e.Known,
		SeenAt: taskTime(e.SeenAt), SeenBy: fsRefModel(e.SeenBy), GoneAt: taskTime(e.GoneAt), GoneBy: fsRefModel(e.GoneBy),
		GoneHow: strPtr(e.GoneHow), ListedAt: taskTime(e.ListedAt), ListedBy: fsRefModel(e.ListedBy),
		Downloaded: fsTransfersModel(e.Downloaded), Uploaded: fsTransfersModel(e.Uploaded)}
	if e.Size != nil {
		n := int(*e.Size)
		m.Size = &n
	}
	if e.Modified != nil {
		n := int(*e.Modified)
		m.Modified = &n
	}
	return m
}

func fsRefModel(r *models.FsTaskRef) *model.FsTaskRef {
	if r == nil {
		return nil
	}
	return &model.FsTaskRef{TaskID: r.TaskID.String(), SessionID: r.SessionID.String(), Number: int(r.Number), User: strPtr(r.User)}
}

func fsTransfersModel(ts []models.FsTransfer) []*model.FsTransfer {
	out := make([]*model.FsTransfer, 0, len(ts))
	for _, t := range ts {
		m := &model.FsTransfer{TaskID: t.TaskID.String(), Size: int(t.Size), Stream: t.Stream, At: t.At.UTC().Format(time.RFC3339Nano)}
		if t.FileID != nil {
			id := t.FileID.String()
			m.FileID = &id
		}
		if len(t.SHA256) > 0 {
			h := hex.EncodeToString(t.SHA256)
			m.Sha256 = &h
		}
		if t.By != nil {
			by := t.By.String()
			m.By = &by
		}
		out = append(out, m)
	}
	return out
}
