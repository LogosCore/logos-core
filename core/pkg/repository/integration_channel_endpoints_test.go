package repository

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"go.mongodb.org/mongo-driver/bson"
)

// TestIntegrationChannelEndpointsList pins the Endpoints page's query: by
// name, case aside, a page at a time; the app's search over the name,
// description, kind and values (search_text, kept up to date on every write,
// and given to older endpoints by the backfill), not the values' keys; and
// the kind filter.
func TestIntegrationChannelEndpointsList(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewChannelEndpointRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	op := uuid.New()
	add := func(name, kind, desc string, values models.JSONMap) *models.ChannelEndpoint {
		t.Helper()
		ep := &models.ChannelEndpoint{OperationID: op, Name: name, ModuleName: kind, Description: desc, SchemaVersion: 1, Values: values}
		if err := repo.Insert(ctx, ep); err != nil {
			t.Fatal(err)
		}
		return ep
	}
	eu := add("edge-eu", "http", "EU CDN front", models.JSONMap{"base_url": "https://edge-eu.example.com", "extra_base_urls": []any{"https://backup.example.net"}})
	add("Alpha", "http", "", models.JSONMap{"base_url": "https://alpha.example.org"})
	add("zone", "dns", "Resolver", models.JSONMap{"domain": "c2.example.com"})
	if err := repo.Insert(ctx, &models.ChannelEndpoint{OperationID: uuid.New(), Name: "edge-eu", ModuleName: "http"}); err != nil {
		t.Fatal(err)
	}

	list := func(f ChannelEndpointFilter) []string {
		t.Helper()
		eps, err := repo.FindByOperationIDWithCursor(ctx, op, f, nil, 10, true)
		if err != nil {
			t.Fatal(err)
		}
		n, err := repo.CountByOperationID(ctx, op, f)
		if err != nil || n != int64(len(eps)) {
			t.Fatalf("count %d %v, listed %d", n, err, len(eps))
		}
		out := make([]string, len(eps))
		for i, ep := range eps {
			out[i] = ep.Name
		}
		return out
	}
	for name, c := range map[string]struct {
		filter ChannelEndpointFilter
		want   []string
	}{
		"by name, case aside":      {ChannelEndpointFilter{}, []string{"Alpha", "edge-eu", "zone"}},
		"a value":                  {ChannelEndpointFilter{Search: "EDGE-EU.EXAMPLE"}, []string{"edge-eu"}},
		"a value in a list":        {ChannelEndpointFilter{Search: "backup.example.net"}, []string{"edge-eu"}},
		"the description":          {ChannelEndpointFilter{Search: "cdn front"}, []string{"edge-eu"}},
		"the kind":                 {ChannelEndpointFilter{Search: "dns"}, []string{"zone"}},
		"a key: nothing":           {ChannelEndpointFilter{Search: "base_url"}, []string{}},
		"by kind":                  {ChannelEndpointFilter{ModuleNames: []string{"http"}}, []string{"Alpha", "edge-eu"}},
		"search and kind":          {ChannelEndpointFilter{Search: "example.com", ModuleNames: []string{"dns"}}, []string{"zone"}},
		"a quoted whole word only": {ChannelEndpointFilter{Search: `"example"`}, []string{"Alpha", "edge-eu", "zone"}},
	} {
		if got := list(c.filter); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}

	// A page at a time, by name.
	page, err := repo.FindByOperationIDWithCursor(ctx, op, ChannelEndpointFilter{}, nil, 2, true)
	if err != nil || len(page) != 2 {
		t.Fatalf("first page %d: %v", len(page), err)
	}
	cur, err := pagination.DecodeCursor(ChannelEndpointCursor(&page[1]))
	if err != nil {
		t.Fatal(err)
	}
	if next, err := repo.FindByOperationIDWithCursor(ctx, op, ChannelEndpointFilter{}, &cur, 2, true); err != nil || len(next) != 1 || next[0].Name != "zone" {
		t.Fatalf("next page %+v: %v", next, err)
	}

	// An update's values are what a search finds afterwards.
	eu.Values = models.JSONMap{"base_url": "https://moved.example.com"}
	if err := repo.Update(ctx, eu); err != nil {
		t.Fatal(err)
	}
	if got := list(ChannelEndpointFilter{Search: "edge-eu.example"}); len(got) != 0 {
		t.Errorf("old value still found: %q", got)
	}
	if got := list(ChannelEndpointFilter{Search: "moved.example"}); !slices.Equal(got, []string{"edge-eu"}) {
		t.Errorf("new value: %q", got)
	}

	if taken, err := repo.NameTaken(ctx, op, "zone"); !taken || err != nil {
		t.Errorf("zone taken: %v %v", taken, err)
	}
	if taken, err := repo.NameTaken(ctx, op, "zone-2"); taken || err != nil {
		t.Errorf("zone-2 taken: %v %v", taken, err)
	}

	// An endpoint stored before endpoints had a search text gets one.
	if err := db.Collection(channelEndpointCollection).UpdateOne(ctx, bson.M{"name": "zone"}, bson.M{"$unset": bson.M{"search_text": ""}}); err != nil {
		t.Fatal(err)
	}
	if got := list(ChannelEndpointFilter{Search: "c2.example.com"}); len(got) != 0 {
		t.Fatalf("found without a search text: %q", got)
	}
	if n, err := repo.BackfillSearchText(ctx); n != 1 || err != nil {
		t.Fatalf("backfill: %d %v", n, err)
	}
	if got := list(ChannelEndpointFilter{Search: "c2.example.com"}); !slices.Equal(got, []string{"zone"}) {
		t.Fatalf("after the backfill: %q", got)
	}
}
