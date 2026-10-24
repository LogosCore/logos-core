package modulemgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// --- effective config ---

func TestComputeEffective_MergeRevisionAndSource(t *testing.T) {
	m := testManifest().ManagementManifest
	mod := &models.ModuleConfig{Revision: 3, Values: models.JSONMap{"limit": float64(20), "mode": "safe", "gone": "x"}}
	inst := &models.ModuleConfig{Revision: 2, Values: models.JSONMap{"limit": float64(50), "token": "s3cret"}}

	eff := ComputeEffective(&m, mod, inst)
	if eff.Revision() != 5 || eff.ModuleRevision != 3 || eff.InstanceRevision != 2 {
		t.Fatalf("revisions = %d/%d/%d, want 5 = 3 + 2", eff.Revision(), eff.ModuleRevision, eff.InstanceRevision)
	}
	byKey := map[string]PropertyState{}
	for _, p := range eff.Properties {
		byKey[p.Spec.Key] = p
	}
	if p := byKey["limit"]; p.Effective != float64(50) || p.Source != SourceInstance || p.ModuleValue != float64(20) {
		t.Errorf("limit = %+v, want instance 50 over module 20", p)
	}
	if p := byKey["mode"]; p.Effective != "safe" || p.Source != SourceModule || p.HasInstance {
		t.Errorf("mode = %+v, want module value", p)
	}
	if p := byKey["token"]; p.Source != SourceInstance || !p.IsSet() {
		t.Errorf("token = %+v", p)
	}

	// Only declared keys with a stored value go on the wire; "gone" is no
	// longer declared and is dropped.
	wire := eff.Wire()
	want := map[string]any{"limit": float64(50), "mode": "safe", "token": "s3cret"}
	if wire.Revision != 5 || !reflect.DeepEqual(wire.Values, want) {
		t.Errorf("wire = %+v, want revision 5 values %v", wire, want)
	}

	// Nothing stored: everything is the manifest default, and nothing is sent.
	empty := ComputeEffective(&m, nil, nil)
	if empty.Revision() != 0 || len(empty.Wire().Values) != 0 {
		t.Errorf("empty = %+v", empty.Wire())
	}
	for _, p := range empty.Properties {
		if p.Source != SourceDefault || !reflect.DeepEqual(p.Effective, p.Spec.Default) {
			t.Errorf("%s = %+v, want default", p.Spec.Key, p)
		}
	}
}

func TestSyncState(t *testing.T) {
	reg := models.Module{Status: models.ModuleStatusRegistered}
	cases := []struct {
		name string
		mod  models.Module
		rev  int64
		want string
	}{
		{"applied", withApplied(reg, 4, ""), 4, SyncInSync},
		{"behind", withApplied(reg, 3, ""), 4, SyncPending},
		{"push failed", withApplied(reg, 3, "timeout"), 4, SyncOutOfSync},
		{"applied despite an old error", withApplied(reg, 4, "timeout"), 4, SyncInSync},
		{"not registered", models.Module{Status: models.ModuleStatusDead, AppliedRevision: 4}, 4, SyncOutOfSync},
	}
	for _, c := range cases {
		if got := SyncState(c.mod, c.rev); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func withApplied(m models.Module, rev int64, applyErr string) models.Module {
	m.AppliedRevision, m.LastApplyError = rev, applyErr
	return m
}

// --- updateModuleConfig ---

func TestUpdateConfig_Validation(t *testing.T) {
	h := newHarness(registered("http-1"), models.Module{Name: "bare", Instance: "bare-1", Status: models.ModuleStatusRegistered})
	ctx := context.Background()
	cases := []struct {
		name string
		req  UpdateRequest
	}{
		{"undeclared key", UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule, Values: []KeyValue{{"nope", 1}}}},
		{"out of range", UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule, Values: []KeyValue{{"limit", json.Number("1000")}}}},
		{"not an integer", UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule, Values: []KeyValue{{"limit", 1.5}}}},
		{"bad enum", UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule, Values: []KeyValue{{"mode", "slow"}}}},
		{"duplicate", UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule, Values: []KeyValue{{"limit", 2}, {"limit", 3}}}},
		{"empty", UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule}},
		{"no manifest", UpdateRequest{Instance: "bare-1", Scope: models.ModuleConfigScopeModule, Values: []KeyValue{{"limit", 2}}}},
	}
	for _, c := range cases {
		_, err := h.svc.UpdateConfig(ctx, c.req)
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Errorf("%s: err = %v, want ValidationError", c.name, err)
		}
	}
	if _, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "ghost", Scope: models.ModuleConfigScopeModule}); !errors.Is(err, ErrModuleNotFound) {
		t.Errorf("unknown instance err = %v", err)
	}
	if h.configs.applies != 0 {
		t.Errorf("invalid requests must not write (%d writes)", h.configs.applies)
	}
}

func TestUpdateConfig_Conflict(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.configs.put("http", models.ModuleConfigScopeModule, "", 4, map[string]any{"limit": float64(5)})
	ctx := context.Background()

	_, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule,
		Values: []KeyValue{{"limit", 6}}, ExpectedRevision: 3})
	var c *ConflictError
	if !errors.As(err, &c) || c.CurrentRevision != 4 {
		t.Fatalf("stale expectedRevision err = %v, want conflict at 4", err)
	}

	// INSTANCE scope has its own revision: 0 while no document exists.
	if _, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeInstance,
		Values: []KeyValue{{"limit", 6}}, ExpectedRevision: 4}); !errors.As(err, &c) {
		t.Errorf("instance scope at wrong revision err = %v, want conflict", err)
	}

	// Lost race at write time: the read matched but the conditional write did not.
	h.configs.raceOnApply = true
	if _, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "http-1", Scope: models.ModuleConfigScopeModule,
		Values: []KeyValue{{"limit", 6}}, ExpectedRevision: 4}); !errors.As(err, &c) {
		t.Errorf("write-time race err = %v, want conflict", err)
	}
	h.svc.Wait()
	if len(h.rpc.calls) != 0 || len(h.changes.rows) != 0 {
		t.Error("a conflict must neither push nor audit")
	}
}

func TestUpdateConfig_WritesAuditsAndPushesModuleScope(t *testing.T) {
	other := registered("http-2")
	dead := registered("http-3")
	dead.Status = models.ModuleStatusDead
	h := newHarness(registered("http-1"), other, dead)
	h.configs.put("http", models.ModuleConfigScopeModule, "", 1, map[string]any{"limit": float64(5), "token": "old"})
	ctx := context.Background()

	mod, err := h.svc.UpdateConfig(ctx, UpdateRequest{
		Instance: "http-1", Scope: models.ModuleConfigScopeModule, ExpectedRevision: 1, Actor: alice,
		Values: []KeyValue{{"limit", json.Number("7")}, {"token", "new"}, {"mode", nil}},
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if mod == nil || mod.Instance != "http-1" {
		t.Fatalf("returned module = %+v", mod)
	}
	h.svc.Wait()

	// Audit: limit and token changed, token redacted; mode was unset already,
	// so clearing it is no change.
	if len(h.changes.rows) != 1 {
		t.Fatalf("audit rows = %d", len(h.changes.rows))
	}
	ch := h.changes.rows[0]
	if ch.Revision != 2 || ch.Scope != models.ModuleConfigScopeModule || ch.Actor != alice || ch.Instance != "" {
		t.Errorf("change = %+v", ch)
	}
	got := map[string]models.ModuleConfigKeyChange{}
	for _, c := range ch.Changes {
		got[c.Key] = c
	}
	if len(got) != 2 || got["limit"].Old != float64(5) || got["limit"].New != float64(7) {
		t.Errorf("changes = %+v", ch.Changes)
	}
	if got["token"].Old != models.RedactedValue || got["token"].New != models.RedactedValue {
		t.Errorf("secret not redacted: %+v", got["token"])
	}

	// Pushed to both registered instances of "http", not the dead one, with the
	// effective config at the new revision.
	applies := h.rpc.callsOf(protocol.TypeMgmtConfigApply)
	queues := map[string]bool{}
	for _, c := range applies {
		queues[c.queue] = true
		var req protocol.ConfigApplyRequest
		_ = json.Unmarshal(c.payload, &req)
		if req.Revision != 2 || req.Values["limit"] != float64(7) || req.Values["token"] != "new" {
			t.Errorf("push = %s", c.payload)
		}
		if req.Actor == nil || req.Actor.ID != alice.ID {
			t.Errorf("push actor = %+v", req.Actor)
		}
	}
	if !queues["q.http-1"] || !queues["q.http-2"] || queues["q.http-3"] {
		t.Errorf("pushed to %v", queues)
	}
	if r := h.reg.row("http-2"); r.AppliedRevision != 2 {
		t.Errorf("http-2 applied = %d, want 2", r.AppliedRevision)
	}
	if h.bus.count() == 0 {
		t.Error("expected module.updated events")
	}
}

func TestUpdateConfig_InstanceScopePushesOnlyThatInstance(t *testing.T) {
	h := newHarness(registered("http-1"), registered("http-2"))
	if _, err := h.svc.UpdateConfig(context.Background(), UpdateRequest{
		Instance: "http-1", Scope: models.ModuleConfigScopeInstance, Values: []KeyValue{{"mode", "safe"}}, Actor: alice,
	}); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	applies := h.rpc.callsOf(protocol.TypeMgmtConfigApply)
	if len(applies) != 1 || applies[0].queue != "q.http-1" {
		t.Fatalf("applies = %+v", applies)
	}
	if h.changes.rows[0].Instance != "http-1" || h.changes.rows[0].Revision != 1 {
		t.Errorf("change = %+v", h.changes.rows[0])
	}
}

func TestUpdateConfig_NoChangeIsANoOp(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.configs.put("http", models.ModuleConfigScopeModule, "", 1, map[string]any{"limit": float64(5)})
	if _, err := h.svc.UpdateConfig(context.Background(), UpdateRequest{
		Instance: "http-1", Scope: models.ModuleConfigScopeModule, ExpectedRevision: 1, Values: []KeyValue{{"limit", 5}},
	}); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	if h.configs.applies != 0 || len(h.rpc.calls) != 0 {
		t.Error("setting a key to its current value must not bump the revision or push")
	}
}

// --- pushing ---

func TestPush_RetriesThenRecordsError(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.rpc.handler = func(rpcCall) (messaging.ReplyEnvelope, error) {
		return messaging.ReplyEnvelope{}, fmt.Errorf("x: %w", messaging.ErrRPCTimeout)
	}
	h.svc.RequestPush("http-1", alice)
	h.svc.Wait()
	if n := len(h.rpc.calls); n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
	row := h.reg.row("http-1")
	if row.LastApplyError == "" {
		t.Fatal("last_apply_error not recorded")
	}
	if SyncState(row, 1) != SyncOutOfSync {
		t.Errorf("sync = %s", SyncState(row, 1))
	}
}

func TestPush_ErrorReplyIsNotRetried(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.rpc.handler = func(rpcCall) (messaging.ReplyEnvelope, error) {
		return errReply(protocol.CodeValidationFailed, "bad config"), nil
	}
	h.svc.RequestPush("http-1", alice)
	h.svc.Wait()
	if n := len(h.rpc.calls); n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
	if row := h.reg.row("http-1"); row.LastApplyError == "" {
		t.Error("expected last_apply_error")
	}
}

func TestPush_RecordsRejectedAndRestartRequired(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.configs.put("http", models.ModuleConfigScopeModule, "", 2, map[string]any{"mode": "safe"})
	h.rpc.handler = func(rpcCall) (messaging.ReplyEnvelope, error) {
		return okReply(protocol.ConfigApplyReply{AppliedRevision: 2, Rejected: map[string]string{"limit": "too big"}, RestartRequired: []string{"mode"}})
	}
	h.svc.RequestPush("http-1", alice)
	h.svc.Wait()
	row := h.reg.row("http-1")
	if row.AppliedRevision != 2 || row.Rejected["limit"] != "too big" || len(row.RestartRequired) != 1 {
		t.Errorf("row = %+v", row)
	}
}

func TestPush_SingleFlightCoalesces(t *testing.T) {
	h := newHarness(registered("http-1"))
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	h.rpc.handler = func(c rpcCall) (messaging.ReplyEnvelope, error) {
		started <- struct{}{}
		<-release
		var req protocol.ConfigApplyRequest
		_ = json.Unmarshal(c.payload, &req)
		return okReply(protocol.ConfigApplyReply{AppliedRevision: req.Revision})
	}
	h.svc.RequestPush("http-1", alice)
	<-started
	for i := 0; i < 5; i++ {
		h.svc.RequestPush("http-1", alice) // all coalesce into one re-run
	}
	close(release)
	h.svc.Wait()
	if n := len(h.rpc.calls); n != 2 {
		t.Errorf("calls = %d, want 2 (the running push + one coalesced re-run)", n)
	}
}

func TestHeartbeatApplied_PushesOnlyWhenBehind(t *testing.T) {
	behind := registered("http-1")
	behind.AppliedRevision = 1
	inSync := registered("http-2")
	inSync.AppliedRevision = 3
	h := newHarness(behind, inSync)
	h.configs.put("http", models.ModuleConfigScopeModule, "", 3, map[string]any{"limit": float64(9)})

	h.svc.HeartbeatApplied("http-1", 1)
	h.svc.HeartbeatApplied("http-2", 3)
	h.svc.Wait()
	applies := h.rpc.callsOf(protocol.TypeMgmtConfigApply)
	if len(applies) != 1 || applies[0].queue != "q.http-1" {
		t.Fatalf("applies = %+v, want one push to http-1", applies)
	}
	var req protocol.ConfigApplyRequest
	_ = json.Unmarshal(applies[0].payload, &req)
	if req.Actor == nil || req.Actor.Type != "system" {
		t.Errorf("drift push actor = %+v, want system", req.Actor)
	}

	// Rate-limited: a second heartbeat within the interval does nothing.
	h.svc.HeartbeatApplied("http-1", 1)
	h.svc.Wait()
	if n := len(h.rpc.callsOf(protocol.TypeMgmtConfigApply)); n != 1 {
		t.Errorf("applies after repeat heartbeat = %d, want 1", n)
	}
}

// --- actions ---

func TestInvocationOutcome(t *testing.T) {
	ok, _ := okReply(protocol.ActionInvokeReply{InvocationID: "i", Result: map[string]any{"flushed": 3}})
	okNoResult, _ := okReply(protocol.ActionInvokeReply{InvocationID: "i"})
	cases := []struct {
		name       string
		reply      messaging.ReplyEnvelope
		err        error
		wantStatus string
		wantResult string
		wantErr    bool
	}{
		{"success", ok, nil, models.ModuleActionSucceeded, `{"flushed":3}`, false},
		{"success without result", okNoResult, nil, models.ModuleActionSucceeded, "", false},
		{"action_failed", errReply(protocol.CodeActionFailed, "disk full"), nil, models.ModuleActionFailed, "", true},
		{"not_found", errReply(protocol.CodeNotFound, "no such action"), nil, models.ModuleActionFailed, "", true},
		{"timeout", messaging.ReplyEnvelope{}, fmt.Errorf("rpc: %w", messaging.ErrRPCTimeout), models.ModuleActionTimedOut, "", true},
		{"unroutable", messaging.ReplyEnvelope{}, messaging.ErrRPCUnroutable, models.ModuleActionFailed, "", true},
	}
	for _, c := range cases {
		status, result, errMsg := InvocationOutcome(c.reply, c.err, 5*time.Second)
		if status != c.wantStatus || result != c.wantResult || (errMsg != "") != c.wantErr {
			t.Errorf("%s: (%s, %q, %q)", c.name, status, result, errMsg)
		}
	}
}

func TestInvokeAction(t *testing.T) {
	h := newHarness(registered("http-1"))
	ctx := context.Background()
	h.rpc.handler = func(c rpcCall) (messaging.ReplyEnvelope, error) {
		var req protocol.ActionInvokeRequest
		_ = json.Unmarshal(c.payload, &req)
		return okReply(protocol.ActionInvokeReply{InvocationID: req.InvocationID, Result: "done"})
	}

	inv, err := h.svc.InvokeAction(ctx, InvokeRequest{Instance: "http-1", Action: "flush", Actor: alice,
		Params: map[string]any{"count": json.Number("2"), "password": "hunter2"}})
	if err != nil {
		t.Fatalf("InvokeAction: %v", err)
	}
	if inv.Status != models.ModuleActionSucceeded || inv.Result() != "done" || inv.FinishedAt == nil {
		t.Errorf("inv = %+v", inv)
	}
	if inv.Params["password"] != models.RedactedValue || inv.Params["count"] != float64(2) {
		t.Errorf("stored params = %v, want secret redacted", inv.Params)
	}
	call := h.rpc.callsOf(protocol.TypeMgmtActionInvoke)[0]
	var sent protocol.ActionInvokeRequest
	_ = json.Unmarshal(call.payload, &sent)
	if sent.Params["password"] != "hunter2" || sent.InvocationID != inv.InvocationID || call.timeout != DefaultActionTimeout {
		t.Errorf("sent = %+v timeout %s", sent, call.timeout)
	}
	if h.invs.finished[inv.InvocationID] != models.ModuleActionSucceeded {
		t.Error("terminal state not persisted")
	}

	// Validation: missing required param, unknown param, undeclared action.
	for _, req := range []InvokeRequest{
		{Instance: "http-1", Action: "flush"},
		{Instance: "http-1", Action: "flush", Params: map[string]any{"count": 1, "extra": 1}},
		{Instance: "http-1", Action: "nope"},
		{Instance: "http-1", Action: "flush", Params: "not an object"},
	} {
		var v *ValidationError
		if _, err := h.svc.InvokeAction(ctx, req); !errors.As(err, &v) {
			t.Errorf("%+v: err = %v, want ValidationError", req, err)
		}
	}

	// High risk needs the extra permission.
	if _, err := h.svc.InvokeAction(ctx, InvokeRequest{Instance: "http-1", Action: "wipe"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("high-risk without permission err = %v", err)
	}
	inv, err = h.svc.InvokeAction(ctx, InvokeRequest{Instance: "http-1", Action: "wipe", AllowHighRisk: true})
	if err != nil || inv.Status != models.ModuleActionSucceeded {
		t.Errorf("high-risk with permission: %+v, %v", inv, err)
	}
	if c := h.rpc.callsOf(protocol.TypeMgmtActionInvoke); c[len(c)-1].timeout != 5*time.Second {
		t.Errorf("declared timeout not used: %s", c[len(c)-1].timeout)
	}

	// A failing action is a normal return.
	h.rpc.handler = func(rpcCall) (messaging.ReplyEnvelope, error) {
		return errReply(protocol.CodeActionFailed, "disk full"), nil
	}
	inv, err = h.svc.InvokeAction(ctx, InvokeRequest{Instance: "http-1", Action: "flush", Params: map[string]any{"count": 1}})
	if err != nil || inv.Status != models.ModuleActionFailed || inv.Error == "" {
		t.Errorf("failed action: %+v, %v", inv, err)
	}

	// Not registered.
	h.reg.rows["http-1"].Status = models.ModuleStatusDead
	var v *ValidationError
	if _, err := h.svc.InvokeAction(ctx, InvokeRequest{Instance: "http-1", Action: "flush", Params: map[string]any{"count": 1}}); !errors.As(err, &v) {
		t.Errorf("dead instance err = %v", err)
	}
}
