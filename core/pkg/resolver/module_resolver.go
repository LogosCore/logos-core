package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/qiniu/qmgo"
)

// ModuleAdmin performs an operator's lifecycle changes on an instance: each
// with the registry update, the data-plane gate-cache bust, the AMQP audit
// event and the in-process bus event. Each returns false when the instance is
// not in a state the change applies to. Satisfied by *lifecycle.Service. Kept
// as a narrow interface so the resolver does not import the whole
// control-plane package and stays unit-testable with a fake.
type ModuleAdmin interface {
	Block(ctx context.Context, instance, reason string, by models.ModuleActor, actor eventbus.Actor) (bool, error)
	Unblock(ctx context.Context, instance string, actor eventbus.Actor) (bool, error)
	Delete(ctx context.Context, instance string, actor eventbus.Actor) (bool, error)
}

// IModuleResolver is the business logic for the Module entity — the app-admin
// view over the module-instance registry. Authorization is enforced upstream by
// the @hasPermission(module:read|module:delete) directive, so these methods do
// no further permission checks.
type IModuleResolver interface {
	// Queries
	Modules(ctx context.Context, status []string) ([]*models.Module, error)
	Module(ctx context.Context, instance string) (*models.Module, error)
	ModuleActionInvocations(ctx context.Context, instance string, limit *int) ([]*model.ModuleActionInvocation, error)
	ModuleConfigChanges(ctx context.Context, instance string, limit *int) ([]*model.ModuleConfigChange, error)

	// Mutations
	BlockModule(ctx context.Context, instance string, reason *string) (*models.Module, error)
	UnblockModule(ctx context.Context, instance string) (*models.Module, error)
	DeleteModule(ctx context.Context, instance string) (bool, error)
	UpdateModuleConfig(ctx context.Context, instance string, values []*model.ModuleConfigValueInput, expectedRevision int) (*models.Module, error)
	InvokeModuleAction(ctx context.Context, instance string, action string, params any) (*model.ModuleActionInvocation, error)

	// Subscriptions
	ModuleLogs(ctx context.Context, instance string, minLevel *model.ModuleLogLevel) (<-chan *model.ModuleLogEvent, error)

	// Management is the Module.management field (module_management.graphql).
	Management(ctx context.Context, obj *models.Module) (*model.ModuleManagement, error)
	// Connection is the Module.connection field (channel_endpoints.graphql).
	Connection(ctx context.Context, obj *models.Module) (*model.ModuleConnectionSchema, error)

	// Field resolvers for the Module type (timestamps → RFC3339 / nullable).
	RegisteredAt(ctx context.Context, obj *models.Module) (string, error)
	LastHeartbeatAt(ctx context.Context, obj *models.Module) (*string, error)
	DeregisteredAt(ctx context.Context, obj *models.Module) (*string, error)
	DeclaredDeadAt(ctx context.Context, obj *models.Module) (*string, error)
	BlockedAt(ctx context.Context, obj *models.Module) (*string, error)
	BlockedBy(ctx context.Context, obj *models.Module) (*model.ModuleActor, error)
}

// maxBlockReason bounds the reason an operator gives for a block.
const maxBlockReason = 500

type moduleResolver struct {
	repo  repository.IModuleRegistryRepository
	admin ModuleAdmin
	mgmt  ModuleManager
	logs  ModuleLogStreamer
}

// NewModuleResolver builds the Module resolver. deregistrar is the lifecycle
// service; removeModule routes through it so the GraphQL path and the RPC
// deregister path share one transition. mgmt is the module-management service
// (nil disables management: Module.management is null, mutations error).
// logs is the live log relay (nil disables moduleLogs).
func NewModuleResolver(repo repository.IModuleRegistryRepository, admin ModuleAdmin, mgmt ModuleManager, logs ModuleLogStreamer) IModuleResolver {
	return &moduleResolver{repo: repo, admin: admin, mgmt: mgmt, logs: logs}
}

// Modules lists module instances filtered to the given lifecycle statuses,
// newest registration first. An empty/nil status list returns every row.
func (r *moduleResolver) Modules(ctx context.Context, status []string) ([]*models.Module, error) {
	mods, err := r.repo.List(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list modules: %w", err)
	}
	out := make([]*models.Module, len(mods))
	for i := range mods {
		out[i] = &mods[i]
	}
	return out, nil
}

// RemoveModule soft-removes (deregisters) a module instance and returns the
// updated row. This is not durable eviction: a still-alive module re-registers
// on its next reconnect and reappears as registered — the all-states list makes
// that flip visible. Returns an error when the instance is not currently
// registered (already deregistered/dead, or never seen).
// adminTarget is the instance an operator's lifecycle change names, read
// first so a refusal can say why. Agent keys are refused: like management,
// blocking and deleting modules is operator-only.
func (r *moduleResolver) adminTarget(ctx context.Context, instance string) (gqlctx.AuthInfo, models.Module, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return auth, models.Module{}, err
	}
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return auth, models.Module{}, moduleGQLError(ctx, errCodeBadUserInput, "instance is required")
	}
	mod, err := r.repo.FindByInstance(ctx, instance)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return auth, mod, moduleGQLError(ctx, errCodeNotFound, "module not found")
	}
	if err != nil {
		return auth, mod, fmt.Errorf("failed to load module: %w", err)
	}
	return auth, mod, nil
}

// reread returns an instance's row after a change, for the client's cache.
func (r *moduleResolver) reread(ctx context.Context, instance string) (*models.Module, error) {
	mod, err := r.repo.FindByInstance(ctx, instance)
	if err != nil {
		return nil, fmt.Errorf("failed to load module: %w", err)
	}
	return &mod, nil
}

func (r *moduleResolver) BlockModule(ctx context.Context, instance string, reason *string) (*models.Module, error) {
	auth, mod, err := r.adminTarget(ctx, instance)
	if err != nil {
		return nil, err
	}
	text := ""
	if reason != nil {
		text = strings.TrimSpace(*reason)
	}
	if utf8.RuneCountInString(text) > maxBlockReason {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("the reason is longer than %d characters", maxBlockReason))
	}
	if mod.Status == models.ModuleStatusBlocked {
		return nil, moduleGQLError(ctx, errCodeConflict, "the module is blocked already")
	}
	if _, err := r.admin.Block(ctx, mod.Instance, text, managementActor(auth), eventActor(auth)); err != nil {
		return nil, fmt.Errorf("failed to block module: %w", err)
	}
	return r.reread(ctx, mod.Instance)
}

func (r *moduleResolver) UnblockModule(ctx context.Context, instance string) (*models.Module, error) {
	auth, mod, err := r.adminTarget(ctx, instance)
	if err != nil {
		return nil, err
	}
	if mod.Status != models.ModuleStatusBlocked {
		return nil, moduleGQLError(ctx, errCodeConflict, "the module is not blocked")
	}
	if _, err := r.admin.Unblock(ctx, mod.Instance, eventActor(auth)); err != nil {
		return nil, fmt.Errorf("failed to unblock module: %w", err)
	}
	return r.reread(ctx, mod.Instance)
}

func (r *moduleResolver) DeleteModule(ctx context.Context, instance string) (bool, error) {
	auth, mod, err := r.adminTarget(ctx, instance)
	if err != nil {
		return false, err
	}
	switch mod.Status {
	case models.ModuleStatusRegistered:
		return false, moduleGQLError(ctx, errCodeConflict,
			"the module is registered and would only register again: block it, or stop it first")
	case models.ModuleStatusBlocked:
		return false, moduleGQLError(ctx, errCodeConflict, "the module is blocked: its row is the block, so unblock it first")
	}
	ok, err := r.admin.Delete(ctx, mod.Instance, eventActor(auth))
	if err != nil {
		return false, fmt.Errorf("failed to delete module: %w", err)
	}
	if !ok {
		return false, moduleGQLError(ctx, errCodeConflict, "the module registered or was blocked meanwhile")
	}
	return true, nil
}

// RegisteredAt converts the registration timestamp to an RFC3339 string.
func (r *moduleResolver) RegisteredAt(_ context.Context, obj *models.Module) (string, error) {
	return obj.RegisteredAt.Format(time.RFC3339), nil
}

// LastHeartbeatAt is null until the instance has beaten at least once.
func (r *moduleResolver) LastHeartbeatAt(_ context.Context, obj *models.Module) (*string, error) {
	return rfc3339Ptr(obj.LastHeartbeatAt), nil
}

// DeregisteredAt is null while the instance is registered/dead.
func (r *moduleResolver) DeregisteredAt(_ context.Context, obj *models.Module) (*string, error) {
	return rfc3339Ptr(obj.DeregisteredAt), nil
}

// DeclaredDeadAt is null until the reaper declares the instance dead.
func (r *moduleResolver) DeclaredDeadAt(_ context.Context, obj *models.Module) (*string, error) {
	return rfc3339Ptr(obj.DeclaredDeadAt), nil
}

// BlockedAt is null unless the instance is blocked.
func (r *moduleResolver) BlockedAt(_ context.Context, obj *models.Module) (*string, error) {
	return rfc3339Ptr(obj.BlockedAt), nil
}

// BlockedBy is null unless the instance is blocked.
func (r *moduleResolver) BlockedBy(_ context.Context, obj *models.Module) (*model.ModuleActor, error) {
	if obj.BlockedBy == nil {
		return nil, nil
	}
	return toActorModel(*obj.BlockedBy), nil
}

// rfc3339Ptr formats an optional timestamp as a nullable RFC3339 string.
func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
