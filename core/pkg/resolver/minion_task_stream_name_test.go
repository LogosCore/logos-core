package resolver

import "testing"

func TestStreamNameOf(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/tool.exe": "tool.exe", `C:\Temp\big.iso`: "big.iso", "tool": "tool",
		"/tmp/": "", `C:\Temp\`: "", "": "", "/tmp/..": "",
	} {
		got, ok := streamNameOf(map[string]any{"path": path})
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", path, got, ok)
		}
	}
	if _, ok := streamNameOf(map[string]any{}); ok {
		t.Error("no path: a name")
	}
}
