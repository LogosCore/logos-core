package models

import (
	"strings"
	"testing"
)

func TestValidateSessionFolder(t *testing.T) {
	for _, ok := range [][4]string{{"Prod web", "", "", ""}, {"DCs", "Server", "", "#E91E63"}, {"Phish", "", "🎣", ""},
		// The wiki icon picker's swatches (icon-color-palette.ts).
		{"Blue", "Server", "", "oklch(0.65 0.16 245)"}, {"Red", "", "", "oklch(0.7 0.17 0)"}, {"Pct", "", "", "oklch(65% 0.1 120deg)"}} {
		if why := ValidateSessionFolder(ok[0], ok[1], ok[2], ok[3]); why != "" {
			t.Errorf("%v: %s", ok, why)
		}
	}
	for name, bad := range map[string][4]string{
		"no name":        {"  ", "", "", ""},
		"long name":      {strings.Repeat("x", 65), "", "", ""},
		"bad icon":       {"a", "not an icon!", "", ""},
		"icon and emoji": {"a", "Server", "🎣", ""},
		"bad color":      {"a", "Server", "", "red"},
		"css injection":  {"a", "Server", "", "oklch(0.6 0.1 20); background: url(x)"},
		"other function": {"a", "Server", "", "rgb(1 2 3)"},
		"empty oklch":    {"a", "Server", "", "oklch()"},
		"no numbers":     {"a", "Server", "", "oklch(  )"},
		"two numbers":    {"a", "Server", "", "oklch(0.6 0.1)"},
	} {
		if ValidateSessionFolder(bad[0], bad[1], bad[2], bad[3]) == "" {
			t.Errorf("%s passed", name)
		}
	}
}
