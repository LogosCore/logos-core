package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/channelendpoints"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Channel endpoints over GraphQL (channel_endpoints.graphql): an operation's
// ready-to-use minion connection settings. The logic is
// channelendpoints.Service; this resolver authorizes by operation role
// (viewer reads, operator writes; agent keys read only) and maps the results.

// ChannelEndpointManager is the endpoint surface the resolver needs.
// Satisfied by *channelendpoints.Service.
type ChannelEndpointManager interface {
	Kinds(ctx context.Context) ([]channelendpoints.Kind, error)
	List(ctx context.Context, operationID uuid.UUID, filter repository.ChannelEndpointFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]channelendpoints.View, error)
	Count(ctx context.Context, operationID uuid.UUID, filter repository.ChannelEndpointFilter) (int64, error)
	FreeName(ctx context.Context, operationID uuid.UUID, name, tag string) (string, error)
	Get(ctx context.Context, id string) (channelendpoints.View, error)
	Create(ctx context.Context, operationID uuid.UUID, moduleName string, in channelendpoints.Input) (channelendpoints.View, error)
	Update(ctx context.Context, id string, expectedRevision int64, in channelendpoints.Input) (channelendpoints.View, error)
	Delete(ctx context.Context, id string) error
	Export(ctx context.Context, id string) (map[string]any, error)
}

// IChannelEndpointResolver is the business logic behind
// channel_endpoints.graphql.
type IChannelEndpointResolver interface {
	ChannelKinds(ctx context.Context) ([]*model.ChannelKind, error)
	ChannelEndpoints(ctx context.Context, operationID string, search *string, moduleNames []string, first *int, after *string, last *int, before *string) (*model.ChannelEndpointConnection, error)
	FreeChannelEndpointName(ctx context.Context, operationID string, name string, tag *string) (string, error)
	ChannelEndpoint(ctx context.Context, id string) (*model.ChannelEndpoint, error)
	ChannelEndpointExport(ctx context.Context, id string) (any, error)
	CreateChannelEndpoint(ctx context.Context, operationID string, moduleName string, input model.ChannelEndpointInput) (*model.ChannelEndpoint, error)
	UpdateChannelEndpoint(ctx context.Context, id string, input model.ChannelEndpointInput, expectedRevision int) (*model.ChannelEndpoint, error)
	DeleteChannelEndpoint(ctx context.Context, id string) (bool, error)
}

type channelEndpointResolver struct {
	endpoints     ChannelEndpointManager
	operationRepo repository.IOperationRepository
}

func NewChannelEndpointResolver(endpoints ChannelEndpointManager, operationRepo repository.IOperationRepository) IChannelEndpointResolver {
	return &channelEndpointResolver{endpoints: endpoints, operationRepo: operationRepo}
}

// authorize enforces a minimum operation role. Writes pass write=true:
// agent keys may read endpoints but never change them.
func (r *channelEndpointResolver) authorize(ctx context.Context, operationID uuid.UUID, minRole models.OperationRole, write bool) error {
	if write && gqlctx.AuthFromContext(ctx).Agent != nil {
		return moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, operationID)
	if err != nil {
		return fmt.Errorf("operation not found: %w", err)
	}
	return authorization.AuthorizeOperationRole(ctx, &op, minRole)
}

// authorizeEndpoint loads an endpoint and authorizes on its operation.
func (r *channelEndpointResolver) authorizeEndpoint(ctx context.Context, id string, minRole models.OperationRole, write bool) (channelendpoints.View, error) {
	v, err := r.endpoints.Get(ctx, id)
	if err != nil {
		return v, mapEndpointError(ctx, err)
	}
	return v, r.authorize(ctx, v.OperationID, minRole, write)
}

func (r *channelEndpointResolver) ChannelKinds(ctx context.Context) ([]*model.ChannelKind, error) {
	kinds, err := r.endpoints.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.ChannelKind, len(kinds))
	for i, k := range kinds {
		out[i] = &model.ChannelKind{ModuleName: k.ModuleName, Schema: toConnectionModel(&k.Schema), Icon: k.Icon, IconLight: k.IconLight}
	}
	return out, nil
}

func (r *channelEndpointResolver) ChannelEndpoints(ctx context.Context, operationID string, search *string, moduleNames []string, first *int, after *string, last *int, before *string) (*model.ChannelEndpointConnection, error) {
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return nil, fmt.Errorf("invalid operation ID: %w", err)
	}
	if err := r.authorize(ctx, opID, models.OperationRoleViewer, false); err != nil {
		return nil, err
	}
	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, fmt.Errorf("invalid pagination args: %w", err)
	}
	filter := repository.ChannelEndpointFilter{ModuleNames: moduleNames}
	if search != nil {
		filter.Search = strings.TrimSpace(*search)
	}
	total, err := r.endpoints.Count(ctx, opID, filter)
	if err != nil {
		return nil, err
	}
	views, err := r.endpoints.List(ctx, opID, filter, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	edges, pageInfo := pagination.BuildEdges(views, args,
		func(v *channelendpoints.View) string { return repository.ChannelEndpointCursor(&v.ChannelEndpoint) },
		func(v *channelendpoints.View, cursor string) *model.ChannelEndpointEdge {
			return &model.ChannelEndpointEdge{Node: toEndpointModel(*v), Cursor: cursor}
		})
	return &model.ChannelEndpointConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

func (r *channelEndpointResolver) FreeChannelEndpointName(ctx context.Context, operationID string, name string, tag *string) (string, error) {
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return "", fmt.Errorf("invalid operation ID: %w", err)
	}
	if err := r.authorize(ctx, opID, models.OperationRoleViewer, false); err != nil {
		return "", err
	}
	free, err := r.endpoints.FreeName(ctx, opID, name, deref(tag))
	if err != nil {
		return "", mapEndpointError(ctx, err)
	}
	return free, nil
}

func (r *channelEndpointResolver) ChannelEndpoint(ctx context.Context, id string) (*model.ChannelEndpoint, error) {
	v, err := r.endpoints.Get(ctx, id)
	if errors.Is(err, channelendpoints.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.authorize(ctx, v.OperationID, models.OperationRoleViewer, false); err != nil {
		return nil, err
	}
	return toEndpointModel(v), nil
}

func (r *channelEndpointResolver) ChannelEndpointExport(ctx context.Context, id string) (any, error) {
	if _, err := r.authorizeEndpoint(ctx, id, models.OperationRoleViewer, false); err != nil {
		return nil, err
	}
	doc, err := r.endpoints.Export(ctx, id)
	if err != nil {
		return nil, mapEndpointError(ctx, err)
	}
	return doc, nil
}

func (r *channelEndpointResolver) CreateChannelEndpoint(ctx context.Context, operationID string, moduleName string, input model.ChannelEndpointInput) (*model.ChannelEndpoint, error) {
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return nil, fmt.Errorf("invalid operation ID: %w", err)
	}
	if err := r.authorize(ctx, opID, models.OperationRoleOperator, true); err != nil {
		return nil, err
	}
	in, err := endpointInput(ctx, input)
	if err != nil {
		return nil, err
	}
	v, err := r.endpoints.Create(ctx, opID, moduleName, in)
	if err != nil {
		return nil, mapEndpointError(ctx, err)
	}
	return toEndpointModel(v), nil
}

func (r *channelEndpointResolver) UpdateChannelEndpoint(ctx context.Context, id string, input model.ChannelEndpointInput, expectedRevision int) (*model.ChannelEndpoint, error) {
	if _, err := r.authorizeEndpoint(ctx, id, models.OperationRoleOperator, true); err != nil {
		return nil, err
	}
	in, err := endpointInput(ctx, input)
	if err != nil {
		return nil, err
	}
	v, err := r.endpoints.Update(ctx, id, int64(expectedRevision), in)
	if err != nil {
		return nil, mapEndpointError(ctx, err)
	}
	return toEndpointModel(v), nil
}

func (r *channelEndpointResolver) DeleteChannelEndpoint(ctx context.Context, id string) (bool, error) {
	if _, err := r.authorizeEndpoint(ctx, id, models.OperationRoleOperator, true); err != nil {
		return false, err
	}
	if err := r.endpoints.Delete(ctx, id); err != nil {
		return false, mapEndpointError(ctx, err)
	}
	return true, nil
}

// endpointInput decodes the input; the caller is a user (agents are refused
// before).
func endpointInput(ctx context.Context, input model.ChannelEndpointInput) (channelendpoints.Input, error) {
	values, ok := input.Values.(map[string]any)
	if !ok {
		return channelendpoints.Input{}, moduleGQLError(ctx, errCodeBadUserInput, "values must be an object")
	}
	in := channelendpoints.Input{
		Name:   input.Name,
		Values: values,
		Actor:  managementActor(gqlctx.AuthFromContext(ctx)),
	}
	if input.Description != nil {
		in.Description = strings.TrimSpace(*input.Description)
	}
	return in, nil
}

// mapEndpointError turns service errors into coded GraphQL errors. Field
// problems travel in extensions.problems as {key, message} pairs.
func mapEndpointError(ctx context.Context, err error) error {
	var conflict *channelendpoints.ConflictError
	var invalid *channelendpoints.ValidationError
	switch {
	case errors.As(err, &conflict):
		return moduleGQLError(ctx, errCodeConflict, conflict.Error())
	case errors.As(err, &invalid):
		ext := map[string]any{"code": errCodeBadUserInput}
		if len(invalid.Problems) > 0 {
			ext["problems"] = problemList(invalid.Problems)
		}
		return &gqlerror.Error{Message: invalid.Message, Path: graphql.GetPath(ctx), Extensions: ext}
	case errors.Is(err, channelendpoints.ErrNotFound):
		return moduleGQLError(ctx, errCodeNotFound, "channel endpoint not found")
	}
	return err
}

func problemList(problems map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(problems))
	for _, k := range protocol.SortedProblemKeys(problems) {
		item := map[string]any{"message": problems[k]}
		if k != "" {
			item["key"] = k
		}
		out = append(out, item)
	}
	return out
}

func toConnectionModel(s *protocol.ConnectionSchema) *model.ModuleConnectionSchema {
	if s == nil {
		return nil
	}
	fields := make([]*model.ModuleField, len(s.Fields))
	for i, f := range s.Fields {
		fields[i] = toFieldModel(f)
		// Endpoint values are operation data shown in clear, so a secret
		// field's default is too.
		if f.Type == protocol.FieldSecret {
			fields[i].Default = f.Default
		}
	}
	return &model.ModuleConnectionSchema{Version: s.Version, Fields: fields}
}

func toEndpointModel(v channelendpoints.View) *model.ChannelEndpoint {
	values := map[string]any{}
	for k, val := range v.Values {
		values[k] = val
	}
	problems := make([]*model.ChannelEndpointProblem, 0, len(v.Problems))
	for _, k := range protocol.SortedProblemKeys(v.Problems) {
		problems = append(problems, &model.ChannelEndpointProblem{Key: strPtr(k), Message: v.Problems[k]})
	}
	return &model.ChannelEndpoint{
		ID:            v.EndpointID.String(),
		OperationID:   v.OperationID.String(),
		Name:          v.Name,
		Description:   strPtr(v.Description),
		ModuleName:    v.ModuleName,
		SchemaVersion: v.SchemaVersion,
		SchemaChanged: v.SchemaChanged(),
		Values:        values,
		Problems:      problems,
		Revision:      int(v.Revision),
		CreatedBy:     toActorModel(v.CreatedBy),
		UpdatedBy:     toActorModel(v.UpdatedBy),
		CreatedAt:     v.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     v.UpdatedAt.Format(time.RFC3339),
	}
}
