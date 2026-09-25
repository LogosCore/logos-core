package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// reorderRepo is the wiki repository as the sibling reorder sees it. It
// counts round trips; everything it does not implement — FindByID among
// them — panics through the nil embed, so a per-document read fails the
// test instead of passing quietly.
type reorderRepo struct {
	repository.IWikiDocumentRepository
	docs       map[uuid.UUID]*models.WikiDocument
	summaries  int
	reloads    int
	bulkWrites [][]repository.DocumentUpdate
	cascades   []uuid.UUID
}

func (f *reorderRepo) FindSummariesByIDs(_ context.Context, ids []uuid.UUID) ([]models.WikiDocument, error) {
	f.summaries++
	return f.find(ids), nil
}

func (f *reorderRepo) FindByIDs(_ context.Context, ids []uuid.UUID) ([]models.WikiDocument, error) {
	f.reloads++
	return f.find(ids), nil
}

func (f *reorderRepo) find(ids []uuid.UUID) []models.WikiDocument {
	var out []models.WikiDocument
	for _, id := range ids {
		if d, ok := f.docs[id]; ok {
			out = append(out, *d)
		}
	}
	return out
}

func (f *reorderRepo) NestingDepth(context.Context, uuid.UUID) (int, error) { return 1, nil }

func (f *reorderRepo) BulkUpdate(_ context.Context, _ uuid.UUID, updates []repository.DocumentUpdate) error {
	f.bulkWrites = append(f.bulkWrites, updates)
	for _, u := range updates {
		d := f.docs[u.DocumentID]
		if v, ok := u.Set["sort_order"]; ok {
			d.SortOrder = v.(string)
		}
		if v, ok := u.Set["parent_document_id"]; ok {
			if p, isID := v.(uuid.UUID); isID {
				d.ParentDocumentID = &p
			} else {
				d.ParentDocumentID = nil
			}
		}
	}
	return nil
}

func (f *reorderRepo) RebuildPathIDsCascade(_ context.Context, id uuid.UUID) error {
	f.cascades = append(f.cascades, id)
	return nil
}

type reorderFixture struct {
	r              *wikiDocumentResolver
	repo           *reorderRepo
	ctx            context.Context
	opID, callerID uuid.UUID
	folder, other  *models.WikiDocument
}

func newReorderFixture(t *testing.T) *reorderFixture {
	t.Helper()
	f := &reorderFixture{opID: uuid.New(), callerID: uuid.New()}
	f.repo = &reorderRepo{docs: map[uuid.UUID]*models.WikiDocument{}}
	f.folder = f.doc(nil, "M")
	f.other = f.doc(nil, "T")
	f.r = &wikiDocumentResolver{
		docRepo: f.repo,
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) {
			return memberOp(f.opID, f.callerID, models.OperationRoleOperator), nil
		}},
		eventBus: eventbus.NewNopEventBus(),
	}
	f.ctx = newCallerCtx(f.callerID)
	return f
}

func (f *reorderFixture) doc(parent *models.WikiDocument, sortOrder string) *models.WikiDocument {
	d := &models.WikiDocument{DocumentID: uuid.New(), OperationID: f.opID, SortOrder: sortOrder}
	if parent != nil {
		d.ParentDocumentID = &parent.DocumentID
	}
	f.repo.docs[d.DocumentID] = d
	return d
}

func (f *reorderFixture) reorder(parent *models.WikiDocument, docs ...*models.WikiDocument) ([]*models.WikiDocument, error) {
	in := model.ReorderWikiDocumentSiblingsInput{OperationID: f.opID.String()}
	if parent != nil {
		pid := parent.DocumentID.String()
		in.ParentDocumentID = &pid
	}
	for _, d := range docs {
		in.OrderedIds = append(in.OrderedIds, d.DocumentID.String())
	}
	return f.r.ReorderWikiDocumentSiblings(f.ctx, in)
}

// TestReorderWikiDocumentSiblings_CostsAFixedNumberOfRoundTrips drops a
// document from another folder between two existing children. The reorder
// used to read, write and re-read each document one at a time; it must now
// cost one read, one bulk write and one reload however long the folder is.
func TestReorderWikiDocumentSiblings_CostsAFixedNumberOfRoundTrips(t *testing.T) {
	f := newReorderFixture(t)
	a := f.doc(f.folder, "a")
	b := f.doc(f.folder, "b")
	moved := f.doc(f.other, "c")

	got, err := f.reorder(f.folder, a, moved, b)
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}

	if f.repo.summaries != 1 || len(f.repo.bulkWrites) != 1 || f.repo.reloads != 1 {
		t.Errorf("round trips: %d reads, %d bulk writes, %d reloads; want 1 each",
			f.repo.summaries, len(f.repo.bulkWrites), f.repo.reloads)
	}
	for i, want := range []*models.WikiDocument{a, moved, b} {
		if got[i].DocumentID != want.DocumentID {
			t.Fatalf("result %d is %s, want %s: results must keep the requested order", i, got[i].DocumentID, want.DocumentID)
		}
	}

	writes := map[uuid.UUID]map[string]interface{}{}
	for _, w := range f.repo.bulkWrites[0] {
		writes[w.DocumentID] = w.Set
	}
	if set := writes[moved.DocumentID]; set["parent_document_id"] != f.folder.DocumentID || set["last_updated_by_id"] != f.callerID {
		t.Errorf("moved document's write = %v; want its new parent and the caller as last editor", set)
	}
	for _, sibling := range []*models.WikiDocument{a, b} {
		if set, ok := writes[sibling.DocumentID]; ok {
			if _, stamped := set["last_updated_by_id"]; stamped {
				t.Errorf("sort-only write for %s stamps last_updated_by_id: %v", sibling.DocumentID, set)
			}
		}
	}
	if len(f.repo.cascades) != 1 || f.repo.cascades[0] != moved.DocumentID {
		t.Errorf("path rebuilds = %v, want only the reparented document", f.repo.cascades)
	}
}

func TestReorderWikiDocumentSiblings_RejectsBadOrderings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument)
		wantErr string
	}{
		{"duplicate id", func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument) {
			a := f.doc(f.folder, "a")
			return []*models.WikiDocument{a, a}, f.folder
		}, "duplicate document ID"},
		{"unknown document", func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument) {
			return []*models.WikiDocument{{DocumentID: uuid.New()}}, f.folder
		}, "document not found"},
		{"another operation's document", func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument) {
			a := f.doc(f.folder, "a")
			a.OperationID = uuid.New()
			return []*models.WikiDocument{a}, f.folder
		}, "belongs to a different operation"},
		{"trashed document", func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument) {
			a := f.doc(f.folder, "a")
			a.DeletedAt = &a.CreateAt
			return []*models.WikiDocument{a}, f.folder
		}, "trashed document"},
		{"missing parent", func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument) {
			a := f.doc(f.folder, "a")
			return []*models.WikiDocument{a}, &models.WikiDocument{DocumentID: uuid.New()}
		}, "parent document not found"},
		{"document as its own parent", func(f *reorderFixture) ([]*models.WikiDocument, *models.WikiDocument) {
			return []*models.WikiDocument{f.folder}, f.folder
		}, "its own parent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReorderFixture(t)
			docs, parent := tc.setup(f)
			if _, err := f.reorder(parent, docs...); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
			if len(f.repo.bulkWrites) != 0 {
				t.Errorf("a rejected ordering still wrote: %v", f.repo.bulkWrites)
			}
		})
	}
}
