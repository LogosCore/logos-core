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

func TestComputeEffective_StoredOverDefault(t *testing.T) {
	m := testManifest().ManagementManifest
	cfg := &models.ModuleConfig{Revision: 3, Values: models.JSONMap{"limit": float64(50), "token": "s3cret", "gone": "x"}}

	eff := ComputeEffective(&m, cfg)
	if eff.Revision != 3 {
		t.Fatalf("revision = %d, want 3", eff.Revision)
	}
	byKey := map[string]PropertyState{}
	for _, p := range eff.Properties {
		byKey[p.Spec.Key] = p
	}
	if p := byKey["limit"]; p.Effective != float64(50) || p.Value != float64(50) || p.Source != SourceSet || !p.IsSet {
		t.Errorf("limit = %+v, want stored 50", p)
	}
	if p := byKey["mode"]; p.Effective != "fast" || p.Source != SourceDefault || p.IsSet || p.Value != nil {
		t.Errorf("mode = %+v, want manifest default", p)
	}
	if p := byKey["token"]; p.Source != SourceSet || !p.IsSet {
		t.Errorf("token = %+v", p)
	}

	// Only declared keys with a stored value go on the wire; "gone" is no
	// longer declared and is dropped.
	wire := eff.Wire()
	want := map[string]any{"limit": float64(50), "token": "s3cret"}
	if wire.Revision != 3 || !reflect.DeepEqual(wire.Values, want) {
		t.Errorf("wire = %+v, want revision 3 values %v", wire, want)
	}

	// Nothing stored: everything is the manifest default, and nothing is sent.
	empty := ComputeEffective(&m, nil)
	if empty.Revision != 0 || len(empty.Wire().Values) != 0 {
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
		{"applied ahead of desired", withApplied(reg, 5, ""), 4, SyncPending},
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
		{"undeclared key", UpdateRequest{Instance: "http-1", Values: []KeyValue{{"nope", 1}}}},
		{"out of range", UpdateRequest{Instance: "http-1", Values: []KeyValue{{"limit", json.Number("1000")}}}},
		{"not an integer", UpdateRequest{Instance: "http-1", Values: []KeyValue{{"limit", 1.5}}}},
		{"bad enum", UpdateRequest{Instance: "http-1", Values: []KeyValue{{"mode", "slow"}}}},
		{"duplicate", UpdateRequest{Instance: "http-1", Values: []KeyValue{{"limit", 2}, {"limit", 3}}}},
		{"empty", UpdateRequest{Instance: "http-1"}},
		{"no manifest", UpdateRequest{Instance: "bare-1", Values: []KeyValue{{"limit", 2}}}},
	}
	for _, c := range cases {
		_, err := h.svc.UpdateConfig(ctx, c.req)
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Errorf("%s: err = %v, want ValidationError", c.name, err)
		}
	}
	if _, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "ghost"}); !errors.Is(err, ErrModuleNotFound) {
		t.Errorf("unknown instance err = %v", err)
	}
	if h.configs.applies != 0 {
		t.Errorf("invalid requests must not write (%d writes)", h.configs.applies)
	}
}

func TestUpdateConfig_Conflict(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.configs.put("http-1", 4, map[string]any{"limit": float64(5)})
	ctx := context.Background()

	_, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "http-1",
		Values: []KeyValue{{"limit", 6}}, ExpectedRevision: 3})
	var c *ConflictError
	if !errors.As(err, &c) || c.CurrentRevision != 4 {
		t.Fatalf("stale expectedRevision err = %v, want conflict at 4", err)
	}

	// Lost race at write time: the read matched but the conditional write did not.
	h.configs.raceOnApply = true
	if _, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "http-1",
		Values: []KeyValue{{"limit", 6}}, ExpectedRevision: 4}); !errors.As(err, &c) {
		t.Errorf("write-time race err = %v, want conflict", err)
	}
	h.svc.Wait()
	if len(h.rpc.calls) != 0 || len(h.changes.rows) != 0 {
		t.Error("a conflict must neither push nor audit")
	}
}

func TestUpdateConfig_WritesAuditsAndPushesThatInstanceOnly(t *testing.T) {
	h := newHarness(registered("http-1"), registered("http-2"))
	h.configs.put("http-1", 1, map[string]any{"limit": float64(5), "token": "old"})
	h.configs.put("http-2", 7, map[string]any{"limit": float64(99)})
	ctx := context.Background()

	mod, err := h.svc.UpdateConfig(ctx, UpdateRequest{
		Instance: "http-1", ExpectedRevision: 1, Actor: alice,
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
	if ch.Revision != 2 || ch.Instance != "http-1" || ch.ModuleName != "http" || ch.Actor != alice {
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

	// Pushed to http-1 only, at its new revision; http-2 and its config are
	// untouched.
	applies := h.rpc.callsOf(protocol.TypeMgmtConfigApply)
	if len(applies) != 1 || applies[0].queue != "q.http-1" {
		t.Fatalf("applies = %+v, want one push to http-1", applies)
	}
	var req protocol.ConfigApplyRequest
	_ = json.Unmarshal(applies[0].payload, &req)
	if req.Revision != 2 || req.Values["limit"] != float64(7) || req.Values["token"] != "new" {
		t.Errorf("push = %s", applies[0].payload)
	}
	if req.Actor == nil || req.Actor.ID != alice.ID {
		t.Errorf("push actor = %+v", req.Actor)
	}
	if r := h.reg.row("http-1"); r.AppliedRevision != 2 {
		t.Errorf("http-1 applied = %d, want 2", r.AppliedRevision)
	}
	if d := h.configs.doc("http-2"); d.Revision != 7 || d.Values["limit"] != float64(99) {
		t.Errorf("http-2 config changed: %+v", d)
	}
	if h.bus.count() == 0 {
		t.Error("expected module.updated events")
	}

	// Reset to default: a nil value removes the stored key.
	if _, err := h.svc.UpdateConfig(ctx, UpdateRequest{Instance: "http-1", ExpectedRevision: 2, Actor: alice,
		Values: []KeyValue{{"limit", nil}}}); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	if d := h.configs.doc("http-1"); d.Revision != 3 || d.Values["limit"] != nil {
		t.Errorf("after reset = %+v", d)
	}
	if last := h.changes.rows[len(h.changes.rows)-1]; last.Changes[0].Old != float64(7) || last.Changes[0].New != nil {
		t.Errorf("reset audit = %+v", last.Changes)
	}
}

func TestUpdateConfig_NoChangeIsANoOp(t *testing.T) {
	h := newHarness(registered("http-1"))
	h.configs.put("http-1", 1, map[string]any{"limit": float64(5)})
	if _, err := h.svc.UpdateConfig(context.Background(), UpdateRequest{
		Instance: "http-1", ExpectedRevision: 1, Values: []KeyValue{{"limit", 5}},
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
	h.configs.put("http-1", 2, map[string]any{"mode": "safe"})
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
	h.configs.put("http-1", 3, map[string]any{"limit": float64(9)})
	h.configs.put("http-2", 3, map[string]any{"limit": float64(9)})

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

// TestCatchUp_AppliedAheadOfDesired covers a config store that was reset or
// migrated while the module kept running: the instance reports applied 2 but
// the stored revision is lower. Modules ignore applies below what they already
// applied, so core raises the revision to applied+1 (values kept, no audit
// entry) and pushes that, which brings the instance back IN_SYNC.
func TestCatchUp_AppliedAheadOfDesired(t *testing.T) {
	ahead := registered("http-1")
	ahead.AppliedRevision = 2
	fresh := registered("http-2")
	fresh.AppliedRevision = 2
	h := newHarness(ahead, fresh)
	h.configs.put("http-1", 1, map[string]any{"limit": float64(9)})
	// http-2 has no config document at all (desired revision 0).
	// The fake module behaves like the real one: it ignores stale revisions.
	h.rpc.handler = func(c rpcCall) (messaging.ReplyEnvelope, error) {
		var req protocol.ConfigApplyRequest
		_ = json.Unmarshal(c.payload, &req)
		applied := int64(2)
		if req.Revision > applied {
			applied = req.Revision
		}
		return okReply(protocol.ConfigApplyReply{AppliedRevision: applied})
	}

	h.svc.HeartbeatApplied("http-1", 2)
	h.svc.HeartbeatApplied("http-2", 2)
	h.svc.Wait()

	for _, inst := range []string{"http-1", "http-2"} {
		d := h.configs.doc(inst)
		if d == nil || d.Revision != 3 {
			t.Fatalf("%s config = %+v, want revision raised to 3", inst, d)
		}
		if row := h.reg.row(inst); row.AppliedRevision != 3 || SyncState(row, d.Revision) != SyncInSync {
			t.Errorf("%s applied = %d sync = %s, want 3 IN_SYNC", inst, row.AppliedRevision, SyncState(row, d.Revision))
		}
	}
	if d := h.configs.doc("http-1"); d.Values["limit"] != float64(9) {
		t.Errorf("raise must not change values: %+v", d.Values)
	}
	pushed := map[string]protocol.ConfigApplyRequest{}
	for _, c := range h.rpc.callsOf(protocol.TypeMgmtConfigApply) {
		var req protocol.ConfigApplyRequest
		_ = json.Unmarshal(c.payload, &req)
		pushed[c.queue] = req
	}
	if r := pushed["q.http-1"]; r.Revision != 3 || r.Values["limit"] != float64(9) {
		t.Errorf("http-1 push = %+v", r)
	}
	if r := pushed["q.http-2"]; r.Revision != 3 || len(r.Values) != 0 {
		t.Errorf("http-2 push = %+v", r)
	}
	if len(h.changes.rows) != 0 {
		t.Errorf("a catch-up raise is not an operator change; audit rows = %d", len(h.changes.rows))
	}

	// The next write continues from the raised revision.
	if _, err := h.svc.UpdateConfig(context.Background(), UpdateRequest{Instance: "http-1", ExpectedRevision: 3,
		Values: []KeyValue{{"limit", 11}}, Actor: alice}); err != nil {
		t.Fatalf("write after raise: %v", err)
	}
	h.svc.Wait()
	if row := h.reg.row("http-1"); row.AppliedRevision != 4 {
		t.Errorf("applied after write = %d, want 4", row.AppliedRevision)
	}
}

func TestManagementConfigFor_RaisesPastAppliedRevision(t *testing.T) {
	h := newHarness()
	m := testManifest().ManagementManifest
	h.configs.put("http-1", 1, map[string]any{"limit": float64(9)})
	ctx := context.Background()

	cfg, err := h.svc.ManagementConfigFor(ctx, "http", "http-1", &m, 5)
	if err != nil || cfg.Revision != 6 || cfg.Values["limit"] != float64(9) {
		t.Fatalf("register config = %+v, %v; want revision 6 with values kept", cfg, err)
	}
	// Not ahead: served as stored, no raise.
	raises := h.configs.raises
	if cfg, err = h.svc.ManagementConfigFor(ctx, "http", "http-1", &m, 0); err != nil || cfg.Revision != 6 || h.configs.raises != raises {
		t.Errorf("register config = %+v, %v (raises %d)", cfg, err, h.configs.raises-raises)
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

// TestHeartbeatApplied_BacksOffWhileRefused: a module that keeps refusing its
// config is re-pushed after DriftCheckInterval, then 2×, 4×… — not on every
// heartbeat — and a new config write resets the back-off.
func TestHeartbeatApplied_BacksOffWhileRefused(t *testing.T) {
	behind := registered("http-1")
	behind.AppliedRevision = 1
	h := newHarness(behind)
	h.configs.put("http-1", 3, map[string]any{"limit": float64(9)})
	h.rpc.handler = func(rpcCall) (messaging.ReplyEnvelope, error) {
		return errReply(protocol.CodeValidationFailed, "refused"), nil
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.svc.now = func() time.Time { return now }
	interval := h.svc.cfg.DriftCheckInterval
	pushes := func() int { return len(h.rpc.callsOf(protocol.TypeMgmtConfigApply)) }
	beat := func(after time.Duration) int {
		now = now.Add(after)
		h.svc.HeartbeatApplied("http-1", 1)
		h.svc.Wait()
		return pushes()
	}

	if n := beat(0); n != 1 {
		t.Fatalf("first drift check: pushes = %d, want 1", n)
	}
	if n := beat(interval); n != 1 {
		t.Errorf("one interval after a refusal: pushes = %d, want still 1 (backing off 2×)", n)
	}
	if n := beat(interval); n != 2 {
		t.Errorf("2× after the refusal: pushes = %d, want 2", n)
	}
	if n := beat(3 * interval); n != 2 {
		t.Errorf("3× after the second refusal: pushes = %d, want still 2 (4×)", n)
	}

	// A config write resets the back-off: its own push, then prompt checks.
	h.svc.RequestPush("http-1", alice)
	h.svc.Wait()
	base := pushes()
	if n := beat(interval); n != base+1 {
		t.Errorf("after a config write: pushes = %d, want %d", n, base+1)
	}
}
