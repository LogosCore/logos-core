package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// The deployment's built-in process marks file: none, said as info, without
// one or with an empty one; none, said as an error, when it does not read (a
// directory at its path) or is not a JSON array of marks; else its marks, a
// mark left out said as a warning, then how many were read.
func TestLoadBuiltinProcessMarks(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if err := os.Mkdir(filepath.Join(dir, "a-dir.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, warn, errs := zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel
	for _, c := range []struct {
		name  string
		path  string
		marks int
		logs  []zapcore.Level
	}{
		{"no file", filepath.Join(dir, "none.json"), 0, []zapcore.Level{info}},
		{"an empty one", write("empty.json", " \n"), 0, []zapcore.Level{info}},
		{"a directory", filepath.Join(dir, "a-dir.json"), 0, []zapcore.Level{errs}},
		{"not an array", write("object.json", `{"name": "sshd", "color": "red-strong"}`), 0, []zapcore.Level{errs}},
		{"a mark left out", write("marks.json", `[{"name": "sshd", "color": "blue-soft"}, {"name": "bash", "color": "purple-mega"}]`), 1,
			[]zapcore.Level{warn, info}},
	} {
		core, logs := observer.New(zapcore.DebugLevel)
		marks := loadBuiltinProcessMarks(c.path, zap.New(core))
		var levels []zapcore.Level
		for _, e := range logs.All() {
			levels = append(levels, e.Level)
		}
		if len(marks) != c.marks || !slices.Equal(levels, c.logs) {
			t.Errorf("%s: %d marks, logs %+v", c.name, len(marks), logs.All())
		}
		if _, ok := marks.Of("sshd"); c.marks == 1 && !ok {
			t.Errorf("%s: sshd's mark not read: %v", c.name, marks)
		}
		if left := logs.FilterLevelExact(warn).All(); len(left) == 1 && !strings.Contains(left[0].ContextMap()["why"].(string), `"bash"`) {
			t.Errorf("%s: the mark left out not said: %+v", c.name, left[0].ContextMap())
		}
	}
}
