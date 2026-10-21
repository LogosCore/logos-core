package htmlbundle

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wiki"
)

// ExportAttr is the node attribute the exporter writes what the editor would
// have looked up live. The viewer's chip extensions declare it; the app's do
// not, so a bundle's JSON never round-trips into a stored document carrying
// it.
const ExportAttr = "export"

var (
	imageURL    = regexp.MustCompile(`/api/v1/wiki/images/([0-9a-fA-F-]{36})`)
	fileURL     = regexp.MustCompile(`/api/v1/wiki/files/([0-9a-fA-F-]{36})`)
	pageLink    = regexp.MustCompile(`^logos://doc/([0-9a-fA-F-]{36})$`)
	appPageLink = regexp.MustCompile(`/wiki/([0-9a-fA-F-]{36})(?:[/?#]|$)`)
)

// blockTypes end a line in the search text, so words in two paragraphs do not
// run together.
var blockTypes = map[string]bool{
	"paragraph": true, "heading": true, "codeBlock": true, "listItem": true,
	"taskItem": true, "tableCell": true, "tableHeader": true, "blockquote": true,
	"wikiNotice": true, "wikiChecklistItem": true,
}

// transform rewrites one page's ProseMirror JSON in place for the viewer.
type transform struct {
	run   *exportRun
	ctx   context.Context
	doc   models.WikiDocument
	label string
	text  strings.Builder
}

func (t *transform) node(n map[string]any) {
	typ, _ := n["type"].(string)
	attrs, _ := n["attrs"].(map[string]any)

	switch typ {
	case "text":
		if s, ok := n["text"].(string); ok {
			t.text.WriteString(s)
		}
		t.marks(n)
	case "image":
		if attrs != nil {
			attrs["src"] = t.imageSrc(attrs["src"])
		}
	case "wikiFile":
		t.wikiFile(attrs)
	case "wikiDocumentReference":
		t.pageChip(attrs)
	case "wikiHostReference":
		t.hostChip(attrs)
	case "wikiHashReference":
		t.hashChip(attrs)
	case "wikiCredentialReference":
		t.credentialChip(attrs)
	case "wikiChecklistItem":
		if p, ok := attrs["prompt"].(string); ok && p != "" {
			t.text.WriteString(p + "\n")
		}
	}

	if kids, ok := n["content"].([]any); ok {
		for _, k := range kids {
			if child, ok := k.(map[string]any); ok {
				t.node(child)
			}
		}
	}
	if blockTypes[typ] {
		t.text.WriteString("\n")
	}
}

// marks rewrites link hrefs. A link to a page in the export points at it in
// the viewer; an in-app link to a page outside it loses its href, because a
// path like /wiki/<id> resolves against the disk and leads nowhere. A full
// URL to the app is left alone: it still works for a reader who has access.
func (t *transform) marks(n map[string]any) {
	marks, _ := n["marks"].([]any)
	for _, m := range marks {
		mark, ok := m.(map[string]any)
		if !ok || mark["type"] != "link" {
			continue
		}
		attrs, _ := mark["attrs"].(map[string]any)
		href, _ := attrs["href"].(string)
		if href == "" {
			continue
		}
		if next, ok := t.rewriteHref(href); ok {
			attrs["href"] = next
		} else {
			delete(attrs, "href")
		}
	}
}

// rewriteHref returns the href a link should carry in the bundle, or false
// for one that should carry none.
func (t *transform) rewriteHref(href string) (string, bool) {
	if m := pageLink.FindStringSubmatch(href); m != nil {
		return t.pageHref(m[1])
	}
	if m := fileURL.FindStringSubmatch(href); m != nil {
		if id, err := uuid.Parse(m[1]); err == nil {
			return t.run.file(t.ctx, t.doc, t.label, id)
		}
	}
	if m := imageURL.FindStringSubmatch(href); m != nil {
		if id, err := uuid.Parse(m[1]); err == nil {
			return t.run.image(t.ctx, t.doc, t.label, id)
		}
	}
	if m := appPageLink.FindStringSubmatch(href); m != nil {
		if next, ok := t.pageHref(m[1]); ok {
			return next, true
		}
		return href, !strings.HasPrefix(href, "/")
	}
	return href, true
}

func (t *transform) pageHref(raw string) (string, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", false
	}
	if _, ok := t.run.inScope[id]; !ok {
		return "", false
	}
	return "#/page/" + id.String(), true
}

func (t *transform) imageSrc(v any) any {
	src, _ := v.(string)
	m := imageURL.FindStringSubmatch(src)
	if m == nil {
		return v
	}
	id, err := uuid.Parse(m[1])
	if err != nil {
		return v
	}
	if p, ok := t.run.image(t.ctx, t.doc, t.label, id); ok {
		return p
	}
	return ""
}

func (t *transform) wikiFile(attrs map[string]any) {
	if attrs == nil {
		return
	}
	if name, ok := attrs["filename"].(string); ok {
		t.text.WriteString(name + "\n")
	}
	raw, _ := attrs["fileId"].(string)
	if raw == "" {
		if u, ok := attrs["url"].(string); ok {
			if m := fileURL.FindStringSubmatch(u); m != nil {
				raw = m[1]
			}
		}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		attrs["url"] = ""
		return
	}
	if p, ok := t.run.file(t.ctx, t.doc, t.label, id); ok {
		attrs["url"] = p
	} else {
		attrs["url"] = ""
	}
}

// pageChip carries the linked page's title and icon, and where it lives in
// the viewer when it came along. A page outside the export keeps its title
// when this export may show it (same operation, or the Public tree), as the
// markdown export does.
func (t *transform) pageChip(attrs map[string]any) {
	id, ok := chipID(attrs, "documentId")
	if !ok {
		return
	}
	if linked, in := t.run.inScope[id]; in {
		attrs[ExportAttr] = pageChipData(linked, "#/page/"+id.String())
		t.text.WriteString(linked.Title)
		return
	}
	if t.run.e.docRepo == nil {
		return
	}
	linked, err := t.run.e.docRepo.FindByID(t.ctx, id)
	if err != nil || linked.DeletedAt != nil ||
		(linked.OperationID != t.doc.OperationID && !models.IsPublicOperation(linked.OperationID)) {
		t.run.report.Warn(t.label, "page_reference_unresolved: "+id.String())
		attrs[ExportAttr] = map[string]any{"missing": true}
		return
	}
	t.run.report.Warn(t.label, "page_reference_outside_scope: "+id.String())
	attrs[ExportAttr] = pageChipData(linked, "")
	t.text.WriteString(linked.Title)
}

func pageChipData(d models.WikiDocument, href string) map[string]any {
	out := map[string]any{
		"title": strings.TrimSpace(d.Title), "emoji": d.Emoji, "icon": d.Icon,
		"color": d.Color, "kind": string(d.Kind.Or()),
	}
	if href != "" {
		out["href"] = href
	}
	return out
}

func (t *transform) hostChip(attrs map[string]any) {
	id, ok := chipID(attrs, "hostId")
	if !ok {
		return
	}
	if t.run.e.hostRepo == nil {
		return
	}
	h, err := t.run.e.hostRepo.FindByID(t.ctx, id)
	if err != nil || h.OperationID != t.doc.OperationID {
		t.run.report.Warn(t.label, "host_reference_unresolved: "+id.String())
		attrs[ExportAttr] = map[string]any{"missing": true}
		return
	}
	ifaces := make([]map[string]any, 0, len(h.Interfaces))
	for _, i := range h.Interfaces {
		ifaces = append(ifaces, map[string]any{"name": i.Name, "addresses": i.Addresses})
	}
	attrs[ExportAttr] = map[string]any{
		"hostname": strings.TrimSpace(h.Hostname), "description": h.Description, "os": h.OS,
		"emoji": h.Emoji, "icon": h.Icon, "color": h.Color, "interfaces": ifaces,
	}
	t.text.WriteString(h.Hostname)
}

func (t *transform) hashChip(attrs map[string]any) {
	id, ok := chipID(attrs, "hashId")
	if !ok {
		return
	}
	if t.run.e.hashRepo == nil {
		return
	}
	h, err := t.run.e.hashRepo.FindByID(t.ctx, id)
	if err != nil || h.OperationID != t.doc.OperationID {
		t.run.report.Warn(t.label, "hash_reference_unresolved: "+id.String())
		attrs[ExportAttr] = map[string]any{"missing": true}
		return
	}
	attrs[ExportAttr] = map[string]any{"value": h.Value, "status": string(h.Status)}
	t.text.WriteString(h.Value)
}

// credentialChip writes the whole record. Credentials here are operation
// records an operator typed in, not secrets this export has to keep back —
// the same call the markdown export makes.
func (t *transform) credentialChip(attrs map[string]any) {
	id, ok := chipID(attrs, "credentialId")
	if !ok {
		return
	}
	if t.run.e.credentials == nil {
		return
	}
	c, err := t.run.e.credentials.FindByID(t.ctx, id)
	if err != nil || c.CredentialID == uuid.Nil || c.OperationID != t.doc.OperationID {
		t.run.report.CredentialsTombstoned++
		attrs[ExportAttr] = map[string]any{"missing": true}
		return
	}
	attrs[ExportAttr] = map[string]any{
		"name": c.Name, "type": string(c.Type), "username": c.Username, "password": c.Password,
		"keys": c.Keys, "properties": c.Properties, "validity": string(c.Validity), "tags": c.Tags,
	}
	t.run.report.CredentialsExported++
	t.text.WriteString(c.Name)
}

func chipID(attrs map[string]any, key string) (uuid.UUID, bool) {
	raw, _ := attrs[key].(string)
	id, err := uuid.Parse(raw)
	return id, err == nil
}

// drawingText is a drawing's words, for search: the text of every text
// element, one per line.
func drawingText(file wiki.ExcalidrawFile) string {
	var b strings.Builder
	for _, raw := range file.Elements {
		var el struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			IsDeleted bool   `json:"isDeleted"`
		}
		if json.Unmarshal(raw, &el) != nil || el.IsDeleted || el.Type != "text" || el.Text == "" {
			continue
		}
		b.WriteString(el.Text)
		b.WriteString("\n")
	}
	return b.String()
}
