package wikitransfer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPageTitle(t *testing.T) {
	if got, cut := pageTitle("  "); got != "Untitled" || cut {
		t.Errorf("blank: %q %v", got, cut)
	}
	// The cap counts characters: a full-length Cyrillic title is kept whole.
	fits := strings.Repeat("ж", maxTitleLength)
	if got, cut := pageTitle(" " + fits + " "); got != fits || cut {
		t.Errorf("a title of %d characters was changed (cut %v)", maxTitleLength, cut)
	}
	// A longer one is cut between characters, never inside one.
	got, cut := pageTitle(fits + "ж")
	if !cut || !utf8.ValidString(got) || utf8.RuneCountInString(got) != maxTitleLength || !strings.HasSuffix(got, "…") {
		t.Errorf("cut %v to %d characters, valid %v: %q", cut, utf8.RuneCountInString(got), utf8.ValidString(got), got)
	}
}
