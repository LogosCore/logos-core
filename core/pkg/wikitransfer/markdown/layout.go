// Package markdown is the foreign wiki transfer format: an Outline-flavoured
// markdown zip that other tools produce and consume. Lossy by design — the
// native bundle (package bundle) is the lossless one. The export is tuned
// for markdown editors that know nothing about Logos (Obsidian, VS Code,
// GitHub): links between pages and to attachments are ordinary relative
// paths that resolve once the zip is unpacked.
//
// The Exporter writes a Scope to this layout and ReadPlan reads it back:
//
//	<rootSlug>/
//	  <doc-a>.md             ← leaf
//	  <doc-b>.md             ← branch (has children)
//	  <doc-b>/               ← children of doc-b, same naming rules
//	    <child>.md
//	  <drawing>.md           ← stub page for a drawing
//	  <drawing>.excalidraw   ← its scene, beside it under the same name
//	  uploads/<documentId>/<attId>/<filename>
//
// Files are named after their page and nothing else. They used to carry the
// page's position among its siblings as a `001-` prefix, so that a re-import
// could rebuild the tree's order — which is an ordering only Logos has an
// opinion about, and it read as wrong everywhere else: a Findings folder
// whose newest page sorts first gave `001-fnd-006…` through `006-fnd-001…`,
// numbered backwards in every file browser. Sorted by name, the titles now
// carry whatever order their author gave them.
//
// See docs/wiki-outline-import.md §2 for the layout invariants the
// importer enforces — this builder is the inverse.
package markdown

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Filename collision-safe slugger. Lowercases, replaces anything that is
// not a letter or digit in any script with "-", collapses runs, trims, caps
// length. Caller suffixes "-2", "-3" when the slug collides inside a
// sibling folder. Zip entry names are UTF-8, so a Cyrillic or CJK title
// keeps its letters rather than collapsing to "untitled".
var nonSlugRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// slugMaxRunes caps a slug's length in characters, not bytes, so a
// multibyte title is never cut mid-rune.
const slugMaxRunes = 80

// slugify produces a filesystem-safe slug from a document title. The output
// is never empty — titles with no letters or digits fall back to "untitled"
// so the importer always has a parseable filename.
func slugify(title string) string {
	lowered := strings.ToLower(title)
	slug := nonSlugRe.ReplaceAllString(lowered, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "untitled"
	}
	if runes := []rune(slug); len(runes) > slugMaxRunes {
		slug = strings.TrimRight(string(runes[:slugMaxRunes]), "-")
		if slug == "" {
			slug = "untitled"
		}
	}
	return slug
}

// uniqueSlug returns slug, slug-2, slug-3, … such that it doesn't collide
// inside used. Mutates `used` to mark the result as taken.
func uniqueSlug(slug string, used map[string]struct{}) string {
	candidate := slug
	for i := 2; ; i++ {
		if _, taken := used[candidate]; !taken {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", slug, i)
	}
}

// buildDocFilename produces the leaf filename for a document: its slug and
// the extension, with nothing in front. See the package comment for why the
// sibling index is not part of it.
func buildDocFilename(slug string) string {
	return slug + ".md"
}

// buildChildrenFolder produces the sibling folder name that holds a
// branch document's children. Matches the leaf filename without the `.md`
// extension, so the importer's "<name>.md + <name>/ folder" convention
// holds (see docs/wiki-outline-import.md §2.2).
func buildChildrenFolder(slug string) string {
	return slug
}

// uploadsZipPath returns the path inside the export zip for an attachment
// belonging to a specific document. Mirrors the Outline layout
// `uploads/<userId>/<attId>/<filename>` — we substitute documentId for the
// userId segment because Logos attachments are document-scoped, not
// user-scoped. The markdown body links to it by the path relative to the
// document's own folder (see relativeLink); the importer keys blobs off
// the `uploads/…` suffix and tolerates the `../` prefixes.
func uploadsZipPath(rootFolder string, docID, attID uuid.UUID, filename string) string {
	return path.Join(
		rootFolder,
		"uploads",
		docID.String(),
		attID.String(),
		filename,
	)
}
