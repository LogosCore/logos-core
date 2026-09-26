package markdown

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRewriteReferenceLinks(t *testing.T) {
	page, host, hash, unknown := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	resolve := func(kind string, id uuid.UUID) (ReferenceTarget, bool) {
		switch {
		case kind == "doc" && id == page:
			return ReferenceTarget{Text: "Peering [edge]", Href: "../002-net/003-peering.md"}, true
		case kind == "host" && id == host:
			return ReferenceTarget{Text: "in-bgp01"}, true
		case kind == "hash" && id == hash:
			return ReferenceTarget{Text: "aad3b435b51404ee"}, true
		}
		return ReferenceTarget{}, false
	}

	body := "Dual [host](logos://host/" + host.String() + ") edge " +
		"peering [page](logos://doc/" + page.String() + ") + " +
		"[page](logos://doc/" + unknown.String() + ") " +
		"hash [hash](logos://hash/" + hash.String() + ") " +
		"ordinary [link](https://example.com) stays"

	got := RewriteReferenceLinks(body, resolve)
	want := "Dual in-bgp01 edge " +
		`peering [Peering \[edge\]](../002-net/003-peering.md) + ` +
		"page " +
		"hash aad3b435b51404ee " +
		"ordinary [link](https://example.com) stays"
	if got != want {
		t.Fatalf("RewriteReferenceLinks\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(got, "logos://") {
		t.Fatal("logos:// scheme leaked into foreign markdown")
	}
}

func TestRewriteReferenceLinks_EmptyTextFallsBackToLabel(t *testing.T) {
	id := uuid.New()
	resolve := func(string, uuid.UUID) (ReferenceTarget, bool) { return ReferenceTarget{Text: ""}, true }
	got := RewriteReferenceLinks("see [host](logos://host/"+id.String()+")", resolve)
	if got != "see host" {
		t.Fatalf("got %q", got)
	}
}
