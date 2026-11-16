package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// A desktop task's recordings: counted on the task (kept, made, its rev
// bumped each time), listed the latest first a page at a time, their
// numbers saved as they go, the stale ones found and ended once, deleted
// one by one or with their operation.
func TestIntegrationRecordings(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewRecordingRepository(db)
	tasks := NewMinionTaskRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	op, task := uuid.New(), uuid.New()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	if err := tasks.Insert(ctx, &models.MinionTask{TaskID: task, OperationID: op, SessionID: uuid.New(), Command: "rdp",
		State: models.MinionTaskRunning, Rev: 1, QueuedAt: t0}); err != nil {
		t.Fatal(err)
	}
	for i, step := range []struct{ kept, made, wantKept, wantMade, wantRev int }{
		{1, 1, 1, 1, 2}, {1, 1, 2, 2, 3}, {-1, 0, 1, 2, 4}, {0, 0, 1, 2, 5},
	} {
		after, err := tasks.AddRecording(ctx, task, step.kept, step.made)
		if err != nil || after.Recordings != step.wantKept || after.RecordingsMade != int64(step.wantMade) || after.Rev != int64(step.wantRev) {
			t.Fatalf("step %d: %+v %v", i, after, err)
		}
	}
	if _, err := tasks.AddRecording(ctx, uuid.New(), 1, 1); !IsNotFound(err) {
		t.Errorf("a task gone: %v", err)
	}

	ids := make([]uuid.UUID, 4)
	for n := 1; n <= 3; n++ {
		ids[n] = uuid.New()
		rec := &models.Recording{RecordingID: ids[n], OperationID: op, TaskID: task, Number: int64(n), Front: "rdp",
			Settings: models.JSONMap{"quality": "best", "view_only": true}, State: models.RecordingStateSaved, StartedAt: t0, SeenAt: t0}
		if n == 3 {
			rec.State, rec.SeenAt = models.RecordingStateRecording, t0.Add(-5*time.Minute)
		}
		if err := repo.Insert(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	other := &models.Recording{RecordingID: uuid.New(), OperationID: uuid.New(), TaskID: uuid.New(), Number: 1,
		State: models.RecordingStateRecording, SeenAt: t0.Add(-time.Hour)}
	if err := repo.Insert(ctx, other); err != nil {
		t.Fatal(err)
	}
	numbers := func(recs []models.Recording) []int64 {
		out := []int64{}
		for _, r := range recs {
			out = append(out, r.Number)
		}
		return out
	}
	if page, err := repo.ListByTask(ctx, task, 0, 2); err != nil || len(page) != 2 || page[0].Number != 3 || page[1].Number != 2 {
		t.Errorf("first page %v %v", numbers(page), err)
	}
	if page, err := repo.ListByTask(ctx, task, 2, 2); err != nil || len(page) != 1 || page[0].Number != 1 {
		t.Errorf("last page %v %v", numbers(page), err)
	}
	if got, err := repo.FindByID(ctx, ids[1]); err != nil || got.Settings["quality"] != "best" || got.Settings["view_only"] != true {
		t.Errorf("settings %+v %v", got, err)
	}

	if err := repo.Save(ctx, ids[3], bson.M{"parts": 2, "size": int64(10), "duration_ms": int64(400)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.FindByID(ctx, ids[3]); got.Parts != 2 || got.Size != 10 || got.DurationMs != 400 {
		t.Errorf("saved %+v", got)
	}
	if err := repo.Save(ctx, uuid.New(), bson.M{"parts": 1}); !IsNotFound(err) {
		t.Errorf("save one gone: %v", err)
	}

	before := t0.Add(-2 * time.Minute)
	stale, err := repo.Stale(ctx, before)
	if err != nil || len(stale) != 2 {
		t.Fatalf("stale %v %v", numbers(stale), err)
	}
	end := bson.M{"state": models.RecordingStateSaved, "end": models.RecordingEndCoreStopped}
	if ok, err := repo.EndStale(ctx, ids[3], before, end); !ok || err != nil {
		t.Errorf("end stale: %v %v", ok, err)
	}
	if ok, err := repo.EndStale(ctx, ids[3], before, end); ok || err != nil {
		t.Errorf("ended twice: %v %v", ok, err)
	}
	if got, _ := repo.FindByID(ctx, ids[3]); got.State != models.RecordingStateSaved || got.End != models.RecordingEndCoreStopped || got.Parts != 2 {
		t.Errorf("ended %+v", got)
	}

	if recs, err := repo.ListByOperation(ctx, op); err != nil || len(recs) != 3 || recs[0].RecordingID == uuid.Nil {
		t.Errorf("by operation %v %v", recs, err)
	}
	if err := repo.Delete(ctx, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, ids[1]); err != nil {
		t.Errorf("delete twice: %v", err)
	}
	if err := repo.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if page, _ := repo.ListByTask(ctx, task, 0, 10); len(page) != 0 {
		t.Errorf("left %v", numbers(page))
	}
	if _, err := repo.FindByID(ctx, other.RecordingID); err != nil {
		t.Errorf("another operation's: %v", err)
	}
}
