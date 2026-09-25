package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// Wiki document projections, against a real server.
//
// A document's weight is almost all content and content_state — the Markdown
// body and the Yjs CRDT blob. The finders split three ways on what they keep:
//
//   - API finders drop content_state only. No GraphQL field reads the CRDT
//     state, and they must keep content for content, hasContent and excerpt.
//   - Summary finders drop both, for callers that only read a document's
//     place in the tree and its label (ancestor crumbs, nesting depth).
//   - Whole finders keep everything, because duplicate, backup and export
//     copy content_state out of their results. A projection added to one of
//     these would not fail anywhere: the copy would silently be empty, and
//     Hocuspocus would rebuild the page from Markdown — losing every drawing.
//
// This test pins all three, so moving a finder between groups is a decision
// someone has to make here rather than a side effect.
func TestIntegrationWikiDocumentProjections(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewWikiDocumentRepository(db)
	opID := uuid.New()
	clock := newSeedClock(time.Minute)
	state := []byte{0x01, 0x02, 0x03}

	create := func(title string, parent *models.WikiDocument) *models.WikiDocument {
		t.Helper()
		doc := &models.WikiDocument{
			DocumentID:   uuid.New(),
			OperationID:  opID,
			Title:        title,
			Content:      title + " body",
			ContentState: state,
			IsTemplate:   true, // lets the template finder see every row
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
	leaf := create("Leaf", child)
	trashed := create("Trashed", root)
	if err := repo.SoftDelete(ctx, trashed, uuid.New()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// Every projection must keep what the tree and the UI key on.
	checkMetadata := func(t *testing.T, finder string, d models.WikiDocument) {
		t.Helper()
		if d.DocumentID == uuid.Nil || d.OperationID != opID || d.Title == "" || d.PathIDs == nil {
			t.Errorf("%s: metadata projected away (id=%v op=%v title=%q path_ids=%v)",
				finder, d.DocumentID, d.OperationID, d.Title, d.PathIDs)
		}
	}
	type group struct {
		wantContent, wantState bool
	}
	var (
		api     = group{wantContent: true}
		summary = group{}
		whole   = group{wantContent: true, wantState: true}
	)
	check := func(finder string, g group, docs []models.WikiDocument, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", finder, err)
		}
		if len(docs) == 0 {
			t.Fatalf("%s: returned no rows", finder)
		}
		for _, d := range docs {
			checkMetadata(t, finder, d)
			if got := d.Content != ""; got != g.wantContent {
				t.Errorf("%s: %q has content=%v, want %v", finder, d.Title, got, g.wantContent)
			}
			if got := len(d.ContentState) > 0; got != g.wantState {
				t.Errorf("%s: %q has content_state=%v, want %v", finder, d.Title, got, g.wantState)
			}
		}
	}
	one := func(d models.WikiDocument, err error) ([]models.WikiDocument, error) {
		return []models.WikiDocument{d}, err
	}
	dropCounts := func(docs []models.WikiDocument, _ map[uuid.UUID]int, err error) ([]models.WikiDocument, error) {
		return docs, err
	}

	// API finders.
	docs, err := dropCounts(repo.FindChildDocumentsWithCounts(ctx, opID, &root.DocumentID))
	check("FindChildDocumentsWithCounts", api, docs, err)
	docs, err = dropCounts(repo.FindDocumentsForRevealPath(ctx, opID, []uuid.UUID{root.DocumentID}))
	check("FindDocumentsForRevealPath", api, docs, err)
	docs, err = repo.FindByOperationIDWithCursor(ctx, opID, WikiDocumentFilter{}, nil, 10, true)
	check("FindByOperationIDWithCursor", api, docs, err)
	docs, err = repo.FindTrashedByOperationIDWithCursor(ctx, opID, nil, 10, true)
	check("FindTrashedByOperationIDWithCursor", api, docs, err)
	docs, err = repo.FindTrashedDescendants(ctx, opID, root.DocumentID)
	check("FindTrashedDescendants", api, docs, err)
	docs, err = repo.FindTemplatesByOperationID(ctx, opID)
	check("FindTemplatesByOperationID", api, docs, err)
	docs, err = repo.FindByIDs(ctx, []uuid.UUID{root.DocumentID, leaf.DocumentID})
	check("FindByIDs", api, docs, err)

	// Summary finders.
	docs, err = repo.FindSummariesByIDs(ctx, []uuid.UUID{root.DocumentID, leaf.DocumentID})
	check("FindSummariesByIDs", summary, docs, err)
	docs, err = repo.FindSummariesByOperationID(ctx, opID, false)
	check("FindSummariesByOperationID", summary, docs, err)
	docs, err = repo.FindAncestors(ctx, leaf.DocumentID)
	check("FindAncestors", summary, docs, err)
	if len(docs) != 3 {
		t.Errorf("FindAncestors(leaf) returned %d documents, want 3 (root, child, leaf)", len(docs))
	}
	if depth, err := repo.NestingDepth(ctx, leaf.DocumentID); err != nil || depth != 3 {
		t.Errorf("NestingDepth(leaf) = %d, %v; want 3 — path_ids must survive the summary projection", depth, err)
	}

	// Whole finders: duplicate, backup and export copy the state out of these.
	docs, err = one(repo.FindByID(ctx, leaf.DocumentID))
	check("FindByID", whole, docs, err)
	docs, err = repo.FindChildDocuments(ctx, opID, root.DocumentID)
	check("FindChildDocuments", whole, docs, err)
	docs, err = repo.FindAllByOperationID(ctx, opID)
	check("FindAllByOperationID", whole, docs, err)
	docs, err = repo.FindDescendants(ctx, opID, root.DocumentID)
	check("FindDescendants", whole, docs, err)
}
