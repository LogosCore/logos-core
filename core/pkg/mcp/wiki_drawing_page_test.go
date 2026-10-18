package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/wiki"
)

func sceneOf(n int, kind string, extra map[string]any) wiki.DrawingScene {
	scene := wiki.DrawingScene{}
	for i := range n {
		el := wiki.DrawingElement{"id": fmt.Sprintf("s%d", i), "type": kind, "x": float64(i)}
		for k, v := range extra {
			el[k] = v
		}
		scene.Elements = append(scene.Elements, el)
	}
	return scene
}

// The outline is what an agent reads for ids. A large canvas used to answer
// it with labels and counts and no ids at all.
func TestDrawingPage_OutlineListsIdsOnALargeCanvas(t *testing.T) {
	view := drawingPage(wikiDocView{}, sceneOf(450, "rectangle", nil), getWikiDrawingArgs{View: "outline"})

	if len(view.Elements) != outlinePageSize {
		t.Fatalf("elements = %d, want a page of %d", len(view.Elements), outlinePageSize)
	}
	if view.Elements[0].ID != "s0" {
		t.Errorf("first id = %q, want s0", view.Elements[0].ID)
	}
	if view.NextOffset != outlinePageSize || view.Count != 450 {
		t.Errorf("next = %d, count = %d", view.NextOffset, view.Count)
	}
	if view.Shapes["rectangle"] != 450 {
		t.Errorf("shapes = %v, want the whole scene counted", view.Shapes)
	}
	if len(view.Notes) != 1 || !strings.Contains(view.Notes[0], "offset:200") {
		t.Errorf("notes = %v, want a pointer to the next page", view.Notes)
	}

	last := drawingPage(wikiDocView{}, sceneOf(450, "rectangle", nil), getWikiDrawingArgs{View: "outline", Offset: 400})
	if len(last.Elements) != 50 || last.Elements[0].ID != "s400" || last.NextOffset != 0 {
		t.Errorf("last page: %d elements from %q, next %d", len(last.Elements), last.Elements[0].ID, last.NextOffset)
	}
}

func TestDrawingPage_SmallCanvasIsOnePage(t *testing.T) {
	view := drawingPage(wikiDocView{}, sceneOf(30, "rectangle", nil), getWikiDrawingArgs{View: "outline"})
	if len(view.Elements) != 30 || view.NextOffset != 0 || view.Shapes != nil || len(view.Notes) != 0 {
		t.Errorf("got %d elements, next %d, shapes %v, notes %v", len(view.Elements), view.NextOffset, view.Shapes, view.Notes)
	}
}

func TestDrawingPage_FullIsCappedByCountAndBytes(t *testing.T) {
	view := drawingPage(wikiDocView{}, sceneOf(120, "rectangle", nil), getWikiDrawingArgs{View: "full", Limit: 1000})
	if len(view.Full) != fullPageSize || view.NextOffset != fullPageSize {
		t.Errorf("full = %d, next = %d; want a capped page of %d", len(view.Full), view.NextOffset, fullPageSize)
	}

	// Freehand strokes heavy enough that the byte budget binds before the count.
	points := make([]any, 4000)
	for i := range points {
		points[i] = []any{float64(i) * 1.37, float64(i) * 2.11}
	}
	heavy := drawingPage(wikiDocView{}, sceneOf(40, "freedraw", map[string]any{"points": points}), getWikiDrawingArgs{View: "full"})
	if len(heavy.Full) == 0 || len(heavy.Full) >= 40 {
		t.Fatalf("heavy page = %d elements, want fewer than all 40 and at least one", len(heavy.Full))
	}
	if heavy.NextOffset != len(heavy.Full) {
		t.Errorf("next = %d, want %d", heavy.NextOffset, len(heavy.Full))
	}
}

// One element over the whole budget still comes back, or the reader would be
// stuck on the same offset.
func TestDrawingPage_FullAlwaysReturnsOne(t *testing.T) {
	points := make([]any, 40000)
	for i := range points {
		points[i] = []any{float64(i), float64(i)}
	}
	view := drawingPage(wikiDocView{}, sceneOf(2, "freedraw", map[string]any{"points": points}), getWikiDrawingArgs{View: "full"})
	if len(view.Full) != 1 || view.NextOffset != 1 {
		t.Errorf("full = %d, next = %d; want 1 and 1", len(view.Full), view.NextOffset)
	}
}

func TestDrawingPage_OutlineLeavesFreehandPointsOut(t *testing.T) {
	scene := sceneOf(1, "freedraw", map[string]any{"points": []any{[]any{0.0, 0.0}, []any{1.0, 1.0}}})
	scene.Elements = append(scene.Elements, wiki.DrawingElement{
		"id": "arr", "type": "arrow", "points": []any{[]any{0.0, 0.0}, []any{5.0, 0.0}},
	})
	view := drawingPage(wikiDocView{}, scene, getWikiDrawingArgs{View: "outline"})
	if view.Elements[0].Points != nil {
		t.Error("a freehand stroke's points are in the outline")
	}
	if len(view.Elements[1].Points) != 2 {
		t.Error("an arrow's points are missing from the outline")
	}
}

func TestDrawingPage_OffsetPastTheEnd(t *testing.T) {
	view := drawingPage(wikiDocView{}, sceneOf(5, "rectangle", nil), getWikiDrawingArgs{View: "outline", Offset: 9})
	if len(view.Elements) != 0 || len(view.Notes) != 1 || !strings.Contains(view.Notes[0], "past the last shape") {
		t.Errorf("elements %d, notes %v", len(view.Elements), view.Notes)
	}
}
