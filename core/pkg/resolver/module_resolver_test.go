package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

// fakeModuleRepo is an in-memory IModuleRegistryRepository. Only List and
// FindByInstance are exercised by the resolver; the rest satisfy the interface.
type fakeModuleRepo struct {
	rows       []models.Module
	lastStatus []string // captured arg from the last List call
	findErr    error
}

func (f *fakeModuleRepo) List(_ context.Context, statuses []string) ([]models.Module, error) {
	f.lastStatus = statuses
	if len(statuses) == 0 {
		return f.rows, nil
	}
	want := map[string]struct{}{}
	for _, s := range statuses {
		want[s] = struct{}{}
	}
	var out []models.Module
	for _, m := range f.rows {
		if _, ok := want[m.Status]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeModuleRepo) Block(context.Context, string, string, models.ModuleActor, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeModuleRepo) Unblock(context.Context, string, time.Time) (bool, error) { return false, nil }
func (f *fakeModuleRepo) Delete(context.Context, string) (bool, error)             { return false, nil }

func (f *fakeModuleRepo) FindByInstance(_ context.Context, instance string) (models.Module, error) {
	if f.findErr != nil {
		return models.Module{}, f.findErr
	}
	for _, m := range f.rows {
		if m.Instance == instance {
			return m, nil
		}
	}
	return models.Module{}, qmgo.ErrNoSuchDocuments
}

func (f *fakeModuleRepo) Upsert(context.Context, *models.Module) error { return nil }
func (f *fakeModuleRepo) TouchHeartbeat(context.Context, string, string, map[string]any, *int64, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeModuleRepo) RecordStatus(context.Context, string, map[string]any) error { return nil }
func (f *fakeModuleRepo) MarkDeregistered(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeModuleRepo) FindStaleRegistered(context.Context, time.Time, int64) ([]models.Module, error) {
	return nil, nil
}
func (f *fakeModuleRepo) MarkDead(context.Context, []string, time.Time) error { return nil }
func (f *fakeModuleRepo) ListActive(context.Context) ([]models.Module, error) { return nil, nil }

// fakeAdmin records an operator's lifecycle changes and returns a scripted
// (ok, err).
type fakeAdmin struct {
	ok     bool
	err    error
	calls  []string // "block http-1", "unblock http-1", "delete http-1"
	reason string
	by     models.ModuleActor
	actor  eventbus.Actor
}

func (f *fakeAdmin) Block(_ context.Context, instance, reason string, by models.ModuleActor, actor eventbus.Actor) (bool, error) {
	f.calls = append(f.calls, "block "+instance)
	f.reason, f.by, f.actor = reason, by, actor
	return f.ok, f.err
}

func (f *fakeAdmin) Unblock(_ context.Context, instance string, actor eventbus.Actor) (bool, error) {
	f.calls = append(f.calls, "unblock "+instance)
	f.actor = actor
	return f.ok, f.err
}

func (f *fakeAdmin) Delete(_ context.Context, instance string, actor eventbus.Actor) (bool, error) {
	f.calls = append(f.calls, "delete "+instance)
	f.actor = actor
	return f.ok, f.err
}

func TestModules_PassesStatusFilterThrough(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{
		{Instance: "http-1", Status: models.ModuleStatusRegistered},
		{Instance: "tg-1", Status: models.ModuleStatusDead},
	}}
	r := NewModuleResolver(repo, &fakeAdmin{}, nil, nil)

	out, err := r.Modules(context.Background(), []string{models.ModuleStatusRegistered})
	if err != nil {
		t.Fatalf("Modules error: %v", err)
	}
	if len(out) != 1 || out[0].Instance != "http-1" {
		t.Fatalf("filtered list = %+v, want [http-1]", out)
	}
	if len(repo.lastStatus) != 1 || repo.lastStatus[0] != models.ModuleStatusRegistered {
		t.Errorf("status filter passed to repo = %v, want [registered]", repo.lastStatus)
	}
}

func TestModules_NilFilterReturnsAll(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{
		{Instance: "http-1", Status: models.ModuleStatusRegistered},
		{Instance: "tg-1", Status: models.ModuleStatusDead},
	}}
	r := NewModuleResolver(repo, &fakeAdmin{}, nil, nil)

	out, err := r.Modules(context.Background(), nil)
	if err != nil {
		t.Fatalf("Modules error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("unfiltered list len = %d, want 2", len(out))
	}
}

func TestBlockModule(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{
		{Instance: "http-1", Type: "channel", Status: models.ModuleStatusRegistered},
		{Instance: "http-2", Type: "channel", Status: models.ModuleStatusBlocked},
	}}
	admin := &fakeAdmin{ok: true}
	r := NewModuleResolver(repo, admin, nil, nil)
	ctx := userCtx("admin")

	reason := "  not one of ours  "
	if _, err := r.BlockModule(ctx, " http-1 ", &reason); err != nil {
		t.Fatalf("BlockModule: %v", err)
	}
	// The reason is trimmed, and the block is the calling user's.
	if len(admin.calls) != 1 || admin.calls[0] != "block http-1" || admin.reason != "not one of ours" ||
		admin.by.ID != "u-1" || admin.by.Name != "alice" || admin.actor.Type != eventbus.ActorUser {
		t.Errorf("admin = %+v", admin)
	}

	long := strings.Repeat("x", maxBlockReason+1)
	for name, c := range map[string]struct {
		instance string
		reason   *string
		code     string
	}{
		"blank instance":  {"  ", nil, errCodeBadUserInput},
		"unknown":         {"ghost", nil, errCodeNotFound},
		"blocked already": {"http-2", nil, errCodeConflict},
		"long reason":     {"http-1", &long, errCodeBadUserInput},
	} {
		if _, err := r.BlockModule(ctx, c.instance, c.reason); gqlCode(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
	if len(admin.calls) != 1 {
		t.Errorf("admin called for refused blocks: %v", admin.calls)
	}
}

func TestBlockModuleReasonCountsCharacters(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{{Instance: "http-1", Type: "channel", Status: models.ModuleStatusRegistered}}}
	r := NewModuleResolver(repo, &fakeAdmin{ok: true}, nil, nil)
	// Two bytes each: a byte count would refuse the first.
	fits, over := strings.Repeat("é", maxBlockReason), strings.Repeat("é", maxBlockReason+1)
	if _, err := r.BlockModule(userCtx("admin"), "http-1", &fits); err != nil {
		t.Errorf("a reason of %d characters: %v", maxBlockReason, err)
	}
	if _, err := r.BlockModule(userCtx("admin"), "http-1", &over); gqlCode(err) != errCodeBadUserInput {
		t.Errorf("a reason of %d characters: %v", maxBlockReason+1, err)
	}
}

func TestUnblockModule(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{
		{Instance: "http-1", Status: models.ModuleStatusRegistered},
		{Instance: "http-2", Status: models.ModuleStatusBlocked},
	}}
	admin := &fakeAdmin{ok: true}
	r := NewModuleResolver(repo, admin, nil, nil)
	if _, err := r.UnblockModule(userCtx("admin"), "http-2"); err != nil || len(admin.calls) != 1 || admin.calls[0] != "unblock http-2" {
		t.Fatalf("UnblockModule: %v, calls %v", err, admin.calls)
	}
	if _, err := r.UnblockModule(userCtx("admin"), "http-1"); gqlCode(err) != errCodeConflict {
		t.Errorf("unblocking a module that is not blocked: %v", err)
	}
}

func TestDeleteModule(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{
		{Instance: "live", Status: models.ModuleStatusRegistered},
		{Instance: "blocked", Status: models.ModuleStatusBlocked},
		{Instance: "gone", Status: models.ModuleStatusDeregistered},
		{Instance: "dead", Status: models.ModuleStatusDead},
	}}
	admin := &fakeAdmin{ok: true}
	r := NewModuleResolver(repo, admin, nil, nil)
	ctx := userCtx("admin")
	for _, instance := range []string{"gone", "dead"} {
		if ok, err := r.DeleteModule(ctx, instance); !ok || err != nil {
			t.Errorf("DeleteModule(%s): %v %v", instance, ok, err)
		}
	}
	for _, instance := range []string{"live", "blocked"} {
		if _, err := r.DeleteModule(ctx, instance); gqlCode(err) != errCodeConflict {
			t.Errorf("DeleteModule(%s): %v, want a conflict", instance, err)
		}
	}
	if len(admin.calls) != 2 {
		t.Errorf("calls = %v", admin.calls)
	}
	// It registered again between the read and the delete.
	admin.ok = false
	if _, err := r.DeleteModule(ctx, "gone"); gqlCode(err) != errCodeConflict {
		t.Errorf("a raced delete: %v", err)
	}
	admin.err = errors.New("mongo down")
	if _, err := r.DeleteModule(ctx, "gone"); err == nil {
		t.Error("the admin's error was dropped")
	}
}

// agentCtx is a delegated agent key's context, whatever its owner's role.
func agentCtx() context.Context {
	return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{
		UserID: "u-1", Roles: []string{"admin"}, Agent: &gqlctx.AgentInfo{AgentKeyID: "a-1"},
	})
}

func TestModuleAdminRefusesAgentKeys(t *testing.T) {
	repo := &fakeModuleRepo{rows: []models.Module{{Instance: "gone", Status: models.ModuleStatusDeregistered}}}
	admin := &fakeAdmin{ok: true}
	r := NewModuleResolver(repo, admin, nil, nil)
	ctx := agentCtx()
	_, errBlock := r.BlockModule(ctx, "gone", nil)
	_, errUnblock := r.UnblockModule(ctx, "gone")
	_, errDelete := r.DeleteModule(ctx, "gone")
	for _, err := range []error{errBlock, errUnblock, errDelete} {
		if gqlCode(err) != errCodeForbidden {
			t.Errorf("an agent key: %v", err)
		}
	}
	if len(admin.calls) != 0 {
		t.Errorf("calls = %v", admin.calls)
	}
}

func TestModuleFieldResolvers_FormatTimestamps(t *testing.T) {
	r := NewModuleResolver(&fakeModuleRepo{}, &fakeAdmin{}, nil, nil)
	hb := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	mod := &models.Module{
		RegisteredAt:    time.Date(2026, 6, 27, 11, 0, 0, 0, time.UTC),
		LastHeartbeatAt: &hb,
	}

	reg, err := r.RegisteredAt(context.Background(), mod)
	if err != nil || reg != "2026-06-27T11:00:00Z" {
		t.Errorf("RegisteredAt = %q (err %v), want RFC3339", reg, err)
	}
	last, err := r.LastHeartbeatAt(context.Background(), mod)
	if err != nil || last == nil || *last != "2026-06-27T12:00:00Z" {
		t.Errorf("LastHeartbeatAt = %v (err %v)", last, err)
	}
	// Nil timestamps render as null.
	dead, err := r.DeclaredDeadAt(context.Background(), mod)
	if err != nil || dead != nil {
		t.Errorf("DeclaredDeadAt = %v, want nil", dead)
	}
}

func (f *fakeModuleRepo) RecordApplyResult(context.Context, string, protocol.ConfigApplyReply, time.Time) error {
	return nil
}
func (f *fakeModuleRepo) RecordApplyError(context.Context, string, string, time.Time) error {
	return nil
}

func (f *fakeModuleRepo) LatestConnectionSchema(context.Context, string) (*models.ModuleConnectionSchema, error) {
	return nil, nil
}

func (f *fakeModuleRepo) LatestConnectionSchemas(context.Context) (map[string]*models.ModuleConnectionSchema, error) {
	return nil, nil
}

func (f *fakeModuleRepo) LatestBuildSchema(context.Context, string) (*models.ModuleBuildSchema, error) {
	return nil, nil
}

func (f *fakeModuleRepo) LatestIcons(context.Context, string) (map[string]string, error) {
	return nil, nil
}
func (f *fakeModuleRepo) LatestBuildSchemas(context.Context) (map[string]*models.ModuleBuildSchema, error) {
	return nil, nil
}
