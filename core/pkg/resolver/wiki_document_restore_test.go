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
	kind       string
	documentID string
	state      []byte
	markdown   string
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
	f.w.roomRestores = append(f.w.roomRestores, restoreCall{"drawing", documentID, state, "", userID})
	return f.w.roomResult, f.w.roomErr
}

func (f restoreRoom) RestoreDocument(_ context.Context, documentID string, state []byte, markdown, userID string) (wiki.RestoreDocumentResult, error) {
	f.w.roomRestores = append(f.w.roomRestores, restoreCall{"document", documentID, state, markdown, userID})
	return wiki.RestoreDocumentResult{
		PreviousState:           f.w.roomResult.PreviousState,
		PreviousContent:         f.w.roomResult.PreviousContent,
		PreviousImageReferences: f.w.roomResult.PreviousImageReferences,
		PreviousFileReferences:  f.w.roomResult.PreviousFileReferences,
	}, f.w.roomErr
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
			Kind: kind, ContentState: []byte("backup state"), Content: "backup text",
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
	if got.kind != "drawing" || got.documentID != w.doc.DocumentID.String() || string(got.state) != "backup state" || got.userID != caller.String() {
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

// The safety copy is indexed for the attachment sweepers, from the room's own
// answer plus the row's: the room can hold an image the row has not heard of
// yet, and a backup that missed it would let the sweeper reclaim it.
func TestRestoreWikiDocumentBackup_SafetyCopyIndexesRoomAndRowReferences(t *testing.T) {
	for _, kind := range []models.WikiDocumentKind{models.WikiDocumentKindDrawing, models.WikiDocumentKindDocument} {
		t.Run(string(kind.Or()), func(t *testing.T) {
			r, w, ctx, _ := newRestoreWorld(t, kind)
			inRow, inRoom := uuid.New(), uuid.New()
			w.doc.ImageReferences = []uuid.UUID{inRow}
			w.roomResult.PreviousImageReferences = []string{inRoom.String(), inRow.String()}
			w.roomResult.PreviousFileReferences = []string{"not-a-uuid"}

			if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err != nil {
				t.Fatalf("RestoreWikiDocumentBackup: %v", err)
			}
			if len(w.safetyCopies) != 1 {
				t.Fatalf("safety copies = %d, want 1", len(w.safetyCopies))
			}
			safety := w.safetyCopies[0]
			if !safety.ReferencesIndexed {
				t.Error("safety copy is not marked indexed")
			}
			if len(safety.ImageReferences) != 2 || safety.ImageReferences[0] != inRow || safety.ImageReferences[1] != inRoom {
				t.Errorf("images = %v, want [%s %s]", safety.ImageReferences, inRow, inRoom)
			}
			if len(safety.FileReferences) != 0 {
				t.Errorf("files = %v, want none (the malformed id dropped)", safety.FileReferences)
			}
		})
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

// Prose pages have the same bug and get the same fix. The backup's stored
// text travels too: it is the body of a backup taken before the page was
// ever opened, which has no state to restore from.
func TestRestoreWikiDocumentBackup_ProseGoesThroughTheRoom(t *testing.T) {
	r, w, ctx, caller := newRestoreWorld(t, models.WikiDocumentKindDocument)

	if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err != nil {
		t.Fatalf("RestoreWikiDocumentBackup: %v", err)
	}

	if w.rowRestores != 0 {
		t.Errorf("a prose restore wrote the row %d times; it must only go through the room", w.rowRestores)
	}
	if len(w.roomRestores) != 1 {
		t.Fatalf("room restores = %d, want 1", len(w.roomRestores))
	}
	got := w.roomRestores[0]
	want := restoreCall{"document", w.doc.DocumentID.String(), []byte("backup state"), "backup text", caller.String()}
	if got.kind != want.kind || got.documentID != want.documentID || string(got.state) != string(want.state) ||
		got.markdown != want.markdown || got.userID != want.userID {
		t.Errorf("room restore called with %+v, want %+v", got, want)
	}
	if len(w.safetyCopies) != 1 || string(w.safetyCopies[0].ContentState) != "live room state" {
		t.Errorf("safety copies = %+v, want one holding the live room's state", w.safetyCopies)
	}
}

func TestRestoreWikiDocumentBackup_ProseFailsWhenTheRoomCannot(t *testing.T) {
	r, w, ctx, _ := newRestoreWorld(t, models.WikiDocumentKindDocument)
	w.roomErr = errors.New("connection refused")

	if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err == nil {
		t.Fatal("a failed room restore reported success")
	}
	if w.rowRestores != 0 || len(w.safetyCopies) != 0 {
		t.Errorf("a failed room restore still wrote: rows=%d safety=%d", w.rowRestores, len(w.safetyCopies))
	}
}

// Without a sidecar wired the row write is all there is, for either kind.
func TestRestoreWikiDocumentBackup_NoRestorerWritesTheRow(t *testing.T) {
	r, w, ctx, _ := newRestoreWorld(t, models.WikiDocumentKindDocument)
	r.restorer = nil

	if _, err := r.RestoreWikiDocumentBackup(ctx, w.doc.DocumentID.String(), w.backup.BackupID.String()); err != nil {
		t.Fatalf("RestoreWikiDocumentBackup: %v", err)
	}
	if w.rowRestores != 1 || len(w.roomRestores) != 0 {
		t.Errorf("rows=%d room=%d, want 1/0", w.rowRestores, len(w.roomRestores))
	}
}
