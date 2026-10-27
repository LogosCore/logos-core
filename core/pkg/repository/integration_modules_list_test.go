package repository

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
)

// TestIntegrationModulesList pins the Modules page's query: newest
// registration first, a page at a time; the app's search over the instance,
// kind, type, version and description; and the status and type filters.
func TestIntegrationModulesList(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewModuleRegistryRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	coll := db.Collection(moduleRegistryCollection)
	for i, m := range []models.Module{
		{Instance: "http-local", Name: "http", Type: "channel", Version: "3.5.0", Description: "HTTP transport", Status: models.ModuleStatusRegistered},
		{Instance: "bob-local", Name: "bob", Type: "factory", Version: "dev", Description: "The factory punching bag", Status: models.ModuleStatusRegistered},
		{Instance: "dns-old", Name: "dns", Type: "channel", Version: "1.0.0", Status: models.ModuleStatusDead},
	} {
		m.RegisteredAt = now.Add(time.Duration(i) * time.Minute)
		if _, err := coll.InsertOne(ctx, &m); err != nil {
			t.Fatal(err)
		}
	}

	list := func(f ModuleFilter) []string {
		t.Helper()
		ms, err := repo.FindWithCursor(ctx, f, nil, 10, true)
		if err != nil {
			t.Fatal(err)
		}
		n, err := repo.Count(ctx, f)
		if err != nil || n != int64(len(ms)) {
			t.Fatalf("count %d %v, listed %d", n, err, len(ms))
		}
		out := make([]string, len(ms))
		for i, m := range ms {
			out[i] = m.Instance
		}
		return out
	}
	for name, c := range map[string]struct {
		filter ModuleFilter
		want   []string
	}{
		"all, newest registration first": {ModuleFilter{}, []string{"dns-old", "bob-local", "http-local"}},
		"the description, case aside":    {ModuleFilter{Search: "PUNCHING"}, []string{"bob-local"}},
		"the kind":                       {ModuleFilter{Search: "dns"}, []string{"dns-old"}},
		"the type":                       {ModuleFilter{Search: "factory"}, []string{"bob-local"}},
		"the version":                    {ModuleFilter{Search: "3.5"}, []string{"http-local"}},
		"by state":                       {ModuleFilter{Statuses: []string{models.ModuleStatusRegistered}}, []string{"bob-local", "http-local"}},
		"by type":                        {ModuleFilter{Types: []string{"channel"}}, []string{"dns-old", "http-local"}},
		"search, state and type":         {ModuleFilter{Search: "local", Statuses: []string{models.ModuleStatusRegistered}, Types: []string{"channel"}}, []string{"http-local"}},
	} {
		if got := list(c.filter); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}

	page, err := repo.FindWithCursor(ctx, ModuleFilter{}, nil, 2, true)
	if err != nil || len(page) != 2 {
		t.Fatalf("first page %d: %v", len(page), err)
	}
	cur, err := pagination.DecodeCursor(ModuleCursor(&page[1]))
	if err != nil {
		t.Fatal(err)
	}
	if next, err := repo.FindWithCursor(ctx, ModuleFilter{}, &cur, 2, true); err != nil || len(next) != 1 || next[0].Instance != "http-local" {
		t.Fatalf("next page %+v: %v", next, err)
	}
}
