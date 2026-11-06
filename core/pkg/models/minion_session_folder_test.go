package models

import (
	"strings"
	"testing"
)

func TestValidateSessionFolder(t *testing.T) {
	for _, ok := range [][4]string{{"Prod web", "", "", ""}, {"DCs", "Server", "", "#E91E63"}, {"Phish", "", "🎣", ""}} {
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
	} {
		if ValidateSessionFolder(bad[0], bad[1], bad[2], bad[3]) == "" {
			t.Errorf("%s passed", name)
		}
	}
}
