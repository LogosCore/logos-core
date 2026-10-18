package wiki

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Drawing scenes, read and written through the sidecar.
//
// Both directions go through the live Y.js room rather than through Mongo, for
// the same reason ApplyMarkdown does: a connected canvas holds the
// authoritative document in memory and writes it back on the next debounce, so
// a direct row write is either erased seconds later or lands mid-stroke.
// Reading through the room additionally means an agent asked to look at a
// diagram somebody is drawing sees what is on their screen, not what was last
// flushed.

// DrawingMode is what a write does to the scene.
type DrawingMode string

const (
	// DrawingAdd inserts elements, leaving everything already there alone.
	DrawingAdd DrawingMode = "add"
	// DrawingUpdate merges fields into existing elements by id. An id that is
	// not on the canvas is skipped rather than inserted — the usual cause is a
	// stale read, and inventing the element would hide that.
	DrawingUpdate DrawingMode = "update"
	// DrawingDelete tombstones elements by id.
	DrawingDelete DrawingMode = "delete"
	// DrawingReplace tombstones the whole scene and writes a new one, in a
	// single transaction so collaborators never see the canvas empty.
	DrawingReplace DrawingMode = "replace"
)

// ErrDrawingElementsInvalid is a caller error: the elements were understood
// and rejected. Kept distinct from a transport failure so the MCP layer can
// refuse (a policy answer) rather than report a fault.
var ErrDrawingElementsInvalid = errors.New("invalid drawing elements")

// DrawingElement is one Excalidraw element, carried opaquely.
//
// Deliberately untyped: the element schema belongs to Excalidraw, changes with
// it, and nothing in Go reads a field of one. Mirroring it here would be a
// second definition to keep in step for no benefit — the sidecar normalizes
// and validates, because that is where the schema already lives.
type DrawingElement map[string]any

type drawingReadRequest struct {
	DocumentID string `json:"documentId"`
}

// DrawingScene is the live scene, with deleted elements already filtered out.
type DrawingScene struct {
	Elements []DrawingElement `json:"elements"`
	Watchers int              `json:"watchers"`
}

type drawingApplyRequest struct {
	DocumentID string           `json:"documentId"`
	Mode       DrawingMode      `json:"mode"`
	Elements   []DrawingElement `json:"elements,omitempty"`
	ElementIDs []string         `json:"elementIds,omitempty"`
	UserID     string           `json:"userId,omitempty"`
}

// ApplyDrawingResult reports what the sidecar did. Watchers is how many people
// had the page open — the difference between an edit somebody watched appear
// and one that happened quietly.
type ApplyDrawingResult struct {
	Applied  int `json:"applied"`
	Watchers int `json:"watchers"`
	// Warning describes something the write did that succeeded and is probably
	// not what was meant — shapes stacked exactly on top of one another, most
	// often. Empty when there is nothing to say.
	Warning string `json:"warning,omitempty"`
	// Missing is the ids an update or delete named that match no live shape:
	// never there, or already erased.
	Missing []string `json:"missing,omitempty"`
}

// ReadDrawing returns the scene as it stands in the live room.
func (c *HocuspocusClient) ReadDrawing(ctx context.Context, documentID string) (DrawingScene, error) {
	var scene DrawingScene

	body, err := json.Marshal(drawingReadRequest{DocumentID: documentID})
	if err != nil {
		return scene, fmt.Errorf("marshal drawing read payload: %w", err)
	}

	resp, err := c.postInternal(ctx, "/internal/drawing/read", body)
	if err != nil {
		return scene, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return scene, fmt.Errorf("drawing read returned %d: %s", resp.StatusCode, string(errBody))
	}

	// A scene is bounded by the sidecar's per-call element cap, but each
	// element is a couple of hundred bytes of geometry, so the envelope is
	// generous rather than tight.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&scene); err != nil {
		return scene, fmt.Errorf("read drawing response: %w", err)
	}
	return scene, nil
}

// ApplyDrawing edits a drawing as a Y.js transaction on the live document.
func (c *HocuspocusClient) ApplyDrawing(
	ctx context.Context,
	documentID string,
	mode DrawingMode,
	elements []DrawingElement,
	elementIDs []string,
	userID string,
) (ApplyDrawingResult, error) {
	var result ApplyDrawingResult

	switch mode {
	case DrawingAdd, DrawingUpdate, DrawingDelete, DrawingReplace:
	default:
		return result, fmt.Errorf("apply-drawing: unknown mode %q", mode)
	}

	body, err := json.Marshal(drawingApplyRequest{
		DocumentID: documentID,
		Mode:       mode,
		Elements:   elements,
		ElementIDs: elementIDs,
		UserID:     userID,
	})
	if err != nil {
		return result, fmt.Errorf("marshal drawing apply payload: %w", err)
	}

	resp, err := c.postInternal(ctx, "/internal/drawing/apply", body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	// 422 is the sidecar saying the elements themselves are wrong, with the
	// offending entry named. That is the agent's to fix, so the message is
	// carried through rather than flattened into "apply failed".
	if resp.StatusCode == http.StatusUnprocessableEntity {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&payload)
		return result, fmt.Errorf("%w: %s", ErrDrawingElementsInvalid, payload.Error)
	}
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("apply-drawing returned %d: %s", resp.StatusCode, string(errBody))
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return result, fmt.Errorf("read apply-drawing response: %w", err)
	}
	return result, nil
}

// postInternal signs a body with the shared secret and posts it to one of the
// sidecar's internal routes. The signature is over the exact bytes sent, which
// is why those routes read the raw body before any JSON middleware.
func (c *HocuspocusClient) postInternal(ctx context.Context, path string, body []byte) (*http.Response, error) {
	if c.internalSecret == "" {
		return nil, fmt.Errorf("%s: no internal secret configured", path)
	}

	mac := hmac.New(sha256.New, []byte(c.internalSecret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Signature-256", signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call hocuspocus %s: %w", path, err)
	}
	return resp, nil
}

// RebaseDrawingRequest rebases a drawing's scene onto the target's ids.
type RebaseDrawingRequest struct {
	ContentState []byte
	// IDMap maps every source attachment id the scene may reference to the id
	// it must carry on the target. An image id absent from the map comes back
	// in Unmapped rather than failing the import.
	IDMap map[string]string
}

// RebaseDrawingResult is fresh scene state plus the projection the Go backend
// needs to create the page fully indexed in one write — the drawing analogue
// of RebaseResult.
type RebaseDrawingResult struct {
	ContentState    []byte
	Content         string   `json:"content"`
	ImageReferences []string `json:"imageReferences"`
	ElementCount    int      `json:"elementCount"`
	VersionSum      int      `json:"versionSum"`
	// Unmapped lists image ids the scene references that the importer did not
	// ingest. The page still imports; those pictures are missing from it.
	Unmapped []string `json:"unmapped"`
	Remapped int      `json:"remapped"`
}

type rebaseDrawingWireRequest struct {
	ContentState string            `json:"contentState"`
	IDMap        map[string]string `json:"idMap"`
}

type rebaseDrawingWireResponse struct {
	ContentState string `json:"contentState"`
	RebaseDrawingResult
}

// RebaseDrawing rewrites a scene's attachment ids for the target installation.
//
// Without it an imported drawing's images point at blobs in the source
// installation: every picture renders as an empty frame, and the attachment
// index records references to things that are not there. The scene itself is
// carried verbatim — it is the ids that have to move, not the shapes.
func (c *HocuspocusClient) RebaseDrawing(ctx context.Context, req RebaseDrawingRequest) (RebaseDrawingResult, error) {
	var result RebaseDrawingResult

	idMap := req.IDMap
	if idMap == nil {
		idMap = map[string]string{}
	}

	body, err := json.Marshal(rebaseDrawingWireRequest{
		ContentState: base64.StdEncoding.EncodeToString(req.ContentState),
		IDMap:        idMap,
	})
	if err != nil {
		return result, fmt.Errorf("marshal rebase-drawing payload: %w", err)
	}

	resp, err := c.postInternal(ctx, "/internal/rebase-drawing", body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("rebase-drawing returned %d: %s", resp.StatusCode, string(errBody))
	}

	var wire rebaseDrawingWireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&wire); err != nil {
		return result, fmt.Errorf("read rebase-drawing response: %w", err)
	}

	state, err := base64.StdEncoding.DecodeString(wire.ContentState)
	if err != nil {
		return result, fmt.Errorf("decode rebased scene: %w", err)
	}

	result = wire.RebaseDrawingResult
	result.ContentState = state
	return result, nil
}

type drawingRestoreRequest struct {
	DocumentID   string `json:"documentId"`
	ContentState string `json:"contentState"`
	UserID       string `json:"userId,omitempty"`
}

// RestoreDrawingResult reports what a restore did to the live scene.
type RestoreDrawingResult struct {
	// Restored is how many elements were put back to their backup copy.
	Restored int `json:"restored"`
	// Removed is how many live elements were erased because the backup did
	// not have them.
	Removed  int `json:"removed"`
	Watchers int `json:"watchers"`
	// PreviousState is the room as it stood immediately before the restore,
	// and PreviousContent its search text: together, the pre-restore safety
	// backup. Read from the room because the Mongo row can be a debounce
	// window behind what was on screen.
	PreviousState   []byte `json:"-"`
	PreviousContent string `json:"previousContent"`
	// PreviousImageReferences and PreviousFileReferences are the attachments
	// that pre-restore page used, for indexing the safety backup.
	PreviousImageReferences []string `json:"previousImageReferences"`
	PreviousFileReferences  []string `json:"previousFileReferences"`
}

type drawingRestoreWireResponse struct {
	RestoreDrawingResult
	PreviousState string `json:"previousState"`
}

// RestoreDrawing puts a backup's scene back on a drawing page, as a
// transaction in the live room.
//
// Not a write over content_state: a loaded room overwrites that on its next
// store, and a browser that reconnects holding newer state merges every shape
// drawn since the backup straight back in. The sidecar instead writes the
// backup's elements at versions above what every canvas holds, so the restore
// lands on screen and persists like any other edit.
func (c *HocuspocusClient) RestoreDrawing(ctx context.Context, documentID string, contentState []byte, userID string) (RestoreDrawingResult, error) {
	var result RestoreDrawingResult

	body, err := json.Marshal(drawingRestoreRequest{
		DocumentID:   documentID,
		ContentState: base64.StdEncoding.EncodeToString(contentState),
		UserID:       userID,
	})
	if err != nil {
		return result, fmt.Errorf("marshal drawing restore payload: %w", err)
	}

	resp, err := c.postInternal(ctx, "/internal/drawing/restore", body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("drawing restore returned %d: %s", resp.StatusCode, string(errBody))
	}

	// The previous state is a whole scene, base64'd, so the response is about
	// 4/3 of the request the sidecar accepts (16 MB): the same 32 MB bound as
	// RestoreDocument. At 16 MB a large drawing's restore was applied in the
	// room and then reported as failed here.
	var wire drawingRestoreWireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&wire); err != nil {
		return result, fmt.Errorf("read drawing restore response: %w", err)
	}
	result = wire.RestoreDrawingResult
	if result.PreviousState, err = base64.StdEncoding.DecodeString(wire.PreviousState); err != nil {
		return result, fmt.Errorf("decode drawing restore previous state: %w", err)
	}
	return result, nil
}

// Diagrams: a drawing read and written as text (hocuspocus/src/diagram-*.ts).
//
// The element tools above ask an agent for Excalidraw's own JSON: coordinates,
// ids and bindings. A diagram asks for nodes, zones and connections in a few
// lines, and the sidecar lays them out, builds the cards and binds the arrows.
// Reading returns the same text, so an agent can change a line and send it
// back.

// DiagramMode is what a diagram write does to what is already drawn.
type DiagramMode string

const (
	// DiagramMerge adds and changes what the text names and leaves
	// everything else on the canvas alone.
	DiagramMerge DiagramMode = "merge"
	// DiagramReplace clears the canvas first.
	DiagramReplace DiagramMode = "replace"
)

// ErrDrawingDiagramInvalid is a caller error: the diagram text was understood
// and rejected. Problems carries each one with its line.
var ErrDrawingDiagramInvalid = errors.New("invalid diagram")

// DiagramProblem is one thing wrong with a diagram text.
type DiagramProblem struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// DiagramInvalidError carries the sidecar's problems through to the caller.
// It wraps ErrDrawingDiagramInvalid for errors.Is.
type DiagramInvalidError struct {
	Problems []DiagramProblem
}

func (e *DiagramInvalidError) Error() string {
	lines := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		lines = append(lines, fmt.Sprintf("line %d: %s", p.Line, p.Message))
	}
	return fmt.Sprintf("%s:\n%s", ErrDrawingDiagramInvalid.Error(), strings.Join(lines, "\n"))
}

func (e *DiagramInvalidError) Unwrap() error { return ErrDrawingDiagramInvalid }

// DiagramOther is a shape outside the diagram: free text, a freehand stroke,
// an arrow with a loose end. Listed so a read mentions everything on the
// canvas, with the id the element tools take.
type DiagramOther struct {
	ID   string  `json:"id"`
	Type string  `json:"type"`
	Text string  `json:"text,omitempty"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// FixedDiagram is a diagram drawn whole from its Mermaid (a sequence,
// timeline or gantt), as that Mermaid. Sending it back changed, with the same
// title, redraws it in place.
type FixedDiagram struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Mermaid string `json:"mermaid"`
}

// DiagramSequence is a sequence diagram on a drawing, as the Mermaid it was
// drawn from.
type DiagramSequence struct {
	ID      string `json:"id"`
	Mermaid string `json:"mermaid"`
}

// DrawingDiagram is a drawing read as diagrams: its nodes, zones and
// connections as a Mermaid flowchart (and in the older diagram language), and
// each sequence diagram as its Mermaid.
type DrawingDiagram struct {
	Mermaid     string            `json:"mermaid"`
	Diagrams    []FixedDiagram    `json:"diagrams"`
	Sequences   []DiagramSequence `json:"sequences"`
	Diagram     string            `json:"diagram"`
	Nodes       int               `json:"nodes"`
	Edges       int               `json:"edges"`
	Zones       int               `json:"zones"`
	Others      []DiagramOther    `json:"others"`
	OthersTotal int               `json:"othersTotal"`
	Watchers    int               `json:"watchers"`
}

// ApplyDiagramResult is what a diagram write did, and the drawing after it.
type ApplyDiagramResult struct {
	DrawingDiagram
	// Kind is what was drawn: flowchart, sequence, state, mindmap,
	// timeline, gantt, or diagram (the older language).
	Kind string `json:"kind"`
	// Sequence is the id of a fixed diagram drawn (sequence, timeline,
	// gantt); the counts describe it.
	Sequence     string   `json:"sequence,omitempty"`
	Participants int      `json:"participants,omitempty"`
	Messages     int      `json:"messages,omitempty"`
	Periods      int      `json:"periods,omitempty"`
	Events       int      `json:"events,omitempty"`
	Tasks        int      `json:"tasks,omitempty"`
	Created      int      `json:"created"`
	Updated      int      `json:"updated"`
	Removed      int      `json:"removed"`
	Connected    int      `json:"connected"`
	Warnings     []string `json:"warnings"`
}

// ReadDrawingDiagram returns the drawing as diagram text.
func (c *HocuspocusClient) ReadDrawingDiagram(ctx context.Context, documentID string) (DrawingDiagram, error) {
	var out DrawingDiagram
	body, err := json.Marshal(drawingReadRequest{DocumentID: documentID})
	if err != nil {
		return out, fmt.Errorf("marshal diagram read payload: %w", err)
	}
	resp, err := c.postInternal(ctx, "/internal/drawing/diagram/read", body)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("diagram read returned %d: %s", resp.StatusCode, string(errBody))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return out, fmt.Errorf("read diagram response: %w", err)
	}
	return out, nil
}

type diagramApplyRequest struct {
	DocumentID string      `json:"documentId"`
	Diagram    string      `json:"diagram"`
	Mode       DiagramMode `json:"mode"`
	UserID     string      `json:"userId,omitempty"`
}

// ApplyDrawingDiagram writes a diagram to the drawing, in the live room.
func (c *HocuspocusClient) ApplyDrawingDiagram(
	ctx context.Context,
	documentID string,
	diagram string,
	mode DiagramMode,
	userID string,
) (ApplyDiagramResult, error) {
	var out ApplyDiagramResult
	switch mode {
	case DiagramMerge, DiagramReplace:
	default:
		return out, fmt.Errorf("apply-diagram: unknown mode %q", mode)
	}
	body, err := json.Marshal(diagramApplyRequest{DocumentID: documentID, Diagram: diagram, Mode: mode, UserID: userID})
	if err != nil {
		return out, fmt.Errorf("marshal diagram apply payload: %w", err)
	}
	resp, err := c.postInternal(ctx, "/internal/drawing/diagram/apply", body)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	// 422: the text is wrong, and every problem is named with its line. That
	// is the agent's to fix, so the problems are carried through whole.
	if resp.StatusCode == http.StatusUnprocessableEntity {
		var payload struct {
			Error    string           `json:"error"`
			Problems []DiagramProblem `json:"problems"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
		if len(payload.Problems) == 0 {
			payload.Problems = []DiagramProblem{{Message: payload.Error}}
		}
		return out, &DiagramInvalidError{Problems: payload.Problems}
	}
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("apply-diagram returned %d: %s", resp.StatusCode, string(errBody))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return out, fmt.Errorf("read apply-diagram response: %w", err)
	}
	return out, nil
}
