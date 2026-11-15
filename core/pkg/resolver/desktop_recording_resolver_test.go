package resolver

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// oneTask is the task repository with one task.
type oneTask struct {
	repository.IMinionTaskRepository
	task models.MinionTask
}

func (o oneTask) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	if id != o.task.TaskID {
		return nil, repository.ErrNotFound
	}
	t := o.task
	return &t, nil
}

// oneSession is the session repository with one session.
type oneSession struct {
	repository.IMinionSessionRepository
	session models.MinionSession
}

func (o oneSession) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionSession, error) {
	if id != o.session.SessionID {
		return nil, repository.ErrNotFound
	}
	s := o.session
	return &s, nil
}

// fakeRecordings are a task's recordings, in memory.
type fakeRecordings struct {
	recs    map[uuid.UUID]models.DesktopRecording
	deleted []uuid.UUID
}

func (f *fakeRecordings) List(_ context.Context, taskID uuid.UUID, before, limit int64) ([]models.DesktopRecording, error) {
	var out []models.DesktopRecording
	for _, r := range f.recs {
		if r.TaskID == taskID && (before == 0 || r.Number < before) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	return out[:min(int64(len(out)), limit)], nil
}

func (f *fakeRecordings) Recording(_ context.Context, id uuid.UUID) (*models.DesktopRecording, error) {
	r, ok := f.recs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &r, nil
}

func (f *fakeRecordings) Delete(_ context.Context, id uuid.UUID) (*models.DesktopRecording, error) {
	r := f.recs[id]
	if r.State == models.DesktopRecordingRecording {
		return &r, recordings.ErrRecording
	}
	delete(f.recs, id)
	f.deleted = append(f.deleted, id)
	return &r, nil
}

// A desktop task's recordings, a page at a time, the latest first, for a
// viewer; deleted by an operator alone, and one recording not at all.
func TestDesktopRecordings(t *testing.T) {
	operator, viewer := uuid.New(), uuid.New()
	op := models.Operation{OperationID: uuid.New(), Members: []models.OperationMember{
		{UserID: operator, Role: models.OperationRoleOperator}, {UserID: viewer, Role: models.OperationRoleViewer}}}
	session := models.MinionSession{SessionID: uuid.New(), OperationID: op.OperationID}
	task := models.MinionTask{TaskID: uuid.New(), SessionID: session.SessionID, OperationID: op.OperationID}
	recs := &fakeRecordings{recs: map[uuid.UUID]models.DesktopRecording{}}
	ids := make([]uuid.UUID, 4)
	started := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	for n := 1; n <= 3; n++ {
		id := uuid.New()
		ids[n] = id
		rec := models.DesktopRecording{RecordingID: id, TaskID: task.TaskID, SessionID: session.SessionID, OperationID: op.OperationID,
			Number: int64(n), Front: "rdp", Username: "ann", State: models.DesktopRecordingSaved, StartedAt: started, Size: 100,
			End: models.DesktopRecordingEndClosed, Settings: models.JSONMap{"quality": "best"}}
		if n == 3 {
			rec.State, rec.End = models.DesktopRecordingRecording, ""
		}
		recs.recs[id] = rec
	}
	r := &minionTaskResolver{tasks: oneTask{task: task}, sessions: oneSession{session: session},
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) { return op, nil }},
		recordings:    recs}
	as := func(user uuid.UUID) context.Context {
		return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: user.String(), Roles: []string{"user"}})
	}

	two := 2
	page, err := r.DesktopRecordings(as(viewer), task.TaskID.String(), &two, nil)
	if err != nil || len(page.Recordings) != 2 || page.Recordings[0].Number != 3 || page.Recordings[1].Number != 2 ||
		page.Before == nil || *page.Before != "2" {
		t.Fatalf("first page %+v %v", page, err)
	}
	if got := page.Recordings[1]; got.Path != "/desktop-recordings/"+ids[2].String() || got.End == nil || *got.End != "closed" ||
		got.StartedAt != "2026-11-15T09:00:00Z" || got.Settings.(map[string]any)["quality"] != "best" || page.Recordings[0].End != nil {
		t.Errorf("recording %+v", got)
	}
	page, err = r.DesktopRecordings(as(viewer), task.TaskID.String(), &two, page.Before)
	if err != nil || len(page.Recordings) != 1 || page.Recordings[0].Number != 1 || page.Before != nil {
		t.Fatalf("last page %+v %v", page, err)
	}
	bad := "x"
	if _, err := r.DesktopRecordings(as(viewer), task.TaskID.String(), nil, &bad); err == nil {
		t.Error("a bad cursor")
	}
	if _, err := r.DesktopRecordings(as(uuid.New()), task.TaskID.String(), nil, nil); err == nil {
		t.Error("listed for one not in the operation")
	}

	if _, err := r.DeleteDesktopRecording(as(viewer), ids[1].String()); err == nil {
		t.Error("a viewer deleted one")
	}
	if _, err := r.DeleteDesktopRecording(as(operator), ids[3].String()); err == nil || !strings.Contains(err.Error(), "still recording") {
		t.Errorf("deleted one recording: %v", err)
	}
	if ok, err := r.DeleteDesktopRecording(as(operator), ids[1].String()); !ok || err != nil || len(recs.deleted) != 1 || recs.deleted[0] != ids[1] {
		t.Errorf("delete: %v %v %v", ok, err, recs.deleted)
	}
	if _, err := r.DeleteDesktopRecording(as(operator), ids[1].String()); err == nil {
		t.Error("deleted twice")
	}
}
