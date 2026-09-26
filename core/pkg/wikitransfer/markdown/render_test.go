package markdown

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCollectAttachmentRefs(t *testing.T) {
	body := `Here is an image: ![alt](/api/v1/wiki/images/11111111-1111-1111-1111-111111111111 " =640x480")

And a file: [report.pdf 1024](/api/v1/wiki/files/22222222-2222-2222-2222-222222222222)

A repeated image: /api/v1/wiki/images/11111111-1111-1111-1111-111111111111

Another file: /api/v1/wiki/files/33333333-3333-3333-3333-333333333333
`
	images, files := collectAttachmentRefs(body)
	if len(images) != 1 {
		t.Errorf("images: got %d, want 1 (deduped) — %v", len(images), images)
	}
	if len(files) != 2 {
		t.Errorf("files: got %d, want 2 — %v", len(files), files)
	}
}

func TestRewriteAttachmentRefs(t *testing.T) {
	imgID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fileID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	body := `Image: /api/v1/wiki/images/` + imgID.String() + `
File: /api/v1/wiki/files/` + fileID.String() + `
Missing: /api/v1/wiki/images/99999999-9999-9999-9999-999999999999`

	out := rewriteAttachmentRefs(body,
		func(id uuid.UUID) (string, bool) {
			if id == imgID {
				return "uploads/x/y/img.png", true
			}
			return "", false
		},
		func(id uuid.UUID) (string, bool) {
			if id == fileID {
				return "uploads/x/z/report.pdf", true
			}
			return "", false
		},
	)
	if !strings.Contains(out, "uploads/x/y/img.png") {
		t.Errorf("image ref not rewritten: %s", out)
	}
	if !strings.Contains(out, "uploads/x/z/report.pdf") {
		t.Errorf("file ref not rewritten: %s", out)
	}
	// Missing refs left untouched.
	if !strings.Contains(out, "/api/v1/wiki/images/99999999-9999-9999-9999-999999999999") {
		t.Errorf("missing ref should be left in place: %s", out)
	}
}

func TestRenderDocMarkdown(t *testing.T) {
	got := renderDocMarkdown("📘", "Intro", "Hello world.")
	want := "# 📘 Intro\n\nHello world.\n"
	if got != want {
		t.Errorf("with emoji: got %q want %q", got, want)
	}

	got = renderDocMarkdown("", "Plain", "")
	want = "# Plain\n\n"
	if got != want {
		t.Errorf("no body: got %q want %q", got, want)
	}
}

// A page's icon and colour stay behind. They only ever travelled as an HTML
// comment that no markdown viewer renders and only our own parser read, so
// in a zip meant for other tools they were bytes nobody could use.
func TestRenderDocMarkdown_DropsLogosMeta(t *testing.T) {
	rendered := renderDocMarkdown("🚀", "Launch", "go\n")
	if strings.Contains(rendered, "logos:meta") || strings.Contains(rendered, "<!--") {
		t.Errorf("markdown still carries Logos-only metadata:\n%s", rendered)
	}

	// What a reader can see still arrives: the emoji is part of the heading
	// text, so it is not metadata and does travel.
	doc := parseOneDoc(t, rendered)
	if doc.Title != "Launch" || doc.Emoji != "🚀" || doc.BodyMarkdown != "go\n" {
		t.Errorf("parsed = %+v", doc)
	}
	if doc.Icon != "" || doc.Color != "" {
		t.Errorf("icon/color should not survive this format: %q / %q", doc.Icon, doc.Color)
	}
}

// The parser keeps reading the comment, because zips exported before this
// change carry one and it would otherwise arrive as visible text.
func TestParser_StillStripsLegacyLogosMeta(t *testing.T) {
	doc := parseOneDoc(t, "# Notes\n<!-- logos:meta icon=\"Adaptive\" color=\"#1f2937\" -->\n\nBody.\n")
	if doc.Icon != "Adaptive" || doc.Color != "#1f2937" {
		t.Errorf("legacy meta not read back: %+v", doc)
	}
	if strings.Contains(doc.BodyMarkdown, "logos:meta") {
		t.Errorf("legacy meta leaked into the body: %q", doc.BodyMarkdown)
	}
}

// parseOneDoc wraps the given markdown body in a minimal in-memory zip
// shaped like an Outline export (one collection folder, one doc inside),
// runs the real import parser over it, and returns the parsed Doc. Avoids
// duplicating the parser's grammar in the export test file.
func parseOneDoc(t *testing.T, body string) *Doc {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("col/doc.md")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	parsed, err := Parse(zr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.Collections) != 1 || len(parsed.Collections[0].Documents) != 1 {
		t.Fatalf("unexpected parse shape: %+v", parsed.Collections)
	}
	return parsed.Collections[0].Documents[0]
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"normal.pdf", "normal.pdf"},
		{"path/sep.png", "sep.png"},
		{"back\\slash.png", "slash.png"},
		{"", "file"},
		{"with\x00null.txt", "withnull.txt"},
		{"trailing. ", "trailing"},
	}
	for _, c := range cases {
		got := sanitizeFilename(c.in)
		if got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
