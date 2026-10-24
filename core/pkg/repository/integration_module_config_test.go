package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// TestIntegrationModuleConfigApply pins the compare-and-set that
// updateModuleConfig's optimistic concurrency rests on: the first write at
// expectedRevision 0 creates the scope document, a second "first write" is a
// conflict (refused by the unique index, not duplicated), a stale revision is a
// conflict, and set/unset touch only the named keys.
func TestIntegrationModuleConfigApply(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_ = db.Collection(moduleConfigCollection).DropCollection(context.Background())
	})
	repo := NewModuleConfigRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	actor := models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}
	at := time.Now().UTC()

	doc, err := repo.Apply(ctx, "http", models.ModuleConfigScopeModule, "", 0,
		map[string]any{"limit": 5, "hosts": []any{"a"}}, nil, actor, at)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if doc.Revision != 1 || doc.Values["limit"] != float64(5) {
		t.Fatalf("doc = %+v, want revision 1 with limit 5", doc)
	}

	if _, err := repo.Apply(ctx, "http", models.ModuleConfigScopeModule, "", 0,
		map[string]any{"limit": 6}, nil, actor, at); !errors.Is(err, ErrModuleConfigConflict) {
		t.Fatalf("second first-write err = %v, want conflict", err)
	}

	doc, err = repo.Apply(ctx, "http", models.ModuleConfigScopeModule, "", 1,
		map[string]any{"limit": 7}, []string{"hosts"}, actor, at)
	if err != nil {
		t.Fatalf("apply at 1: %v", err)
	}
	if doc.Revision != 2 || doc.Values["limit"] != float64(7) {
		t.Fatalf("doc = %+v, want revision 2 with limit 7", doc)
	}
	if _, ok := doc.Values["hosts"]; ok {
		t.Errorf("hosts should have been unset: %+v", doc.Values)
	}

	if _, err := repo.Apply(ctx, "http", models.ModuleConfigScopeModule, "", 1,
		map[string]any{"limit": 8}, nil, actor, at); !errors.Is(err, ErrModuleConfigConflict) {
		t.Fatalf("stale apply err = %v, want conflict", err)
	}

	if _, err := repo.Apply(ctx, "http", models.ModuleConfigScopeInstance, "http-1", 0,
		map[string]any{"limit": 9}, nil, actor, at); err != nil {
		t.Fatalf("instance apply: %v", err)
	}
	mod, inst, err := repo.FindForInstance(ctx, "http", "http-1")
	if err != nil {
		t.Fatal(err)
	}
	if mod == nil || inst == nil || mod.Revision != 2 || inst.Revision != 1 || inst.Values["limit"] != float64(9) {
		t.Fatalf("FindForInstance = %+v / %+v", mod, inst)
	}
	if _, other, _ := repo.FindForInstance(ctx, "http", "http-2"); other != nil {
		t.Errorf("http-2 must not see http-1's instance scope: %+v", other)
	}
}
