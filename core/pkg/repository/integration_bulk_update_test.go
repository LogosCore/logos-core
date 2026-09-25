package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// BulkUpdate, against a real server: the sibling reorder writes every
// changed document through it in one round trip. It must apply each $set to
// its own document, and — like Update — only within the operation it is
// given, so an id smuggled in from another operation is not written.
func TestIntegrationWikiDocumentBulkUpdate(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewWikiDocumentRepository(db)
	clock := newSeedClock(time.Minute)

	opID, otherOp := uuid.New(), uuid.New()
	create := func(op uuid.UUID, title string) *models.WikiDocument {
		t.Helper()
		doc := &models.WikiDocument{
			DocumentID:   uuid.New(),
			OperationID:  op,
			Title:        title,
			SortOrder:    "M",
			DefaultField: createdAt(clock.next()),
		}
		if err := repo.Create(ctx, doc); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		return doc
	}
	folder := create(opID, "Folder")
	a, b := create(opID, "A"), create(opID, "B")
	foreign := create(otherOp, "Foreign")

	err := repo.BulkUpdate(ctx, opID, []DocumentUpdate{
		{DocumentID: a.DocumentID, Set: map[string]interface{}{"sort_order": "C"}},
		{DocumentID: b.DocumentID, Set: map[string]interface{}{"sort_order": "Q", "parent_document_id": folder.DocumentID}},
		{DocumentID: foreign.DocumentID, Set: map[string]interface{}{"sort_order": "Z"}},
	})
	if err != nil {
		t.Fatalf("BulkUpdate: %v", err)
	}

	got := func(id uuid.UUID) models.WikiDocument {
		t.Helper()
		d, err := repo.FindByID(ctx, id)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		return d
	}
	if d := got(a.DocumentID); d.SortOrder != "C" || d.ParentDocumentID != nil {
		t.Errorf("A = sort %q parent %v; want C at the root", d.SortOrder, d.ParentDocumentID)
	}
	if d := got(b.DocumentID); d.SortOrder != "Q" || d.ParentDocumentID == nil || *d.ParentDocumentID != folder.DocumentID {
		t.Errorf("B = sort %q parent %v; want Q under the folder", d.SortOrder, d.ParentDocumentID)
	}
	if d := got(foreign.DocumentID); d.SortOrder != "M" {
		t.Errorf("another operation's document was written: sort %q, want it untouched", d.SortOrder)
	}

	if err := repo.BulkUpdate(ctx, opID, nil); err != nil {
		t.Errorf("an empty BulkUpdate must be a no-op, got %v", err)
	}
}
