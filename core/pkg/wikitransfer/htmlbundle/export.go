// Package htmlbundle exports a wiki tree as a static site: a zip somebody
// unpacks and opens index.html from, with no server, no network and no
// Logos account, and that still looks like the wiki.
//
// The archive is a prebuilt viewer (frontend/src/viewer, embedded here at
// build time) plus the data it renders:
//
//	<root>/index.html             the viewer shell
//	<root>/assets/viewer.{js,css} the viewer, the app's own styles inlined
//	<root>/assets/drawing.js      Excalidraw's SVG export, loaded on demand
//	<root>/assets/simple-icons.js brand icons, loaded on demand
//	<root>/data/tree.js           tree, titles, icons, search text
//	<root>/data/pages/<id>.js     one page body each
//	<root>/images/<id>.<ext>      image attachments
//	<root>/files/<id>/<name>      file attachments
//	<root>/data/files/<id>.js     a previewable file's bytes, for the viewer
//	<root>/REPORT.json            what was left out, and why
//
// Data travels as scripts, not JSON files, because a page opened from disk
// may not fetch(): every browser refuses file:// requests. A <script> tag is
// the one way to load another local file into it, so each data file calls a
// global the viewer installed — window.__logosExport for the tree,
// window.__logosPage for a page.
//
// Page bodies are ProseMirror JSON, the tree the editor stores, so the
// viewer renders them with the editor's own extensions and the page comes
// out with the app's markup. What the editor looks up live — a chip's
// title, a host's name, a credential — is resolved here and written onto
// the node, and links and attachments are rewritten to paths inside the zip.
package htmlbundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/wiki"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer/markdown"
	"go.uber.org/zap"
)

// FormatVersion is the shape of the data files, read by the viewer. Bump it
// with any change an older viewer would misread; both ship in one zip, so
// this guards only against a viewer and data built from different commits.
const FormatVersion = 1

// Renderer converts a prose document's Y.js content_state into ProseMirror
// JSON. Satisfied by *wiki.HocuspocusClient.
type Renderer interface {
	YjsToProseMirror(ctx context.Context, contentState []byte) (json.RawMessage, error)
}

// Config bounds one export. Zero means unlimited, as for every wiki export
// format — see bundle.Config.
type Config struct {
	MaxDocuments       int
	MaxAttachmentBytes int64
}

// Exporter renders a Scope to an HTML bundle.
type Exporter struct {
	imageRepo   repository.IWikiImageRepository
	fileRepo    repository.IWikiFileRepository
	imageStore  blob.ObjectStore
	fileStore   blob.ObjectStore
	docRepo     repository.IWikiDocumentRepository
	hostRepo    repository.IHostRepository
	hashRepo    repository.IHashRepository
	renderer    Renderer
	drawings    *wiki.DrawingFileRenderer
	credentials wikitransfer.CredentialLookup
	viewer      ViewerAssets
	logger      *zap.Logger
	cfg         Config
	now         func() time.Time
}

// NewExporter wires the exporter. docRepo, hostRepo, hashRepo and
// credentials may be nil; chips they would have resolved then show their
// generic label. drawings may be nil, which records each drawing as a skip.
// viewer nil means the embedded build (see viewer.go).
func NewExporter(
	imageRepo repository.IWikiImageRepository,
	fileRepo repository.IWikiFileRepository,
	imageStore, fileStore blob.ObjectStore,
	docRepo repository.IWikiDocumentRepository,
	hostRepo repository.IHostRepository,
	hashRepo repository.IHashRepository,
	renderer Renderer,
	drawings *wiki.DrawingFileRenderer,
	credentials wikitransfer.CredentialLookup,
	viewer ViewerAssets,
	logger *zap.Logger,
	cfg Config,
) *Exporter {
	if logger == nil {
		logger = zap.NewNop()
	}
	if viewer == nil {
		viewer = EmbeddedViewer()
	}
	return &Exporter{
		imageRepo: imageRepo, fileRepo: fileRepo, imageStore: imageStore, fileStore: fileStore,
		docRepo: docRepo, hostRepo: hostRepo, hashRepo: hashRepo,
		renderer: renderer, drawings: drawings, credentials: credentials,
		viewer: viewer, logger: logger, cfg: cfg, now: time.Now,
	}
}

// treePage is one page as the viewer's sidebar and search see it.
type treePage struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Title    string `json:"title"`
	Emoji    string `json:"emoji,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Color    string `json:"color,omitempty"`
	Kind     string `json:"kind"`
	Template bool   `json:"template,omitempty"`
	// Text is the page's plain text, for the viewer's full-text search.
	Text string `json:"text,omitempty"`
	// Missing marks a page whose body could not be exported. It stays in
	// the tree, so its children are still reachable and the reader sees
	// that something is gone instead of a hole.
	Missing bool `json:"missing,omitempty"`
}

type treeData struct {
	Version    int        `json:"version"`
	Title      string     `json:"title"`
	Operation  string     `json:"operation"`
	Scope      string     `json:"scope"`
	ExportedAt string     `json:"exportedAt"`
	Pages      []treePage `json:"pages"`
}

type pageData struct {
	Kind  string          `json:"kind"`
	Doc   json.RawMessage `json:"doc,omitempty"`
	Scene any             `json:"scene,omitempty"`
}

type exportRun struct {
	e        *Exporter
	zw       *zip.Writer
	scope    *wikitransfer.Scope
	root     string
	inScope  map[uuid.UUID]models.WikiDocument
	report   *wikitransfer.ExportReport
	progress wikitransfer.Progress
	done     int

	// Attachments are written once however many pages show them; the maps
	// hold the in-zip path of each one written, or "" for one refused.
	images          map[uuid.UUID]string
	files           map[uuid.UUID]string
	attachmentBytes int64
}

// Run writes the scope into zw. The caller owns zw and closes it.
func (e *Exporter) Run(ctx context.Context, zw *zip.Writer, scope *wikitransfer.Scope, progress wikitransfer.Progress) (*wikitransfer.ExportReport, error) {
	if e.cfg.MaxDocuments > 0 && len(scope.Docs) > e.cfg.MaxDocuments {
		return nil, fmt.Errorf("export exceeds %d documents (got %d)", e.cfg.MaxDocuments, len(scope.Docs))
	}
	assets, err := e.viewer.Files()
	if err != nil {
		return nil, err
	}
	root := markdown.Slugify(scope.Title())
	if root == "" {
		root = "wiki"
	}
	r := &exportRun{
		e: e, zw: zw, scope: scope, root: root + "/", progress: progress,
		inScope: make(map[uuid.UUID]models.WikiDocument, len(scope.Docs)),
		report: &wikitransfer.ExportReport{
			BundleID: uuid.New(), Format: string(models.WikiTransferHTML), Scope: scope.Label(),
			RootTitle: scope.Title(), TotalDocs: len(scope.Docs),
		},
		images: map[uuid.UUID]string{},
		files:  map[uuid.UUID]string{},
	}
	for _, d := range scope.Docs {
		r.inScope[d.DocumentID] = d
	}

	for name, body := range assets {
		if err := r.write(name, body); err != nil {
			return r.report, fmt.Errorf("write viewer: %w", err)
		}
	}

	tree := treeData{
		Version: FormatVersion, Title: scope.Title(), Operation: scope.OperationName,
		Scope: scope.Label(), ExportedAt: e.now().UTC().Format(time.RFC3339),
	}
	var walkErr error
	scope.Walk(func(doc models.WikiDocument, _, _ int) bool {
		if walkErr = ctx.Err(); walkErr != nil {
			return false
		}
		tree.Pages = append(tree.Pages, r.writePage(ctx, doc))
		return true
	})
	if walkErr != nil {
		return r.report, walkErr
	}

	if err := r.writeScript("data/tree.js", "__logosExport", nil, tree); err != nil {
		return r.report, fmt.Errorf("write tree: %w", err)
	}
	var rep bytes.Buffer
	if err := wikitransfer.EncodeJSON(&rep, r.report); err != nil {
		return r.report, fmt.Errorf("encode report: %w", err)
	}
	if err := r.write("REPORT.json", rep.Bytes()); err != nil {
		return r.report, fmt.Errorf("write report: %w", err)
	}
	return r.report, nil
}

func (r *exportRun) advance() {
	r.done++
	if r.progress != nil {
		r.progress(r.done, r.report.TotalDocs)
	}
}

// writePage writes one page's data file and returns its tree entry. A page
// whose body fails is still in the tree, marked missing.
func (r *exportRun) writePage(ctx context.Context, doc models.WikiDocument) treePage {
	defer r.advance()
	entry := treePage{
		ID: doc.DocumentID.String(), Title: strings.TrimSpace(doc.Title),
		Emoji: doc.Emoji, Icon: doc.Icon, Color: doc.Color,
		Kind: string(doc.Kind.Or()), Template: doc.IsTemplate,
	}
	if doc.ParentDocumentID != nil {
		if _, ok := r.inScope[*doc.ParentDocumentID]; ok {
			entry.ParentID = doc.ParentDocumentID.String()
		}
	}
	label := entry.Title
	if label == "" {
		label = "Untitled"
	}

	data, text, err := r.renderBody(ctx, doc, label)
	if err != nil {
		r.report.Skip(label, err.Error())
		entry.Missing = true
		return entry
	}
	entry.Text = text
	if err := r.writeScript("data/pages/"+entry.ID+".js", "__logosPage", &entry.ID, data); err != nil {
		r.report.Skip(label, "zip_write_failed: "+err.Error())
		entry.Missing = true
		return entry
	}
	r.report.ExportedDocs++
	return entry
}

var errNotConfigured = errors.New("renderer_not_configured")

func (r *exportRun) renderBody(ctx context.Context, doc models.WikiDocument, label string) (pageData, string, error) {
	state, err := r.scope.ContentState(ctx, doc.DocumentID)
	if err != nil {
		return pageData{}, "", fmt.Errorf("content_state_read_failed: %w", err)
	}

	if doc.Kind.IsDrawing() {
		if r.e.drawings == nil {
			return pageData{}, "", fmt.Errorf("drawing_%w", errNotConfigured)
		}
		file, result, err := r.e.drawings.File(ctx, doc, state, wiki.DrawingFileOptions{
			MaxImageBytes: r.remainingAttachmentBudget(),
		})
		if err != nil {
			return pageData{}, "", fmt.Errorf("drawing_render_failed: %w", err)
		}
		for _, skip := range result.Skips {
			r.report.Warn(label, skip.Reason+": "+skip.ImageID)
		}
		r.attachmentBytes += result.Bytes
		r.report.ImagesExported += result.ImagesEmbedded
		return pageData{Kind: "drawing", Scene: file}, drawingText(file), nil
	}

	if doc.Kind.Or() != models.WikiDocumentKindDocument {
		return pageData{}, "", fmt.Errorf("unsupported_kind: %s", doc.Kind.Or())
	}
	if r.e.renderer == nil {
		return pageData{}, "", fmt.Errorf("prose_%w", errNotConfigured)
	}
	raw, err := r.e.renderer.YjsToProseMirror(ctx, state)
	if err != nil {
		return pageData{}, "", fmt.Errorf("render_failed: %w", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return pageData{}, "", fmt.Errorf("render_failed: %w", err)
	}
	t := &transform{run: r, ctx: ctx, doc: doc, label: label}
	t.node(tree)
	body, err := json.Marshal(tree)
	if err != nil {
		return pageData{}, "", fmt.Errorf("encode_failed: %w", err)
	}
	return pageData{Kind: "document", Doc: body}, t.text.String(), nil
}

// writeScript writes `window.<global>(<id>, <json>);`, or without the id.
// JSON is valid JavaScript (ES2019 settled U+2028/2029), so the payload
// needs no escaping beyond what encoding/json does.
func (r *exportRun) writeScript(name, global string, id *string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("window." + global + "(")
	if id != nil {
		idJSON, _ := json.Marshal(*id)
		buf.Write(idJSON)
		buf.WriteString(",")
	}
	buf.Write(payload)
	buf.WriteString(");\n")
	return r.write(name, buf.Bytes())
}

func (r *exportRun) write(name string, body []byte) error {
	w, err := r.zw.Create(r.root + name)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func (r *exportRun) remainingAttachmentBudget() int64 {
	if r.e.cfg.MaxAttachmentBytes <= 0 {
		return 0
	}
	if left := r.e.cfg.MaxAttachmentBytes - r.attachmentBytes; left > 0 {
		return left
	}
	// Not zero, which reads as unlimited: one byte fits no image.
	return 1
}

// image copies a wiki image into the zip once and returns its path relative
// to index.html, or false when it cannot be shown. The security boundary is
// the operation, as for the markdown export: a page may show an image
// another page uploaded, never one from another operation.
func (r *exportRun) image(ctx context.Context, doc models.WikiDocument, label string, id uuid.UUID) (string, bool) {
	if p, seen := r.images[id]; seen {
		return p, p != ""
	}
	r.images[id] = ""
	if r.e.imageRepo == nil {
		return "", false
	}
	img, err := r.e.imageRepo.FindByID(ctx, id)
	switch {
	case err != nil:
		r.report.Warn(label, "image_not_found: "+id.String())
		return "", false
	case img.OperationID != doc.OperationID:
		r.report.Warn(label, "image_operation_mismatch: "+id.String())
		return "", false
	case img.DeletedAt != nil:
		r.report.Warn(label, "image_deleted: "+id.String())
		return "", false
	}
	name := "images/" + markdown.ImageFilenameFor(id.String(), img.ContentType)
	if !r.copyBlob(ctx, r.e.imageStore, img.ObjectKey, name, img.SizeBytes, label, nil) {
		return "", false
	}
	r.images[id] = name
	r.report.ImagesExported++
	return name, true
}

// file is image for file attachments. The returned path is URL-escaped,
// because a filename may hold spaces or any script.
func (r *exportRun) file(ctx context.Context, doc models.WikiDocument, label string, id uuid.UUID) (string, bool) {
	if p, seen := r.files[id]; seen {
		return p, p != ""
	}
	r.files[id] = ""
	if r.e.fileRepo == nil {
		return "", false
	}
	f, err := r.e.fileRepo.FindByID(ctx, id)
	switch {
	case err != nil:
		r.report.Warn(label, "file_not_found: "+id.String())
		return "", false
	case f.OperationID != doc.OperationID:
		r.report.Warn(label, "file_operation_mismatch: "+id.String())
		return "", false
	case f.DeletedAt != nil:
		r.report.Warn(label, "file_deleted: "+id.String())
		return "", false
	}
	filename := markdown.SanitizeFilename(f.Filename)
	name := "files/" + id.String() + "/" + filename
	var preview *bytes.Buffer
	if inlinePreviewable(f.ContentType, f.SizeBytes) {
		preview = &bytes.Buffer{}
	}
	if !r.copyBlob(ctx, r.e.fileStore, f.ObjectKey, name, f.SizeBytes, label, preview) {
		return "", false
	}
	if preview != nil {
		// The file card previews text, office and HTML files by fetching
		// their bytes, which a file:// page may not do. The bytes travel as a
		// script instead, and the viewer answers that fetch from it.
		idStr := id.String()
		if err := r.writeScript("data/files/"+idStr+".js", "__logosFile", &idStr, base64.StdEncoding.EncodeToString(preview.Bytes())); err != nil {
			r.report.Warn(label, "file_preview_write_failed: "+err.Error())
		}
	}
	href := "files/" + id.String() + "/" + url.PathEscape(filename)
	r.files[id] = href
	r.report.FilesExported++
	return href, true
}

// maxInlinePreviewBytes is the largest file the viewer can preview from its
// bytes: the frontend's own ceiling (MAX_INLINE_HTML_BYTES in
// wiki-file-preview-source.ts). Anything larger is offered as a download
// there too, so carrying its bytes twice would buy nothing.
const maxInlinePreviewBytes = 25 << 20

// inlinePreviewable reports whether the file card may fetch this file to
// preview it. Images, PDFs, video and audio are shown by pointing an element
// at the file itself, which works from disk; only the rest needs its bytes.
func inlinePreviewable(contentType string, size int64) bool {
	if size <= 0 || size > maxInlinePreviewBytes {
		return false
	}
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch {
	case strings.HasPrefix(ct, "image/"), strings.HasPrefix(ct, "video/"), strings.HasPrefix(ct, "audio/"),
		ct == "application/pdf":
		return false
	}
	return true
}

// copyBlob streams a blob into the zip at name, and into also when it is
// non-nil.
func (r *exportRun) copyBlob(ctx context.Context, store blob.ObjectStore, key, name string, size int64, label string, also *bytes.Buffer) bool {
	if store == nil {
		return false
	}
	if r.e.cfg.MaxAttachmentBytes > 0 && r.attachmentBytes+size > r.e.cfg.MaxAttachmentBytes {
		r.report.Warn(label, "attachment_budget_exhausted")
		return false
	}
	reader, _, err := store.Get(ctx, key)
	if err != nil {
		r.report.Warn(label, "blob_get_failed: "+err.Error())
		return false
	}
	defer reader.Close()
	w, err := r.zw.Create(r.root + name)
	if err != nil {
		r.report.Warn(label, "zip_create_failed: "+err.Error())
		return false
	}
	var dst io.Writer = w
	if also != nil {
		dst = io.MultiWriter(w, also)
	}
	n, err := io.Copy(dst, reader)
	if err != nil {
		r.report.Warn(label, "blob_copy_failed: "+err.Error())
		return false
	}
	r.attachmentBytes += n
	return true
}
