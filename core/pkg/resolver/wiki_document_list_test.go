package resolver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// wikiListRepo serves one fixed set of rows to the documents and trash lists,
// honouring only the limit.
type wikiListRepo struct {
	repository.IWikiDocumentRepository
	rows []models.WikiDocument
}

func (f *wikiListRepo) page(limit int64) []models.WikiDocument {
	if int64(len(f.rows)) > limit {
		return f.rows[:limit]
	}
	return f.rows
}

func (f *wikiListRepo) CountByOperationID(context.Context, uuid.UUID, repository.WikiDocumentFilter) (int64, error) {
	return int64(len(f.rows)), nil
}

func (f *wikiListRepo) FindByOperationIDWithCursor(_ context.Context, _ uuid.UUID, _ repository.WikiDocumentFilter, _ *pagination.Cursor, limit int64, _ bool) ([]models.WikiDocument, error) {
	return f.page(limit), nil
}

func (f *wikiListRepo) FindTrashedByOperationIDWithCursor(_ context.Context, _ uuid.UUID, _ *pagination.Cursor, limit int64, _ bool) ([]models.WikiDocument, error) {
	return f.page(limit), nil
}

// TestWikiLists_ReportTheNextPage guards the documents and trash lists, which
// trimmed their surplus row before pagination.BuildEdges could see it. That
// made hasNextPage always false, so the recent-documents modal and the trash
// panel never loaded a second page.
func TestWikiLists_ReportTheNextPage(t *testing.T) {
	caller, opID := uuid.New(), uuid.New()
	rows := make([]models.WikiDocument, 25)
	for i := range rows {
		rows[i] = models.WikiDocument{DocumentID: uuid.New(), OperationID: opID}
	}
	r := &wikiDocumentResolver{
		docRepo: &wikiListRepo{rows: rows},
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) {
			return memberOp(opID, caller, models.OperationRoleViewer), nil
		}},
	}
	ctx := newCallerCtx(caller)
	first := 20

	for name, list := range map[string]func() (*model.WikiDocumentConnection, error){
		"wikiDocuments": func() (*model.WikiDocumentConnection, error) {
			return r.WikiDocuments(ctx, opID.String(), nil, nil, nil, &first, nil, nil, nil)
		},
		"wikiDocumentTrash": func() (*model.WikiDocumentConnection, error) {
			return r.WikiDocumentTrash(ctx, opID.String(), &first, nil, nil, nil)
		},
	} {
		conn, err := list()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(conn.Edges) != first || !conn.PageInfo.HasNextPage {
			t.Errorf("%s: %d edges, hasNextPage=%v; want %d and true — 25 rows do not fit a page of %d",
				name, len(conn.Edges), conn.PageInfo.HasNextPage, first, first)
		}
	}
}
