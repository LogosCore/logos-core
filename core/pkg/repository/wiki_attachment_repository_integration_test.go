package repository

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo/field"
)

// attachmentRows is the repository surface wiki images and files share.
type attachmentRows[T any] interface {
	Create(ctx context.Context, attachment *T) error
	FindByID(ctx context.Context, id uuid.UUID) (T, error)
	FindByDocumentID(ctx context.Context, docID uuid.UUID) ([]T, error)
	FindCandidatesOlderThan(ctx context.Context, cutoff time.Time, limit int64) ([]T, error)
	HardDelete(ctx context.Context, id uuid.UUID) error
}

// TestIntegrationWikiAttachments runs the storage images and files share
// against a real server, once per kind. The field an attachment is keyed by
// is a string each constructor passes in, and a wrong one fails silently:
// every lookup comes back not-found, and the sweeper deletes nothing.
func TestIntegrationWikiAttachments(t *testing.T) {
	// One database for both kinds: they live in separate collections, and a
	// subtest's name (it contains a slash) is not a valid database name.
	db := integrationDB(t)
	t.Run("images", func(t *testing.T) {
		exerciseAttachmentRepository(t, NewWikiImageRepository(db),
			func(id, docID uuid.UUID, created time.Time) models.WikiImage {
				return models.WikiImage{DefaultField: field.DefaultField{CreateAt: created}, ImageID: id, DocumentID: docID}
			},
			func(img models.WikiImage) uuid.UUID { return img.ImageID })
	})
	t.Run("files", func(t *testing.T) {
		exerciseAttachmentRepository(t, NewWikiFileRepository(db),
			func(id, docID uuid.UUID, created time.Time) models.WikiFile {
				return models.WikiFile{DefaultField: field.DefaultField{CreateAt: created}, FileID: id, DocumentID: docID}
			},
			func(file models.WikiFile) uuid.UUID { return file.FileID })
	})
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
}

func exerciseAttachmentRepository[T any](
	t *testing.T,
	repo attachmentRows[T],
	row func(id, docID uuid.UUID, created time.Time) T,
	idOf func(T) uuid.UUID,
) {
	t.Helper()
	ctx := testCtx(t)
	ids := func(rows []T) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = idOf(r).String()
		}
		sort.Strings(out)
		return out
	}
	sorted := func(in ...uuid.UUID) []string {
		out := make([]string, len(in))
		for i, id := range in {
			out[i] = id.String()
		}
		sort.Strings(out)
		return out
	}

	docA, docB := uuid.New(), uuid.New()
	old1, old2, fresh := uuid.New(), uuid.New(), uuid.New()
	longAgo := time.Now().Add(-2 * time.Hour)
	for _, r := range []T{
		row(old1, docA, longAgo),
		row(old2, docA, longAgo),
		row(fresh, docB, time.Now()),
	} {
		if err := repo.Create(ctx, &r); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.FindByID(ctx, old1)
	if err != nil || idOf(got) != old1 {
		t.Fatalf("FindByID(old1) = %v, %v; want the row", idOf(got), err)
	}
	if _, err := repo.FindByID(ctx, uuid.New()); err == nil {
		t.Error("FindByID(unknown id) found a row")
	}

	byDoc, err := repo.FindByDocumentID(ctx, docA)
	if err != nil {
		t.Fatalf("FindByDocumentID: %v", err)
	}
	if got, want := ids(byDoc), sorted(old1, old2); !equalStrings(got, want) {
		t.Errorf("FindByDocumentID(docA) = %v, want %v", got, want)
	}

	cutoff := time.Now().Add(-time.Hour)
	candidates, err := repo.FindCandidatesOlderThan(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("FindCandidatesOlderThan: %v", err)
	}
	if got, want := ids(candidates), sorted(old1, old2); !equalStrings(got, want) {
		t.Errorf("FindCandidatesOlderThan = %v, want the two old rows %v", got, want)
	}
	if limited, err := repo.FindCandidatesOlderThan(ctx, cutoff, 1); err != nil || len(limited) != 1 {
		t.Errorf("FindCandidatesOlderThan(limit 1) = %d rows, %v; want 1", len(limited), err)
	}

	duplicate := row(old2, docB, time.Now())
	if err := repo.Create(ctx, &duplicate); err == nil {
		t.Error("Create accepted a second row with an existing id; the unique index is missing")
	}

	if err := repo.HardDelete(ctx, old1); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	if _, err := repo.FindByID(ctx, old1); err == nil {
		t.Error("the deleted row is still there")
	}
	if _, err := repo.FindByID(ctx, old2); err != nil {
		t.Errorf("HardDelete(old1) took old2 with it: %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
