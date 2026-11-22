package repository

import (
	"slices"
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

// What a retention cleanup takes: saved recordings that ended before the
// age and hold something, their files kept, not converting under a lease
// running, their task not marked important (a task gone is not); the
// oldest first, of one operation or every one; summed by operation. The
// mark is taken once, and off again; a recording marked keeps the
// converter off it and is saved only by Save.
func TestIntegrationRecordingCleanups(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewRecordingRepository(db)
	tasks := NewMinionTaskRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	ago := func(days int) *time.Time { at := now.Add(-time.Duration(days) * 24 * time.Hour); return &at }
	op1, op2 := uuid.New(), uuid.New()
	plain, starred, other := uuid.New(), uuid.New(), uuid.New()
	for _, task := range []uuid.UUID{plain, starred, other} {
		op := op1
		if task == other {
			op = op2
		}
		if err := tasks.Insert(ctx, &models.MinionTask{TaskID: task, OperationID: op, SessionID: uuid.New(), Command: "rdp",
			State: models.MinionTaskDone, Rev: 1, QueuedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := tasks.SetImportant(ctx, starred, true, uuid.New(), now); err != nil {
		t.Fatal(err)
	}
	lease, lapsed := now.Add(time.Hour), now.Add(-time.Hour)
	recs := map[string]*models.Recording{
		"old":        {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(41), Size: 100},
		"video":      {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(40), Video: models.RecordingVideoConverted, VideoSize: 50},
		"lapsed":     {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(39), Size: 10, Video: models.RecordingVideoConverting, VideoLease: &lapsed},
		"op2":        {OperationID: op2, TaskID: other, State: models.RecordingStateSaved, EndedAt: ago(50), Size: 400},
		"task gone":  {OperationID: op2, TaskID: uuid.New(), State: models.RecordingStateSaved, EndedAt: ago(49), Size: 1},
		"important":  {OperationID: op1, TaskID: starred, State: models.RecordingStateSaved, EndedAt: ago(90), Size: 999},
		"recent":     {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(10), Size: 999},
		"recording":  {OperationID: op1, TaskID: plain, State: models.RecordingStateRecording, Size: 999},
		"failed":     {OperationID: op1, TaskID: plain, State: models.RecordingStateFailed, EndedAt: ago(40)},
		"nothing":    {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(40)},
		"converting": {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(40), Size: 999, Video: models.RecordingVideoConverting, VideoLease: &lease},
		"cleaned":    {OperationID: op1, TaskID: plain, State: models.RecordingStateSaved, EndedAt: ago(45), Size: 999, CleanedAt: ago(1)},
	}
	names := map[uuid.UUID]string{}
	for name, rec := range recs {
		rec.RecordingID, rec.Front, rec.StartedAt, rec.SeenAt = uuid.New(), "rdp", now.Add(-100*24*time.Hour), now
		names[rec.RecordingID] = name
		if err := repo.Insert(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	named := func(got []models.Recording) []string {
		out := []string{}
		for _, r := range got {
			out = append(out, names[r.RecordingID])
		}
		return out
	}
	before := now.Add(-30 * 24 * time.Hour)
	got, err := repo.ToClean(ctx, before, now, nil, 100)
	if want := []string{"op2", "task gone", "old", "video", "lapsed"}; err != nil || !slices.Equal(named(got), want) {
		t.Fatalf("to clean %v %v, want %v", named(got), err, want)
	}
	if got, _ := repo.ToClean(ctx, before, now, &op1, 100); !slices.Equal(named(got), []string{"old", "video", "lapsed"}) {
		t.Errorf("of op1 %v", named(got))
	}
	if got, _ := repo.ToClean(ctx, before, now, nil, 2); !slices.Equal(named(got), []string{"op2", "task gone"}) {
		t.Errorf("two %v", named(got))
	}
	sums, err := repo.ToCleanByOperation(ctx, before, now)
	byOp := map[uuid.UUID][2]int64{}
	for _, s := range sums {
		byOp[s.OperationID] = [2]int64{s.Recordings, s.Bytes}
	}
	if err != nil || len(byOp) != 2 || byOp[op1] != [2]int64{3, 160} || byOp[op2] != [2]int64{2, 401} {
		t.Fatalf("by operation %v %v", byOp, err)
	}

	cleanup := uuid.New()
	marked, err := repo.MarkCleaned(ctx, recs["old"].RecordingID, cleanup, now)
	if err != nil || marked == nil || marked.CleanedAt == nil || !marked.CleanedAt.Equal(now) || *marked.CleanedBy != cleanup || marked.Size != 100 {
		t.Fatalf("marked %+v %v", marked, err)
	}
	for _, name := range []string{"old", "converting", "recording", "cleaned"} {
		if again, err := repo.MarkCleaned(ctx, recs[name].RecordingID, uuid.New(), now); again != nil || err != nil {
			t.Errorf("%s marked: %+v %v", name, again, err)
		}
	}
	if again, err := repo.MarkCleaned(ctx, uuid.New(), cleanup, now); again != nil || err != nil {
		t.Errorf("one gone marked: %+v %v", again, err)
	}
	if got, _ := repo.ToClean(ctx, before, now, &op1, 100); !slices.Equal(named(got), []string{"video", "lapsed"}) {
		t.Errorf("after the mark %v", named(got))
	}
	if err := repo.SaveKept(ctx, recs["old"].RecordingID, bson.M{"video": models.RecordingVideoConverted}); !IsNotFound(err) {
		t.Errorf("saved kept, one marked: %v", err)
	}
	if err := repo.SaveKept(ctx, recs["recent"].RecordingID, bson.M{"duration_ms": int64(5)}); err != nil {
		t.Errorf("saved kept, one not marked: %v", err)
	}
	if err := repo.UnmarkCleaned(ctx, recs["old"].RecordingID); err != nil {
		t.Fatal(err)
	}
	if back, _ := repo.FindByID(ctx, recs["old"].RecordingID); back.CleanedAt != nil || back.CleanedBy != nil {
		t.Errorf("unmarked %+v", back)
	}
	if got, _ := repo.ToClean(ctx, before, now, &op1, 100); !slices.Equal(named(got), []string{"old", "video", "lapsed"}) {
		t.Errorf("after the mark came off %v", named(got))
	}
	if kept, _ := repo.ListByOperation(ctx, op1); len(kept) != 10 {
		t.Errorf("by operation: %d", len(kept))
	} else {
		for _, r := range kept {
			if (names[r.RecordingID] == "cleaned") != (r.CleanedAt != nil) {
				t.Errorf("by operation, %s: cleaned at %v", names[r.RecordingID], r.CleanedAt)
			}
		}
	}

	// The converter passes over one marked.
	if err := repo.DeleteByOperationID(ctx, op1); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByOperationID(ctx, op2); err != nil {
		t.Fatal(err)
	}
	waiting := &models.Recording{RecordingID: uuid.New(), OperationID: op1, TaskID: plain, Front: "rdp", State: models.RecordingStateSaved,
		EndedAt: ago(40), Size: 5, StartedAt: now, SeenAt: now, CleanedAt: ago(1)}
	if err := repo.Insert(ctx, waiting); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimToConvert(ctx, now, now.Add(time.Hour)); claimed != nil || err != nil {
		t.Fatalf("claimed one marked: %+v %v", claimed, err)
	}
	if err := repo.UnmarkCleaned(ctx, waiting.RecordingID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimToConvert(ctx, now, now.Add(time.Hour)); err != nil || claimed == nil || claimed.RecordingID != waiting.RecordingID {
		t.Fatalf("claimed %+v %v", claimed, err)
	}
}
