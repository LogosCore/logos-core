package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"go.mongodb.org/mongo-driver/bson"
)

// Core's records of what it keeps in the object store, summed by purpose,
// operation and kind: a file kept, or coming (its bytes so far); a
// recording's video once converted, else its parts; a build's files and an
// artifact not deleted; the wiki's attachments not deleted; every skill
// version. What holds no object is left out: a minion file that failed, a
// recording a retention cleanup took, an artifact an operator deleted, an
// attachment deleted, a build with none.
func TestIntegrationStorageUsage(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	opA, opB := uuid.New(), uuid.New()
	now := time.Now().UTC()
	insert := func(collection string, docs ...any) {
		for _, d := range docs {
			if _, err := db.Collection(collection).InsertOne(ctx, d); err != nil {
				t.Fatalf("%s: %v", collection, err)
			}
		}
	}
	file := func(op uuid.UUID, kind, state string, received int64) *models.MinionFile {
		return &models.MinionFile{FileID: uuid.New(), OperationID: op, SessionID: uuid.New(), Kind: kind, State: state,
			Total: received, Received: received, Xfer: uuid.New().NodeID()}
	}
	coming := file(opA, "", models.MinionFileReceiving, 30)
	coming.Total = 1000
	insert(minionFileCollection,
		file(opA, "", models.MinionFileComplete, 100), coming,
		file(opA, models.MinionFileKindScreenshot, models.MinionFileComplete, 50),
		file(opB, models.MinionFileKindTask, models.MinionFileComplete, 7),
		file(opA, "", models.MinionFileFailed, 999), file(opA, "", models.MinionFileCancelled, 999))
	insert(recordingCollection,
		&models.Recording{RecordingID: uuid.New(), OperationID: opB, Front: "rdp", Size: 500},
		&models.Recording{RecordingID: uuid.New(), OperationID: opB, Front: "vnc", Size: 900, Video: "done", VideoSize: 200},
		&models.Recording{RecordingID: uuid.New(), OperationID: opB, Front: "terminal", Size: 40},
		&models.Recording{RecordingID: uuid.New(), OperationID: opB, Front: "rdp", Size: 9999, CleanedAt: &now})
	insert(buildFileCollection, &models.BuildFile{FileID: uuid.New(), OperationID: opA, Size: 11})
	insert(buildCollection,
		&models.Build{BuildID: uuid.New(), OperationID: opA, Artifact: &models.BuildArtifact{Size: 1000, ObjectKey: "artifacts/a"}},
		&models.Build{BuildID: uuid.New(), OperationID: opA, Artifact: &models.BuildArtifact{Size: 999, ObjectKey: "artifacts/b", Deleted: true}},
		&models.Build{BuildID: uuid.New(), OperationID: opA})
	insert(wikiImageCollection,
		&models.WikiImage{ImageID: uuid.New(), OperationID: opB, SizeBytes: 5},
		&models.WikiImage{ImageID: uuid.New(), OperationID: opB, SizeBytes: 6, DeletedAt: &now})
	insert(wikiFileCollection, &models.WikiFile{FileID: uuid.New(), OperationID: opB, SizeBytes: 8})
	insert(skillVersionCollection,
		&models.SkillVersion{SkillID: uuid.New(), SizeBytes: 70, ObjectKey: "skills/a"},
		&models.SkillVersion{SkillID: uuid.New(), SizeBytes: 30, ObjectKey: "skills/b"})

	rows, err := NewStorageUsageRepository(db).Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]int64{}
	for _, r := range rows {
		op := "none"
		switch r.OperationID {
		case opA:
			op = "A"
		case opB:
			op = "B"
		}
		got[fmt.Sprintf("%s/%s/%s", r.Purpose, r.Kind, op)] = [2]int64{r.Objects, r.Bytes}
	}
	want := map[string][2]int64{
		"minion_files/download/A":   {2, 130},
		"minion_files/screenshot/A": {1, 50},
		"minion_files/upload/B":     {1, 7},
		"recordings/desktop/B":      {2, 700},
		"recordings/terminal/B":     {1, 40},
		"builds/file/A":             {1, 11},
		"builds/artifact/A":         {1, 1000},
		"wiki_images/image/B":       {1, 5},
		"wiki_files/file/B":         {1, 8},
		"skills/version/none":       {2, 100},
	}
	if len(got) != len(want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %v, want %v (all: %v)", k, got[k], w, got)
		}
	}
}

// One action of a kind for a bucket runs at a time; ended, it may run again.
// Those a stopped core left running end failed; the list is the latest
// first, a page at a time.
func TestIntegrationStorageActions(t *testing.T) {
	db := integrationDB(t)
	repo := NewStorageActionRepository(db)
	ctx := context.Background()
	if err := db.IndexSetupErr(); err != nil {
		t.Fatal(err)
	}
	mk := func(action, bucket string) *models.StorageAction {
		return &models.StorageAction{ActionID: uuid.New(), Action: action, Bucket: bucket, Status: models.StorageActionRunning,
			Actor: models.ModuleActor{Type: "user", ID: "u", Name: "alice"}, StartedAt: time.Now().UTC()}
	}
	reclaim := mk(models.StorageActionReclaim, "")
	if err := repo.Insert(ctx, reclaim); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, mk(models.StorageActionReclaim, "")); !errors.Is(err, ErrStorageActionRunning) {
		t.Fatalf("a second reclaim while one runs: %v", err)
	}
	growA, growB := mk(models.StorageActionGrow, "a"), mk(models.StorageActionGrow, "b")
	if err := repo.Insert(ctx, growA); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, growB); err != nil {
		t.Fatalf("a grow of another bucket: %v", err)
	}
	ended, err := repo.Finish(ctx, reclaim.ActionID, models.StorageActionSucceeded, "", 42, 0, time.Now().UTC())
	if err != nil || ended.Status != models.StorageActionSucceeded || ended.Freed != 42 || ended.FinishedAt == nil {
		t.Fatalf("finished %+v %v", ended, err)
	}
	if _, err := repo.Finish(ctx, reclaim.ActionID, models.StorageActionFailed, "again", 0, 0, time.Now().UTC()); !IsNotFound(err) {
		t.Fatalf("an action ended twice: %v", err)
	}
	if err := repo.Insert(ctx, mk(models.StorageActionReclaim, "")); err != nil {
		t.Fatalf("a reclaim after the last ended: %v", err)
	}
	n, err := repo.EndRunning(ctx, "core stopped before it ended", time.Now().UTC())
	if err != nil || n != 3 {
		t.Fatalf("ended %d running: %v", n, err)
	}

	page, err := repo.FindWithCursor(ctx, nil, 3, true)
	if err != nil || len(page) != 3 || page[0].Action != models.StorageActionReclaim || page[0].Status != models.StorageActionFailed ||
		page[0].Err != "core stopped before it ended" {
		t.Fatalf("first page %+v %v", page, err)
	}
	c, _ := pagination.DecodeCursor(StorageActionCursor(&page[2]))
	rest, err := repo.FindWithCursor(ctx, &c, 3, true)
	if err != nil || len(rest) != 1 || rest[0].ActionID != reclaim.ActionID {
		t.Fatalf("the page after %+v %v", rest, err)
	}
	if total, err := repo.Count(ctx); err != nil || total != 4 {
		t.Fatalf("count %d %v", total, err)
	}
}

// A cleanup's progress is kept while it runs; the latest cleanup an actor
// ran is found by its actor, not another's.
func TestIntegrationStorageCleanupActions(t *testing.T) {
	db := integrationDB(t)
	repo := NewStorageActionRepository(db)
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	cleanup := func(actor models.ModuleActor, at time.Time) *models.StorageAction {
		a := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionCleanup, Bucket: "desktop-recordings", Actor: actor,
			Status: models.StorageActionRunning, StartedAt: at, OlderThanDays: 30}
		if err := repo.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	if last, err := repo.Last(ctx, models.StorageActionCleanup, models.RetentionActor); last != nil || err != nil {
		t.Fatalf("none yet: %+v %v", last, err)
	}
	first := cleanup(models.RetentionActor, t0)
	if err := repo.Progress(ctx, first.ActionID, 7, 700); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Finish(ctx, first.ActionID, models.StorageActionSucceeded, "", 0, 0, t0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Progress(ctx, first.ActionID, 8, 800); !IsNotFound(err) {
		t.Errorf("progress of one ended: %v", err)
	}
	alice := cleanup(models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}, t0.Add(time.Hour))
	last, err := repo.Last(ctx, models.StorageActionCleanup, models.RetentionActor)
	if err != nil || last == nil || last.ActionID != first.ActionID || last.Recordings != 7 || last.Bytes != 700 || last.OlderThanDays != 30 {
		t.Fatalf("the policy's last %+v %v", last, err)
	}
	if last, _ := repo.Last(ctx, models.StorageActionCleanup, alice.Actor); last == nil || last.ActionID != alice.ActionID {
		t.Fatalf("alice's last %+v", last)
	}
	if last, _ := repo.Last(ctx, models.StorageActionReclaim, models.RetentionActor); last != nil {
		t.Fatalf("a reclaim by the policy %+v", last)
	}
}

// The retention policy: off at 30 days before anyone set it; set, it reads
// back as set; set again, it is replaced.
func TestIntegrationStorageRetention(t *testing.T) {
	db := integrationDB(t)
	repo := NewStorageRetentionRepository(db)
	ctx := context.Background()
	if err := db.IndexSetupErr(); err != nil {
		t.Fatal(err)
	}
	if p, err := repo.Get(ctx); err != nil || p.Enabled || p.OlderThanDays != models.DefaultRetentionDays || p.UpdatedAt != nil {
		t.Fatalf("before anyone set it %+v %v", p, err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	by := models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}
	if err := repo.Set(ctx, models.StorageRetention{Enabled: true, OlderThanDays: 45, UpdatedAt: &at, UpdatedBy: &by}); err != nil {
		t.Fatal(err)
	}
	if p, err := repo.Get(ctx); err != nil || !p.Enabled || p.OlderThanDays != 45 || !p.UpdatedAt.Equal(at) || *p.UpdatedBy != by {
		t.Fatalf("set %+v %v", p, err)
	}
	if err := repo.Set(ctx, models.StorageRetention{OlderThanDays: 60, UpdatedAt: &at, UpdatedBy: &by}); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.Get(ctx); p.Enabled || p.OlderThanDays != 60 {
		t.Fatalf("set again %+v", p)
	}
	if n, err := db.Collection(storageRetentionCollection).Find(ctx, bson.M{}).Count(); err != nil || n != 1 {
		t.Fatalf("%d documents %v", n, err)
	}
}
