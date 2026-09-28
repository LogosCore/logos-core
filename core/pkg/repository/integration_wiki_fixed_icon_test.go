package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// The startup backfill for drawings stored with an icon — every drawing the
// SPA's create dialog made got "Adaptive". Against a real server because the
// filter's shape (the $nin over "" and null, the explicit kind) is the part
// that can be wrong while still returning plausible counts.
func TestIntegrationClearFixedIcons(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewWikiDocumentRepository(db)
	op := uuid.New()

	doc := func(kind models.WikiDocumentKind, emoji, icon, color string, trashed bool) uuid.UUID {
		d := &models.WikiDocument{
			DocumentID: uuid.New(), OperationID: op, Title: "t", TitleLower: "t",
			Kind: kind, Emoji: emoji, Icon: icon, Color: color,
		}
		if trashed {
			now := time.Now()
			d.DeletedAt = &now
		}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
		return d.DocumentID
	}
	drawingIcon := doc(models.WikiDocumentKindDrawing, "", "Adaptive", "", false)
	drawingColour := doc(models.WikiDocumentKindDrawing, "", "", "#E91E63", false)
	drawingTrashed := doc(models.WikiDocumentKindDrawing, "🗺️", "", "", true)
	drawingClean := doc(models.WikiDocumentKindDrawing, "", "", "", false)
	prose := doc(models.WikiDocumentKindDocument, "🐧", "Server", "#fff", false)
	legacyProse := doc("", "", "Adaptive", "", false)

	n, err := repo.ClearFixedIcons(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("changed %d rows, want 3 (two live drawings and a trashed one)", n)
	}

	for _, id := range []uuid.UUID{drawingIcon, drawingColour, drawingTrashed, drawingClean} {
		d, err := repo.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if d.Emoji != "" || d.Icon != "" || d.Color != "" {
			t.Errorf("drawing %s kept %q/%q/%q", id, d.Emoji, d.Icon, d.Color)
		}
	}
	if d, _ := repo.FindByID(ctx, prose); d.Icon != "Server" || d.Emoji != "🐧" || d.Color != "#fff" {
		t.Errorf("prose page changed: %q/%q/%q", d.Emoji, d.Icon, d.Color)
	}
	if d, _ := repo.FindByID(ctx, legacyProse); d.Icon != "Adaptive" {
		t.Errorf("legacy prose page (no kind) lost its icon: %q", d.Icon)
	}

	// Idempotent: a second boot finds nothing to do.
	if n, err := repo.ClearFixedIcons(ctx); err != nil || n != 0 {
		t.Errorf("second run changed %d rows (err %v), want 0", n, err)
	}
}
