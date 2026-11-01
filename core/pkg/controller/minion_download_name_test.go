package controller

import (
	"net/url"
	"strings"
	"testing"
)

// A minion's file downloads under its own name, any script: the header has
// it percent-encoded as UTF-8, and an ASCII fallback; its path and control
// characters never reach the header.
func TestDownloadName(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/Downloads/21.08.26 камери перевірені нові (із 1го списку).docx": "21.08.26 камери перевірені нові (із 1го списку).docx",
		`C:\Users\Анна\звіт 2026.pdf`: "звіт 2026.pdf",
		"/tmp/it's \"quoted\".txt":    `it's "quoted".txt`,
		"/tmp/bad\r\nSet-Cookie: x=1": "badSet-Cookie: x=1",
		"/etc/":                       "file-1",
		"/tmp/..":                     "file-1",
		"\x00\x01":                    "file-1",
		"plain.bin":                   "plain.bin",
	} {
		if got := downloadName(in, "file-1"); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}

	name := downloadName("/home/u/21.08.26 камери (1).docx", "file-1")
	h := contentDispositionFor(name, "application/octet-stream", false)
	if strings.ContainsAny(h, "\r\n") {
		t.Fatalf("a line break in the header: %q", h)
	}
	_, star, ok := strings.Cut(h, "filename*=UTF-8''")
	if !ok {
		t.Fatalf("no filename*: %q", h)
	}
	if decoded, err := url.PathUnescape(star); err != nil || decoded != "21.08.26 камери (1).docx" {
		t.Fatalf("filename* %q decodes to %q (%v)", star, decoded, err)
	}
	fallback := h[strings.Index(h, `filename="`)+len(`filename="`):]
	fallback = fallback[:strings.Index(fallback, `"`)]
	for _, r := range fallback {
		if r > 0x7f {
			t.Fatalf("the fallback is not ASCII: %q", fallback)
		}
	}
}
