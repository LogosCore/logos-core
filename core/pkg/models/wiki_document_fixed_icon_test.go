package models

import "testing"

func TestFixedIconConflict(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name               string
		kind               WikiDocumentKind
		emoji, icon, color *string
		want               bool
	}{
		{"prose takes anything", WikiDocumentKindDocument, s("🐧"), s("Server"), s("#fff"), false},
		{"legacy prose (no kind) too", "", nil, s("Server"), nil, false},
		{"drawing with nothing", WikiDocumentKindDrawing, nil, nil, nil, false},
		{"drawing clearing fields", WikiDocumentKindDrawing, s(""), s(""), s(""), false},
		{"drawing with an icon", WikiDocumentKindDrawing, nil, s("Adaptive"), nil, true},
		{"drawing with an emoji", WikiDocumentKindDrawing, s("🗺️"), nil, nil, true},
		{"drawing with a colour", WikiDocumentKindDrawing, nil, nil, s("#E91E63"), true},
	}
	for _, c := range cases {
		if got := c.kind.FixedIconConflict(c.emoji, c.icon, c.color); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClearFixedIcon(t *testing.T) {
	d := WikiDocument{Kind: WikiDocumentKindDrawing, Emoji: "🗺️", Icon: "Adaptive", Color: "#E91E63"}
	d.ClearFixedIcon()
	if d.Emoji != "" || d.Icon != "" || d.Color != "" {
		t.Errorf("drawing kept %q/%q/%q", d.Emoji, d.Icon, d.Color)
	}
	p := WikiDocument{Emoji: "🐧", Icon: "Server", Color: "#fff"}
	p.ClearFixedIcon()
	if p.Emoji != "🐧" || p.Icon != "Server" || p.Color != "#fff" {
		t.Error("a prose page lost its identity")
	}
}
