package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wiki"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// The drawing half of the wiki tool surface.
//
// Three tools, not eight. Every tool definition is paid for on every turn (see
// the note on add_wiki_section), so the read collapses its variants into a
// `view` argument and the element write collapses add/update/delete/replace
// into a `mode` — the same shape the Markdown tools settled on, for the same
// reason.
//
// Two ways to write. draw_wiki_diagram takes a few lines of diagram text
// (nodes, zones, connections) and the sidecar lays them out and builds the
// shapes; the default read returns the same text. That is the way in for
// every model: no coordinates, no ids to invent, no bindings to wire.
// edit_wiki_drawing takes Excalidraw elements, for exact geometry and for
// anything the diagram language has no word for.
//
// Both go through the sidecar's live room rather than the database, which is
// what lets an agent work on a diagram while the operator has it open: the
// edit merges as a Y.js transaction and appears on their canvas as it lands.
// `watchers` says whether anybody was in fact looking.

type getWikiDrawingArgs struct {
	DocumentID string `json:"document_id" jsonschema:"Page id of a drawing."`
	View       string `json:"view,omitempty" jsonschema:"mermaid (default): the drawing as Mermaid, the format draw_wiki_diagram takes. diagram: the older diagram language. outline: every shape with its id and geometry, a page at a time. full: complete element JSON, a page at a time."`
	Offset     int    `json:"offset,omitempty" jsonschema:"outline and full: skip this many shapes, to read the next page. A paged read says where to continue."`
	Limit      int    `json:"limit,omitempty"  jsonschema:"outline and full: at most this many shapes. Default and maximum 200 for outline, 50 for full."`
}

type drawWikiDiagramArgs struct {
	IdempotencyKey
	DocumentID string `json:"document_id"    jsonschema:"Page id of a drawing."`
	Diagram    string `json:"diagram"        jsonschema:"Mermaid: flowchart, sequenceDiagram, stateDiagram-v2, mindmap, timeline or gantt, with our card kinds as classes; see drawings.md. The older diagram language is still accepted."`
	Mode       string `json:"mode,omitempty" jsonschema:"merge (default): add and change what the text names, leave the rest. replace: clear the canvas first."`
}

type editWikiDrawingArgs struct {
	IdempotencyKey
	DocumentID string `json:"document_id"           jsonschema:"Page id of a drawing."`
	Mode       string `json:"mode,omitempty"        jsonschema:"add (default) to draw new shapes; update to change existing ones by id; delete to erase them; replace to swap the whole scene."`
	// Elements is Excalidraw's own element shape. Every field but `type` is
	// optional — the server fills in the bookkeeping (seed, nonce, group ids)
	// that nobody composing a diagram should have to supply.
	Elements   []map[string]any `json:"elements,omitempty"    jsonschema:"Excalidraw elements; type is required. See drawings.md for label, z, bindings. Not used with mode:delete."`
	ElementIDs []string         `json:"element_ids,omitempty" jsonschema:"Ids to erase, from get_wiki_drawing. Only for mode:delete."`
}

func registerWikiDrawingTools(s *Server) {
	register(s, &mcp.Tool{
		Name: "get_wiki_drawing",
		Description: "Read a drawing page as Mermaid: its nodes, zones and connections as a flowchart, " +
			"each sequence, timeline and gantt as the Mermaid it was drawn from, plus any loose shapes. Read before " +
			"changing one, because changes refer to nodes by the ids this returns. view:\"outline\" lists " +
			"every shape with its geometry instead, for edit_wiki_drawing.",
	}, readTool, handleGetWikiDrawing)

	register(s, &mcp.Tool{
		Name: "draw_wiki_diagram",
		Description: "Draw on a drawing page from Mermaid: flowchart, sequenceDiagram, stateDiagram-v2, " +
			"mindmap, timeline or gantt. Our card kinds, badges and colours are classes (:::server). The " +
			"server lays it out and draws it; nothing already on the canvas moves. Re-sending a node by " +
			"id changes it; re-sending a sequence, timeline or gantt with the same title redraws it in place.",
	}, writeTool, handleDrawWikiDiagram)

	register(s, &mcp.Tool{
		Name: "edit_wiki_drawing",
		Description: "Change a drawing's shapes as Excalidraw elements, for exact geometry or shapes the " +
			"diagram language has no word for; draw_wiki_diagram is simpler for nodes and connections. " +
			"Adds by default; mode:update changes shapes by id, mode:delete erases them, mode:replace " +
			"swaps the whole canvas. Safe while the operator is drawing.",
	}, writeTool, handleEditWikiDrawing)
}

// drawingElementView is one shape, flattened to what an agent needs in order
// to describe or edit it.
//
// The full element carries about twenty-five fields of rendering bookkeeping.
// Returning all of them for every shape turns a thirty-box diagram into tens
// of kilobytes per read, most of it seeds and nonces that mean nothing to the
// reader — so the default read is this, and `view:"full"` is there for the
// cases that genuinely need the rest.
type drawingElementView struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
	// Points is where a line actually goes, for arrows and lines. Carried in
	// the default view because without it two arrows drawn on top of each
	// other are indistinguishable from two arrows side by side: the ids
	// differ, the geometry does not, and the canvas shows one stroke.
	Points []any `json:"points,omitempty"`
	// Bindings name the shapes an arrow is attached to. An arrow can be bound
	// correctly and still drawn in the wrong place, so both matter.
	StartBinding string `json:"startBinding,omitempty"`
	EndBinding   string `json:"endBinding,omitempty"`
	// Text is the shape's words, for a text element. It is the single most
	// useful field for working out what a diagram says.
	Text string `json:"text,omitempty"`
	// Z is the element's layer. Reported so the order of the list is
	// explainable: the elements come back back-to-front, and this says why.
	Z float64 `json:"z,omitempty"`
	// ContainerID is set on a label bound to a shape, which is how Excalidraw
	// models "a box with writing in it" — without it an agent cannot tell a
	// caption apart from a free-floating note.
	ContainerID string `json:"containerId,omitempty"`
}

type drawingSceneView struct {
	wikiDocView
	// Elements come back in paint order: back first, front last.
	Elements []drawingElementView `json:"elements,omitempty"`
	// Full is populated only for view:"full".
	Full []map[string]any `json:"fullElements,omitempty"`
	// Shapes counts the whole scene by element type, when a read returns
	// only a page of it.
	Shapes map[string]int `json:"shapes,omitempty"`
	Count  int            `json:"elementCount"`
	// NextOffset is where the next page starts, when there is one.
	NextOffset int      `json:"nextOffset,omitempty"`
	Watchers   int      `json:"watchers,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

// Page sizes for the element views.
//
// Both used to be unbounded in practice. Over 120 shapes the outline turned
// into a summary of labels and counts, with no ids — the one thing an agent
// reads it for, and what drawings.md sends it there to get — and its note said
// to ask for view:"full", which returned every element whole: seeds, nonces
// and every point of every freehand stroke, megabytes on a busy canvas.
//
// Now each is a page. An outline row is small, so pages are large; a full
// element can be large, so pages are small and also stop at a byte budget.
const (
	outlinePageSize = 200
	fullPageSize    = 50
	fullPageBytes   = 256 << 10
)

func handleGetWikiDrawing(ctx context.Context, s *Server, args getWikiDrawingArgs) (toolResult, error) {
	doc, err := s.loadWikiDocument(ctx, args.DocumentID, models.OperationRoleViewer)
	if err != nil {
		return toolResult{}, err
	}
	if err := requireDrawing(doc, "read"); err != nil {
		return toolResult{}, err
	}
	if s.deps.Hocuspocus == nil {
		return toolResult{}, fmt.Errorf("reading drawings is unavailable: the collaboration service is not configured")
	}

	switch args.View {
	case "", "mermaid", "diagram":
		return readWikiDiagram(ctx, s, doc, args.View == "diagram")
	case "outline", "full":
	default:
		return toolResult{}, refuse("unknown view %q. Use mermaid (the default), diagram, outline or full.", args.View)
	}

	scene, err := s.deps.Hocuspocus.ReadDrawing(ctx, doc.DocumentID.String())
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to read wiki drawing",
			zap.String("document_id", doc.DocumentID.String()), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to read the drawing: %w", err)
	}

	return toolResult{
		Payload:     drawingPage(toWikiDocView(doc), scene, args),
		OperationID: &doc.OperationID,
		Summary:     fmt.Sprintf("read wiki drawing %s", doc.Title),
	}, nil
}

// drawingPage is one page of a drawing's elements, as the outline or the
// full view, with a note saying where the next page starts.
func drawingPage(doc wikiDocView, scene wiki.DrawingScene, args getWikiDrawingArgs) drawingSceneView {
	total := len(scene.Elements)
	view := drawingSceneView{wikiDocView: doc, Count: total, Watchers: scene.Watchers}
	if total == 0 {
		view.Notes = append(view.Notes, "This canvas is empty.")
		return view
	}

	full := args.View == "full"
	pageSize := outlinePageSize
	if full {
		pageSize = fullPageSize
	}
	limit := args.Limit
	if limit <= 0 || limit > pageSize {
		limit = pageSize
	}
	offset := max(args.Offset, 0)
	if offset >= total {
		view.Notes = append(view.Notes, fmt.Sprintf(
			"offset %d is past the last shape; this canvas has %d.", offset, total))
		return view
	}

	end := min(offset+limit, total)
	if full {
		budget := 0
		view.Full = make([]map[string]any, 0, end-offset)
		for i := offset; i < end; i++ {
			size := approxJSONSize(scene.Elements[i])
			// Always at least one, or one oversized stroke would stall the
			// reader on the same offset for ever.
			if len(view.Full) > 0 && budget+size > fullPageBytes {
				end = i
				break
			}
			budget += size
			view.Full = append(view.Full, scene.Elements[i])
		}
	} else {
		view.Elements = make([]drawingElementView, 0, end-offset)
		for _, el := range scene.Elements[offset:end] {
			view.Elements = append(view.Elements, summariseElement(el))
		}
	}

	if offset > 0 || end < total {
		view.Shapes = map[string]int{}
		for _, el := range scene.Elements {
			view.Shapes[stringField(el, "type")]++
		}
	}
	if end < total {
		view.NextOffset = end
		view.Notes = append(view.Notes, fmt.Sprintf(
			"Shapes %d–%d of %d, back to front. Read on with offset:%d.", offset+1, end, total, end))
	}
	return view
}

// approxJSONSize is an element's encoded size, for the full view's budget.
func approxJSONSize(el wiki.DrawingElement) int {
	b, err := json.Marshal(el)
	if err != nil {
		return 0
	}
	return len(b)
}

func summariseElement(el wiki.DrawingElement) drawingElementView {
	kind := stringField(el, "type")
	// Points only where they say where a line goes. A freehand stroke carries
	// hundreds, which its box describes well enough for an outline.
	var points []any
	if kind == "arrow" || kind == "line" {
		points = sliceField(el, "points")
	}
	return drawingElementView{
		ID:          stringField(el, "id"),
		Type:        kind,
		X:           floatField(el, "x"),
		Y:           floatField(el, "y"),
		Width:       floatField(el, "width"),
		Height:      floatField(el, "height"),
		Text:        stringField(el, "text"),
		Z:           floatField(el, "z"),
		ContainerID: stringField(el, "containerId"),

		Points:       points,
		StartBinding: bindingTarget(el, "startBinding"),
		EndBinding:   bindingTarget(el, "endBinding"),
	}
}

func sliceField(el wiki.DrawingElement, key string) []any {
	if v, ok := el[key].([]any); ok {
		return v
	}
	return nil
}

// bindingTarget pulls the bound shape's id out of Excalidraw's binding object,
// which also carries focus and gap — geometry the server maintains and nobody
// composing a diagram needs to see.
func bindingTarget(el wiki.DrawingElement, key string) string {
	binding, ok := el[key].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := binding["elementId"].(string)
	return id
}

func stringField(el wiki.DrawingElement, key string) string {
	if v, ok := el[key].(string); ok {
		return v
	}
	return ""
}

func floatField(el wiki.DrawingElement, key string) float64 {
	if v, ok := el[key].(float64); ok {
		return v
	}
	return 0
}

type drawingWriteView struct {
	wikiDocView
	Mode     string   `json:"mode"`
	Applied  int      `json:"applied"`
	Watchers int      `json:"watchers,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

func handleEditWikiDrawing(ctx context.Context, s *Server, args editWikiDrawingArgs) (toolResult, error) {
	doc, err := s.loadWikiDocument(ctx, args.DocumentID, models.OperationRoleOperator)
	if err != nil {
		return toolResult{}, err
	}
	if err := requireDrawing(doc, "draw on"); err != nil {
		return toolResult{}, err
	}
	if s.deps.Hocuspocus == nil {
		return toolResult{}, fmt.Errorf("drawing is unavailable: the collaboration service is not configured")
	}

	mode := wiki.DrawingMode(args.Mode)
	if args.Mode == "" {
		mode = wiki.DrawingAdd
	}
	switch mode {
	case wiki.DrawingAdd, wiki.DrawingUpdate, wiki.DrawingDelete, wiki.DrawingReplace:
	default:
		return toolResult{}, refuse(
			"unknown mode %q. Use add, update, delete or replace.", args.Mode)
	}

	if mode == wiki.DrawingDelete && len(args.ElementIDs) == 0 {
		return toolResult{}, refuse(
			"mode:delete needs element_ids — the ids of the shapes to erase, as get_wiki_drawing reports them.")
	}
	if mode != wiki.DrawingDelete && len(args.Elements) == 0 {
		return toolResult{}, refuse(
			"elements is required: each one needs at least a type (rectangle, ellipse, diamond, " +
				"text, arrow, line, freedraw, image or frame). Everything else is optional.")
	}

	elements := make([]wiki.DrawingElement, 0, len(args.Elements))
	for _, el := range args.Elements {
		elements = append(elements, wiki.DrawingElement(el))
	}

	// Attributed to the key's owner, not to the key: a page records which
	// person last changed it, and an agent acts for one.
	result, err := s.deps.Hocuspocus.ApplyDrawing(
		ctx, doc.DocumentID.String(), mode, elements, args.ElementIDs, viewerID(ctx))
	if errors.Is(err, wiki.ErrDrawingElementsInvalid) {
		// A refusal, not a fault: the request was understood and wrong, and
		// the sidecar's message names the offending element.
		return toolResult{}, refuse("%s", err.Error())
	}
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to apply wiki drawing edit",
			zap.String("document_id", doc.DocumentID.String()),
			zap.String("mode", string(mode)), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to save the drawing: %w", err)
	}

	view := drawingWriteView{
		wikiDocView: toWikiDocView(doc),
		Mode:        string(mode),
		Applied:     result.Applied,
		Watchers:    result.Watchers,
	}
	if result.Warning != "" {
		view.Notes = append(view.Notes, result.Warning)
	}
	if result.Watchers > 0 {
		view.Notes = append(view.Notes, "The operator saw this edit.")
	}
	// An update or delete that matched nothing is the one outcome that looks
	// like success and is not: the usual cause is ids from a read taken before
	// somebody else changed the canvas.
	if result.Applied == 0 && (mode == wiki.DrawingUpdate || mode == wiki.DrawingDelete) {
		view.Notes = append(view.Notes,
			"No shape on the canvas matched those ids, so nothing changed. "+
				"Read the drawing again — it may have been edited since.")
	} else if len(result.Missing) > 0 {
		// Some applied and some did not: the count alone reads as a success.
		view.Notes = append(view.Notes, fmt.Sprintf(
			"%d of those ids matched no shape on the canvas — never there, or erased since — "+
				"and were skipped: %s. Read the drawing again for current ids.",
			len(result.Missing), strings.Join(result.Missing, ", ")))
	}

	return toolResult{
		Payload:     view,
		OperationID: &doc.OperationID,
		Summary:     fmt.Sprintf("edited wiki drawing %s", doc.Title),
	}, nil
}

// --- Diagrams -----------------------------------------------------------

type drawingDiagramView struct {
	wikiDocView
	// Mermaid is the drawing's nodes, zones and connections as a flowchart,
	// in the form draw_wiki_diagram takes.
	Mermaid string `json:"mermaid,omitempty"`
	// Diagrams are its sequence, timeline and gantt diagrams, each as the
	// Mermaid it was drawn from: send one back changed, same title, to
	// redraw it in place.
	Diagrams []wiki.FixedDiagram `json:"diagrams,omitempty"`
	// Sequences are its sequence diagrams alone, under the name skills
	// before v46 know. Each is the Mermaid it was drawn
	// from: send one back changed, same title, to redraw it in place.
	Sequences []wiki.DiagramSequence `json:"sequences,omitempty"`
	// Diagram is the same graph in the older diagram language, on request.
	Diagram string `json:"diagram,omitempty"`
	Nodes   int    `json:"nodes"`
	Edges   int    `json:"connections"`
	Zones   int    `json:"zones,omitempty"`
	// Others are shapes outside the diagram: free text, freehand, loose
	// arrows. Listed with ids for edit_wiki_drawing.
	Others   []wiki.DiagramOther `json:"otherShapes,omitempty"`
	Watchers int                 `json:"watchers,omitempty"`
	Notes    []string            `json:"notes,omitempty"`
}

func diagramView(doc *models.WikiDocument, d wiki.DrawingDiagram, older bool) drawingDiagramView {
	view := drawingDiagramView{
		wikiDocView: toWikiDocView(doc),
		Mermaid:     d.Mermaid,
		Diagrams:    d.Diagrams,
		Sequences:   d.Sequences,
		Nodes:       d.Nodes,
		Edges:       d.Edges,
		Zones:       d.Zones,
		Others:      d.Others,
		Watchers:    d.Watchers,
	}
	if older {
		view.Mermaid = ""
		view.Diagrams = nil
		view.Sequences = nil
		view.Diagram = d.Diagram
	}
	if d.Nodes == 0 && d.Zones == 0 && d.OthersTotal == 0 && len(d.Diagrams) == 0 {
		view.Notes = append(view.Notes, "This canvas is empty.")
	}
	if d.OthersTotal > 0 {
		view.Notes = append(view.Notes,
			"otherShapes are on the canvas but not part of the diagram (free text, freehand, loose arrows). "+
				"draw_wiki_diagram leaves them alone; edit_wiki_drawing changes them by id.")
	}
	if more := d.OthersTotal - len(d.Others); more > 0 {
		view.Notes = append(view.Notes, fmt.Sprintf(
			"%d more loose shapes are not listed; view:\"outline\" lists every shape.", more))
	}
	return view
}

func readWikiDiagram(ctx context.Context, s *Server, doc *models.WikiDocument, older bool) (toolResult, error) {
	d, err := s.deps.Hocuspocus.ReadDrawingDiagram(ctx, doc.DocumentID.String())
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to read wiki drawing as a diagram",
			zap.String("document_id", doc.DocumentID.String()), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to read the drawing: %w", err)
	}
	return toolResult{
		Payload:     diagramView(doc, d, older),
		OperationID: &doc.OperationID,
		Summary:     fmt.Sprintf("read wiki drawing %s", doc.Title),
	}, nil
}

type diagramWriteView struct {
	drawingDiagramView
	Mode string `json:"mode"`
	// Kind is what was drawn: flowchart, sequence, state, mindmap,
	// timeline, gantt or diagram.
	Kind         string `json:"kind,omitempty"`
	Sequence     string `json:"sequence,omitempty"`
	Participants int    `json:"participants,omitempty"`
	Periods      int    `json:"periods,omitempty"`
	Events       int    `json:"events,omitempty"`
	Tasks        int    `json:"tasks,omitempty"`
	Messages     int    `json:"messages,omitempty"`
	Created      int    `json:"created"`
	Updated      int    `json:"updated"`
	Removed      int    `json:"removed,omitempty"`
	Connected    int    `json:"connected"`
}

func handleDrawWikiDiagram(ctx context.Context, s *Server, args drawWikiDiagramArgs) (toolResult, error) {
	doc, err := s.loadWikiDocument(ctx, args.DocumentID, models.OperationRoleOperator)
	if err != nil {
		return toolResult{}, err
	}
	if err := requireDrawing(doc, "draw on"); err != nil {
		return toolResult{}, err
	}
	if s.deps.Hocuspocus == nil {
		return toolResult{}, fmt.Errorf("drawing is unavailable: the collaboration service is not configured")
	}

	mode := wiki.DiagramMode(args.Mode)
	if args.Mode == "" {
		mode = wiki.DiagramMerge
	}
	if mode != wiki.DiagramMerge && mode != wiki.DiagramReplace {
		return toolResult{}, refuse("unknown mode %q. Use merge (the default) or replace.", args.Mode)
	}
	if strings.TrimSpace(args.Diagram) == "" {
		return toolResult{}, refuse(
			"diagram is required: Mermaid, for example\n" +
				"flowchart LR\n" +
				"  ws01[\"WS01\"]:::workstation -->|RDP| dc01[\"DC01<br/>10.0.5.10\"]:::domain-controller")
	}

	// Attributed to the key's owner, not to the key: a page records which
	// person last changed it, and an agent acts for one.
	result, err := s.deps.Hocuspocus.ApplyDrawingDiagram(ctx, doc.DocumentID.String(), args.Diagram, mode, viewerID(ctx))
	if errors.Is(err, wiki.ErrDrawingDiagramInvalid) {
		// Every problem at once, with its line: one round trip fixes them all.
		// Nothing was drawn.
		return toolResult{}, refuse("%s\nNothing was drawn. Fix these and send the whole text again.", err.Error())
	}
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to apply wiki diagram",
			zap.String("document_id", doc.DocumentID.String()), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to save the drawing: %w", err)
	}

	view := diagramWriteView{
		drawingDiagramView: diagramView(doc, result.DrawingDiagram, result.Kind == "diagram"),
		Mode:               string(mode),
		Kind:               result.Kind,
		Sequence:           result.Sequence,
		Participants:       result.Participants,
		Periods:            result.Periods,
		Events:             result.Events,
		Tasks:              result.Tasks,
		Messages:           result.Messages,
		Created:            result.Created,
		Updated:            result.Updated,
		Removed:            result.Removed,
		Connected:          result.Connected,
	}
	view.Notes = append(result.Warnings, view.Notes...)
	if result.Watchers > 0 {
		view.Notes = append(view.Notes, "The operator saw this edit.")
	}
	return toolResult{
		Payload:     view,
		OperationID: &doc.OperationID,
		Summary:     fmt.Sprintf("drew on wiki drawing %s", doc.Title),
	}, nil
}
