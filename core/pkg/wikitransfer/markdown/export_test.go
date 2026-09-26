package markdown

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer/transfertest"
	"go.uber.org/zap"
)

// exportWorld is a two-level tree with every kind of reference the
// exporter has to lower for a foreign editor:
//
//	Network/            (branch, links to its child, a sibling, a host,
//	                     a hash and a page outside the scope)
//	  Peering           (leaf, links back up to the sibling and embeds
//	                     an image)
//	Hosts               (leaf)
type exportWorld struct {
	op                       uuid.UUID
	network, peering, hosts  models.WikiDocument
	outside, public, foreign models.WikiDocument
	hostID, hashID, imageID  uuid.UUID
	scope                    *wikitransfer.Scope
	exporter                 *Exporter
}

func newExportWorld(t *testing.T) *exportWorld {
	t.Helper()
	w := &exportWorld{op: uuid.New(), hostID: uuid.New(), hashID: uuid.New(), imageID: uuid.New()}
	w.network = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Network", SortOrder: "a"}
	w.peering = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Peering / IX", SortOrder: "a", ParentDocumentID: &w.network.DocumentID}
	w.hosts = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Hosts", SortOrder: "b"}
	w.outside = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Runbook", SortOrder: "c"}
	w.public = models.WikiDocument{DocumentID: uuid.New(), OperationID: models.PublicOperationID, Title: "BGP cheat sheet"}
	w.foreign = models.WikiDocument{DocumentID: uuid.New(), OperationID: uuid.New(), Title: "Other op secret"}

	w.network.ContentState = []byte("Dual [host](logos://host/" + w.hostID.String() + ") edge peering " +
		"[page](logos://doc/" + w.peering.DocumentID.String() + ") and " +
		"[page](logos://doc/" + w.hosts.DocumentID.String() + "), see " +
		"[page](logos://doc/" + w.outside.DocumentID.String() + "), " +
		"[page](logos://doc/" + w.public.DocumentID.String() + "), " +
		"[page](logos://doc/" + w.foreign.DocumentID.String() + "). NTLM [hash](logos://hash/" + w.hashID.String() + ")\n")
	w.peering.ContentState = []byte("Back to [page](logos://doc/" + w.hosts.DocumentID.String() + ")\n\n" +
		"![diagram](/api/v1/wiki/images/" + w.imageID.String() + ")\n")
	w.hosts.ContentState = []byte("plain\n")

	docs := transfertest.NewDocRepo(w.network, w.peering, w.hosts, w.outside, w.public, w.foreign)

	// Built by hand rather than through CollectScope: these tests turn on
	// what happens to a link that leaves the scope, so `outside`, `public`
	// and `foreign` have to exist in the repository without being in it.
	// The bodies still come from the repository, as they do in production.
	w.scope = &wikitransfer.Scope{
		OperationID:   w.op,
		OperationName: "ACME",
		Docs:          []models.WikiDocument{w.network, w.peering, w.hosts},
		TopLevel:      []models.WikiDocument{w.network, w.hosts},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{
			w.network.DocumentID: {w.peering},
		},
		States: docs,
	}

	imageStore := transfertest.NewStore()
	if err := imageStore.Put(context.Background(), "img-key", bytes.NewReader([]byte("png")), 3, "image/png"); err != nil {
		t.Fatal(err)
	}
	images := transfertest.NewImageRepo(models.WikiImage{
		ImageID: w.imageID, OperationID: w.op, DocumentID: w.peering.DocumentID,
		ObjectKey: "img-key", ContentType: "image/png", SizeBytes: 3,
	})
	hostRepo := &transfertest.HostRepo{Hosts: map[uuid.UUID]models.Host{
		w.hostID: {HostID: w.hostID, OperationID: w.op, Hostname: "in-bgp01"},
	}}
	hashRepo := &transfertest.HashRepo{Hashes: map[uuid.UUID]models.Hash{
		w.hashID: {HashID: w.hashID, OperationID: w.op, Value: "aad3b435b51404ee"},
	}}
	w.exporter = NewExporter(images, transfertest.NewFileRepo(), imageStore, transfertest.NewStore(),
		docs, hostRepo, hashRepo, transfertest.Renderer{},
		transfertest.NewDrawingRenderer(images, imageStore), nil, zap.NewNop(), Config{})
	return w
}

func (w *exportWorld) run(t *testing.T) (map[string]string, *wikitransfer.ExportReport) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	report, err := w.exporter.Run(context.Background(), zw, w.scope, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		entries[f.Name] = string(data)
	}
	return entries, report
}

func TestExport_ForeignMarkdownLinks(t *testing.T) {
	w := newExportWorld(t)
	entries, report := w.run(t)

	network, ok := entries["acme/network.md"]
	if !ok {
		t.Fatalf("missing branch page; entries: %v", keys(entries))
	}
	peering, ok := entries["acme/network/peering-ix.md"]
	if !ok {
		t.Fatalf("missing child page; entries: %v", keys(entries))
	}

	wantNetwork := "Dual in-bgp01 edge peering [Peering / IX](network/peering-ix.md) and [Hosts](hosts.md), see Runbook, BGP cheat sheet, page. NTLM aad3b435b51404ee"
	if !strings.Contains(network, wantNetwork) {
		t.Errorf("branch body:\n%s\nwant to contain:\n%s", network, wantNetwork)
	}
	wantPeering := "Back to [Hosts](../hosts.md)"
	if !strings.Contains(peering, wantPeering) {
		t.Errorf("child body:\n%s\nwant to contain:\n%s", peering, wantPeering)
	}
	wantImage := "![diagram](../uploads/" + w.peering.DocumentID.String() + "/" + w.imageID.String() + "/" + w.imageID.String() + ".png)"
	if !strings.Contains(peering, wantImage) {
		t.Errorf("child body:\n%s\nwant image link:\n%s", peering, wantImage)
	}
	if _, ok := entries["acme/uploads/"+w.peering.DocumentID.String()+"/"+w.imageID.String()+"/"+w.imageID.String()+".png"]; !ok {
		t.Errorf("image blob not in zip; entries: %v", keys(entries))
	}

	// Nothing in the zip may be readable only by us. Each of these is a
	// construct the editor's markdown carries that a foreign reader would
	// see as literal punctuation, JSON or an invisible comment.
	for name, body := range entries {
		for _, leak := range []string{"logos://", "logos:meta", "logos-credential", ":::checklist", "<!--"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s leaks %q:\n%s", name, leak, body)
			}
		}
	}
	if report.ExportedDocs != 3 || report.ImagesExported != 1 {
		t.Errorf("report = %+v", report)
	}
	if !hasWarning(report, "page_reference_outside_scope: "+w.outside.DocumentID.String()) ||
		!hasWarning(report, "page_reference_outside_scope: "+w.public.DocumentID.String()) {
		t.Errorf("expected outside-scope warnings, got %+v", report.Warnings)
	}
	if !hasWarning(report, "page_reference_unresolved: "+w.foreign.DocumentID.String()) {
		t.Errorf("a page of another operation must not leak its title, got %+v", report.Warnings)
	}
}

// A Logos markdown zip fed back through the foreign importer must degrade
// gracefully: relative page links stay ordinary links, and attachments
// linked by `../uploads/…` are still ingested.
func TestExport_ReimportsAsPlainMarkdown(t *testing.T) {
	w := newExportWorld(t)
	entries, _ := w.run(t)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	plan, err := ReadPlan(zr)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if len(plan.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(plan.Attachments))
	}
	var peering *wikitransfer.Page
	plan.Walk(func(p *wikitransfer.Page, _ int) {
		if p.Title == "Peering / IX" {
			peering = p
		}
	})
	if peering == nil {
		t.Fatalf("peering page not imported; plan: %+v", plan.Pages)
	}
	if strings.Contains(peering.Markdown, "uploads/") {
		t.Errorf("attachment link not rewritten to canonical URL:\n%s", peering.Markdown)
	}
	if !strings.Contains(peering.Markdown, "[Hosts](../hosts.md)") {
		t.Errorf("relative page link should survive as a plain link:\n%s", peering.Markdown)
	}
}

// Filenames carry the page's title and not its place in our tree.
//
// The case that prompted this: a Findings branch holds FND-001 … FND-006
// with the newest first, so numbering the files by sibling order produced
// `001-fnd-006…` through `006-fnd-001…` — a folder that reads as backwards
// in every file browser. Sorted by name, the titles now carry the order
// their author gave them.
func TestExport_FilenamesFollowTitlesNotTreeOrder(t *testing.T) {
	w := newExportWorld(t)
	parent := models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Findings", SortOrder: "a"}

	// Newest first, exactly as the tree holds them.
	var children []models.WikiDocument
	for i, title := range []string{"FND-006 sql", "FND-005 ot", "FND-004 mfa", "FND-003 backup", "FND-002 share", "FND-001 portal"} {
		children = append(children, models.WikiDocument{
			DocumentID: uuid.New(), OperationID: w.op, Title: title,
			SortOrder: string(rune('a' + i)), ParentDocumentID: &parent.DocumentID,
		})
	}
	docs := transfertest.NewDocRepo(append([]models.WikiDocument{parent}, children...)...)
	w.scope = &wikitransfer.Scope{
		OperationID: w.op, OperationName: "ACME",
		Docs:             append([]models.WikiDocument{parent}, children...),
		TopLevel:         []models.WikiDocument{parent},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{parent.DocumentID: children},
		States:           docs,
	}

	entries, _ := w.run(t)

	var got []string
	for name := range entries {
		if strings.HasPrefix(name, "acme/findings/") {
			got = append(got, strings.TrimPrefix(name, "acme/findings/"))
		}
	}
	sort.Strings(got)
	want := []string{
		"fnd-001-portal.md", "fnd-002-share.md", "fnd-003-backup.md",
		"fnd-004-mfa.md", "fnd-005-ot.md", "fnd-006-sql.md",
	}
	if !slices.Equal(got, want) {
		t.Errorf("sorted by name the folder reads:\n  %v\nwant:\n  %v", got, want)
	}
}

// Two siblings with the same title still get distinct files. The tree's
// order decides which one keeps the plain slug — it just does not show up
// in the name.
func TestExport_DuplicateTitlesStayDistinct(t *testing.T) {
	w := newExportWorld(t)
	first := models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Notes", SortOrder: "a"}
	second := models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Notes", SortOrder: "b"}
	docs := transfertest.NewDocRepo(first, second)
	w.scope = &wikitransfer.Scope{
		OperationID: w.op, OperationName: "ACME",
		Docs:             []models.WikiDocument{first, second},
		TopLevel:         []models.WikiDocument{first, second},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{},
		States:           docs,
	}

	entries, _ := w.run(t)
	for _, want := range []string{"acme/notes.md", "acme/notes-2.md"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("missing %s; entries: %v", want, keys(entries))
		}
	}
}

// A drawing leaves as a scene file plus a stub page. Both have to be there:
// the scene so the diagram is still editable, the stub so the tree keeps the
// page and every link to it still resolves.
func TestExport_DrawingLeavesAsExcalidrawScene(t *testing.T) {
	w := newExportWorld(t)

	drawingID := uuid.New()
	imageID := uuid.New()
	scene := `{"elements":[{"id":"a","type":"rectangle","x":0,"y":0},` +
		`{"id":"b","type":"image","fileId":"` + imageID.String() + `"}],` +
		`"appState":{"viewBackgroundColor":"#fff"},"imageIds":["` + imageID.String() + `"]}`
	drawing := models.WikiDocument{
		DocumentID: drawingID, OperationID: w.op, Title: "Topology",
		SortOrder: "c", Kind: models.WikiDocumentKindDrawing,
		ContentState: []byte(scene),
	}

	imageStore := transfertest.NewStore()
	if err := imageStore.Put(context.Background(), "drawing-key", bytes.NewReader([]byte("PNGBYTES")), 8, "image/png"); err != nil {
		t.Fatal(err)
	}
	images := transfertest.NewImageRepo(models.WikiImage{
		ImageID: imageID, OperationID: w.op, DocumentID: drawingID,
		ObjectKey: "drawing-key", ContentType: "image/png", SizeBytes: 8,
	})
	docs := transfertest.NewDocRepo(drawing)

	w.scope = &wikitransfer.Scope{
		OperationID:      w.op,
		OperationName:    "ACME",
		Docs:             []models.WikiDocument{drawing},
		TopLevel:         []models.WikiDocument{drawing},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{},
		States:           docs,
	}
	w.exporter = NewExporter(images, transfertest.NewFileRepo(), imageStore, transfertest.NewStore(),
		docs, nil, nil, transfertest.Renderer{},
		transfertest.NewDrawingRenderer(images, imageStore), nil, zap.NewNop(), Config{})

	entries, report := w.run(t)

	raw, ok := entries["acme/topology.excalidraw"]
	if !ok {
		t.Fatalf("missing scene file; entries: %v", keys(entries))
	}
	var file struct {
		Type     string `json:"type"`
		Version  int    `json:"version"`
		Elements []struct {
			ID string `json:"id"`
		} `json:"elements"`
		AppState map[string]any `json:"appState"`
		Files    map[string]struct {
			MimeType string `json:"mimeType"`
			DataURL  string `json:"dataURL"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &file); err != nil {
		t.Fatalf("scene is not valid JSON: %v\n%s", err, raw)
	}
	if file.Type != "excalidraw" || file.Version != 2 {
		t.Errorf("envelope = %q v%d, want excalidraw v2", file.Type, file.Version)
	}
	if len(file.Elements) != 2 {
		t.Errorf("elements = %d, want the 2 the scene held", len(file.Elements))
	}
	if file.AppState["viewBackgroundColor"] != "#fff" {
		t.Errorf("appState lost the page background: %v", file.AppState)
	}
	// The image has to travel inside the scene: Excalidraw reads bytes from
	// the files map, so a link to uploads/ would open as a hole in the
	// diagram.
	embedded, ok := file.Files[imageID.String()]
	if !ok {
		t.Fatalf("image not embedded; files: %v", file.Files)
	}
	if embedded.MimeType != "image/png" ||
		embedded.DataURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("PNGBYTES")) {
		t.Errorf("embedded image = %+v", embedded)
	}

	stub, ok := entries["acme/topology.md"]
	if !ok {
		t.Fatalf("missing stub page; entries: %v", keys(entries))
	}
	if !strings.Contains(stub, "# Topology") {
		t.Errorf("stub lost the page title:\n%s", stub)
	}
	if !strings.Contains(stub, "(topology.excalidraw)") {
		t.Errorf("stub does not link the scene beside it:\n%s", stub)
	}
	if report.ExportedDocs != 1 || report.SkippedDocs != 0 || report.ImagesExported != 1 {
		t.Errorf("report = %+v", report)
	}
}

// The scene has to survive a trip back through our own importer. It returns
// as an attachment on the stub rather than as a canvas — the bundle is what
// restores a drawing as a drawing — but it is not dropped, and the stub's
// link to it resolves.
func TestExport_DrawingSceneSurvivesReimport(t *testing.T) {
	w := newExportWorld(t)

	drawing := models.WikiDocument{
		DocumentID: uuid.New(), OperationID: w.op, Title: "Topology",
		Kind: models.WikiDocumentKindDrawing, ContentState: []byte(`{"elements":[{"id":"a","type":"rectangle"}]}`),
	}
	docs := transfertest.NewDocRepo(drawing)
	w.scope = &wikitransfer.Scope{
		OperationID:      w.op,
		OperationName:    "ACME",
		Docs:             []models.WikiDocument{drawing},
		TopLevel:         []models.WikiDocument{drawing},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{},
		States:           docs,
	}

	entries, _ := w.run(t)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	plan, err := ReadPlan(zr)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if len(plan.Attachments) != 1 {
		t.Fatalf("attachments = %d, want the scene", len(plan.Attachments))
	}
	var att *wikitransfer.Attachment
	for _, a := range plan.Attachments {
		att = a
	}
	if att.ContentType != "application/json" || !strings.HasSuffix(att.Filename, ".excalidraw") {
		t.Errorf("attachment = %+v, want the scene as json", att)
	}
	var page *wikitransfer.Page
	plan.Walk(func(p *wikitransfer.Page, _ int) {
		if p.Title == "Topology" {
			page = p
		}
	})
	if page == nil {
		t.Fatalf("drawing page not imported; plan: %+v", plan.Pages)
	}
	if !strings.Contains(page.Markdown, canonicalURL(att)) {
		t.Errorf("stub link not rewritten to the ingested scene:\n%s", page.Markdown)
	}
}

// An empty canvas still produces a page, and no scene file for a scene that
// has nothing in it.
func TestExport_EmptyDrawingStillExportsPage(t *testing.T) {
	w := newExportWorld(t)

	drawing := models.WikiDocument{
		DocumentID: uuid.New(), OperationID: w.op, Title: "Blank",
		Kind: models.WikiDocumentKindDrawing,
	}
	docs := transfertest.NewDocRepo(drawing)
	w.scope = &wikitransfer.Scope{
		OperationID:      w.op,
		OperationName:    "ACME",
		Docs:             []models.WikiDocument{drawing},
		TopLevel:         []models.WikiDocument{drawing},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{},
		States:           docs,
	}

	entries, report := w.run(t)

	if _, ok := entries["acme/blank.md"]; !ok {
		t.Fatalf("missing stub page; entries: %v", keys(entries))
	}
	if _, ok := entries["acme/blank.excalidraw"]; ok {
		t.Errorf("an empty canvas should not produce a scene file")
	}
	if report.ExportedDocs != 1 {
		t.Errorf("report = %+v", report)
	}
}

func hasWarning(r *wikitransfer.ExportReport, reason string) bool {
	for _, w := range r.Warnings {
		if w.Reason == reason {
			return true
		}
	}
	return false
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
