package markdown

import (
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hello World", "hello-world"},
		{"  Hello   World  ", "hello-world"},
		{"Title/with\\slashes", "title-with-slashes"},
		{"Émoji 🎉 stays text", "émoji-stays-text"},
		{"Отчёт по хосту DC-01", "отчёт-по-хосту-dc-01"},
		{"日本語 のページ", "日本語-のページ"},
		{"!!! 🎉", "untitled"},
		{"", "untitled"},
		{
			"this title is far too long to fit inside the eighty character cap so it gets truncated nicely",
			"this-title-is-far-too-long-to-fit-inside-the-eighty-character-cap-so-it-gets-tru",
		},
	}
	for _, c := range cases {
		got := slugify(c.in)
		if got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUniqueSlug(t *testing.T) {
	used := map[string]struct{}{}
	if got := uniqueSlug("foo", used); got != "foo" {
		t.Fatalf("first call: got %q want foo", got)
	}
	if got := uniqueSlug("foo", used); got != "foo-2" {
		t.Fatalf("second call: got %q want foo-2", got)
	}
	if got := uniqueSlug("foo", used); got != "foo-3" {
		t.Fatalf("third call: got %q want foo-3", got)
	}
	if got := uniqueSlug("bar", used); got != "bar" {
		t.Fatalf("new slug: got %q want bar", got)
	}
}

// A file is named after its page and nothing else — no sibling index, so a
// folder sorted by name is sorted by title.
func TestBuildDocFilename(t *testing.T) {
	if got := buildDocFilename("intro"); got != "intro.md" {
		t.Errorf("buildDocFilename = %q, want intro.md", got)
	}
	if got := buildChildrenFolder("intro"); got != "intro" {
		t.Errorf("buildChildrenFolder = %q, want intro", got)
	}
}

func TestRelativeLink(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"root/a.md", "root/b.md", "b.md"},
		{"root/a.md", "root/a/c.md", "a/c.md"},
		{"root/a/c.md", "root/b.md", "../b.md"},
		{"root/a/c/d.md", "root/a/e.md", "../e.md"},
		{"root/a/c.md", "root/uploads/d/a/img.png", "../uploads/d/a/img.png"},
		{"root/a.md", "root/uploads/d/a/img.png", "uploads/d/a/img.png"},
		{"root/a.md", "root/a.md", "a.md"},
	}
	for _, c := range cases {
		if got := relativeLink(c.from, c.to); got != c.want {
			t.Errorf("relativeLink(%q, %q) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
}
