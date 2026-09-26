package markdown

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-core/core/pkg/wiki"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer"
	"go.uber.org/zap"
)

// Renderer converts a prose document's Y.js content_state into markdown.
// Satisfied by *wiki.HocuspocusClient. A drawing takes the other route,
// through wiki.DrawingFileRenderer.
type Renderer interface {
	YjsToMarkdown(ctx context.Context, contentState []byte) (string, error)
}

// Config bounds one export. Every budget defaults to unlimited — see
// bundle.Config for why a wiki export does not cap itself. Zero means no
// limit.
type Config struct {
	// MaxDocuments caps the pages in one export. 0 = unlimited.
	MaxDocuments int
	// MaxBodyBytes caps total markdown bytes. 0 = unlimited.
	MaxBodyBytes int64
	// MaxAttachmentBytes caps total attachment bytes. 0 = unlimited.
	MaxAttachmentBytes int64
}

func (c Config) withDefaults() Config {
	return c
}

// Exporter renders a Scope to a markdown zip for editors that know nothing
// about Logos: Obsidian, VS Code, GitHub. Lossy by design — the native
// bundle (package bundle) is the lossless, re-importable format. Nothing
// Logos-specific survives in the output: page chips become relative links
// to the page's .md file, host and hash chips become their display value,
// attachments are linked by their real relative path, and a drawing leaves
// as an `.excalidraw` scene beside a stub page (see drawing.go).
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
	credentials CredentialLookup
	logger      *zap.Logger
	cfg         Config
}

// NewExporter wires the exporter. credentials may be nil, leaving every
// credential fence id-only. docRepo, hostRepo and hashRepo may be nil;
// chips they would have resolved are then lowered to their generic label.
// drawings may be nil, which records each drawing page as a skip rather
// than exporting a scene nothing built.
func NewExporter(
	imageRepo repository.IWikiImageRepository,
	fileRepo repository.IWikiFileRepository,
	imageStore, fileStore blob.ObjectStore,
	docRepo repository.IWikiDocumentRepository,
	hostRepo repository.IHostRepository,
	hashRepo repository.IHashRepository,
	renderer Renderer,
	drawings *wiki.DrawingFileRenderer,
	credentials CredentialLookup,
	logger *zap.Logger,
	cfg Config,
) *Exporter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Exporter{
		imageRepo: imageRepo, fileRepo: fileRepo, imageStore: imageStore, fileStore: fileStore,
		docRepo: docRepo, hostRepo: hostRepo, hashRepo: hashRepo,
		renderer: renderer, drawings: drawings, credentials: credentials,
		logger: logger, cfg: cfg.withDefaults(),
	}
}

type exportRun struct {
	e        *Exporter
	zw       *zip.Writer
	scope    *wikitransfer.Scope
	rootSlug string
	// paths maps every in-scope document to its zip path, decided before
	// any body is rendered so a page can link forward to a sibling that
	// has not been written yet.
	paths            map[uuid.UUID]string
	titles           map[uuid.UUID]string
	report           *wikitransfer.ExportReport
	totalBody        int64
	totalAttachments int64
	progress         wikitransfer.Progress
	done             int
}

// Run writes the scope into zw. The caller owns zw and closes it; Run
// writes REPORT.json as its last entry so a partial archive still explains
// itself.
func (e *Exporter) Run(ctx context.Context, zw *zip.Writer, scope *wikitransfer.Scope, progress wikitransfer.Progress) (*wikitransfer.ExportReport, error) {
	if e.cfg.MaxDocuments > 0 && len(scope.Docs) > e.cfg.MaxDocuments {
		return nil, fmt.Errorf("export exceeds %d documents (got %d)", e.cfg.MaxDocuments, len(scope.Docs))
	}
	r := &exportRun{
		e: e, zw: zw, scope: scope, rootSlug: slugify(scope.Title()), progress: progress,
		report: &wikitransfer.ExportReport{
			BundleID: uuid.New(), Format: "markdown", Scope: scope.Label(), RootTitle: scope.Title(), TotalDocs: len(scope.Docs),
		},
	}
	placements := planPlacements(scope, r.rootSlug)
	r.paths = make(map[uuid.UUID]string, len(placements))
	r.titles = make(map[uuid.UUID]string, len(placements))
	for _, p := range placements {
		r.paths[p.doc.DocumentID] = p.path
		r.titles[p.doc.DocumentID] = p.doc.Title
	}
	for _, p := range placements {
		r.writeDoc(ctx, p.doc, p.path)
	}
	if len(scope.TopLevel) == 0 {
		_, _ = zw.Create(r.rootSlug + "/.gitkeep")
	}
	if err := writeJSON(zw, "REPORT.json", r.report); err != nil {
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

// placement is one document and the zip path its markdown lands at.
type placement struct {
	doc  models.WikiDocument
	path string
}

// planPlacements decides the zip path of every document in scope, parent
// first. The layout is
//
//	<rootSlug>/<slug>.md          leaf
//	<rootSlug>/<slug>.md          branch
//	<rootSlug>/<slug>/<child>.md
//
// The walk is still in the tree's own order, because that is what decides
// which of two pages with the same title gets the plain slug and which gets
// `-2`. The order does not reach the filenames: see the package comment.
//
// Slugs are unique among siblings; "uploads" and "report.json" are reserved
// at the root so a page named like them cannot shadow the attachment folder.
func planPlacements(scope *wikitransfer.Scope, rootSlug string) []placement {
	var out []placement
	var visit func(docs []models.WikiDocument, folder string, used map[string]struct{})
	visit = func(docs []models.WikiDocument, folder string, used map[string]struct{}) {
		for _, doc := range docs {
			slug := uniqueSlug(slugify(doc.Title), used)
			out = append(out, placement{doc: doc, path: folder + "/" + buildDocFilename(slug)})
			children := scope.ChildrenByParent[doc.DocumentID]
			if len(children) == 0 {
				continue
			}
			visit(children, folder+"/"+buildChildrenFolder(slug), map[string]struct{}{})
		}
	}
	visit(scope.TopLevel, rootSlug, map[string]struct{}{"uploads": {}, "report.json": {}})
	return out
}

func (r *exportRun) writeDoc(ctx context.Context, doc models.WikiDocument, docPath string) {
	// One page's body at a time: the Scope carries the tree's shape, not its
	// bytes, so an export of any size costs the same memory.
	state, err := r.scope.ContentState(ctx, doc.DocumentID)
	if err != nil {
		r.report.Skip(docPath, "content_state_read_failed: "+err.Error())
		r.advance()
		return
	}

	// A drawing is a scene, not prose. It leaves as an `.excalidraw` file
	// beside a stub page that links to it — the format Excalidraw itself
	// and Obsidian's Excalidraw plugin open, so the diagram arrives as a
	// diagram somebody can edit rather than as a line in the report saying
	// it was left behind.
	if doc.Kind.IsDrawing() {
		r.writeDrawing(ctx, doc, docPath, state)
		return
	}

	body, err := r.e.renderer.YjsToMarkdown(ctx, state)
	if err != nil {
		r.report.Skip(docPath, "render_failed: "+err.Error())
		r.advance()
		return
	}
	if r.e.cfg.MaxBodyBytes > 0 && r.totalBody+int64(len(body)) > r.e.cfg.MaxBodyBytes {
		r.report.Skip(docPath, "export_body_budget_exhausted")
		r.advance()
		return
	}

	body, written, tombstoned := LowerCredentialFences(ctx, body, doc.OperationID, r.e.credentials, CredentialValues)
	r.report.CredentialsExported += written
	r.report.CredentialsTombstoned += tombstoned

	body = r.streamAttachments(ctx, doc, body, docPath)
	body = RewriteReferenceLinks(body, r.referenceResolver(ctx, doc, docPath))
	// Last, so the chips and attachment links inside a checklist's answer
	// are already ordinary markdown by the time the item is rebuilt.
	body = LowerChecklists(body)
	r.totalBody += int64(len(body))

	full := renderDocMarkdown(doc.Emoji, doc.Title, body)
	if err := writeZipFile(r.zw, docPath, []byte(full)); err != nil {
		r.report.Skip(docPath, "zip_write_failed: "+err.Error())
		r.advance()
		return
	}
	r.report.ExportedDocs++
	r.advance()
}

// referenceResolver renders the chips of one document. A page inside the
// scope links to its .md file; a page outside it (or a host or hash)
// becomes plain text carrying the name the reader would have seen in the
// editor. Anything the repos cannot answer is reported and left to the
// generic label.
func (r *exportRun) referenceResolver(ctx context.Context, doc models.WikiDocument, docPath string) ReferenceResolver {
	return func(kind string, id uuid.UUID) (ReferenceTarget, bool) {
		switch kind {
		case "doc":
			return r.resolvePage(ctx, doc, docPath, id)
		case "host":
			return r.resolveHost(ctx, doc, docPath, id)
		case "hash":
			return r.resolveHash(ctx, doc, docPath, id)
		}
		return ReferenceTarget{}, false
	}
}

func (r *exportRun) resolvePage(ctx context.Context, doc models.WikiDocument, docPath string, id uuid.UUID) (ReferenceTarget, bool) {
	if target, ok := r.paths[id]; ok {
		title := strings.TrimSpace(r.titles[id])
		if title == "" {
			title = "Untitled"
		}
		return ReferenceTarget{Text: title, Href: relativeLink(docPath, target)}, true
	}
	if r.e.docRepo == nil {
		r.report.Warn(docPath, "page_reference_unresolved: "+id.String())
		return ReferenceTarget{}, false
	}
	linked, err := r.e.docRepo.FindByID(ctx, id)
	if err != nil || !pageTitleVisibleFrom(doc, linked) {
		r.report.Warn(docPath, "page_reference_unresolved: "+id.String())
		return ReferenceTarget{}, false
	}
	r.report.Warn(docPath, "page_reference_outside_scope: "+id.String())
	return ReferenceTarget{Text: strings.TrimSpace(linked.Title)}, true
}

// pageTitleVisibleFrom reports whether an export of `from` may print the
// title of `linked`. Pages of the same operation qualify, and so do pages
// of the Public tree, which every authenticated user can read — a chip
// pointing into it is the common case for an operation page that cites
// shared reference material.
func pageTitleVisibleFrom(from, linked models.WikiDocument) bool {
	return linked.OperationID == from.OperationID || models.IsPublicOperation(linked.OperationID)
}

func (r *exportRun) resolveHost(ctx context.Context, doc models.WikiDocument, docPath string, id uuid.UUID) (ReferenceTarget, bool) {
	if r.e.hostRepo == nil {
		r.report.Warn(docPath, "host_reference_unresolved: "+id.String())
		return ReferenceTarget{}, false
	}
	h, err := r.e.hostRepo.FindByID(ctx, id)
	if err != nil || h.OperationID != doc.OperationID {
		r.report.Warn(docPath, "host_reference_unresolved: "+id.String())
		return ReferenceTarget{}, false
	}
	return ReferenceTarget{Text: strings.TrimSpace(h.Hostname)}, true
}

func (r *exportRun) resolveHash(ctx context.Context, doc models.WikiDocument, docPath string, id uuid.UUID) (ReferenceTarget, bool) {
	if r.e.hashRepo == nil {
		r.report.Warn(docPath, "hash_reference_unresolved: "+id.String())
		return ReferenceTarget{}, false
	}
	h, err := r.e.hashRepo.FindByID(ctx, id)
	if err != nil || h.OperationID != doc.OperationID {
		r.report.Warn(docPath, "hash_reference_unresolved: "+id.String())
		return ReferenceTarget{}, false
	}
	return ReferenceTarget{Text: strings.TrimSpace(h.Value)}, true
}

// streamAttachments copies every attachment the body references into
// uploads/<docId>/<attId>/<filename> and rewrites the links to the path
// relative to the document's own folder, so an unpacked zip previews in
// any markdown editor. The security
// boundary is the operation, not the owning page: a page may legitimately
// reference an attachment another page uploaded.
func (r *exportRun) streamAttachments(ctx context.Context, doc models.WikiDocument, body, docPath string) string {
	imageIDs, fileIDs := collectAttachmentRefs(body)
	imageRel := map[uuid.UUID]string{}
	fileRel := map[uuid.UUID]string{}

	for _, id := range imageIDs {
		img, err := r.e.imageRepo.FindByID(ctx, id)
		switch {
		case err != nil:
			r.report.Warn(docPath, "image_not_found: "+id.String())
			continue
		case img.OperationID != doc.OperationID:
			r.report.Warn(docPath, "image_operation_mismatch: "+id.String())
			continue
		case img.DeletedAt != nil:
			r.report.Warn(docPath, "image_deleted: "+id.String())
			continue
		}
		filename := sanitizeFilename(ImageFilenameFor(id.String(), img.ContentType))
		zipPath := uploadsZipPath(r.rootSlug, doc.DocumentID, id, filename)
		if !r.streamBlob(ctx, r.e.imageStore, img.ObjectKey, zipPath, img.SizeBytes, docPath) {
			continue
		}
		imageRel[id] = relativeLink(docPath, zipPath)
		r.report.ImagesExported++
	}
	for _, id := range fileIDs {
		file, err := r.e.fileRepo.FindByID(ctx, id)
		switch {
		case err != nil:
			r.report.Warn(docPath, "file_not_found: "+id.String())
			continue
		case file.OperationID != doc.OperationID:
			r.report.Warn(docPath, "file_operation_mismatch: "+id.String())
			continue
		case file.DeletedAt != nil:
			r.report.Warn(docPath, "file_deleted: "+id.String())
			continue
		}
		filename := sanitizeFilename(file.Filename)
		zipPath := uploadsZipPath(r.rootSlug, doc.DocumentID, id, filename)
		if !r.streamBlob(ctx, r.e.fileStore, file.ObjectKey, zipPath, file.SizeBytes, docPath) {
			continue
		}
		fileRel[id] = relativeLink(docPath, zipPath)
		r.report.FilesExported++
	}

	return rewriteAttachmentRefs(body,
		func(id uuid.UUID) (string, bool) { rel, ok := imageRel[id]; return rel, ok },
		func(id uuid.UUID) (string, bool) { rel, ok := fileRel[id]; return rel, ok },
	)
}

func (r *exportRun) streamBlob(ctx context.Context, store blob.ObjectStore, objectKey, zipPath string, declaredSize int64, docPath string) bool {
	if r.e.cfg.MaxAttachmentBytes > 0 && r.totalAttachments+declaredSize > r.e.cfg.MaxAttachmentBytes {
		r.report.Warn(docPath, "attachment_budget_exhausted")
		return false
	}
	reader, _, err := store.Get(ctx, objectKey)
	if err != nil {
		r.report.Warn(docPath, "blob_get_failed: "+err.Error())
		return false
	}
	defer reader.Close()
	w, err := r.zw.Create(zipPath)
	if err != nil {
		r.report.Warn(docPath, "zip_create_failed: "+err.Error())
		return false
	}
	n, err := io.Copy(w, reader)
	if err != nil {
		r.report.Warn(docPath, "blob_copy_failed: "+err.Error())
		return false
	}
	r.totalAttachments += n
	return true
}

func writeZipFile(zw *zip.Writer, path string, body []byte) error {
	w, err := zw.Create(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(body))
	return err
}

func writeJSON(zw *zip.Writer, path string, v any) error {
	w, err := zw.Create(path)
	if err != nil {
		return err
	}
	return wikitransfer.EncodeJSON(w, v)
}

// ImageFilenameFor reconstructs a filename for an image whose original
// upload name is not stored: `<imageId>.<ext>`, `.bin` for unknown types.
func ImageFilenameFor(imageID, contentType string) string {
	ext := ".bin"
	switch contentType {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	case "image/avif":
		ext = ".avif"
	case "image/svg+xml":
		ext = ".svg"
	}
	return imageID + ext
}
