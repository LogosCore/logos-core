// The parser below ingests an Outline (getoutline.com) markdown-format
// workspace export; ReadPlan (import.go) turns its output into a Plan.
//
// See docs/wiki-outline-import.md for the format spec and
// docs/wiki-outline-import-implementation-plan.md for the architecture.
//
// The parser in this file is a pure function: it takes an *archive/zip
// reader and produces an in-memory tree describing the export. It does
// not touch Mongo, S3, or the Hocuspocus sidecar — that's the job of
// orchestrator.go.
package markdown

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// logosMetaPattern matches the metadata comment the exporter used to emit
// directly after the H1 line, carrying WikiDocument.Icon and Color.
//
//	<!-- logos:meta icon="Adaptive" color="#1f2937" -->
//
// The exporter no longer writes one: an HTML comment renders as nothing in
// every markdown viewer, which made it a field only Logos could read in a
// format meant for everyone else (see render.go). This stays because zips
// exported before that change carry one, and a comment we did not recognise
// would arrive as the first line of the page's body.
//
// Both fields are optional; missing keys are treated as empty.
var logosMetaPattern = regexp.MustCompile(
	`^<!--\s*logos:meta(?:\s+([^>]*?))?\s*-->\s*$`,
)

// logosMetaField pulls key="value" pairs out of the matched metadata content.
// Values are double-quoted; embedded quotes aren't supported (icon and color
// values are always alphanumeric / hex in practice).
var logosMetaField = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)

// ParsedExport is the in-memory shape of an Outline markdown export.
type ParsedExport struct {
	// Collections is one entry per top-level folder in the zip. Order
	// follows the case-insensitive sort of the folder names so multiple
	// imports of the same export produce identical trees.
	Collections []*Collection

	// AttachmentBlobs maps every uploads/... entry the parser saw in the
	// zip to the zip.File we can stream bytes out of later. Keyed by the
	// full zip-internal path (e.g. "test/uploads/<userId>/<attId>/x.pdf").
	AttachmentBlobs map[string]*zip.File

	// SceneBlobs maps every `.excalidraw` file outside uploads to its zip
	// entry, keyed by its path with any wrapper folder removed (see
	// wrapperPrefix). A document claims one through Doc.SceneRef.
	SceneBlobs map[string]*zip.File

	// Warnings lists what the parser did to make the zip importable that
	// the operator should know about: a wrapper folder skipped, a folder
	// that had no page of its own, a page that could not be read. ReadPlan
	// hands them to the import report, so an import that had to improvise
	// says so instead of reporting a clean success.
	Warnings []ParseNote
}

// ParseNote is one entry in ParsedExport.Warnings. Path is the
// zip-internal path the note is about.
type ParseNote struct {
	Path   string
	Reason string
}

// Collection is one top-level folder in the zip. In Outline terms, this
// is one of the user's wiki collections. Names match the folder name.
type Collection struct {
	Name      string
	Documents []*Doc // root-level documents within this collection
}

// Doc is one parsed Outline document. Children are populated for documents
// whose .md has a sibling folder of the same name in the zip.
type Doc struct {
	// Title is the H1 line text (with any leading emoji stripped).
	Title string

	// Emoji is the leading emoji on the H1 line, if any. Empty string
	// when no leading emoji was found.
	Emoji string

	// Icon is the WikiDocument.Icon value carried in the optional
	// `<!-- logos:meta icon="..." color="..." -->` comment a Logos export
	// emits right after the H1. Empty for plain Outline exports.
	Icon string

	// Color is the WikiDocument.Color value carried in the same logos-meta
	// comment as Icon. Empty for plain Outline exports.
	Color string

	// BodyMarkdown is the document body with the H1 line removed and any
	// leading blank line stripped. Attachment URLs are NOT yet rewritten
	// — that happens in the orchestrator after attachments are ingested.
	BodyMarkdown string

	// SortKey is the original .md filename (without extension), used to
	// produce a stable sibling order. Outline's markdown export does not
	// preserve fractional indices, so we sort case-insensitively by
	// filename and assign fresh SortOrder values during ingest.
	SortKey string

	// Children are documents whose markdown lives in a sibling folder of
	// this document's .md file.
	Children []*Doc

	// AttachmentRefs are the zip-internal paths (uploads/...) that this
	// document's body references via image or link syntax. Each entry is
	// a key into ParsedExport.AttachmentBlobs.
	AttachmentRefs []string

	// SceneRef is the zip path of the `.excalidraw` file sitting beside
	// this document's .md, when there is one — a drawing that left a Logos
	// markdown export. A key into ParsedExport.SceneBlobs.
	//
	// The page comes back as prose with the scene attached, not as a
	// drawing: this zip is the foreign format, and rebuilding a canvas from
	// it would be guessing at what another tool may have edited. The
	// native bundle is what restores a drawing as a drawing. What this
	// buys is that the scene is not dropped on the floor, and that the
	// stub's link to it still resolves after a round trip.
	SceneRef string
}

// Parse walks the zip and produces the export tree. Returns an error only
// for invariant violations (path traversal, no markdown at all); a document
// that cannot be read becomes an empty page and a warning, so the import
// reports what it lost rather than aborting.
func Parse(zr *zip.Reader) (*ParsedExport, error) {
	if zr == nil {
		return nil, fmt.Errorf("nil zip reader")
	}

	files, err := indexZip(zr)
	if err != nil {
		return nil, err
	}

	if len(files.collections) == 0 {
		// The usual cause is not a broken zip but the wrong export: Outline's
		// JSON export carries no .md files at all. Saying so beats creating
		// one empty page per folder and reporting success.
		return nil, fmt.Errorf("zip contains no Markdown (.md) documents inside a folder; " +
			"export from Outline with the Markdown format, not JSON")
	}

	out := &ParsedExport{
		AttachmentBlobs: files.attachments,
		SceneBlobs:      files.scenes,
	}
	if files.wrapper != "" {
		out.Warnings = append(out.Warnings, ParseNote{
			Path:   files.wrapper,
			Reason: "wrapper folder around the export was skipped",
		})
	}

	collectionNames := make([]string, 0, len(files.collections))
	for name := range files.collections {
		collectionNames = append(collectionNames, name)
	}
	sort.Slice(collectionNames, func(i, j int) bool {
		return strings.ToLower(collectionNames[i]) < strings.ToLower(collectionNames[j])
	})

	for _, name := range collectionNames {
		mds := files.collections[name]
		coll := &Collection{Name: name}
		coll.Documents = buildDocTree(name, mds, files.scenes, out)
		out.Collections = append(out.Collections, coll)
	}

	return out, nil
}

// --- internals ---

type indexedZip struct {
	// collections maps collection name → list of (path, *zip.File) for
	// every .md file inside that collection. Path is relative to the
	// collection root and includes the .md suffix.
	collections map[string][]mdEntry
	// attachments maps zip-internal path → *zip.File for every file
	// under any "<collection>/uploads/" subtree.
	attachments map[string]*zip.File
	// scenes maps zip-internal path → *zip.File for every `.excalidraw`
	// file outside uploads. A Logos markdown export writes one beside the
	// stub page of each drawing; keying on the full path rather than the
	// filename keeps two drawings with the same slug in different branches
	// apart.
	scenes map[string]*zip.File
	// wrapper is the folder prefix (with trailing slash) stripped from
	// every name, or "" when the zip had none. See wrapperPrefix.
	wrapper string
}

type mdEntry struct {
	relPath string // relative to the collection root, e.g. "test/1234.md"
	file    *zip.File
}

// isArchiverJunk reports whether a zip entry is metadata an archiver added
// rather than part of the export. macOS "Compress" adds a top-level
// __MACOSX/ tree of `._name` resource forks beside the real folder: left in,
// it is a second top-level folder (so the real one is not recognised as a
// wrapper) and every `._page.md` in it parses as a page of binary garbage.
func isArchiverJunk(segments []string) bool {
	if segments[0] == "__MACOSX" {
		return true
	}
	base := segments[len(segments)-1]
	return base == ".DS_Store" || strings.HasPrefix(base, "._")
}

// wrapperPrefix returns the folder that wraps the whole export, such as
// "outline-export/", or "" when there is none.
//
// The parser reads each top-level folder as a collection. A zip somebody
// unzipped and compressed again — or one an archiver built from the folder
// rather than its contents — has a single extra folder around everything,
// which then read as one collection holding no pages of its own: the import
// created one empty page named after the archive and reported success. A
// collection folder always holds at least one page directly, so a single
// top-level folder that holds no files directly is a wrapper. Nested
// wrappers are peeled one at a time.
func wrapperPrefix(names [][]string) string {
	prefix := ""
	depth := 0
	for {
		var top string
		for _, segs := range names {
			if len(segs) <= depth+1 {
				// A file directly at this level: nothing here is a wrapper.
				return prefix
			}
			if top == "" {
				top = segs[depth]
			} else if segs[depth] != top {
				return prefix
			}
		}
		if top == "" {
			return prefix
		}
		// Peel only if a folder level remains under it to read collections
		// from; a folder whose files sit one level down is a collection.
		for _, segs := range names {
			if len(segs) <= depth+2 {
				return prefix
			}
		}
		prefix += top + "/"
		depth++
	}
}

func indexZip(zr *zip.Reader) (*indexedZip, error) {
	out := &indexedZip{
		collections: map[string][]mdEntry{},
		attachments: map[string]*zip.File{},
		scenes:      map[string]*zip.File{},
	}

	// The wrapper is found on the entries that are actually part of the
	// export; unsafe names are refused by the loop below either way.
	var names [][]string
	for _, f := range zr.File {
		segments := strings.Split(f.Name, "/")
		if f.FileInfo().IsDir() || isArchiverJunk(segments) {
			continue
		}
		names = append(names, segments)
	}
	out.wrapper = wrapperPrefix(names)

	for _, f := range zr.File {
		// Reject path traversal up front — same-mountpoint sandbox isn't a
		// substitute for never trusting names from a third-party archive.
		if strings.HasPrefix(f.Name, "/") {
			return nil, fmt.Errorf("zip contains unsafe path: %s", f.Name)
		}
		segments := strings.Split(f.Name, "/")
		for _, seg := range segments {
			// Only ".." as a whole path segment is traversal. Two dots
			// inside a filename (e.g. trailing period + ".md" extension)
			// are legal — Outline emits them when the source title ended
			// in punctuation.
			if seg == ".." {
				return nil, fmt.Errorf("zip contains unsafe path: %s", f.Name)
			}
		}
		if f.FileInfo().IsDir() || isArchiverJunk(segments) {
			continue
		}
		name := strings.TrimPrefix(f.Name, out.wrapper)
		segments = strings.Split(name, "/")

		if len(segments) < 2 {
			// File at zip root — not a collection member, skip silently.
			continue
		}

		collName := segments[0]
		if collName == "" {
			continue
		}

		// Attachments live in some `<...>/uploads/<userId>/<attId>/<filename>`
		// path. Outline puts the `uploads/` directory at the collection root
		// in a single-collection export but DEEP inside each containing
		// folder in a workspace export — `avia.ru/caica.ru/Humans/uploads/...`
		// is real. Markdown refs always say `uploads/<userId>/<attId>/<file>`
		// regardless of doc location, so we key the map by that suffix
		// (`uploads/...`) and let the orchestrator look up exactly what the
		// markdown wrote.
		if uploadsIdx := indexOfUploadsSegment(segments); uploadsIdx >= 0 {
			suffix := strings.Join(segments[uploadsIdx:], "/")
			out.attachments[suffix] = f
			continue
		}

		if strings.HasSuffix(name, drawingExtension) {
			out.scenes[name] = f
			continue
		}

		if !strings.HasSuffix(name, ".md") {
			// Unknown non-markdown file outside uploads — ignore.
			continue
		}

		rel := strings.TrimPrefix(name, collName+"/")
		out.collections[collName] = append(out.collections[collName], mdEntry{
			relPath: rel,
			file:    f,
		})
	}

	return out, nil
}

// indexOfUploadsSegment returns the index of the first segment named
// "uploads" that is followed by at least one more segment (so the entry is
// an attachment file, not an empty uploads dir). Returns -1 when none found.
func indexOfUploadsSegment(segments []string) int {
	for i := 0; i < len(segments)-1; i++ {
		if segments[i] == "uploads" {
			return i
		}
	}
	return -1
}

// buildDocTree turns a flat list of (relPath, file) pairs into a nested Doc
// tree using Outline's parallel-folder convention:
//
//	foo.md         — leaf doc "foo" at the level it was found
//	foo.md + foo/  — doc "foo" with children inside foo/
//	foo/bar.md     — doc "bar" whose parent is "foo"
//	foo/bar.md, no foo.md — an empty page "foo" holding "bar"
//
// The last case is not something Outline writes, but a folder with no page
// beside it used to make every document under it unreachable, silently. It
// gets an empty page named after the folder instead, and a warning.
//
// Sibling order within each level is case-insensitive by filename.
func buildDocTree(collectionName string, entries []mdEntry, scenes map[string]*zip.File, notes *ParsedExport) []*Doc {
	// Group entries by their parent directory (relative to the collection
	// root). Root docs have parent dir == "".
	byParentDir := map[string][]mdEntry{}
	// Every directory that holds a document somewhere below it, keyed by its
	// own parent directory.
	subdirs := map[string]map[string]struct{}{}
	for _, e := range entries {
		dir := cleanDir(path.Dir(e.relPath))
		byParentDir[dir] = append(byParentDir[dir], e)
		for d := dir; d != ""; d = cleanDir(path.Dir(d)) {
			parent := cleanDir(path.Dir(d))
			if subdirs[parent] == nil {
				subdirs[parent] = map[string]struct{}{}
			}
			subdirs[parent][d] = struct{}{}
		}
	}

	// One level's siblings: its .md files, plus folders with no .md of the
	// same name beside them. A folder sorts as if it were "<name>.md", so a
	// zip gains no new ordering rule.
	type sibling struct {
		key   string
		entry *mdEntry
		dir   string
	}
	siblingsOf := func(parentDir string) []sibling {
		var out []sibling
		claimed := map[string]struct{}{}
		for i := range byParentDir[parentDir] {
			e := &byParentDir[parentDir][i]
			out = append(out, sibling{key: strings.ToLower(e.relPath), entry: e})
			claimed[strings.TrimSuffix(e.relPath, ".md")] = struct{}{}
		}
		for d := range subdirs[parentDir] {
			if _, ok := claimed[d]; ok {
				continue
			}
			out = append(out, sibling{key: strings.ToLower(d + ".md"), dir: d})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
		return out
	}

	// Materialise the root level recursively.
	var build func(parentDir string) []*Doc
	build = func(parentDir string) []*Doc {
		var out []*Doc
		for _, s := range siblingsOf(parentDir) {
			if s.entry == nil {
				base := path.Base(s.dir)
				notes.Warnings = append(notes.Warnings, ParseNote{
					Path:   path.Join(collectionName, s.dir) + "/",
					Reason: "folder had no page of its own; created an empty page for it",
				})
				out = append(out, &Doc{Title: base, SortKey: base, Children: build(s.dir)})
				continue
			}
			e := *s.entry
			base := strings.TrimSuffix(path.Base(e.relPath), ".md")
			doc := parseDoc(e.file, base)
			if doc == nil {
				// Skipping it outright would take its children with it. The
				// page's own body is what is lost, so that is what is reported.
				notes.Warnings = append(notes.Warnings, ParseNote{
					Path:   path.Join(collectionName, e.relPath),
					Reason: "could not be read from the zip; created an empty page in its place",
				})
				doc = &Doc{Title: base, SortKey: base}
			}
			// A drawing's scene sits beside its stub, under the same name.
			scenePath := path.Join(collectionName, parentDir, base+drawingExtension)
			if _, ok := scenes[scenePath]; ok {
				doc.SceneRef = scenePath
			}
			// Children of this doc live in a sibling folder named after
			// the doc's base name.
			childDir := path.Join(parentDir, base)
			doc.Children = build(childDir)
			out = append(out, doc)
		}
		return out
	}

	return build("")
}

// cleanDir maps path.Dir's "." for the collection root to "".
func cleanDir(d string) string {
	if d == "." {
		return ""
	}
	return d
}

// parseDoc reads a .md zip entry and extracts the title, emoji, and body.
// Returns nil if the file can't be read — the caller puts an empty page in
// its place and records a warning rather than aborting the whole import.
func parseDoc(file *zip.File, base string) *Doc {
	rc, err := file.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil
	}

	body := string(raw)
	title, emoji, rest := splitH1(body)
	if title == "" {
		// No H1 — fall back to the filename as the title so the import
		// still produces a navigable doc.
		title = base
	}

	icon, color, rest := stripLogosMeta(rest)

	return &Doc{
		Title:          title,
		Emoji:          emoji,
		Icon:           icon,
		Color:          color,
		BodyMarkdown:   rest,
		SortKey:        base,
		AttachmentRefs: scanAttachmentRefs(rest),
	}
}

// splitH1 inspects the first non-empty line of body. If it starts with
// "# " it is treated as the title; the rest of the body is returned with
// the H1 line and one trailing blank line consumed. Returns ("", "", body)
// when no H1 is present.
//
// If the title text starts with an emoji (a single grapheme that is in a
// Unicode emoji-presentation range, generally codepoint >= U+1F000 with
// optional ZWJ sequences), it's stripped from the title and returned as
// the doc's Emoji attribute.
func splitH1(body string) (title, emoji, rest string) {
	lines := strings.SplitN(body, "\n", 2)
	if len(lines) == 0 {
		return "", "", body
	}
	first := strings.TrimRight(lines[0], "\r")
	if !strings.HasPrefix(first, "# ") {
		return "", "", body
	}
	titleLine := strings.TrimSpace(first[2:])
	maybeEmoji, remainder := stripLeadingEmoji(titleLine)
	// If stripping the emoji leaves nothing, the H1 was an emoji-only
	// title (e.g. "# 🚀") — keep it as the title rather than splitting
	// it into a decorative emoji with no text.
	if remainder != "" {
		emoji, titleLine = maybeEmoji, remainder
	}
	rest = ""
	if len(lines) > 1 {
		rest = strings.TrimLeft(lines[1], "\n")
	}
	return titleLine, emoji, rest
}

// stripLogosMeta scans the first non-empty line of body for the optional
// `<!-- logos:meta icon="..." color="..." -->` comment the exporter emits
// to carry WikiDocument.Icon and Color across the round-trip. If found,
// the comment line plus any surrounding blank lines are consumed from the
// body. Returns ("", "", body) for plain Outline-format exports.
//
// We tolerate any blank lines between the H1 and the meta comment so an
// exporter that pads for readability doesn't drop the metadata silently.
func stripLogosMeta(body string) (icon, color, rest string) {
	// Find the first non-empty line. Track its start/end so we can splice
	// it out cleanly when it's a meta line.
	i := 0
	for i < len(body) {
		// Walk past whitespace-only lines.
		lineEnd := strings.IndexByte(body[i:], '\n')
		if lineEnd < 0 {
			lineEnd = len(body) - i
		}
		line := strings.TrimSpace(body[i : i+lineEnd])
		if line != "" {
			m := logosMetaPattern.FindStringSubmatch(line)
			if m == nil {
				return "", "", body
			}
			// Parse key="value" pairs.
			if len(m) > 1 {
				for _, kv := range logosMetaField.FindAllStringSubmatch(m[1], -1) {
					switch kv[1] {
					case "icon":
						icon = kv[2]
					case "color":
						color = kv[2]
					}
				}
			}
			// Splice out the meta line. Consume the trailing newline too,
			// plus one further blank line if present, so the editor's
			// first paragraph doesn't carry a leading empty line.
			cut := i + lineEnd
			if cut < len(body) && body[cut] == '\n' {
				cut++
			}
			if cut < len(body) && body[cut] == '\n' {
				cut++
			}
			rest = body[:i] + body[cut:]
			rest = strings.TrimLeft(rest, "\n")
			return icon, color, rest
		}
		// Empty line — advance past it.
		if lineEnd >= len(body)-i {
			break
		}
		i += lineEnd + 1
	}
	return "", "", body
}

// stripLeadingEmoji extracts the leading emoji codepoint sequence (if any)
// from s and returns it separately along with the remaining text. We use
// a conservative heuristic: the first rune must be in the Supplementary
// Multilingual Plane or above (>= 0x1F000). Outline always serialises an
// emoji directly followed by a space then the title text, so this catches
// the cases the test fixture exercises ("# 😑 test") without needing a
// full emoji-property database.
func stripLeadingEmoji(s string) (emoji, rest string) {
	if s == "" {
		return "", s
	}
	runes := []rune(s)
	first := runes[0]
	if first < 0x1F000 {
		return "", s
	}

	// Consume the leading emoji grapheme cluster. We follow ZWJ joiners
	// and variation selectors so multi-codepoint emojis come out whole.
	end := 1
	for end < len(runes) {
		r := runes[end]
		if r == 0x200D || r == 0xFE0F || r == 0xFE0E {
			end++
			continue
		}
		// Allow trailing emoji modifiers (skin tones, U+1F3FB-U+1F3FF).
		if r >= 0x1F3FB && r <= 0x1F3FF {
			end++
			continue
		}
		// Allow a chain of emoji codepoints joined by ZWJ.
		if r >= 0x1F000 && end > 0 && runes[end-1] == 0x200D {
			end++
			continue
		}
		break
	}

	emoji = string(runes[:end])
	rest = strings.TrimLeft(string(runes[end:]), " \t")
	return emoji, rest
}

// scanAttachmentRefs walks the body markdown and returns every uploads/...
// path it sees in image src or link href positions. Used by the
// orchestrator to determine which zip blobs need to be ingested per
// document.
//
// Outline writes `uploads/…` regardless of the document's depth; a Logos
// markdown export writes the real relative path, `../../uploads/…`. Both
// are accepted: the returned string is the link target verbatim so the
// caller can replace it, and uploadsKey strips the relative prefix for
// the blob lookup.
//
// Returns a deduplicated list preserving first-occurrence order.
func scanAttachmentRefs(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range scanLinkTargets(body) {
		if !strings.HasPrefix(uploadsKey(t), "uploads/") || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// uploadsKey strips the leading `./` and `../` segments from a link
// target, leaving the `uploads/…` suffix the blob index is keyed by.
func uploadsKey(target string) string {
	for {
		switch {
		case strings.HasPrefix(target, "./"):
			target = target[2:]
		case strings.HasPrefix(target, "../"):
			target = target[3:]
		default:
			return target
		}
	}
}

// scanLinkTargets finds every `](<target>)` link destination in
// body. CommonMark §6.6 allows literal
// `(` and `)` inside link destinations as long as they're balanced, so a
// naïve `[^)]+` regex truncates Outline filenames like
// "Логин+пароль(2026).docx" at the first inner `)`. This scanner tracks
// paren depth and stops only at the unbalanced closing `)`.
//
// The returned string is the link destination verbatim as it appears in
// body (URL plus any trailing `<space>"title"`), so the orchestrator can
// pass it directly to strings.ReplaceAll when rewriting.
func scanLinkTargets(body string) []string {
	const needle = "]("
	var out []string
	i := 0
	for i < len(body) {
		hit := strings.Index(body[i:], needle)
		if hit < 0 {
			return out
		}
		start := i + hit + 2 // first byte of the link target, on the prefix
		depth := 0
		end := start
		closed := false
		for end < len(body) {
			c := body[end]
			if c == '\n' || c == '\r' {
				// Markdown link destinations must stay on one line.
				break
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					closed = true
					break
				}
				depth--
			}
			end++
		}
		if closed {
			out = append(out, body[start:end])
		}
		// Advance past the closing paren (or the broken-link position) so we
		// keep scanning for further matches.
		if end < len(body) {
			i = end + 1
		} else {
			i = end
		}
	}
	return out
}
