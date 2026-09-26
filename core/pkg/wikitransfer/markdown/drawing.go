package markdown

import (
	"context"
	"path"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wiki"
)

// A drawing page in a markdown export.
//
// The zip is for tools that know nothing about Logos, and a diagram is the
// one page whose content is not text. It leaves as two files: the scene in
// Excalidraw's own format (built by wiki.DrawingFileRenderer, which the
// bundle and the editor's Export menu also use, so all three produce the
// same file), and a stub `.md` beside it holding the page's title and a
// link to the scene.
//
// The stub is not decoration. Every other page in the export links to its
// neighbours by their `.md` path, so a drawing that shipped as a scene file
// alone would leave a dangling link on every page that cites it — and the
// tree would have a hole where a page used to be. One file keeps the
// diagram editable, the other keeps the wiki navigable.

// drawingExtension is the suffix Excalidraw gives a scene file.
const drawingExtension = wiki.ExcalidrawExtension

func (r *exportRun) writeDrawing(ctx context.Context, doc models.WikiDocument, docPath string, state []byte) {
	defer r.advance()

	scenePath := strings.TrimSuffix(docPath, ".md") + drawingExtension
	var sceneWritten bool

	if len(state) > 0 {
		if r.e.drawings == nil {
			r.report.Skip(docPath, "drawing_render_not_configured")
			return
		}
		file, result, err := r.e.drawings.File(ctx, doc, state, wiki.DrawingFileOptions{
			MaxImageBytes: r.remainingAttachmentBudget(),
		})
		if err != nil {
			r.report.Skip(docPath, "drawing_render_failed: "+err.Error())
			return
		}
		for _, skip := range result.Skips {
			r.report.Warn(docPath, skip.Reason+": "+skip.ImageID)
		}
		r.totalAttachments += result.Bytes
		r.report.ImagesExported += result.ImagesEmbedded

		if !file.IsEmpty() {
			if err := writeJSON(r.zw, scenePath, file); err != nil {
				r.report.Skip(docPath, "zip_write_failed: "+err.Error())
				return
			}
			sceneWritten = true
		}
	}

	body := drawingStubBody(path.Base(scenePath), sceneWritten)
	full := renderDocMarkdown(doc.Emoji, doc.Title, body)
	if err := writeZipFile(r.zw, docPath, []byte(full)); err != nil {
		r.report.Skip(docPath, "zip_write_failed: "+err.Error())
		return
	}
	r.report.ExportedDocs++
}

// remainingAttachmentBudget is what is left of the export's attachment
// allowance, in the form the renderer takes: 0 for unlimited, which is both
// the default and what an exhausted budget must never be reported as.
func (r *exportRun) remainingAttachmentBudget() int64 {
	if r.e.cfg.MaxAttachmentBytes <= 0 {
		return 0
	}
	remaining := r.e.cfg.MaxAttachmentBytes - r.totalAttachments
	if remaining <= 0 {
		// Not zero, which would read as "unlimited": one byte is a budget
		// no image can fit in, which is what an exhausted one means.
		return 1
	}
	return remaining
}

// drawingStubBody is the markdown that stands in for the canvas. It says
// what the page is and where the scene went, because the person who opens
// the zip is looking for a diagram and needs to be told, in the file that
// bears its name, that they have not lost it.
func drawingStubBody(sceneFile string, sceneWritten bool) string {
	if !sceneWritten {
		return "This page is a drawing. Its canvas is empty.\n"
	}
	var b strings.Builder
	b.WriteString("This page is a drawing. The scene is in ")
	b.WriteString("[" + sceneFile + "](" + sceneFile + ")")
	b.WriteString(", which opens in Excalidraw and in Obsidian's Excalidraw plugin.\n")
	return b.String()
}
