package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MinionProcessMark is how one user marks the processes of a name for
// themselves, in every process list of every operation: a mark as a
// session's is (a color or a style, an icon or an emoji: ValidateSessionMark),
// private to them. Name is the name as it was marked; Key, the name in lower
// case, is what a process's name matches it by, whatever its case (Windows'
// names have none: Firefox.exe is firefox.exe).
type MinionProcessMark struct {
	UserID    uuid.UUID `bson:"user_id" json:"user_id"`
	Key       string    `bson:"key" json:"key"`
	Name      string    `bson:"name" json:"name"`
	Color     string    `bson:"color,omitempty" json:"color,omitempty"`
	Icon      string    `bson:"icon,omitempty" json:"icon,omitempty"`
	Emoji     string    `bson:"emoji,omitempty" json:"emoji,omitempty"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

// Empty reports whether the mark shows nothing: a user's cleared mark,
// kept where it hides the name's built-in one.
func (m MinionProcessMark) Empty() bool { return m.Color == "" && m.Icon == "" && m.Emoji == "" }

// MaxProcessMarkName is the longest name a process mark takes, in bytes.
const MaxProcessMarkName = 255

// ProcessMarkKey is what a process mark matches a process's name by: the
// name, trimmed, in lower case.
func ProcessMarkKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// BuiltinProcessMarksFile is where core reads the deployment's built-in
// process marks, once, at start (docs: core-infrastructure, "Built-in
// process marks"): a file the deployment mounts. Without one there are none.
const BuiltinProcessMarksFile = "/etc/logos/builtin-process-marks.json"

// BuiltinProcessMark is one of a deployment's own marks on processes by
// name: a color or a style, an icon or an emoji, as a user's mark (Name
// matched whatever its case). Its JSON is an entry of the file.
type BuiltinProcessMark struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Emoji string `json:"emoji,omitempty"`
}

// BuiltinProcessMarks are a deployment's own marks on processes, by key
// (ProcessMarkKey), which every user sees until they clear one (their empty
// mark, kept, hides it) or set their own; resetMinionProcessMark goes back to
// it. Nil has none.
type BuiltinProcessMarks map[string]BuiltinProcessMark

// Of is the built-in mark of a name, if it has one.
func (b BuiltinProcessMarks) Of(name string) (BuiltinProcessMark, bool) {
	m, ok := b[ProcessMarkKey(name)]
	return m, ok
}

// ParseBuiltinProcessMarks reads the built-in process marks file: a JSON
// array of marks, [{"name": "sshd", "color": "blue-soft", "emoji": "🔑"}];
// none when it is empty, blank or null. An error says it is not such an
// array. A mark that does not check as a user's would (a name of 1 to 255
// bytes, trimmed; something it shows; ValidateSessionMark; no field but
// these four), or a second one for a name whatever its case, is left out,
// each said in skipped with its place in the array.
func ParseBuiltinProcessMarks(data []byte) (marks BuiltinProcessMarks, skipped []string, err error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, nil, err
	}
	marks = make(BuiltinProcessMarks, len(entries))
	for i, raw := range entries {
		b, why := parseBuiltinProcessMark(raw)
		key := ProcessMarkKey(b.Name)
		if _, second := marks[key]; why == "" && second {
			why = "a second mark for the name"
		}
		if why != "" {
			at := fmt.Sprintf("mark %d", i+1)
			if b.Name != "" {
				at += fmt.Sprintf(" (%q)", b.Name)
			}
			skipped = append(skipped, at+": "+why)
			continue
		}
		marks[key] = b
	}
	return marks, skipped, nil
}

// parseBuiltinProcessMark is one entry of the file, its name trimmed, and
// why it does not check ("" when it does).
func parseBuiltinProcessMark(raw json.RawMessage) (BuiltinProcessMark, string) {
	var b BuiltinProcessMark
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&b)
	b.Name = strings.TrimSpace(b.Name)
	switch {
	case err != nil:
		return b, err.Error()
	case b.Name == "" || len(b.Name) > MaxProcessMarkName:
		return b, fmt.Sprintf("name: a process's name, 1 to %d bytes", MaxProcessMarkName)
	case (MinionProcessMark{Color: b.Color, Icon: b.Icon, Emoji: b.Emoji}).Empty():
		return b, "a mark that shows nothing: it needs a color, an icon or an emoji"
	}
	return b, ValidateSessionMark(b.Color, b.Icon, b.Emoji)
}
