package repository

import (
	"context"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// TestIntegrationModuleRegistryRecordStatus pins how status read outside a
// heartbeat (after an action) is stored: it replaces last_metrics, not merges
// into it, leaves liveness alone, and never touches an instance that is no
// longer registered.
func TestIntegrationModuleRegistryRecordStatus(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewModuleRegistryRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	if err := repo.Upsert(ctx, &models.Module{Type: "channel", Name: "http", Instance: "http-1", RPCQueue: "q.http-1", Version: "dev"}); err != nil {
		t.Fatal(err)
	}
	beat := time.Now().UTC().Truncate(time.Millisecond)
	if ok, err := repo.TouchHeartbeat(ctx, "http-1", "healthy", map[string]any{"draining": false, "open": 3}, nil, beat); !ok || err != nil {
		t.Fatalf("heartbeat: %v %v", ok, err)
	}

	if err := repo.RecordStatus(ctx, "http-1", map[string]any{"draining": true}); err != nil {
		t.Fatalf("RecordStatus: %v", err)
	}
	got, err := repo.FindByInstance(ctx, "http-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LastMetrics) != 1 || got.LastMetrics["draining"] != true {
		t.Errorf("last_metrics = %v, want only the status read", got.LastMetrics)
	}
	if got.LastHeartbeatAt == nil || !got.LastHeartbeatAt.Equal(beat) || got.LastStatus != "healthy" {
		t.Errorf("liveness changed: %v %q", got.LastHeartbeatAt, got.LastStatus)
	}

	// Not registered any more, or never: nothing written, no error.
	if ok, err := repo.MarkDeregistered(ctx, "http-1", "shutdown", time.Now()); !ok || err != nil {
		t.Fatalf("deregister: %v %v", ok, err)
	}
	for _, instance := range []string{"http-1", "nobody"} {
		if err := repo.RecordStatus(ctx, instance, map[string]any{"draining": false}); err != nil {
			t.Errorf("RecordStatus(%s): %v", instance, err)
		}
	}
	if got, _ := repo.FindByInstance(ctx, "http-1"); got.LastMetrics["draining"] != true {
		t.Errorf("a deregistered instance's status changed: %v", got.LastMetrics)
	}
}

// TestIntegrationModuleRegistryLatestIcons pins a kind's icon: the one its
// most recent registration that sent an icon carried, whatever the instance's
// state, and only kinds of the type asked for.
func TestIntegrationModuleRegistryLatestIcons(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewModuleRegistryRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	register := func(typ, name, instance, icon string, minutes int) {
		t.Helper()
		m := &models.Module{Type: typ, Name: name, Instance: instance, RPCQueue: "q." + instance, Icon: icon,
			RegisteredAt: at.Add(time.Duration(minutes) * time.Minute)}
		if err := repo.Upsert(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	register("channel", "http", "http-1", "<svg old/>", 0)
	register("channel", "http", "http-2", "<svg new/>", 5)
	register("channel", "http", "http-3", "", 9) // a newer instance without one keeps the kind's icon
	register("channel", "dns", "dns-1", "", 1)
	register("factory", "bob", "bob-1", "<svg bob/>", 2)
	if ok, err := repo.MarkDeregistered(ctx, "http-2", "shutdown", at.Add(10*time.Minute)); !ok || err != nil {
		t.Fatalf("deregister: %v %v", ok, err)
	}

	got, err := repo.LatestIcons(ctx, "channel")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["http"] != "<svg new/>" {
		t.Errorf("channel icons = %v, want only http's newest", got)
	}
	if got, err := repo.LatestIcons(ctx, "factory"); err != nil || len(got) != 1 || got["bob"] != "<svg bob/>" {
		t.Errorf("factory icons = %v (%v)", got, err)
	}
}
