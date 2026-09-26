package mcp

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Guarding the reference chip.
//
// `logos://` names two unrelated things and only one of them belongs in a
// page body:
//
//   - `[page](logos://doc/<id>)`, `[host](logos://host/<id>)` and
//     `[hash](logos://hash/<id>)` are reference chips. The sidecar's parser
//     lowers those three links into inline atoms, which render as a live chip
//     carrying the subject's current title and a link the operator can follow.
//   - `logos://guide`, `logos://session/focus` and
//     `logos://op/<op>/wiki/<doc>` are MCP resource URIs: addresses an agent
//     reads a record through. On a page they are inert text.
//
// The second kind reaches pages constantly, and not out of carelessness. It
// is what the resource surface hands out — an agent that has just read
// `logos://op/<op>/wiki/<doc>` has been given something that looks exactly
// like a link to that page, and the guides then tell it to link pages with
// `logos://`. The failure is silent in the way that matters: the write is
// accepted, the markdown reads back the way it was sent, and the operator
// finds a link that goes nowhere.
//
// So a logos:// link the renderer would not turn into a chip is refused
// before the write lands, naming the three that work. Same reasoning as a
// malformed credential fence, whose header tells the same story from the
// other direction: the contract is invisible from the tool schema, and a
// refusal an agent can read and correct beats a write that quietly did less
// than it claimed. See credential_fence.go.

// referenceChipSegments are the path segments the sidecar lowers into chips,
// mapped to the label the guides use for each. Must stay in step with
// REFERENCE_CHIP_KINDS in hocuspocus/src/markdown-serializer.ts.
var referenceChipSegments = map[string]string{
	"doc":  "page",
	"host": "host",
	"hash": "hash",
}

// resourceRoots are the first segments of the MCP resource URIs in
// resources.go. They earn a different refusal from an invented kind: the
// agent did not guess these, it read them off the resource surface, so the
// correction is "that is how you read, this is how you link" rather than
// "no such kind".
var resourceRoots = map[string]bool{
	"op":      true,
	"guide":   true,
	"session": true,
}

// logosURI matches a logos:// URI wherever it sits — inside a markdown link
// target or bare in prose. The excluded bytes are what closes a link, a
// sentence or a code span around one.
var logosURI = regexp.MustCompile("logos://[^\\s)\\]}>\"'`]*")

// checkReferenceLinks refuses markdown carrying a logos:// link that would
// not render as a reference chip.
//
// Code is skipped — both fenced blocks and inline spans. A page explaining
// the scheme (this is how the guides are written) is content, not a mistake,
// and quoting the URI is the escape hatch the refusals point at.
func checkReferenceLinks(markdown string) error {
	var openDelim string

	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		delim := leadingFenceDelimiter(trimmed)

		if openDelim == "" {
			if delim != "" {
				openDelim = delim
				continue
			}
			for _, uri := range logosURI.FindAllString(stripInlineCode(line), -1) {
				if err := validateReferenceLink(uri); err != nil {
					return err
				}
			}
			continue
		}

		// Inside a fence: a closing fence is the same character, at least as
		// long, and carries no info string. Everything else is content.
		if delim != "" && delim[0] == openDelim[0] && len(delim) >= len(openDelim) &&
			strings.TrimSpace(trimmed[len(delim):]) == "" {
			openDelim = ""
		}
	}
	return nil
}

// validateReferenceLink reports what is wrong with one logos:// URI, or nil
// when it is a chip the renderer will make.
func validateReferenceLink(uri string) error {
	rest := strings.TrimPrefix(uri, referenceLinkScheme)
	segment, id, _ := strings.Cut(rest, "/")

	if label, ok := referenceChipSegments[segment]; ok {
		if isCanonicalUUID(id) {
			return nil
		}
		if id == "" {
			return refuse(
				"%s has no id, so it renders as plain text rather than a %s chip. "+
					"Write it as logos://%s/<uuid>, with the id a tool returned.",
				uri, label, segment)
		}
		return refuse(
			"%s does not name a %s: the id has to be the full uuid a tool returned, not "+
				"a name or a fragment. A chip whose id resolves to nothing renders on the "+
				"page as a missing-record placeholder.",
			uri, label)
	}

	if segment == "credential" {
		return refuse(
			"a credential is a block, not a link: %s resolves to nothing and renders as "+
				"plain text. Fence it instead:\n\n%s\n\nTake the id from find_credentials "+
				"or get_credential.",
			uri, "```logos-credential\n{\"id\": \"<credential-uuid>\"}\n```")
	}

	if resourceRoots[segment] {
		return refuse(
			"%s is a resource address — it is how you read that record, not a link the "+
				"operator can follow, and on a page it renders as dead text. To link a "+
				"page use its chip, [page](logos://doc/<id>), with the uuid from "+
				"list_wiki_tree or search_wiki. Quote the URI in backticks if you meant "+
				"to write about it.",
			uri)
	}

	return refuse(
		"%s is not a reference chip. Three kinds render as one: "+
			"[page](logos://doc/<id>), [host](logos://host/<id>) and "+
			"[hash](logos://hash/<id>). A credential is a logos-credential block "+
			"instead; anything else has no chip, so name it in prose or link the page "+
			"that covers it. Quote the URI in backticks if you meant to write about it.",
		uri)
}

// referenceLinkScheme is the scheme both directions key on. It must stay in
// step with REFERENCE_LINK_SCHEME in hocuspocus/src/markdown-serializer.ts.
const referenceLinkScheme = "logos://"

// isCanonicalUUID reports whether id is a uuid in the 36-character dashed
// form.
//
// The stricter of the two readers decides this. The sidecar's parser accepts
// any non-empty id and makes a chip out of it, but the export rewriter's
// pattern in wikitransfer/markdown/references.go matches only the 36-char
// form — so a uuid written any other way would render in the app and then
// survive an export as a raw logos:// link in someone else's markdown.
func isCanonicalUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	_, err := uuid.Parse(id)
	return err == nil
}

// stripInlineCode blanks the contents of inline code spans so a URI quoted in
// backticks is not read as a link. Returns a string of the same length, which
// keeps the result usable for anything that reports an offset later.
func stripInlineCode(line string) string {
	out := []byte(line)
	i := 0
	for i < len(out) {
		if out[i] != '`' {
			i++
			continue
		}
		open := i
		for i < len(out) && out[i] == '`' {
			i++
		}
		runLen := i - open
		// A closing run is the same length; an unmatched opener is literal
		// text and the rest of the line stays visible.
		for j := i; j < len(out); j++ {
			if out[j] != '`' {
				continue
			}
			end := j
			for end < len(out) && out[end] == '`' {
				end++
			}
			if end-j != runLen {
				j = end - 1
				continue
			}
			for k := i; k < j; k++ {
				out[k] = ' '
			}
			i = end
			break
		}
		if i == open+runLen {
			// No closing run: nothing on this line is code.
			break
		}
	}
	return string(out)
}
