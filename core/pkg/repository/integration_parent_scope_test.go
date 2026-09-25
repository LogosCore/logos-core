package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// Lookups by parent_document_id, against a real server.
//
// Every index on parent_document_id leads with operation_id, so a query that
// filters on the parent alone cannot use one: Mongo answers it by scanning the
// whole collection, across every operation, and returns the right rows — just
// slower as the wiki grows. The child count behind every sidebar expand ran
// that way. Nothing fails, so this test asks the profiler what the queries
// actually did, and separately checks that the operation really scopes them.
func TestIntegrationWikiParentLookupsAreOperationScoped(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewWikiDocumentRepository(db)
	opID := uuid.New()
	clock := newSeedClock(time.Minute)

	create := func(title string, parent *models.WikiDocument) *models.WikiDocument {
		t.Helper()
		doc := &models.WikiDocument{
			DocumentID:   uuid.New(),
			OperationID:  opID,
			Title:        title,
			DefaultField: createdAt(clock.next()),
		}
		if parent != nil {
			doc.ParentDocumentID = &parent.DocumentID
		}
		if err := repo.Create(ctx, doc); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		return doc
	}
	root := create("Root", nil)
	child := create("Child", root)
	create("Leaf", child)
	trashed := create("Trashed", root)
	if err := repo.SoftDelete(ctx, trashed, uuid.New()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	raw, err := db.Collection(wikiDocumentCollection).(*database.QmgoCollection).RawCollection()
	if err != nil {
		t.Fatalf("raw collection: %v", err)
	}
	mdb := raw.Database()
	if err := mdb.RunCommand(ctx, bson.D{{Key: "profile", Value: 2}}).Err(); err != nil {
		t.Skipf("cannot enable the profiler on the scratch database (%v); needs dbAdmin", err)
	}
	t.Cleanup(func() { _ = mdb.RunCommand(ctx, bson.D{{Key: "profile", Value: 0}}).Err() })

	// Results first: the operation scoping must not change what callers get.
	children, err := repo.FindChildDocuments(ctx, opID, root.DocumentID)
	if err != nil || len(children) != 1 || children[0].DocumentID != child.DocumentID {
		t.Errorf("FindChildDocuments(root) = %d docs, %v; want just the active child", len(children), err)
	}
	if n, err := repo.CountChildDocuments(ctx, opID, root.DocumentID); err != nil || n != 1 {
		t.Errorf("CountChildDocuments(root) = %d, %v; want 1 (the trashed child is not counted)", n, err)
	}
	_, counts, err := repo.FindChildDocumentsWithCounts(ctx, opID, &root.DocumentID)
	if err != nil || counts[child.DocumentID] != 1 {
		t.Errorf("FindChildDocumentsWithCounts(root) counts = %v, %v; want the child's leaf counted", counts, err)
	}
	if _, _, err := repo.FindDocumentsForRevealPath(ctx, opID, []uuid.UUID{root.DocumentID, child.DocumentID}); err != nil {
		t.Errorf("FindDocumentsForRevealPath: %v", err)
	}
	if descendants, err := repo.FindDescendants(ctx, opID, root.DocumentID); err != nil || len(descendants) != 2 {
		t.Errorf("FindDescendants(root) = %d docs, %v; want 2 (child, leaf)", len(descendants), err)
	}
	if trash, err := repo.FindTrashedDescendants(ctx, opID, root.DocumentID); err != nil || len(trash) != 1 {
		t.Errorf("FindTrashedDescendants(root) = %d docs, %v; want 1", len(trash), err)
	}
	if err := repo.RebuildPathIDsCascade(ctx, root.DocumentID); err != nil {
		t.Errorf("RebuildPathIDsCascade: %v", err)
	}

	// The operation is part of the lookup, not decoration: another
	// operation's id must find nothing under the same parent.
	otherOp := uuid.New()
	if docs, _ := repo.FindChildDocuments(ctx, otherOp, root.DocumentID); len(docs) != 0 {
		t.Errorf("FindChildDocuments with a foreign operation returned %d docs", len(docs))
	}
	if n, _ := repo.CountChildDocuments(ctx, otherOp, root.DocumentID); n != 0 {
		t.Errorf("CountChildDocuments with a foreign operation returned %d", n)
	}
	if docs, _ := repo.FindDescendants(ctx, otherOp, root.DocumentID); len(docs) != 0 {
		t.Errorf("FindDescendants with a foreign operation returned %d docs", len(docs))
	}

	// Every find, count and aggregate that filtered on a parent must have
	// used an index. Counts arrive as aggregates ($match first).
	cur, err := mdb.Collection("system.profile").Find(ctx, bson.M{
		"ns":          mdb.Name() + "." + wikiDocumentCollection,
		"planSummary": "COLLSCAN",
	})
	if err != nil {
		t.Fatalf("read profiler: %v", err)
	}
	var scans []struct {
		Command bson.M `bson:"command"`
	}
	if err := cur.All(ctx, &scans); err != nil {
		t.Fatalf("decode profiler: %v", err)
	}
	for _, s := range scans {
		if filtersOnParent(s.Command) {
			t.Errorf("collection scan on a parent lookup: %v", s.Command)
		}
	}
}

// filtersOnParent reports whether a profiled find or aggregate command
// filtered on parent_document_id.
func filtersOnParent(cmd bson.M) bool {
	var filter any = cmd["filter"]
	if pipeline, ok := cmd["pipeline"].(bson.A); ok && len(pipeline) > 0 {
		if stage, ok := pipeline[0].(bson.M); ok {
			filter = stage["$match"]
		}
	}
	m, ok := filter.(bson.M)
	if !ok {
		return false
	}
	_, found := m["parent_document_id"]
	return found
}
