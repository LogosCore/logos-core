package repository

import (
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
)

func TestRegisteredInstanceFilter_ScopesToRegistered(t *testing.T) {
	f := registeredInstanceFilter("http-1")
	if f["instance"] != "http-1" {
		t.Errorf("instance = %v, want http-1", f["instance"])
	}
	if f["status"] != models.ModuleStatusRegistered {
		t.Errorf("status = %v, want %q", f["status"], models.ModuleStatusRegistered)
	}
}

func TestHeartbeatUpdate_OmitsEmptyStatusAndNilMetrics(t *testing.T) {
	at := time.Unix(1000, 0).UTC()

	full := heartbeatUpdate("healthy", map[string]any{"x": 1}, nil, at)
	set := full["$set"].(bson.M)
	if set["last_heartbeat_at"] != at {
		t.Errorf("last_heartbeat_at = %v, want %v", set["last_heartbeat_at"], at)
	}
	if set["last_status"] != "healthy" {
		t.Errorf("last_status = %v, want healthy", set["last_status"])
	}
	if _, ok := set["last_metrics"]; !ok {
		t.Error("expected last_metrics to be set")
	}

	bare := heartbeatUpdate("", nil, nil, at)["$set"].(bson.M)
	if _, ok := bare["last_status"]; ok {
		t.Error("empty status should not be written")
	}
	if _, ok := bare["last_metrics"]; ok {
		t.Error("nil metrics should not be written")
	}
	if bare["last_heartbeat_at"] != at {
		t.Error("last_heartbeat_at must always be written")
	}
}

func TestDeregisterUpdate_Shape(t *testing.T) {
	at := time.Unix(2000, 0).UTC()
	set := deregisterUpdate("shutdown", at)["$set"].(bson.M)
	if set["status"] != models.ModuleStatusDeregistered {
		t.Errorf("status = %v, want deregistered", set["status"])
	}
	if set["deregister_reason"] != "shutdown" {
		t.Errorf("reason = %v, want shutdown", set["deregister_reason"])
	}
	if set["deregistered_at"] != at {
		t.Errorf("deregistered_at = %v, want %v", set["deregistered_at"], at)
	}
}

func TestMarkDeadUpdate_Shape(t *testing.T) {
	at := time.Unix(3000, 0).UTC()
	set := markDeadUpdate(at)["$set"].(bson.M)
	if set["status"] != models.ModuleStatusDead {
		t.Errorf("status = %v, want dead", set["status"])
	}
	if set["declared_dead_at"] != at {
		t.Errorf("declared_dead_at = %v, want %v", set["declared_dead_at"], at)
	}
}

func TestTakeoverUpdate_RevivesByUnsettingEndState(t *testing.T) {
	reg := &models.Module{
		Type:         "channel",
		Name:         "http",
		Instance:     "http-1",
		Version:      "1.2.0",
		RPCQueue:     "logos.channel.rpc.http-1",
		RegisteredAt: time.Unix(4000, 0).UTC(),
	}
	u := takeoverUpdate(reg)

	set := u["$set"].(bson.M)
	if set["status"] != models.ModuleStatusRegistered {
		t.Errorf("status = %v, want registered", set["status"])
	}
	if set["rpc_queue"] != "logos.channel.rpc.http-1" {
		t.Errorf("rpc_queue = %v", set["rpc_queue"])
	}
	if set["module_name"] != "http" {
		t.Errorf("module_name = %v, want http", set["module_name"])
	}

	unset := u["$unset"].(bson.M)
	for _, k := range []string{"deregistered_at", "deregister_reason", "declared_dead_at", "last_heartbeat_at", "last_status", "last_metrics"} {
		if _, ok := unset[k]; !ok {
			t.Errorf("expected %q to be unset on takeover", k)
		}
	}
}

func TestListFilter_EmptyMatchesAll(t *testing.T) {
	if f := listFilter(nil); len(f) != 0 {
		t.Errorf("nil statuses → %v, want empty filter (all rows)", f)
	}
	if f := listFilter([]string{}); len(f) != 0 {
		t.Errorf("empty statuses → %v, want empty filter (all rows)", f)
	}
}

func TestListFilter_ScopesToStatuses(t *testing.T) {
	f := listFilter([]string{models.ModuleStatusRegistered, models.ModuleStatusDead})
	cond, ok := f["status"].(bson.M)
	if !ok {
		t.Fatalf("status condition = %T, want bson.M", f["status"])
	}
	in, ok := cond["$in"].([]string)
	if !ok || len(in) != 2 {
		t.Fatalf("$in = %v, want 2-element string slice", cond["$in"])
	}
	if in[0] != models.ModuleStatusRegistered || in[1] != models.ModuleStatusDead {
		t.Errorf("$in = %v, want [registered dead]", in)
	}
}

func TestStaleRegisteredFilter_UsesIfNullFallback(t *testing.T) {
	cutoff := time.Unix(5000, 0).UTC()
	f := staleRegisteredFilter(cutoff)

	if f["status"] != models.ModuleStatusRegistered {
		t.Errorf("status = %v, want registered", f["status"])
	}
	expr, ok := f["$expr"].(bson.M)
	if !ok {
		t.Fatalf("$expr missing or wrong type: %T", f["$expr"])
	}
	lt, ok := expr["$lt"].(bson.A)
	if !ok || len(lt) != 2 {
		t.Fatalf("$lt = %v, want 2-element array", expr["$lt"])
	}
	ifNull, ok := lt[0].(bson.M)
	if !ok {
		t.Fatalf("expected $ifNull operand, got %T", lt[0])
	}
	args, ok := ifNull["$ifNull"].(bson.A)
	if !ok || args[0] != "$last_heartbeat_at" || args[1] != "$registered_at" {
		t.Errorf("$ifNull args = %v, want [$last_heartbeat_at $registered_at]", ifNull["$ifNull"])
	}
	if lt[1] != cutoff {
		t.Errorf("cutoff operand = %v, want %v", lt[1], cutoff)
	}
}

func TestTakeoverUpdate_ManagementFields(t *testing.T) {
	withManifest := &models.Module{
		Instance: "http-1", AppliedRevision: 4,
		Manifest:     &models.ModuleManifest{ManagementManifest: protocol.ManagementManifest{SchemaVersion: "1"}},
		ManifestHash: "abc",
	}
	u := takeoverUpdate(withManifest)
	set, unset := u["$set"].(bson.M), u["$unset"].(bson.M)
	if set["manifest"] != withManifest.Manifest || set["manifest_hash"] != "abc" || set["applied_revision"] != int64(4) {
		t.Errorf("$set = %v", set)
	}
	for _, k := range []string{"rejected", "restart_required", "last_apply_error"} {
		if _, ok := unset[k]; !ok {
			t.Errorf("%q should be cleared on register", k)
		}
	}
	if _, ok := unset["manifest"]; ok {
		t.Error("manifest must not be unset when declared")
	}

	// A registration that declares none removes the old manifest.
	u = takeoverUpdate(&models.Module{Instance: "http-1"})
	set, unset = u["$set"].(bson.M), u["$unset"].(bson.M)
	if _, ok := set["manifest"]; ok {
		t.Error("manifest set without a declaration")
	}
	if _, ok := unset["manifest"]; !ok {
		t.Error("manifest should be unset when not declared")
	}
	if _, ok := unset["manifest_hash"]; !ok {
		t.Error("manifest_hash should be unset when not declared")
	}
	if _, ok := unset["connection"]; !ok {
		t.Error("connection should be unset when not declared")
	}

	if _, ok := unset["icon"]; !ok {
		t.Error("icon should be unset when not sent")
	}
	if u := takeoverUpdate(&models.Module{Instance: "http-1", Icon: "<svg/>"}); u["$set"].(bson.M)["icon"] != "<svg/>" {
		t.Error("icon not set when sent")
	}

	conn := &models.ModuleConnectionSchema{ConnectionSchema: protocol.ConnectionSchema{SchemaVersion: "1", Version: 1}}
	u = takeoverUpdate(&models.Module{Instance: "http-1", Connection: conn})
	if u["$set"].(bson.M)["connection"] != conn {
		t.Error("connection not set when declared")
	}
	if _, ok := u["$unset"].(bson.M)["build"]; !ok {
		t.Error("build should be unset when not declared")
	}

	build := &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{SchemaVersion: "1", Version: 1}}
	u = takeoverUpdate(&models.Module{Instance: "bob-1", Build: build})
	if u["$set"].(bson.M)["build"] != build {
		t.Error("build not set when declared")
	}
}

func TestHeartbeatUpdate_AppliedRevision(t *testing.T) {
	rev := int64(7)
	set := heartbeatUpdate("healthy", nil, &rev, time.Unix(1, 0))["$set"].(bson.M)
	if set["applied_revision"] != int64(7) {
		t.Errorf("applied_revision = %v", set["applied_revision"])
	}
	if _, ok := heartbeatUpdate("healthy", nil, nil, time.Unix(1, 0))["$set"].(bson.M)["applied_revision"]; ok {
		t.Error("absent applied_revision must not be written")
	}
}

func TestApplyResultUpdate(t *testing.T) {
	at := time.Unix(9, 0).UTC()
	u := applyResultUpdate(protocol.ConfigApplyReply{AppliedRevision: 3, Rejected: map[string]string{"k": "no"}, RestartRequired: []string{"m"}}, at)
	set, unset := u["$set"].(bson.M), u["$unset"].(bson.M)
	if set["applied_revision"] != int64(3) || set["last_apply_at"] != at || set["rejected"] == nil || set["restart_required"] == nil {
		t.Errorf("$set = %v", set)
	}
	if _, ok := unset["last_apply_error"]; !ok {
		t.Error("a successful apply must clear last_apply_error")
	}
	u = applyResultUpdate(protocol.ConfigApplyReply{AppliedRevision: 3}, at)
	unset = u["$unset"].(bson.M)
	if _, ok := unset["rejected"]; !ok {
		t.Error("no rejections → rejected unset")
	}
	if _, ok := unset["restart_required"]; !ok {
		t.Error("nothing to restart → restart_required unset")
	}
}

func TestLatestPerKind(t *testing.T) {
	v := func(n int) *models.ModuleConnectionSchema {
		return &models.ModuleConnectionSchema{ConnectionSchema: protocol.ConnectionSchema{Version: n}}
	}
	// Sorted as the query sorts: by name, newest first within a name.
	got := latestPerKind([]models.Module{
		{Name: "dns", Connection: v(1)},
		{Name: "http", Connection: v(3)},
		{Name: "http", Connection: v(2)},
	})
	if len(got) != 2 || got["http"].Version != 3 || got["dns"].Version != 1 {
		t.Errorf("latest = %v", got)
	}
}
