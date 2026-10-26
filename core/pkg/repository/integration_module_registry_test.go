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
