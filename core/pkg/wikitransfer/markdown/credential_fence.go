package markdown

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wikitransfer"
)

// credentialFenceInfo is the info-string the hocuspocus serializer uses on
// every credential reference fence. Mirrors CREDENTIAL_FENCE_INFO in
// hocuspocus/src/markdown-serializer.ts — any change here MUST be made in
// both places at once or the export and the importer will silently drift.
const credentialFenceInfo = "logos-credential"

// credentialFencePattern matches a fenced code block whose info-string is
// exactly `logos-credential`. The body (group 1) is captured greedily up to
// the first closing fence of equal length on its own line. The hocuspocus
// serializer always emits triple-backtick fences (no language with
// backticks in the JSON), so a tight match on ``` is sufficient.
//
// Multiline `(?s)` flag is set so `.` matches newlines inside the body.
var credentialFencePattern = regexp.MustCompile(
	"(?s)```" + credentialFenceInfo + "\\s*\\n(.*?)\\n```",
)

// CredentialLookup is the export-side credential resolver. Alias of the
// shared interface so the markdown and bundle exporters take one type.
type CredentialLookup = wikitransfer.CredentialLookup

// CredentialDetail is how much of a credential the reader is allowed to see.
type CredentialDetail int

const (
	// CredentialValues writes the credential out in full: username,
	// password, properties, keys. What a wiki export does, gated on the
	// requester holding operator on the operation.
	CredentialValues CredentialDetail = iota
	// CredentialNamesOnly writes the name and kind and stops. For a reader
	// who may read the page but not its secrets — the chip already showed
	// them that a credential belongs here, so naming it tells them nothing
	// the page did not.
	CredentialNamesOnly
)

// LowerCredentialFences walks every `logos-credential` fence in body, looks
// the credential up by id, and replaces the fence with the credential
// written out as ordinary markdown — a bold name, a list of its fields, and
// a code block per key.
//
// The fence is not kept. Its info-string means nothing outside Logos, and
// what a foreign reader would get is a page of JSON where the editor showed
// a credential. The fields themselves are the content and they all survive;
// what goes is the envelope, and with it this zip's ability to re-create the
// credential record on import. That is the bundle's job.
//
// docOperationID gates which credentials may be written out. A chip
// referencing a credential in a different operation is silently lowered
// to a tombstone — chips never carried real cross-op semantics
// (credentials are operation-private; see persistence.ts:35-38) so
// emitting the full record would either leak data or break the
// operation-private boundary.
//
// Returns the rewritten body plus the count of written-out and tombstoned
// references so the orchestrator can roll them into the export report.
func LowerCredentialFences(
	ctx context.Context,
	body string,
	docOperationID uuid.UUID,
	lookup CredentialLookup,
	detail CredentialDetail,
) (rewritten string, hydrated int, tombstoned int) {
	if !strings.Contains(body, credentialFenceInfo) {
		return body, 0, 0
	}

	// Resolve each unique id at most once. A doc that references the same
	// credential five times produces five identical blocks; one lookup is
	// enough.
	rendered := map[string]string{}

	rewritten = credentialFencePattern.ReplaceAllStringFunc(body, func(match string) string {
		bodyMatch := credentialFencePattern.FindStringSubmatch(match)
		if len(bodyMatch) < 2 {
			return match
		}
		fenceBody := bodyMatch[1]

		// Pull the id out of the fence body. The serializer always emits an
		// `id` field; if it's missing or malformed, leave the fence as-is —
		// we don't want to mangle a fence we don't fully understand, and a
		// code block of JSON is at least still the page's content.
		var parsed struct {
			ID      string `json:"id"`
			Deleted bool   `json:"deleted"`
		}
		if err := json.Unmarshal([]byte(fenceBody), &parsed); err != nil {
			return match
		}
		if parsed.ID == "" {
			return match
		}

		if cached, ok := rendered[parsed.ID]; ok {
			return cached
		}

		// A fence already marked deleted, or one naming a credential this
		// export may not read, says the same thing: something was here and
		// is not coming with us.
		tombstone := func() string {
			tomb := buildCredentialTombstone()
			rendered[parsed.ID] = tomb
			tombstoned++
			return tomb
		}
		if parsed.Deleted {
			return tombstone()
		}

		credID, err := uuid.Parse(parsed.ID)
		if err != nil {
			return tombstone()
		}
		if lookup == nil {
			return tombstone()
		}
		cred, err := lookup.FindByID(ctx, credID)
		if err != nil || cred.CredentialID == uuid.Nil || cred.OperationID != docOperationID {
			return tombstone()
		}

		full := buildCredentialMarkdown(&cred, detail)
		rendered[parsed.ID] = full
		hydrated++
		return full
	})

	return rewritten, hydrated, tombstoned
}

// buildCredentialMarkdown writes a credential out as markdown any reader
// can follow: a bold line naming it and what kind it is, a list of its
// single-line fields, then one fenced block per key.
//
// Keys are not list items because they are not one line — an SSH private
// key is forty of them, and a reader wanting to use it needs to be able to
// select it cleanly.
//
// Fields carried are a subset of models.Credential, for the same reasons
// the JSON payload carried a subset: CreatedByID and the comment thread
// lose their meaning outside the operation they were written in, and the
// timestamps are not the reader's business.
//
// At CredentialNamesOnly it stops after the name line. Nothing secret is
// assembled and then dropped — the values never enter the string.
func buildCredentialMarkdown(c *models.Credential, detail CredentialDetail) string {
	var b strings.Builder

	b.WriteString("**Credential")
	if name := strings.TrimSpace(c.Name); name != "" {
		b.WriteString(": " + name)
	}
	b.WriteString("**")
	if qualifier := credentialQualifier(c, detail); qualifier != "" {
		b.WriteString(" (" + qualifier + ")")
	}
	if detail == CredentialNamesOnly {
		return b.String()
	}
	b.WriteString("\n")

	var fields []string
	addField := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		fields = append(fields, "- "+label+": "+inlineCode(value))
	}
	addField("Username", c.Username)
	addField("Password", c.Password)
	for _, p := range c.Properties {
		addField(escapeLabel(p.Name), p.Value)
	}
	if len(c.Tags) > 0 {
		fields = append(fields, "- Tags: "+strings.Join(c.Tags, ", "))
	}
	if len(fields) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(fields, "\n"))
		b.WriteString("\n")
	}

	for _, k := range c.Keys {
		if strings.TrimSpace(k.Content) == "" {
			continue
		}
		label := strings.TrimSpace(k.Name)
		if label == "" {
			label = "Key"
		}
		fence := fenceFor(k.Content)
		b.WriteString("\n" + escapeLabel(label) + ":\n\n")
		b.WriteString(fence + "\n" + strings.TrimRight(k.Content, "\n") + "\n" + fence + "\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// credentialQualifier is the parenthetical after the name: what kind of
// secret this is and whether it was known to work.
//
// A names-only rendering says where the secret is instead of whether it
// works, because the reader is being told what they are not being given.
func credentialQualifier(c *models.Credential, detail CredentialDetail) string {
	var parts []string
	if t := strings.TrimSpace(string(c.Type)); t != "" {
		parts = append(parts, strings.ToLower(strings.ReplaceAll(t, "_", " ")))
	}
	if detail == CredentialNamesOnly {
		return strings.Join(append(parts, "held in Logos"), ", ")
	}
	if v := strings.TrimSpace(string(c.Validity)); v != "" {
		parts = append(parts, strings.ToLower(v))
	}
	return strings.Join(parts, ", ")
}

// buildCredentialTombstone is what stands in for a credential the export
// may not or cannot write out.
//
// It carries no id. A uuid tells a reader outside Logos nothing they can
// act on, and the whole point of this pass is to stop shipping identifiers
// only we can resolve. What it does say is that something was referenced
// here, so the sentence the chip sat in still makes sense.
func buildCredentialTombstone() string {
	return "**Credential** (no longer available)"
}

// inlineCode wraps a value in backticks, widening the run when the value
// contains backticks of its own, and padding so a value that begins or ends
// with one still renders.
func inlineCode(value string) string {
	value = strings.TrimRight(value, "\n")
	longest := 0
	run := 0
	for _, r := range value {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	if longest == 0 {
		return "`" + value + "`"
	}
	ticks := strings.Repeat("`", longest+1)
	return ticks + " " + value + " " + ticks
}

// fenceFor returns a backtick run long enough to fence content that may
// contain backtick runs of its own.
func fenceFor(content string) string {
	longest := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		n := 0
		for n < len(trimmed) && trimmed[n] == '`' {
			n++
		}
		if n > longest {
			longest = n
		}
	}
	if longest < 3 {
		return "```"
	}
	return strings.Repeat("`", longest+1)
}

// escapeLabel neutralises the markdown a user's own property or key name
// could contain, so a credential called `**` does not turn the rest of the
// page bold.
func escapeLabel(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`,
		"[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`,
	)
	return replacer.Replace(strings.TrimSpace(s))
}

// CollectCredentialIDs returns the distinct credential ids named by the
// body's logos-credential fences, tombstones excluded.
func CollectCredentialIDs(body string) []uuid.UUID {
	if !strings.Contains(body, credentialFenceInfo) {
		return nil
	}
	seen := map[uuid.UUID]struct{}{}
	var out []uuid.UUID
	for _, m := range credentialFencePattern.FindAllStringSubmatch(body, -1) {
		var p struct {
			ID      string `json:"id"`
			Deleted bool   `json:"deleted"`
		}
		if err := json.Unmarshal([]byte(m[1]), &p); err != nil || p.Deleted {
			continue
		}
		id, err := uuid.Parse(p.ID)
		if err != nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
