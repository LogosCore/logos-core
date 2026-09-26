package markdown

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// referenceLinkFull matches a whole inline reference chip as the sidecar
// serializer writes it: `[label](logos://<kind>/<id>)`. The label is the
// generic word the serializer uses ("page", "host", "hash") because the
// sidecar only sees ids; the exporter is the first place that knows what
// the id names.
var referenceLinkFull = regexp.MustCompile(`\[([^\]\n]*)\]\(logos://(doc|host|hash)/([0-9a-fA-F-]{36})\)`)

// ReferenceTarget is what a chip becomes in the foreign markdown. An empty
// Href renders as plain text — the reader still sees what was referenced,
// there is just nowhere to send them.
type ReferenceTarget struct {
	Text string
	Href string
}

// ReferenceResolver maps one chip to its rendering. Returning ok=false
// means "nothing better known": the chip is lowered to its original label
// as plain text so no logos:// link ever reaches the reader.
//
// Where the chip points is the caller's to decide, because it depends on
// where the markdown is going: a zip sends a page chip to that page's file,
// while a single page copied out of the editor sends it to the page in the
// app.
type ReferenceResolver func(kind string, id uuid.UUID) (ReferenceTarget, bool)

// RewriteReferenceLinks replaces every `[label](logos://kind/id)` link with
// the resolver's rendering. Markdown that leaves Logos is for readers that
// know nothing about it, so the output never contains the logos:// scheme:
// a resolved page becomes `[Title](href)`, a host or hash becomes its
// display value, and anything unresolvable becomes the label.
func RewriteReferenceLinks(body string, resolve ReferenceResolver) string {
	return referenceLinkFull.ReplaceAllStringFunc(body, func(match string) string {
		m := referenceLinkFull.FindStringSubmatch(match)
		label, kind := m[1], m[2]
		id, err := uuid.Parse(m[3])
		if err != nil {
			return label
		}
		target, ok := resolve(kind, id)
		if !ok || target.Text == "" {
			return label
		}
		if target.Href == "" {
			return target.Text
		}
		return "[" + escapeLinkText(target.Text) + "](" + target.Href + ")"
	})
}

// escapeLinkText makes a title safe as markdown link text. Titles are free
// text; an unescaped `]` would end the link early.
func escapeLinkText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)
	return r.Replace(s)
}

// relativeLink returns the path of target relative to the directory that
// holds from, in the `../a/b.md` form markdown editors resolve once the
// zip is unpacked. Both arguments are zip-internal paths with forward
// slashes.
func relativeLink(from, to string) string {
	fromDirs := splitDirs(from)
	toSegs := strings.Split(to, "/")
	toDirs := toSegs[:len(toSegs)-1]

	common := 0
	for common < len(fromDirs) && common < len(toDirs) && fromDirs[common] == toDirs[common] {
		common++
	}

	var b strings.Builder
	for i := common; i < len(fromDirs); i++ {
		b.WriteString("../")
	}
	b.WriteString(strings.Join(toSegs[common:], "/"))
	return b.String()
}

// splitDirs returns the directory segments of a zip path, excluding the
// final file name.
func splitDirs(p string) []string {
	segs := strings.Split(p, "/")
	return segs[:len(segs)-1]
}
