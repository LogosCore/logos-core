package mcp

import (
	"strings"
	"testing"
)

const testChipID = "3f2a9c1e-5b7d-4e81-9a06-2c4d8e1f7b35"

func TestCheckReferenceLinks(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		wantErr  bool
		// wantMsg is a fragment the refusal has to carry, so a wrong-but-
		// refusing branch does not pass as the right one.
		wantMsg string
	}{
		{
			name:     "the three chip kinds",
			markdown: "see [page](logos://doc/" + testChipID + "), [host](logos://host/" + testChipID + ") and [hash](logos://hash/" + testChipID + ")",
		},
		{
			name:     "no logos uris at all",
			markdown: "# dc01\n\nA page with an [ordinary link](https://example.com).",
		},
		{
			// The one that reaches pages: handed out by the resource surface,
			// and it looks exactly like a link to the page it names.
			name:     "a resource uri pasted into prose",
			markdown: "the write-up is at [dc01](logos://op/" + testChipID + "/wiki/" + testChipID + ")",
			wantErr:  true,
			wantMsg:  "resource address",
		},
		{
			name:     "the guide resource",
			markdown: "background in [the guide](logos://guide)",
			wantErr:  true,
			wantMsg:  "resource address",
		},
		{
			name:     "the focus resource",
			markdown: "current focus: logos://session/focus",
			wantErr:  true,
			wantMsg:  "resource address",
		},
		{
			// Generalising correctly from three out of four cases, and wrong.
			name:     "a credential link",
			markdown: "creds: [credential](logos://credential/" + testChipID + ")",
			wantErr:  true,
			wantMsg:  "logos-credential",
		},
		{
			name:     "an invented kind",
			markdown: "tracked in [task](logos://task/" + testChipID + ")",
			wantErr:  true,
			wantMsg:  "not a reference chip",
		},
		{
			name:     "a chip kind carrying a name rather than a uuid",
			markdown: "see [dc01](logos://doc/dc01)",
			wantErr:  true,
			wantMsg:  "full uuid",
		},
		{
			// Renders in the app and then leaves a raw logos:// link in
			// exported markdown, because the export rewriter matches only the
			// dashed form.
			name:     "a uuid without its dashes",
			markdown: "see [page](logos://doc/3f2a9c1e5b7d4e819a062c4d8e1f7b35)",
			wantErr:  true,
			wantMsg:  "full uuid",
		},
		{
			name:     "a chip kind with no id",
			markdown: "see [page](logos://doc/)",
			wantErr:  true,
			wantMsg:  "no id",
		},
		{
			name:     "an uppercase uuid, which parses",
			markdown: "see [page](logos://doc/" + strings.ToUpper(testChipID) + ")",
		},
		{
			// How the guides and any page explaining the scheme are written.
			name:     "a bad uri quoted in backticks",
			markdown: "`logos://op/<op>/wiki/<doc>` is how you read a page.",
		},
		{
			name:     "a bad uri inside a fence",
			markdown: "```text\nlogos://guide\n```",
		},
		{
			name:     "a bad uri inside an indented fence",
			markdown: "- like this:\n\n  ```text\n  logos://credential/abc\n  ```\n",
		},
		{
			name:     "an unmatched backtick does not hide the rest of the line",
			markdown: "a ` stray tick and logos://guide after it",
			wantErr:  true,
			wantMsg:  "resource address",
		},
		{
			name:     "code spans of differing lengths",
			markdown: "``a ` b`` then logos://guide",
			wantErr:  true,
			wantMsg:  "resource address",
		},
		{
			// A fence opened and closed must not swallow what follows it.
			name:     "prose after a closed fence is still checked",
			markdown: "```text\nfine\n```\n\nand [x](logos://task/" + testChipID + ")",
			wantErr:  true,
			wantMsg:  "not a reference chip",
		},
		{
			name:     "a good chip and a bad one, the bad one wins",
			markdown: "[page](logos://doc/" + testChipID + ") and [c](logos://credential/" + testChipID + ")",
			wantErr:  true,
			wantMsg:  "logos-credential",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkReferenceLinks(tt.markdown)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("checkReferenceLinks() = nil, want a refusal")
				}
				if !isRefusal(err) {
					t.Errorf("checkReferenceLinks() returned %T, want a refusal", err)
				}
				if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("refusal %q does not mention %q", err, tt.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("checkReferenceLinks() = %v, want nil", err)
			}
		})
	}
}

// The refusals exist to be acted on, so each one has to carry the shape that
// works rather than only the complaint.
func TestCheckReferenceLinksRefusalsNameTheFix(t *testing.T) {
	for _, markdown := range []string{
		"[x](logos://guide)",
		"[x](logos://credential/" + testChipID + ")",
		"[x](logos://task/" + testChipID + ")",
		"[x](logos://doc/dc01)",
	} {
		err := checkReferenceLinks(markdown)
		if err == nil {
			t.Fatalf("checkReferenceLinks(%q) = nil, want a refusal", markdown)
		}
		if !strings.Contains(err.Error(), "logos://doc/") &&
			!strings.Contains(err.Error(), "logos-credential") {
			t.Errorf("refusal for %q names no working form: %v", markdown, err)
		}
	}
}
