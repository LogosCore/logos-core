package wiki

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// A drawing page as a file somebody else can open.
//
// `.excalidraw` is Excalidraw's own format, which excalidraw.com and
// Obsidian's Excalidraw plugin read directly. Three callers produce one: the
// markdown export writes it beside each drawing's stub page, the native
// bundle writes it as the human-readable companion to the page's CRDT
// state, and the editor's Export menu hands it to whoever asked. They go
// through here rather than each building their own, because a file that
// differs depending on which button produced it is a bug nobody reports —
// they just find that the diagram they exported is missing a layer, or its
// shapes are stacked in the wrong order.

// ExcalidrawFormat and ExcalidrawVersion are the discriminator and schema
// version Excalidraw writes into a scene file and checks when opening one.
const (
	ExcalidrawFormat  = "excalidraw"
	ExcalidrawVersion = 2
	// ExcalidrawExtension is the suffix Excalidraw gives a scene file.
	ExcalidrawExtension = ".excalidraw"
	// excalidrawSource is the `source` field Excalidraw stamps with the
	// origin that wrote the file. Ours is not a reachable URL and is not
	// meant to be: it names the producer in a field that expects one.
	excalidrawSource = "https://logos.local"
)

// ExcalidrawScene is one drawing's scene as the sidecar reads it out of the
// stored CRDT state: elements already in paint order, the shared slice of
// appState, and the wiki image ids the scene's image elements point at.
//
// Distinct from DrawingScene, which is a read of the *live room* for an
// agent looking at what somebody has on screen. This one is the saved
// bytes, which is what an export must copy — and it carries the two things
// a file needs and a live read does not: the page's appState and the images
// to embed.
//
// Elements stay as raw JSON so the sidecar's bytes reach the file untouched.
// An Excalidraw element carries about twenty-five fields that mean nothing
// to this package, and a struct here would quietly drop whichever of them
// the editor adds next.
type ExcalidrawScene struct {
	Elements []json.RawMessage `json:"elements"`
	AppState map[string]any    `json:"appState"`
	ImageIDs []string          `json:"imageIds"`
}

// ExcalidrawFile is the on-disk shape of a `.excalidraw` file.
type ExcalidrawFile struct {
	Type     string                          `json:"type"`
	Version  int                             `json:"version"`
	Source   string                          `json:"source"`
	Elements []json.RawMessage               `json:"elements"`
	AppState map[string]any                  `json:"appState"`
	Files    map[string]ExcalidrawBinaryFile `json:"files"`
}

// IsEmpty reports whether the file holds a canvas with nothing on it.
func (f ExcalidrawFile) IsEmpty() bool { return len(f.Elements) == 0 }

// ExcalidrawBinaryFile mirrors Excalidraw's BinaryFileData: the bytes of one
// image the scene places, inline.
type ExcalidrawBinaryFile struct {
	MimeType string `json:"mimeType"`
	ID       string `json:"id"`
	DataURL  string `json:"dataURL"`
	Created  int64  `json:"created"`
}

// DrawingImageSkip is one image the scene wanted and did not get, with the
// reason. Exports fold these into their report; the editor's download
// ignores them, because a diagram with one missing picture is still the
// diagram the person asked for.
type DrawingImageSkip struct {
	ImageID string
	Reason  string
}

// SceneReader turns a drawing page's stored Y.js state into a scene.
// Satisfied by *HocuspocusClient — the sidecar wrote those bytes and is the
// only thing that can read them back.
type SceneReader interface {
	YjsToExcalidraw(ctx context.Context, contentState []byte) (ExcalidrawScene, error)
}

// DrawingFileRenderer builds `.excalidraw` files.
type DrawingFileRenderer struct {
	scenes SceneReader
	images repository.IWikiImageRepository
	store  blob.ObjectStore
}

// NewDrawingFileRenderer wires the renderer. images and store may be nil,
// which leaves every scene's `files` map empty: the shapes still arrive, the
// pictures on them do not.
func NewDrawingFileRenderer(scenes SceneReader, images repository.IWikiImageRepository, store blob.ObjectStore) *DrawingFileRenderer {
	return &DrawingFileRenderer{scenes: scenes, images: images, store: store}
}

// DrawingFileOptions varies one render.
type DrawingFileOptions struct {
	// MaxImageBytes caps the image bytes embedded in this one file.
	// 0 means unlimited.
	MaxImageBytes int64
}

// DrawingFileResult reports what embedding the images cost and what it could
// not do.
type DrawingFileResult struct {
	// ImagesEmbedded is how many images' bytes made it into the file.
	ImagesEmbedded int
	// Bytes is the total size of those images, before base64.
	Bytes int64
	// Skips names every image that did not make it, and why.
	Skips []DrawingImageSkip
}

// File renders one drawing page's stored state into an Excalidraw file with
// its images inline.
//
// An error means the scene could not be read at all. Images that cannot be
// read are reported in the result rather than failing the render: a scene
// missing one picture is worth having, and the caller decides whether to say
// so out loud.
func (d *DrawingFileRenderer) File(ctx context.Context, doc models.WikiDocument, state []byte, opts DrawingFileOptions) (ExcalidrawFile, DrawingFileResult, error) {
	file := ExcalidrawFile{
		Type:     ExcalidrawFormat,
		Version:  ExcalidrawVersion,
		Source:   excalidrawSource,
		AppState: map[string]any{},
		Files:    map[string]ExcalidrawBinaryFile{},
	}
	var result DrawingFileResult

	if len(state) == 0 {
		return file, result, nil
	}
	if d.scenes == nil {
		return file, result, fmt.Errorf("no scene reader configured")
	}

	scene, err := d.scenes.YjsToExcalidraw(ctx, state)
	if err != nil {
		return file, result, err
	}
	file.Elements = scene.Elements
	if scene.AppState != nil {
		file.AppState = scene.AppState
	}

	file.Files, result = d.embedImages(ctx, doc.OperationID, scene.ImageIDs, opts)
	return file, result, nil
}

// embedImages resolves the wiki images a scene places on the canvas.
//
// A fileId that is not a uuid belongs to an image Excalidraw minted and the
// editor never adopted, so there is nothing stored to embed: the scene keeps
// the element and loses only its bytes, which is what the canvas already
// shows.
func (d *DrawingFileRenderer) embedImages(ctx context.Context, operationID uuid.UUID, ids []string, opts DrawingFileOptions) (map[string]ExcalidrawBinaryFile, DrawingFileResult) {
	out := map[string]ExcalidrawBinaryFile{}
	var result DrawingFileResult
	if len(ids) == 0 || d.images == nil || d.store == nil {
		return out, result
	}

	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		if err != nil {
			result.Skips = append(result.Skips, DrawingImageSkip{raw, "drawing_image_unadopted"})
			continue
		}
		img, err := d.images.FindByID(ctx, id)
		switch {
		case err != nil:
			result.Skips = append(result.Skips, DrawingImageSkip{raw, "image_not_found"})
			continue
		case img.OperationID != operationID:
			result.Skips = append(result.Skips, DrawingImageSkip{raw, "image_operation_mismatch"})
			continue
		case img.DeletedAt != nil:
			result.Skips = append(result.Skips, DrawingImageSkip{raw, "image_deleted"})
			continue
		}
		if opts.MaxImageBytes > 0 && result.Bytes+img.SizeBytes > opts.MaxImageBytes {
			result.Skips = append(result.Skips, DrawingImageSkip{raw, "attachment_budget_exhausted"})
			continue
		}
		data, err := d.readBlob(ctx, img.ObjectKey)
		if err != nil {
			result.Skips = append(result.Skips, DrawingImageSkip{raw, "blob_get_failed: " + err.Error()})
			continue
		}
		result.Bytes += int64(len(data))
		result.ImagesEmbedded++
		out[raw] = ExcalidrawBinaryFile{
			MimeType: img.ContentType,
			ID:       raw,
			DataURL:  dataURL(img.ContentType, data),
			Created:  img.CreateAt.UnixMilli(),
		}
	}
	return out, result
}

// readBlob pulls one image into memory. Everything else an export copies is
// streamed through to the zip without ever being whole in memory, but a data
// URL has to be built from the complete bytes.
func (d *DrawingFileRenderer) readBlob(ctx context.Context, objectKey string) ([]byte, error) {
	reader, _, err := d.store.Get(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func dataURL(contentType string, data []byte) string {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}
