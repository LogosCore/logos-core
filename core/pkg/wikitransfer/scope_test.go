package wikitransfer_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer/transfertest"
)

// A collected scope carries the shape of the tree and none of its bodies.
//
// This is the property the whole export flow rests on: a wiki of any size
// costs one page's CRDT state at a time, not all of them at once. It reads
// like a detail and is not — it is why there is no document cap any more, and
// a finder swapped back to the whole-row one would restore the old ceiling
// without failing a single other test.
func TestCollectScope_CarriesNoBodies(t *testing.T) {
	opID := uuid.New()
	root := models.WikiDocument{
		DocumentID: uuid.New(), OperationID: opID, Title: "Root", SortOrder: "a",
		ContentState: []byte("root body"), Content: "root body",
	}
	child := models.WikiDocument{
		DocumentID: uuid.New(), OperationID: opID, Title: "Child", SortOrder: "a",
		ParentDocumentID: &root.DocumentID, PathIDs: []uuid.UUID{root.DocumentID},
		ContentState: []byte("child body"), Content: "child body",
	}
	docs := transfertest.NewDocRepo(root, child)

	for _, tc := range []struct {
		name   string
		rootID *uuid.UUID
	}{
		{"tree", nil},
		{"subtree", &root.DocumentID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := wikitransfer.CollectScope(context.Background(), docs, opID, "ACME", tc.rootID)
			if err != nil {
				t.Fatalf("CollectScope: %v", err)
			}
			if len(scope.Docs) != 2 {
				t.Fatalf("docs = %d, want 2", len(scope.Docs))
			}
			for _, d := range scope.Docs {
				if len(d.ContentState) != 0 {
					t.Errorf("%s still carries its CRDT body", d.Title)
				}
				if d.Content != "" {
					t.Errorf("%s still carries its search projection", d.Title)
				}
			}
			if scope.Root != nil && len(scope.Root.ContentState) != 0 {
				t.Error("the subtree root carries its body")
			}
		})
	}
}

// The bodies are still reachable — through the loader, one at a time.
func TestScope_ContentStateReadsThroughLoader(t *testing.T) {
	opID := uuid.New()
	doc := models.WikiDocument{
		DocumentID: uuid.New(), OperationID: opID, Title: "Root",
		ContentState: []byte("the body"),
	}
	docs := transfertest.NewDocRepo(doc)

	scope, err := wikitransfer.CollectScope(context.Background(), docs, opID, "ACME", nil)
	if err != nil {
		t.Fatalf("CollectScope: %v", err)
	}
	state, err := scope.ContentState(context.Background(), doc.DocumentID)
	if err != nil {
		t.Fatalf("ContentState: %v", err)
	}
	if string(state) != "the body" {
		t.Errorf("state = %q, want the stored body", state)
	}

	// A page nobody has opened has no state, which is a page with an empty
	// body rather than a failed export.
	state, err = scope.ContentState(context.Background(), uuid.New())
	if err != nil || len(state) != 0 {
		t.Errorf("unknown document = (%q, %v), want empty and no error", state, err)
	}
}
