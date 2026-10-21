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
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer/transfertest"
	"go.uber.org/zap"
)

// pmRenderer stands in for the sidecar: a fake page's content_state is its
// ProseMirror JSON, handed back verbatim, so a test pins exactly what the
// exporter does with a given tree. "fail" makes the render fail.
type pmRenderer struct{}

func (pmRenderer) YjsToProseMirror(_ context.Context, state []byte) (json.RawMessage, error) {
	if string(state) == "fail" {
		return nil, errors.New("sidecar down")
	}
	if len(state) == 0 {
		return json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`), nil
	}
	return json.RawMessage(state), nil
}

type fakeViewer struct{ err error }

func (v *fakeViewer) Files() (map[string][]byte, error) {
	if v.err != nil {
		return nil, v.err
	}
	out := map[string][]byte{}
	for _, name := range ViewerFiles {
		out[name] = []byte("/* " + name + " */")
	}
	return out, nil
}

// world is a small tree with every kind of thing the exporter rewrites:
//
//	Network   (prose: page, host, hash and credential chips, links, an image
//	           shown twice, a file)
//	  Peering (prose, never opened)
//	Diagram   (drawing)
type world struct {
	op                        uuid.UUID
	network, peering, diagram models.WikiDocument
	outside, foreign          models.WikiDocument
	hostID, hashID, credID    uuid.UUID
	imageID, fileID, notesID  uuid.UUID
	scope                     *wikitransfer.Scope
	exporter                  *Exporter
	viewer                    fakeViewer
}

func text(s string, marks ...map[string]any) map[string]any {
	n := map[string]any{"type": "text", "text": s}
	if len(marks) > 0 {
		ms := make([]any, len(marks))
		for i, m := range marks {
			ms[i] = m
		}
		n["marks"] = ms
	}
	return n
}

func link(href string) map[string]any {
	return map[string]any{"type": "link", "attrs": map[string]any{"href": href}}
}

func node(typ string, attrs map[string]any, content ...map[string]any) map[string]any {
	n := map[string]any{"type": typ}
	if attrs != nil {
		n["attrs"] = attrs
	}
	if len(content) > 0 {
		c := make([]any, len(content))
		for i, x := range content {
			c[i] = x
		}
		n["content"] = c
	}
	return n
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		op: uuid.New(), hostID: uuid.New(), hashID: uuid.New(), credID: uuid.New(),
		imageID: uuid.New(), fileID: uuid.New(), notesID: uuid.New(),
	}
	w.network = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Network", Emoji: "🌐", SortOrder: "a"}
	w.peering = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Peering", SortOrder: "a", ParentDocumentID: &w.network.DocumentID}
	w.diagram = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Diagram", SortOrder: "b", Kind: models.WikiDocumentKindDrawing}
	w.outside = models.WikiDocument{DocumentID: uuid.New(), OperationID: w.op, Title: "Runbook"}
	w.foreign = models.WikiDocument{DocumentID: uuid.New(), OperationID: uuid.New(), Title: "Other op"}

	img := "/api/v1/wiki/images/" + w.imageID.String()
	body := node("doc", nil,
		node("paragraph", nil,
			text("see "),
			node("wikiDocumentReference", map[string]any{"documentId": w.peering.DocumentID.String()}),
			node("wikiDocumentReference", map[string]any{"documentId": w.outside.DocumentID.String()}),
			node("wikiDocumentReference", map[string]any{"documentId": w.foreign.DocumentID.String()}),
			node("wikiHostReference", map[string]any{"hostId": w.hostID.String()}),
			node("wikiHashReference", map[string]any{"hashId": w.hashID.String()}),
			node("wikiCredentialReference", map[string]any{"credentialId": w.credID.String()}),
		),
		node("paragraph", nil,
			text("inside", link("logos://doc/"+w.peering.DocumentID.String())),
			text("app", link("/wiki/"+w.outside.DocumentID.String())),
			text("web", link("https://example.com/x")),
		),
		node("image", map[string]any{"src": img}),
		node("image", map[string]any{"src": img}),
		node("wikiFile", map[string]any{"fileId": w.fileID.String(), "url": "/api/v1/wiki/files/" + w.fileID.String(), "filename": "report final.pdf"}),
		node("wikiFile", map[string]any{"fileId": w.notesID.String(), "url": "/api/v1/wiki/files/" + w.notesID.String(), "filename": "sweep.txt"}),
	)
	state, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w.network.ContentState = state
	w.diagram.ContentState = []byte("Router")

	docs := transfertest.NewDocRepo(w.network, w.peering, w.diagram, w.outside, w.foreign)
	w.scope = &wikitransfer.Scope{
		OperationID: w.op, OperationName: "ACME",
		Docs:             []models.WikiDocument{w.network, w.peering, w.diagram},
		TopLevel:         []models.WikiDocument{w.network, w.diagram},
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{w.network.DocumentID: {w.peering}},
		States:           docs,
	}

	store := transfertest.NewStore()
	ctx := context.Background()
	if err := store.Put(ctx, "img-key", bytes.NewReader([]byte("png")), 3, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "file-key", bytes.NewReader([]byte("pdf")), 3, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "notes-key", bytes.NewReader([]byte("22/tc")), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	images := transfertest.NewImageRepo(models.WikiImage{
		ImageID: w.imageID, OperationID: w.op, DocumentID: w.network.DocumentID,
		ObjectKey: "img-key", ContentType: "image/png", SizeBytes: 3,
	})
	files := transfertest.NewFileRepo(models.WikiFile{
		FileID: w.fileID, OperationID: w.op, DocumentID: w.network.DocumentID,
		ObjectKey: "file-key", Filename: "report final.pdf", ContentType: "application/pdf", SizeBytes: 3,
	}, models.WikiFile{
		FileID: w.notesID, OperationID: w.op, DocumentID: w.network.DocumentID,
		ObjectKey: "notes-key", Filename: "sweep.txt", ContentType: "text/plain", SizeBytes: 5,
	})
	hosts := &transfertest.HostRepo{Hosts: map[uuid.UUID]models.Host{
		w.hostID: {HostID: w.hostID, OperationID: w.op, Hostname: "in-bgp01", OS: "Linux",
			Interfaces: []models.Interface{{Name: "eth0", Addresses: []string{"10.0.1.10/24"}}}},
	}}
	hashes := &transfertest.HashRepo{Hashes: map[uuid.UUID]models.Hash{
		w.hashID: {HashID: w.hashID, OperationID: w.op, Value: "aad3b435", Status: models.HashStatusCracked},
	}}
	creds := transfertest.NewCredentialRepo(models.Credential{
		CredentialID: w.credID, OperationID: w.op, Name: "svc_backup", Username: "backup", Password: "Winter2026!",
	})
	w.exporter = NewExporter(images, files, store, store, docs, hosts, hashes, pmRenderer{},
		transfertest.NewDrawingRenderer(images, store), creds, &w.viewer, zap.NewNop(), Config{})
	return w
}

func (w *world) run(t *testing.T) (map[string]string, *wikitransfer.ExportReport) {
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

// script unwraps `window.<global>(…);` and decodes its last argument.
func script(t *testing.T, body, global string, into any) {
	t.Helper()
	prefix := "window." + global + "("
	if !strings.HasPrefix(body, prefix) || !strings.HasSuffix(body, ");\n") {
		t.Fatalf("not a %s script: %.80q", global, body)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(body, prefix), ");\n")
	if global == "__logosPage" || global == "__logosFile" {
		// The id comes first, as a JSON string.
		comma := strings.Index(inner, ",")
		inner = inner[comma+1:]
	}
	if err := json.Unmarshal([]byte(inner), into); err != nil {
		t.Fatalf("decode %s: %v", global, err)
	}
}

// find returns every node of the given type, depth first.
func find(n map[string]any, typ string) []map[string]any {
	var out []map[string]any
	if n["type"] == typ {
		out = append(out, n)
	}
	for _, c := range n["content"].([]any) {
		if child, ok := c.(map[string]any); ok {
			if _, has := child["content"]; !has {
				child["content"] = []any{}
			}
			out = append(out, find(child, typ)...)
		}
	}
	return out
}

func TestExport_LayoutAndTree(t *testing.T) {
	w := newWorld(t)
	entries, report := w.run(t)

	for _, name := range append([]string{"data/tree.js", "REPORT.json",
		"data/pages/" + w.network.DocumentID.String() + ".js",
		"data/pages/" + w.peering.DocumentID.String() + ".js",
		"data/pages/" + w.diagram.DocumentID.String() + ".js",
		"images/" + w.imageID.String() + ".png",
		"files/" + w.fileID.String() + "/report final.pdf",
	}, ViewerFiles...) {
		if _, ok := entries["acme/"+name]; !ok {
			t.Errorf("missing acme/%s", name)
		}
	}

	var tree treeData
	script(t, entries["acme/data/tree.js"], "__logosExport", &tree)
	if tree.Version != FormatVersion || tree.Title != "ACME" || len(tree.Pages) != 3 {
		t.Fatalf("tree = %+v", tree)
	}
	got := []string{tree.Pages[0].Title, tree.Pages[1].Title, tree.Pages[2].Title}
	if strings.Join(got, ",") != "Network,Peering,Diagram" {
		t.Errorf("tree order = %v", got)
	}
	if tree.Pages[1].ParentID != w.network.DocumentID.String() || tree.Pages[0].ParentID != "" {
		t.Errorf("parents = %q, %q", tree.Pages[0].ParentID, tree.Pages[1].ParentID)
	}
	if tree.Pages[0].Emoji != "🌐" || tree.Pages[2].Kind != "drawing" {
		t.Errorf("identity = %+v / %+v", tree.Pages[0], tree.Pages[2])
	}
	for _, want := range []string{"see", "Peering", "in-bgp01", "aad3b435", "svc_backup", "report final.pdf"} {
		if !strings.Contains(tree.Pages[0].Text, want) {
			t.Errorf("search text %q lacks %q", tree.Pages[0].Text, want)
		}
	}
	if !strings.Contains(tree.Pages[2].Text, "Router") {
		t.Errorf("drawing search text = %q", tree.Pages[2].Text)
	}
	if report.ExportedDocs != 3 || report.SkippedDocs != 0 || report.Format != "html" {
		t.Errorf("report = %+v", report)
	}
}

func TestExport_ResolvesChipsAndRewritesLinks(t *testing.T) {
	w := newWorld(t)
	entries, report := w.run(t)

	var page pageData
	script(t, entries["acme/data/pages/"+w.network.DocumentID.String()+".js"], "__logosPage", &page)
	if page.Kind != "document" {
		t.Fatalf("kind = %q", page.Kind)
	}
	var doc map[string]any
	if err := json.Unmarshal(page.Doc, &doc); err != nil {
		t.Fatal(err)
	}

	pages := find(doc, "wikiDocumentReference")
	exp := func(n map[string]any) map[string]any {
		e, _ := n["attrs"].(map[string]any)[ExportAttr].(map[string]any)
		return e
	}
	if e := exp(pages[0]); e["title"] != "Peering" || e["href"] != "#/page/"+w.peering.DocumentID.String() {
		t.Errorf("in-scope page chip = %v", e)
	}
	if e := exp(pages[1]); e["title"] != "Runbook" || e["href"] != nil {
		t.Errorf("out-of-scope page chip = %v", e)
	}
	if e := exp(pages[2]); e["missing"] != true || e["title"] != nil {
		t.Errorf("another operation's page must not leak its title: %v", e)
	}
	if e := exp(find(doc, "wikiHostReference")[0]); e["hostname"] != "in-bgp01" || e["os"] != "Linux" ||
		!strings.Contains(fmt.Sprint(e["interfaces"]), "10.0.1.10/24") {
		t.Errorf("host chip = %v", e)
	}
	if e := exp(find(doc, "wikiHashReference")[0]); e["value"] != "aad3b435" || e["status"] != "CRACKED" {
		t.Errorf("hash chip = %v", e)
	}
	if e := exp(find(doc, "wikiCredentialReference")[0]); e["name"] != "svc_backup" || e["password"] != "Winter2026!" {
		t.Errorf("credential chip = %v", e)
	}

	var hrefs []any
	for _, n := range find(doc, "text") {
		marks, _ := n["marks"].([]any)
		for _, m := range marks {
			hrefs = append(hrefs, m.(map[string]any)["attrs"].(map[string]any)["href"])
		}
	}
	want := []any{"#/page/" + w.peering.DocumentID.String(), nil, "https://example.com/x"}
	if len(hrefs) != 3 || hrefs[0] != want[0] || hrefs[1] != want[1] || hrefs[2] != want[2] {
		t.Errorf("link hrefs = %v, want %v", hrefs, want)
	}

	imgs := find(doc, "image")
	wantSrc := "images/" + w.imageID.String() + ".png"
	if imgs[0]["attrs"].(map[string]any)["src"] != wantSrc || imgs[1]["attrs"].(map[string]any)["src"] != wantSrc {
		t.Errorf("image srcs = %v", imgs)
	}
	if url := find(doc, "wikiFile")[0]["attrs"].(map[string]any)["url"]; url != "files/"+w.fileID.String()+"/report%20final.pdf" {
		t.Errorf("file url = %v", url)
	}
	// Shown twice, written once.
	if report.ImagesExported != 1 || report.FilesExported != 2 || report.CredentialsExported != 1 {
		t.Errorf("report = %+v", report)
	}
}

func TestExport_DrawingCarriesItsScene(t *testing.T) {
	w := newWorld(t)
	entries, _ := w.run(t)
	var page struct {
		Kind  string `json:"kind"`
		Scene struct {
			Type     string            `json:"type"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"scene"`
	}
	script(t, entries["acme/data/pages/"+w.diagram.DocumentID.String()+".js"], "__logosPage", &page)
	if page.Kind != "drawing" || page.Scene.Type != "excalidraw" || len(page.Scene.Elements) != 1 {
		t.Errorf("drawing page = %+v", page)
	}
}

func TestExport_FailedPageStaysInTheTree(t *testing.T) {
	w := newWorld(t)
	w.network.ContentState = []byte("fail")
	w.scope.States = transfertest.NewDocRepo(w.network, w.peering, w.diagram)
	entries, report := w.run(t)

	var tree treeData
	script(t, entries["acme/data/tree.js"], "__logosExport", &tree)
	if !tree.Pages[0].Missing || tree.Pages[1].Missing {
		t.Errorf("missing flags = %v, %v", tree.Pages[0].Missing, tree.Pages[1].Missing)
	}
	if _, ok := entries["acme/data/pages/"+w.network.DocumentID.String()+".js"]; ok {
		t.Error("a failed page must not write a data file")
	}
	if report.SkippedDocs != 1 || !strings.HasPrefix(report.Skipped[0].Reason, "render_failed") {
		t.Errorf("report = %+v", report)
	}
}

func TestExport_RefusesWithoutAViewer(t *testing.T) {
	w := newWorld(t)
	w.viewer.err = ErrViewerNotBuilt
	_, err := w.exporter.Run(context.Background(), zip.NewWriter(io.Discard), w.scope, nil)
	if !errors.Is(err, ErrViewerNotBuilt) {
		t.Fatalf("err = %v", err)
	}
}

func TestExport_PreviewableFilesTravelAsScripts(t *testing.T) {
	w := newWorld(t)
	entries, _ := w.run(t)

	// The text file is fetched by the file card's preview, so its bytes come
	// along as a script; the PDF is shown by an iframe and needs none.
	var b64 string
	script(t, entries["acme/data/files/"+w.notesID.String()+".js"], "__logosFile", &b64)
	if got, _ := base64.StdEncoding.DecodeString(b64); string(got) != "22/tc" {
		t.Errorf("preview bytes = %q", got)
	}
	if _, ok := entries["acme/data/files/"+w.fileID.String()+".js"]; ok {
		t.Error("a PDF needs no preview script")
	}
	if entries["acme/files/"+w.notesID.String()+"/sweep.txt"] != "22/tc" {
		t.Error("the file itself must still be in the zip")
	}
}

func TestInlinePreviewable(t *testing.T) {
	for _, c := range []struct {
		ct   string
		size int64
		want bool
	}{
		{"text/plain", 10, true},
		{"text/plain; charset=utf-8", 10, true},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", 10, true},
		{"application/pdf", 10, false},
		{"image/png", 10, false},
		{"video/mp4", 10, false},
		{"text/plain", 0, false},
		{"text/plain", maxInlinePreviewBytes + 1, false},
	} {
		if got := inlinePreviewable(c.ct, c.size); got != c.want {
			t.Errorf("inlinePreviewable(%q, %d) = %v", c.ct, c.size, got)
		}
	}
}
