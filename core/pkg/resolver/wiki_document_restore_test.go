package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/wiki"
)

// restoreWorld records what RestoreWikiDocumentBackup asks of the row and of
// the sidecar. Every method it does not implement panics through the nil
// embeds, so a path that reaches for something unexpected fails loudly.
type restoreWorld struct {
	doc    models.WikiDocument
	backup models.WikiDocumentBackup

	rowRestores  int
	safetyCopies []models.WikiDocumentBackup

	roomRestores []restoreCall
	roomResult   wiki.RestoreDrawingResult
	roomErr      error
}

type restoreCall struct {
	documentID string
	state      []byte
	userID     string
}

type restoreDocs struct {
	repository.IWikiDocumentRepository
	w *restoreWorld
}

func (f restoreDocs) FindByID(context.Context, uuid.UUID) (models.WikiDocument, error) {
	return f.w.doc, nil
}

func (f restoreDocs) RestoreFromBackup(context.Context, uuid.UUID, string, []byte) error {
	f.w.rowRestores++
	return nil
}

func (f restoreDocs) Update(context.Context, *models.WikiDocument, map[string]interface{}) error {
	return nil
}

type restoreBackups struct {
	repository.IWikiDocumentBackupRepository
	w *restoreWorld
}

func (f restoreBackups) FindByID(context.Context, uuid.UUID) (models.WikiDocumentBackup, error) {
	return f.w.backup, nil
}

func (f restoreBackups) Create(_ context.Context, b *models.WikiDocumentBackup) error {
	f.w.safetyCopies = append(f.w.safetyCopies, *b)
	return nil
}

type restoreRoom struct{ w *restoreWorld }

func (f restoreRoom) RestoreDrawing(_ context.Context, documentID string, state []byte, userID string) (wiki.RestoreDrawingResult, error) {
	f.w.roomRestores = append(f.w.roomRestores, restoreCall{documentID, state, userID})
	return f.w.roomResult, f.w.roomErr
}

func newRestoreWorld(t *testing.T, kind models.WikiDocumentKind) (*wikiDocumentResolver, *restoreWorld, context.Context, uuid.UUID) {
	t.Helper()
	caller, opID, docID := uuid.New(), uuid.New(), uuid.New()
	w := &restoreWorld{
		doc: models.WikiDocument{
			DocumentID: docID, OperationID: opID, Title: "Network map", Kind: kind,
			Content: "stale row text", ContentState: []byte("stale row state"),
		},
		backup: models.WikiDocumentBackup{
			BackupID: uuid.New(), DocumentID: docID, OperationID: opID, Title: "Network map",
			Kind: kind, ContentState: []byte("backup state"),
		},
		roomResult: wiki.RestoreDrawingResult{
			Restored: 3, Removed: 1,
			PreviousState: []byte("live room state"), PreviousContent: "live room text",
		},
	}
	r := &wikiDocumentResolver{
		docRepo:    restoreDocs{w: w},
		backupRepo: restoreBackups{w: w},
		operationRepo: &mockOpRepo{findByIDFn: func(context.Context, uuid.UUID) (models.Operation, error) {
			return memberOp(opID, caller, models.OperationRoleOperator), nil
		}},
		eventBus: &countingBus{},
		restorer: restoreRoom{w: w},
	}
	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: caller.String()})
	return r, w, ctx, caller
}

// The fix itself: a drawing goes back through its room, never over the row,
// which a loaded room overwrites and a reconnecting browser merges away.
func TestRestoreWikiDocumentBackup_DrawingGoesThroughTheRoom(t *testing.T) {
	r, w, ctx, caller := newRestoreWorld(t, models.WikiDocumentKindDrawing)

	if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err != nil {
		t.Fatalf("RestoreWikiDocumentBackup: %v", err)
	}

	if w.rowRestores != 0 {
		t.Errorf("a drawing restore wrote the row %d times; it must only go through the room", w.rowRestores)
	}
	if len(w.roomRestores) != 1 {
		t.Fatalf("room restores = %d, want 1", len(w.roomRestores))
	}
	got := w.roomRestores[0]
	if got.documentID != w.doc.DocumentID.String() || string(got.state) != "backup state" || got.userID != caller.String() {
		t.Errorf("room restore called with %+v", got)
	}
}

// The safety copy is what was on screen, which the room knows and the row may
// not yet.
func TestRestoreWikiDocumentBackup_DrawingSafetyCopyIsTheLiveRoom(t *testing.T) {
	r, w, ctx, _ := newRestoreWorld(t, models.WikiDocumentKindDrawing)

	if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err != nil {
		t.Fatalf("RestoreWikiDocumentBackup: %v", err)
	}

	if len(w.safetyCopies) != 1 {
		t.Fatalf("safety copies = %d, want 1", len(w.safetyCopies))
	}
	safety := w.safetyCopies[0]
	if string(safety.ContentState) != "live room state" || safety.Content != "live room text" {
		t.Errorf("safety copy holds %q / %q, want the live room's state", safety.ContentState, safety.Content)
	}
	if safety.Kind != models.WikiDocumentKindDrawing {
		t.Errorf("safety copy kind = %q, want drawing", safety.Kind)
	}
}

// A sidecar that cannot be reached must fail the restore. Falling back to the
// row write would report success for a restore that does not stick.
func TestRestoreWikiDocumentBackup_DrawingFailsWhenTheRoomCannot(t *testing.T) {
	r, w, ctx, _ := newRestoreWorld(t, models.WikiDocumentKindDrawing)
	w.roomErr = errors.New("connection refused")

	_, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String())
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want the sidecar failure", err)
	}
	if w.rowRestores != 0 || len(w.safetyCopies) != 0 {
		t.Errorf("a failed room restore still wrote: rows=%d safety=%d", w.rowRestores, len(w.safetyCopies))
	}
}

// Prose is untouched by this change.
func TestRestoreWikiDocumentBackup_ProseStillWritesTheRow(t *testing.T) {
	r, w, ctx, _ := newRestoreWorld(t, models.WikiDocumentKindDocument)

	if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err != nil {
		t.Fatalf("RestoreWikiDocumentBackup: %v", err)
	}
	if w.rowRestores != 1 || len(w.roomRestores) != 0 {
		t.Errorf("prose restore: rows=%d room=%d, want 1/0", w.rowRestores, len(w.roomRestores))
	}
}
