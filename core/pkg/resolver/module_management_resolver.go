package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/logoscore/logos-core/core/pkg/auth/permissions"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/modulemgmt"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Module management over GraphQL (module_management.graphql). The business
// logic is modulemgmt.Service; this file maps it to the schema, enforces the
// checks the @hasPermission directive cannot express (agent keys refused,
// module:action:invoke:high for high-risk actions), and keeps secrets out of
// every response.

// ModuleManager is the management surface the resolver needs. Satisfied by
// *modulemgmt.Service.
type ModuleManager interface {
	Effective(ctx context.Context, mod models.Module) (modulemgmt.EffectiveConfig, error)
	UpdateConfig(ctx context.Context, req modulemgmt.UpdateRequest) (*models.Module, error)
	InvokeAction(ctx context.Context, req modulemgmt.InvokeRequest) (*models.ModuleActionInvocation, error)
	ConfigChanges(ctx context.Context, instance string, limit int64) ([]models.ModuleConfigChange, error)
	Invocations(ctx context.Context, instance string, limit int64) ([]models.ModuleActionInvocation, error)
}

// List limits for the history queries.
const (
	defaultModuleHistoryLimit = 20
	maxModuleHistoryLimit     = 100
)

// Error codes in extensions.code.
const (
	errCodeConflict     = "CONFLICT"
	errCodeForbidden    = "FORBIDDEN"
	errCodeNotFound     = "NOT_FOUND"
	errCodeBadUserInput = "BAD_USER_INPUT"
)

func moduleGQLError(ctx context.Context, code, msg string) error {
	return &gqlerror.Error{
		Message:    msg,
		Path:       graphql.GetPath(ctx),
		Extensions: map[string]interface{}{"code": code},
	}
}

// mapManagementError turns service errors into coded GraphQL errors.
func mapManagementError(ctx context.Context, err error) error {
	var conflict *modulemgmt.ConflictError
	var invalid *modulemgmt.ValidationError
	switch {
	case errors.As(err, &conflict):
		return moduleGQLError(ctx, errCodeConflict, conflict.Error())
	case errors.As(err, &invalid):
		return moduleGQLError(ctx, errCodeBadUserInput, invalid.Message)
	case errors.Is(err, modulemgmt.ErrModuleNotFound):
		return moduleGQLError(ctx, errCodeNotFound, "module not found")
	case errors.Is(err, modulemgmt.ErrForbidden):
		return moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	return err
}

// managementActor is the caller as a management actor. Agent keys never get
// here (refused first), so the caller is a user.
func managementActor(auth gqlctx.AuthInfo) models.ModuleActor {
	return models.ModuleActor{Type: "user", ID: auth.UserID, Name: auth.Username}
}

// refuseAgent rejects delegated agent keys: changing a module's config or
// running its actions is operator-only, whatever the owner's role.
func refuseAgent(ctx context.Context) (gqlctx.AuthInfo, error) {
	auth := gqlctx.AuthFromContext(ctx)
	if auth.Agent != nil {
		return auth, moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	if auth.UserID == "" {
		return auth, moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	return auth, nil
}

func historyLimit(limit *int) int64 {
	if limit == nil || *limit <= 0 {
		return defaultModuleHistoryLimit
	}
	if *limit > maxModuleHistoryLimit {
		return maxModuleHistoryLimit
	}
	return int64(*limit)
}

// --- queries ---

// Module returns one module row, or null when the instance is unknown.
func (r *moduleResolver) Module(ctx context.Context, instance string) (*models.Module, error) {
	mod, err := r.repo.FindByInstance(ctx, strings.TrimSpace(instance))
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load module: %w", err)
	}
	return &mod, nil
}

func (r *moduleResolver) ModuleActionInvocations(ctx context.Context, instance string, limit *int) ([]*model.ModuleActionInvocation, error) {
	if r.mgmt == nil {
		return []*model.ModuleActionInvocation{}, nil
	}
	rows, err := r.mgmt.Invocations(ctx, strings.TrimSpace(instance), historyLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list module action invocations: %w", err)
	}
	out := make([]*model.ModuleActionInvocation, len(rows))
	for i := range rows {
		out[i] = toInvocationModel(&rows[i])
	}
	return out, nil
}

func (r *moduleResolver) ModuleConfigChanges(ctx context.Context, instance string, limit *int) ([]*model.ModuleConfigChange, error) {
	if r.mgmt == nil {
		return []*model.ModuleConfigChange{}, nil
	}
	rows, err := r.mgmt.ConfigChanges(ctx, strings.TrimSpace(instance), historyLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list module config changes: %w", err)
	}
	out := make([]*model.ModuleConfigChange, len(rows))
	for i := range rows {
		out[i] = toConfigChangeModel(&rows[i])
	}
	return out, nil
}

// Management is the Module.management field: null without a manifest.
func (r *moduleResolver) Management(ctx context.Context, obj *models.Module) (*model.ModuleManagement, error) {
	if obj == nil || obj.Manifest == nil || r.mgmt == nil {
		return nil, nil
	}
	eff, err := r.mgmt.Effective(ctx, *obj)
	if err != nil {
		return nil, fmt.Errorf("failed to load module config: %w", err)
	}
	return buildManagement(obj, eff), nil
}

// buildManagement renders the management view. Secret values never leave
// here: effective/value/default are null for secrets, and isSet says whether
// one is stored.
func buildManagement(mod *models.Module, eff modulemgmt.EffectiveConfig) *model.ModuleManagement {
	m := mod.Manifest.ManagementManifest
	restart := make(map[string]bool, len(mod.RestartRequired))
	for _, k := range mod.RestartRequired {
		restart[k] = true
	}

	props := make([]*model.ModuleProperty, 0, len(eff.Properties))
	for _, p := range eff.Properties {
		secret := p.Spec.Type == protocol.FieldSecret
		mp := &model.ModuleProperty{
			Field:           toFieldModel(p.Spec.FieldSpec),
			Apply:           string(p.Spec.Apply),
			Source:          p.Source,
			IsSet:           p.IsSet,
			RestartRequired: restart[p.Spec.Key],
		}
		if !secret {
			mp.Effective = p.Effective
			mp.Value = p.Value
		}
		if reason, ok := mod.Rejected[p.Spec.Key]; ok {
			mp.RejectedReason = &reason
		}
		props = append(props, mp)
	}

	actions := make([]*model.ModuleAction, 0, len(m.Actions))
	for _, a := range m.Actions {
		timeout := a.TimeoutSeconds
		if timeout <= 0 {
			timeout = int(modulemgmt.DefaultActionTimeout / time.Second)
		}
		params := make([]*model.ModuleField, 0, len(a.Params))
		for _, f := range a.Params {
			params = append(params, toFieldModel(f))
		}
		actions = append(actions, &model.ModuleAction{
			Name:           a.Name,
			Label:          orKey(a.Label, a.Name),
			Description:    strPtr(a.Description),
			Risk:           string(a.Risk),
			Confirm:        strPtr(a.Confirm),
			Params:         params,
			TimeoutSeconds: timeout,
		})
	}

	status := make([]*model.ModuleStatusValue, 0, len(m.Status))
	for _, st := range m.Status {
		var v any
		if raw, ok := mod.LastMetrics[st.Key]; ok {
			v = plainJSON(raw)
		}
		status = append(status, &model.ModuleStatusValue{
			Key:         st.Key,
			Type:        string(st.Type),
			Label:       orKey(st.Label, st.Key),
			Description: strPtr(st.Description),
			Unit:        strPtr(st.Unit),
			Value:       v,
			Alert:       st.Alert,
		})
	}

	return &model.ModuleManagement{
		SchemaVersion:   m.SchemaVersion,
		Properties:      props,
		Actions:         actions,
		Status:          status,
		Capabilities:    append([]string{}, m.Capabilities...),
		Revision:        int(eff.Revision),
		AppliedRevision: int(mod.AppliedRevision),
		SyncState:       model.ModuleSyncState(modulemgmt.SyncState(*mod, eff.Revision)),
		LastApplyError:  strPtr(mod.LastApplyError),
	}
}

func toFieldModel(f protocol.FieldSpec) *model.ModuleField {
	out := &model.ModuleField{
		Key:         f.Key,
		Type:        string(f.Type),
		Label:       orKey(f.Label, f.Key),
		Description: strPtr(f.Description),
		Group:       strPtr(f.Group),
		Order:       f.Order,
		Unit:        strPtr(f.Unit),
		Placeholder: strPtr(f.Placeholder),
		Format:      strPtr(f.Format),
		Required:    f.Required,
		Min:         f.Min,
		Max:         f.Max,
		Pattern:     strPtr(f.Pattern),
	}
	if f.Type != protocol.FieldSecret {
		out.Default = f.Default
	}
	if f.MaxLength > 0 {
		ml := f.MaxLength
		out.MaxLength = &ml
	}
	out.KeyPattern = strPtr(f.KeyPattern)
	if f.MaxItems > 0 {
		mi := f.MaxItems
		out.MaxItems = &mi
	}
	if len(f.Accept) > 0 {
		out.Accept = f.Accept
	}
	if f.MaxSize > 0 {
		out.MaxSize = f.MaxSize
	}
	if len(f.Options) > 0 {
		out.Options = make([]*model.ModuleEnumOption, len(f.Options))
		for i, o := range f.Options {
			out.Options[i] = &model.ModuleEnumOption{Value: o.Value, Label: strPtr(o.Label), Description: strPtr(o.Description)}
		}
	}
	return out
}

func toActorModel(a models.ModuleActor) *model.ModuleActor {
	return &model.ModuleActor{Type: a.Type, ID: a.ID, Name: strPtr(a.Name)}
}

func toInvocationModel(inv *models.ModuleActionInvocation) *model.ModuleActionInvocation {
	out := &model.ModuleActionInvocation{
		ID:         inv.InvocationID,
		Instance:   inv.Instance,
		Action:     inv.Action,
		Status:     model.ModuleActionStatus(inv.Status),
		Result:     inv.Result(),
		Error:      strPtr(inv.Error),
		Actor:      toActorModel(inv.Actor),
		StartedAt:  inv.StartedAt.Format(time.RFC3339),
		FinishedAt: rfc3339Ptr(inv.FinishedAt),
	}
	if len(inv.Params) > 0 {
		out.Params = map[string]any(inv.Params)
	}
	return out
}

func toConfigChangeModel(c *models.ModuleConfigChange) *model.ModuleConfigChange {
	changes := make([]*model.ModuleConfigKeyChange, len(c.Changes))
	for i, k := range c.Changes {
		changes[i] = &model.ModuleConfigKeyChange{Key: k.Key, Old: k.Old, New: k.New}
	}
	return &model.ModuleConfigChange{
		ID:       c.Id.Hex(),
		Instance: c.Instance,
		Revision: int(c.Revision),
		Changes:  changes,
		Actor:    toActorModel(c.Actor),
		At:       c.At.Format(time.RFC3339),
	}
}

// --- mutations ---

func (r *moduleResolver) UpdateModuleConfig(ctx context.Context, instance string, values []*model.ModuleConfigValueInput, expectedRevision int) (*models.Module, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	if r.mgmt == nil {
		return nil, fmt.Errorf("module management is not available")
	}
	kvs := make([]modulemgmt.KeyValue, 0, len(values))
	for _, v := range values {
		if v == nil {
			continue
		}
		kvs = append(kvs, modulemgmt.KeyValue{Key: v.Key, Value: v.Value})
	}
	mod, err := r.mgmt.UpdateConfig(ctx, modulemgmt.UpdateRequest{
		Instance:         strings.TrimSpace(instance),
		Values:           kvs,
		ExpectedRevision: int64(expectedRevision),
		Actor:            managementActor(auth),
	})
	if err != nil {
		return nil, mapManagementError(ctx, err)
	}
	return mod, nil
}

func (r *moduleResolver) InvokeModuleAction(ctx context.Context, instance string, action string, params any) (*model.ModuleActionInvocation, error) {
	auth, err := refuseAgent(ctx)
	if err != nil {
		return nil, err
	}
	if r.mgmt == nil {
		return nil, fmt.Errorf("module management is not available")
	}
	inv, err := r.mgmt.InvokeAction(ctx, modulemgmt.InvokeRequest{
		Instance:      strings.TrimSpace(instance),
		Action:        action,
		Params:        params,
		Actor:         managementActor(auth),
		AllowHighRisk: permissions.HasPermissionForRoles(auth.Roles, permissions.ModuleActionInvokeHighPermission),
	})
	if err != nil {
		return nil, mapManagementError(ctx, err)
	}
	return toInvocationModel(inv), nil
}

// --- helpers ---

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func orKey(label, key string) string {
	if label != "" {
		return label
	}
	return key
}

// plainJSON converts values decoded by the driver's default codecs (heartbeat
// metrics are stored as a plain map) into JSON-native shapes, so the JSON
// scalar renders a document as an object rather than a list of key/value pairs.
func plainJSON(v any) any {
	switch t := v.(type) {
	case primitive.D:
		m := make(map[string]any, len(t))
		for _, e := range t {
			m[e.Key] = plainJSON(e.Value)
		}
		return m
	case bson.M:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = plainJSON(e)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = plainJSON(e)
		}
		return m
	case primitive.A:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = plainJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = plainJSON(e)
		}
		return out
	case primitive.DateTime:
		return t.Time().UTC().Format(time.RFC3339)
	}
	return v
}

// Connection is the Module.connection field (channel_endpoints.graphql).
func (r *moduleResolver) Connection(_ context.Context, obj *models.Module) (*model.ModuleConnectionSchema, error) {
	if obj.Connection == nil {
		return nil, nil
	}
	return toConnectionModel(&obj.Connection.ConnectionSchema), nil
}
