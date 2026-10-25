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
// expectedRevision 0 creates the instance's document, a second "first write"
// is a conflict (refused by the unique index, not duplicated), a stale
// revision is a conflict, set/unset touch only the named keys, and documents
// are per instance.
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

	doc, err := repo.Apply(ctx, "http", "http-1", 0,
		map[string]any{"limit": 5, "hosts": []any{"a"}}, nil, actor, at)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if doc.Revision != 1 || doc.Values["limit"] != float64(5) || doc.Instance != "http-1" || doc.ModuleName != "http" {
		t.Fatalf("doc = %+v, want revision 1 with limit 5", doc)
	}

	if _, err := repo.Apply(ctx, "http", "http-1", 0,
		map[string]any{"limit": 6}, nil, actor, at); !errors.Is(err, ErrModuleConfigConflict) {
		t.Fatalf("second first-write err = %v, want conflict", err)
	}

	doc, err = repo.Apply(ctx, "http", "http-1", 1,
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

	if _, err := repo.Apply(ctx, "http", "http-1", 1,
		map[string]any{"limit": 8}, nil, actor, at); !errors.Is(err, ErrModuleConfigConflict) {
		t.Fatalf("stale apply err = %v, want conflict", err)
	}

	got, err := repo.FindByInstance(ctx, "http-1")
	if err != nil || got == nil || got.Revision != 2 || got.Values["limit"] != float64(7) {
		t.Fatalf("FindByInstance = %+v, %v", got, err)
	}
	if other, err := repo.FindByInstance(ctx, "http-2"); err != nil || other != nil {
		t.Errorf("http-2 must have no config: %+v, %v", other, err)
	}
}

// TestIntegrationModuleConfigRaiseRevision pins the catch-up raise: it lifts
// the revision without touching values, never lowers it, and creates an empty
// document for an instance that has none.
func TestIntegrationModuleConfigRaiseRevision(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_ = db.Collection(moduleConfigCollection).DropCollection(context.Background())
	})
	repo := NewModuleConfigRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	at := time.Now().UTC()

	if _, err := repo.Apply(ctx, "http", "http-1", 0, map[string]any{"limit": 5}, nil, models.ModuleActor{Type: "user", ID: "u1"}, at); err != nil {
		t.Fatal(err)
	}
	doc, err := repo.RaiseRevision(ctx, "http", "http-1", 4, at)
	if err != nil || doc.Revision != 4 || doc.Values["limit"] != float64(5) {
		t.Fatalf("raise = %+v, %v; want revision 4 with values kept", doc, err)
	}
	if doc, err = repo.RaiseRevision(ctx, "http", "http-1", 2, at); err != nil || doc.Revision != 4 {
		t.Fatalf("lower raise = %+v, %v; want revision unchanged at 4", doc, err)
	}
	if doc, err = repo.RaiseRevision(ctx, "http", "http-2", 3, at); err != nil || doc.Revision != 3 || len(doc.Values) != 0 || doc.ModuleName != "http" {
		t.Fatalf("raise on missing doc = %+v, %v", doc, err)
	}
	// A write after the raise continues from the raised revision.
	if doc, err = repo.Apply(ctx, "http", "http-2", 3, map[string]any{"limit": 9}, nil, models.ModuleActor{Type: "user", ID: "u1"}, at); err != nil || doc.Revision != 4 {
		t.Fatalf("apply after raise = %+v, %v", doc, err)
	}
}
