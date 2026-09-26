package markdown

import (
	"encoding/json"
	"strings"
)

// Lowering the constructs only Logos understands.
//
// The body the sidecar serializes is Outline-flavoured markdown plus a few
// extensions of ours. The extensions are what let a zip re-import into
// Logos with its checklists and credential chips intact — and they are
// exactly what a foreign reader cannot use: `:::checklist {"prompt":…}`
// renders as literal colons and a line of JSON in anything that is not us.
//
// So this zip drops them, and keeps what they said. A checklist becomes a
// GFM task list item, which every markdown reader draws as a checkbox; a
// credential block becomes a heading and a list of its fields. The
// structure is lost and the content survives, which is the trade this
// format exists to make. The native bundle is the one that keeps both.
//
// What is deliberately left alone: `:::info` / `:::success` / `:::warning`
// / `:::tip` and the `![](url " =WxH")` image size hint. Those are Outline's
// own syntax, not ours, and Outline is one of the tools this zip is for.

const checklistContainer = "checklist"

// checklistAttrs is the JSON on a checklist container's info line. Only the
// fields that say something to a reader are named: `key` and `required` are
// bookkeeping for our own coverage bar and go no further.
type checklistAttrs struct {
	Prompt             string `json:"prompt"`
	CommandHint        string `json:"commandHint"`
	CommandHintEnabled bool   `json:"commandHintEnabled"`
	State              string `json:"state"`
}

// LowerChecklists rewrites every `:::checklist` container in the body as a
// task list item.
//
// Runs last of the three passes: it indents an answer to sit under its
// bullet, and a credential block or chip rewritten afterwards would be
// rewritten at the wrong indentation.
//
// Scanned line by line rather than matched with a regex: a checklist body
// may hold a fenced code block (an answer is usually command output) and may
// hold another container (a notice inside an answer), and a non-greedy match
// would end the checklist at the first `:::` either of those contains.
func LowerChecklists(body string) string {
	lines := strings.Split(body, "\n")
	var out []string

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// A fence outside any container: copy it through whole, so `:::`
		// inside sample output is never read as markup.
		if fence := openingFence(line); fence != "" {
			end := closingFenceIndex(lines, i+1, fence)
			out = append(out, lines[i:min(end+1, len(lines))]...)
			i = end
			continue
		}

		attrs, ok := parseChecklistOpen(line)
		if !ok {
			out = append(out, line)
			continue
		}

		end := containerCloseIndex(lines, i+1)
		inner := lines[i+1 : min(end, len(lines))]
		out = append(out, renderChecklistItem(attrs, inner)...)
		i = end
	}

	return strings.Join(out, "\n")
}

// parseChecklistOpen recognises `:::checklist` with an optional JSON info
// object, and nothing else — `:::checklistish` is not a checklist.
func parseChecklistOpen(line string) (checklistAttrs, bool) {
	rest, ok := strings.CutPrefix(strings.TrimRight(line, " \t"), ":::"+checklistContainer)
	if !ok {
		return checklistAttrs{}, false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return checklistAttrs{}, true
	}
	if !strings.HasPrefix(rest, "{") {
		return checklistAttrs{}, false
	}
	var attrs checklistAttrs
	if err := json.Unmarshal([]byte(rest), &attrs); err != nil {
		// Info we cannot read is info we cannot lower safely. Leaving the
		// container alone keeps the answer in the export, which matters
		// more than the shape it arrives in.
		return checklistAttrs{}, false
	}
	return attrs, true
}

// containerCloseIndex finds the `:::` that closes the container opened
// before `from`, counting nested containers and skipping fenced code. It
// returns len(lines) when the container is never closed, which makes the
// rest of the document the item's body — the same reading a markdown parser
// would take.
func containerCloseIndex(lines []string, from int) int {
	depth := 1
	for i := from; i < len(lines); i++ {
		if fence := openingFence(lines[i]); fence != "" {
			i = closingFenceIndex(lines, i+1, fence)
			continue
		}
		trimmed := strings.TrimRight(lines[i], " \t")
		if trimmed == ":::" {
			depth--
			if depth == 0 {
				return i
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":::") {
			depth++
		}
	}
	return len(lines)
}

// openingFence returns the backtick or tilde run that opens a fenced code
// block on this line, or "" when the line opens none.
func openingFence(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	for _, marker := range []string{"```", "~~~"} {
		if !strings.HasPrefix(trimmed, marker) {
			continue
		}
		n := 0
		for n < len(trimmed) && trimmed[n] == marker[0] {
			n++
		}
		return trimmed[:n]
	}
	return ""
}

// closingFenceIndex finds the line closing a fence opened with `fence`, or
// the last line when it is never closed.
func closingFenceIndex(lines []string, from int, fence string) int {
	for i := from; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])) == "" {
			return i
		}
	}
	return len(lines) - 1
}

// renderChecklistItem draws one checklist as a task list item: the prompt on
// the bullet, the answer indented beneath it as the item's content.
func renderChecklistItem(attrs checklistAttrs, inner []string) []string {
	var head strings.Builder
	head.WriteString("- [")
	if checklistIsSettled(attrs.State) {
		head.WriteString("x")
	} else {
		head.WriteString(" ")
	}
	head.WriteString("] ")
	if prompt := strings.TrimSpace(attrs.Prompt); prompt != "" {
		head.WriteString("**" + prompt + "**")
	}
	if label := checklistStateLabel(attrs.State); label != "" {
		if strings.TrimSpace(attrs.Prompt) != "" {
			head.WriteString(" ")
		}
		head.WriteString("_(" + label + ")_")
	}

	out := []string{strings.TrimRight(head.String(), " ")}
	if attrs.CommandHintEnabled && strings.TrimSpace(attrs.CommandHint) != "" {
		out = append(out, "", "  `"+strings.TrimSpace(attrs.CommandHint)+"`")
	}

	body := indentBlock(trimBlankEdges(inner), "  ")
	if len(body) > 0 {
		out = append(out, "")
		out = append(out, body...)
	}
	return out
}

// checklistIsSettled reports whether the box is drawn ticked. Answered and
// not-applicable are both "this one is dealt with"; flagged and unanswered
// are not.
func checklistIsSettled(state string) bool {
	return state == "answered" || state == "not_applicable"
}

// checklistStateLabel is the wording for a state a checkbox cannot carry on
// its own. An answered item needs none — the tick says it.
func checklistStateLabel(state string) string {
	switch state {
	case "flagged":
		return "flagged"
	case "not_applicable":
		return "not applicable"
	default:
		return ""
	}
}

// indentBlock indents every non-empty line, leaving blank lines empty so a
// list item's blocks stay separated.
func indentBlock(lines []string, indent string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, indent+line)
	}
	return out
}

// trimBlankEdges drops leading and trailing blank lines.
func trimBlankEdges(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}
