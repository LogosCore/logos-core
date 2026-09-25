package repository

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// The repository half of emptying a wiki's trash, against a real server:
// name the trashed documents, delete their backups, drop their ids from task
// references, and hard-delete exactly those documents. The sweep used to
// delete every trashed document in the operation — including ones trashed
// after it had collected which backups to remove.
func TestIntegrationWikiTrashSweep(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	docs := NewWikiDocumentRepository(db)
	backups := NewWikiDocumentBackupRepository(db)
	tasks := NewTaskRepository(db)
	clock := newSeedClock(time.Minute)

	opID := uuid.New()
	create := func(op uuid.UUID, title string, trashed bool) *models.WikiDocument {
		t.Helper()
		doc := &models.WikiDocument{
			DocumentID:   uuid.New(),
			OperationID:  op,
			Title:        title,
			DefaultField: createdAt(clock.next()),
		}
		if err := docs.Create(ctx, doc); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		if trashed {
			if err := docs.SoftDelete(ctx, doc, uuid.New()); err != nil {
				t.Fatalf("trash %q: %v", title, err)
			}
		}
		if err := backups.Create(ctx, &models.WikiDocumentBackup{
			BackupID: uuid.New(), DocumentID: doc.DocumentID, OperationID: op, Title: title,
		}); err != nil {
			t.Fatalf("backup %q: %v", title, err)
		}
		return doc
	}
	gone1, gone2 := create(opID, "Gone 1", true), create(opID, "Gone 2", true)
	live := create(opID, "Live", false)
	lateTrash := create(opID, "Trashed after the ids were read", false)
	foreign := create(uuid.New(), "Another operation's trash", true)

	task := &models.Task{
		TaskID:         uuid.New(),
		OperationID:    opID,
		Name:           "Refers to everything",
		WikiReferences: []uuid.UUID{gone1.DocumentID, live.DocumentID, gone2.DocumentID},
	}
	if err := tasks.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	ids, err := docs.FindTrashedIDs(ctx, opID)
	if err != nil {
		t.Fatalf("FindTrashedIDs: %v", err)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	want := []uuid.UUID{gone1.DocumentID, gone2.DocumentID}
	slices.SortFunc(want, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	if !slices.Equal(ids, want) {
		t.Fatalf("FindTrashedIDs = %v, want the operation's two trashed documents %v", ids, want)
	}

	// Trashed after the ids were read: not in this sweep, so it must survive.
	if err := docs.SoftDelete(ctx, lateTrash, uuid.New()); err != nil {
		t.Fatalf("late trash: %v", err)
	}

	if err := backups.DeleteByDocumentIDs(ctx, ids); err != nil {
		t.Fatalf("DeleteByDocumentIDs: %v", err)
	}
	if err := tasks.PullWikiReferences(ctx, opID, ids); err != nil {
		t.Fatalf("PullWikiReferences: %v", err)
	}
	// Two ids the delete must refuse: an active document, as one restored
	// mid-sweep would be, and another operation's trashed document.
	if err := docs.HardDeleteTrashedByIDs(ctx, opID, append(ids, live.DocumentID, foreign.DocumentID)); err != nil {
		t.Fatalf("HardDeleteTrashedByIDs: %v", err)
	}

	for _, d := range []*models.WikiDocument{gone1, gone2} {
		if _, err := docs.FindByID(ctx, d.DocumentID); !IsNotFound(err) {
			t.Errorf("%q survived the sweep (err %v)", d.Title, err)
		}
		if n := countBackups(t, backups, d.DocumentID); n != 0 {
			t.Errorf("%q still has %d backups", d.Title, n)
		}
	}
	for _, d := range []*models.WikiDocument{live, lateTrash, foreign} {
		if _, err := docs.FindByID(ctx, d.DocumentID); err != nil {
			t.Errorf("%q was deleted, but it was not in the sweep's trash: %v", d.Title, err)
		}
		if n := countBackups(t, backups, d.DocumentID); n != 1 {
			t.Errorf("%q has %d backups, want its 1 untouched", d.Title, n)
		}
	}

	got, err := tasks.FindByID(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if !slices.Equal(got.WikiReferences, []uuid.UUID{live.DocumentID}) {
		t.Errorf("task references = %v, want only the live document %s", got.WikiReferences, live.DocumentID)
	}
}

func countBackups(t *testing.T, backups IWikiDocumentBackupRepository, docID uuid.UUID) int {
	t.Helper()
	rows, err := backups.FindByDocumentIDWithCursor(testCtx(t), docID, nil, nil, 10, true)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	return len(rows)
}
