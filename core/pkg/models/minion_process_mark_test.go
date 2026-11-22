package models

import (
	"strings"
	"testing"
)

// The file's marks, found by name whatever its case, the name trimmed; an
// empty, blank or null file, or an empty array, has none; a file that is
// not a JSON array of marks does not read.
func TestParseBuiltinProcessMarks(t *testing.T) {
	marks, skipped, err := ParseBuiltinProcessMarks([]byte(`[
		{"name": " MsMpEng.exe ", "color": "red-strong", "icon": "ShieldAlert"},
		{"name": "sshd", "emoji": "🔑"},
		{"name": "System", "color": "gold"}
	]`))
	if err != nil || len(skipped) != 0 || len(marks) != 3 {
		t.Fatalf("read: %v %v %v", marks, skipped, err)
	}
	if b, ok := marks.Of("msmpeng.EXE"); !ok || b != (BuiltinProcessMark{Name: "MsMpEng.exe", Color: "red-strong", Icon: "ShieldAlert"}) {
		t.Fatalf("by name, whatever its case: %+v %v", b, ok)
	}
	if b, ok := marks.Of("sshd"); !ok || b.Emoji != "🔑" || b.Color != "" {
		t.Fatalf("an emoji alone: %+v %v", b, ok)
	}
	if _, ok := marks.Of("bash"); ok {
		t.Fatal("a name with none has one")
	}
	if _, ok := BuiltinProcessMarks(nil).Of("sshd"); ok {
		t.Fatal("none has one")
	}
	for _, none := range []string{"", " \n\t", "null", "[]"} {
		if marks, skipped, err := ParseBuiltinProcessMarks([]byte(none)); err != nil || len(marks) != 0 || len(skipped) != 0 {
			t.Errorf("%q: %v %v %v", none, marks, skipped, err)
		}
	}
	for _, bad := range []string{
		`{"name": "sshd", "color": "red-strong"}`,  // an object, not an array
		`[{"name": "sshd", "color": "red-strong"}`, // cut short
		`"sshd"`,
		`[] []`,
	} {
		if marks, _, err := ParseBuiltinProcessMarks([]byte(bad)); err == nil || marks != nil {
			t.Errorf("%q read: %v", bad, marks)
		}
	}
}

// A mark that does not check is left out, said with its place and its name,
// and the others kept: a second one for a name (the first stays), a field
// it does not know, no name or one past 255 bytes, nothing to show, a color,
// icon or emoji that does not check, an icon and an emoji, not an object.
func TestParseBuiltinProcessMarksLeavesOut(t *testing.T) {
	marks, skipped, err := ParseBuiltinProcessMarks([]byte(`[
		{"name": "sshd", "color": "blue-soft"},
		{"name": "SSHD", "color": "red-strong"},
		{"name": "bash", "colour": "red-strong"},
		{"name": "  ", "color": "red-strong"},
		{"name": "` + strings.Repeat("x", 256) + `", "color": "red-strong"},
		{"name": "vim"},
		{"name": "nano", "color": "purple-mega"},
		{"name": "htop", "icon": "no spaces"},
		{"name": "less", "emoji": "not an emoji at all"},
		{"name": "top", "icon": "Flame", "emoji": "🔥"},
		7,
		{"name": "cron", "emoji": "⏰"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 2 || marks["sshd"].Color != "blue-soft" || marks["cron"].Emoji != "⏰" {
		t.Fatalf("kept: %v", marks)
	}
	want := []struct{ at, why string }{
		{`mark 2 ("SSHD")`, "a second mark for the name"},
		{`mark 3 ("bash")`, `unknown field "colour"`},
		{`mark 4:`, "name: a process's name, 1 to 255 bytes"},
		{`mark 5 ("xxx`, "name: a process's name, 1 to 255 bytes"},
		{`mark 6 ("vim")`, "shows nothing"},
		{`mark 7 ("nano")`, "not one of the mark colors or styles"},
		{`mark 8 ("htop")`, "not an icon name"},
		{`mark 9 ("less")`, "not an emoji"},
		{`mark 10 ("top")`, "an icon or an emoji, not both"},
		{`mark 11:`, "cannot unmarshal number"},
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped %d, want %d: %q", len(skipped), len(want), skipped)
	}
	for i, w := range want {
		if !strings.HasPrefix(skipped[i], w.at) || !strings.Contains(skipped[i], w.why) {
			t.Errorf("skipped[%d] = %q, want %s ... %s", i, skipped[i], w.at, w.why)
		}
	}
}
