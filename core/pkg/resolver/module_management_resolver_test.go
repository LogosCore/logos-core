package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/modulemgmt"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type fakeManager struct {
	eff       modulemgmt.EffectiveConfig
	updateErr error
	updates   []modulemgmt.UpdateRequest
	invokes   []modulemgmt.InvokeRequest
	invokeErr error
}

func (f *fakeManager) Effective(context.Context, models.Module) (modulemgmt.EffectiveConfig, error) {
	return f.eff, nil
}

func (f *fakeManager) UpdateConfig(_ context.Context, req modulemgmt.UpdateRequest) (*models.Module, error) {
	f.updates = append(f.updates, req)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &models.Module{Instance: req.Instance}, nil
}

func (f *fakeManager) InvokeAction(_ context.Context, req modulemgmt.InvokeRequest) (*models.ModuleActionInvocation, error) {
	f.invokes = append(f.invokes, req)
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return &models.ModuleActionInvocation{InvocationID: "inv-1", Instance: req.Instance, Action: req.Action,
		Status: models.ModuleActionTimedOut, Error: "no reply within 30s", Actor: req.Actor}, nil
}

func (f *fakeManager) ConfigChanges(context.Context, models.Module, int64) ([]models.ModuleConfigChange, error) {
	return nil, nil
}

func (f *fakeManager) Invocations(context.Context, string, int64) ([]models.ModuleActionInvocation, error) {
	return nil, nil
}

func userCtx(roles ...string) context.Context {
	return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: "u-1", Username: "alice", Roles: roles})
}

func gqlCode(err error) string {
	var ge *gqlerror.Error
	if errors.As(err, &ge) {
		code, _ := ge.Extensions["code"].(string)
		return code
	}
	return ""
}

func TestUpdateModuleConfig_RejectsAgentKeys(t *testing.T) {
	mgr := &fakeManager{}
	r := NewModuleResolver(&fakeModuleRepo{}, &fakeDeregistrar{}, mgr)
	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{
		UserID: "u-1", Roles: []string{"admin"}, Agent: &gqlctx.AgentInfo{AgentKeyID: "a-1"},
	})
	_, err := r.UpdateModuleConfig(ctx, "http-1", model.ModuleConfigScopeModule, nil, 0)
	if gqlCode(err) != "FORBIDDEN" {
		t.Errorf("err = %v, want FORBIDDEN", err)
	}
	_, err = r.InvokeModuleAction(ctx, "http-1", "flush", nil)
	if gqlCode(err) != "FORBIDDEN" {
		t.Errorf("invoke err = %v, want FORBIDDEN", err)
	}
	if len(mgr.updates)+len(mgr.invokes) != 0 {
		t.Error("agent requests must not reach the service")
	}
}

func TestUpdateModuleConfig_MapsErrorsAndPassesActor(t *testing.T) {
	mgr := &fakeManager{}
	r := NewModuleResolver(&fakeModuleRepo{}, &fakeDeregistrar{}, mgr)

	mod, err := r.UpdateModuleConfig(userCtx("admin"), "http-1", model.ModuleConfigScopeInstance,
		[]*model.ModuleConfigValueInput{{Key: "limit", Value: json.Number("5")}, {Key: "mode", Value: nil}}, 3)
	if err != nil || mod.Instance != "http-1" {
		t.Fatalf("UpdateModuleConfig = %+v, %v", mod, err)
	}
	req := mgr.updates[0]
	if req.Scope != models.ModuleConfigScopeInstance || req.ExpectedRevision != 3 || len(req.Values) != 2 ||
		req.Actor != (models.ModuleActor{Type: "user", ID: "u-1", Name: "alice"}) {
		t.Errorf("request = %+v", req)
	}

	for err, code := range map[error]string{
		&modulemgmt.ConflictError{Scope: "module", CurrentRevision: 4}: "CONFLICT",
		&modulemgmt.ValidationError{Message: "bad"}:                    "BAD_USER_INPUT",
		modulemgmt.ErrModuleNotFound:                                   "NOT_FOUND",
	} {
		mgr.updateErr = err
		if _, got := r.UpdateModuleConfig(userCtx("admin"), "http-1", model.ModuleConfigScopeModule,
			[]*model.ModuleConfigValueInput{{Key: "limit", Value: 1}}, 0); gqlCode(got) != code {
			t.Errorf("%v → %v, want code %s", err, got, code)
		}
	}
}

func TestInvokeModuleAction_HighRiskPermissionAndNormalFailure(t *testing.T) {
	mgr := &fakeManager{}
	r := NewModuleResolver(&fakeModuleRepo{}, &fakeDeregistrar{}, mgr)

	inv, err := r.InvokeModuleAction(userCtx("user"), "http-1", "wipe", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("a timed-out action is a normal return, got %v", err)
	}
	if inv.Status != model.ModuleActionStatusTimedOut || inv.Error == nil || inv.Actor.ID != "u-1" {
		t.Errorf("inv = %+v", inv)
	}
	if mgr.invokes[0].AllowHighRisk {
		t.Error("role user must not get module:action:invoke:high")
	}
	if _, err := r.InvokeModuleAction(userCtx("admin"), "http-1", "wipe", nil); err != nil {
		t.Fatal(err)
	}
	if !mgr.invokes[1].AllowHighRisk {
		t.Error("admin must get module:action:invoke:high")
	}

	mgr.invokeErr = modulemgmt.ErrForbidden
	if _, err := r.InvokeModuleAction(userCtx("user"), "http-1", "wipe", nil); gqlCode(err) != "FORBIDDEN" {
		t.Errorf("err = %v, want FORBIDDEN", err)
	}
}

func TestManagementField_NullWithoutManifestAndHidesSecrets(t *testing.T) {
	m := protocol.ManagementManifest{
		SchemaVersion: "1",
		Properties: []protocol.PropertySpec{
			{FieldSpec: protocol.FieldSpec{Key: "token", Type: protocol.FieldSecret, Default: "dflt"}, Apply: protocol.ApplyLive},
			{FieldSpec: protocol.FieldSpec{Key: "limit", Type: protocol.FieldInt, Label: "Limit", Default: float64(10)}, Apply: protocol.ApplyRestart},
		},
		Actions: []protocol.ActionSpec{{Name: "flush", Risk: protocol.RiskLow}},
		Status:  []protocol.StatusSpec{{Key: "depth", Type: protocol.FieldInt}, {Key: "never", Type: protocol.FieldInt}},
	}
	mod := &models.Module{
		Instance: "http-1", Name: "http", Status: models.ModuleStatusRegistered,
		Manifest:        &models.ModuleManifest{ManagementManifest: m},
		AppliedRevision: 2, Rejected: map[string]string{"limit": "too big"}, RestartRequired: []string{"limit"},
		LastMetrics: map[string]any{"depth": int32(4)},
	}
	eff := modulemgmt.ComputeEffective(&m,
		&models.ModuleConfig{Revision: 2, Values: models.JSONMap{"token": "s3cret"}},
		&models.ModuleConfig{Revision: 1, Values: models.JSONMap{"limit": float64(50)}})
	r := NewModuleResolver(&fakeModuleRepo{}, &fakeDeregistrar{}, &fakeManager{eff: eff})

	if got, err := r.Management(context.Background(), &models.Module{Instance: "bare"}); got != nil || err != nil {
		t.Errorf("no manifest → %+v, %v; want null", got, err)
	}

	got, err := r.Management(context.Background(), mod)
	if err != nil || got == nil {
		t.Fatalf("Management = %+v, %v", got, err)
	}
	if got.Revision != 3 || got.ModuleRevision != 2 || got.InstanceRevision != 1 || got.AppliedRevision != 2 ||
		got.SyncState != model.ModuleSyncStatePending {
		t.Errorf("revisions/sync = %d %d %d %d %s", got.Revision, got.ModuleRevision, got.InstanceRevision, got.AppliedRevision, got.SyncState)
	}
	token, limit := got.Properties[0], got.Properties[1]
	if token.Effective != nil || token.ModuleValue != nil || token.InstanceValue != nil || token.Field.Default != nil {
		t.Errorf("secret leaked: %+v / default %v", token, token.Field.Default)
	}
	if !token.IsSet || token.Source != modulemgmt.SourceModule || token.Field.Label != "token" {
		t.Errorf("token = %+v", token)
	}
	if limit.Effective != float64(50) || limit.Source != modulemgmt.SourceInstance || !limit.RestartRequired ||
		limit.RejectedReason == nil || *limit.RejectedReason != "too big" || limit.Field.Label != "Limit" {
		t.Errorf("limit = %+v", limit)
	}
	if got.Actions[0].TimeoutSeconds != 30 || got.Actions[0].Label != "flush" {
		t.Errorf("action = %+v", got.Actions[0])
	}
	if got.Status[0].Value != int32(4) || got.Status[1].Value != nil {
		t.Errorf("status = %+v %+v", got.Status[0], got.Status[1])
	}
}
