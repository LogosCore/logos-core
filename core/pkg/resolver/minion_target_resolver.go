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
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/miniontargets"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Minion targets over GraphQL (minion_targets.graphql): where bind minions
// are reached. The logic is miniontargets.Service; this resolver authorizes
// by operation role (viewer reads, operator writes) and maps the results.

// MinionTargets is the targets surface the resolver needs. Satisfied by
// *miniontargets.Service.
type MinionTargets interface {
	BindConnections(ctx context.Context, b *models.Build) ([]miniontargets.BindConnection, error)
	Add(ctx context.Context, b *models.Build, conn int, session *uuid.UUID, in miniontargets.Input) (*models.MinionTarget, error)
	Update(ctx context.Context, id uuid.UUID, in miniontargets.Input) (*models.MinionTarget, error)
	Check(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error)
	Connect(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error)
	Disconnect(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error)
	Remove(ctx context.Context, id uuid.UUID) error
	Get(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error)
	OfSession(ctx context.Context, sessionID uuid.UUID) (*models.MinionTarget, error)
	List(ctx context.Context, operationID uuid.UUID, f repository.MinionTargetFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionTarget, error)
	Count(ctx context.Context, operationID uuid.UUID, f repository.MinionTargetFilter) (int64, error)
	Problems(ctx context.Context, t *models.MinionTarget) (map[string]string, error)
}

// IMinionTargetResolver is the business logic behind minion_targets.graphql.
type IMinionTargetResolver interface {
	MinionTargets(ctx context.Context, operationID string, search *string, buildID *string, states []string, first *int, after *string, last *int, before *string) (*model.MinionTargetConnection, error)
	MinionTarget(ctx context.Context, id string) (*model.MinionTarget, error)
	AddMinionTarget(ctx context.Context, buildID string, conn int, input model.MinionTargetInput, sessionID *string) (*model.MinionTarget, error)
	UpdateMinionTarget(ctx context.Context, id string, input model.MinionTargetInput) (*model.MinionTarget, error)
	CheckMinionTarget(ctx context.Context, id string) (*model.MinionTarget, error)
	RemoveMinionTarget(ctx context.Context, id string) (bool, error)
	// BindConnections are a build's bind connections (Build.bindConnections).
	BindConnections(ctx context.Context, b *model.Build) ([]*model.BindConnection, error)
	// BindTarget is a session's target (MinionSession.bindTarget).
	BindTarget(ctx context.Context, s *model.MinionSession) (*model.MinionTarget, error)
	// Load is one target for an event, nil when it is gone or unreadable.
	Load(ctx context.Context, id string) *model.MinionTarget
	ConnectMinionTarget(ctx context.Context, id string) (*model.MinionTarget, error)
	DisconnectMinionTarget(ctx context.Context, id string) (*model.MinionTarget, error)
}

type minionTargetResolver struct {
	targets       MinionTargets
	builds        repository.IBuildRepository
	operationRepo repository.IOperationRepository
}

func NewMinionTargetResolver(targets MinionTargets, builds repository.IBuildRepository, operationRepo repository.IOperationRepository) IMinionTargetResolver {
	return &minionTargetResolver{targets: targets, builds: builds, operationRepo: operationRepo}
}

// authorize enforces a minimum operation role; writes refuse an agent key.
func (r *minionTargetResolver) authorize(ctx context.Context, operationID uuid.UUID, minRole models.OperationRole, write bool) error {
	if write && gqlctx.AuthFromContext(ctx).Agent != nil {
		return moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, operationID)
	if err != nil {
		return fmt.Errorf("operation not found: %w", err)
	}
	return authorization.AuthorizeOperationRole(ctx, &op, minRole)
}

// authorizeTarget loads a target and authorizes on its operation.
func (r *minionTargetResolver) authorizeTarget(ctx context.Context, id string, minRole models.OperationRole, write bool) (*models.MinionTarget, error) {
	tid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeNotFound, "target not found")
	}
	t, err := r.targets.Get(ctx, tid)
	if err != nil {
		return nil, mapTargetError(ctx, err)
	}
	return t, r.authorize(ctx, t.OperationID, minRole, write)
}

func (r *minionTargetResolver) MinionTargets(ctx context.Context, operationID string, search *string, buildID *string, states []string, first *int, after *string, last *int, before *string) (*model.MinionTargetConnection, error) {
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
	filter := repository.MinionTargetFilter{States: states}
	if search != nil {
		filter.Search = strings.TrimSpace(*search)
	}
	if buildID != nil && *buildID != "" {
		id, err := uuid.Parse(*buildID)
		if err != nil {
			return nil, fmt.Errorf("invalid build ID: %w", err)
		}
		filter.BuildID = &id
	}
	total, err := r.targets.Count(ctx, opID, filter)
	if err != nil {
		return nil, err
	}
	rows, err := r.targets.List(ctx, opID, filter, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, err
	}
	builds := map[uuid.UUID]*models.Build{}
	edges, pageInfo := pagination.BuildEdges(rows, args,
		func(t *models.MinionTarget) string { return repository.MinionTargetCursor(t) },
		func(t *models.MinionTarget, cursor string) *model.MinionTargetEdge {
			return &model.MinionTargetEdge{Node: r.toModel(ctx, t, builds), Cursor: cursor}
		})
	return &model.MinionTargetConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

func (r *minionTargetResolver) MinionTarget(ctx context.Context, id string) (*model.MinionTarget, error) {
	t, err := r.authorizeTarget(ctx, id, models.OperationRoleViewer, false)
	if err != nil {
		return nil, err
	}
	return r.toModel(ctx, t, nil), nil
}

func (r *minionTargetResolver) AddMinionTarget(ctx context.Context, buildID string, conn int, input model.MinionTargetInput, sessionID *string) (*model.MinionTarget, error) {
	bid, err := uuid.Parse(buildID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeNotFound, "build not found")
	}
	b, err := r.builds.FindByBuildID(ctx, bid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "build not found")
		}
		return nil, err
	}
	if err := r.authorize(ctx, b.OperationID, models.OperationRoleOperator, true); err != nil {
		return nil, err
	}
	in, err := targetInput(ctx, input)
	if err != nil {
		return nil, err
	}
	var session *uuid.UUID
	if sessionID != nil && *sessionID != "" {
		sid, err := uuid.Parse(*sessionID)
		if err != nil {
			return nil, moduleGQLError(ctx, errCodeNotFound, "session not found")
		}
		session = &sid
	}
	t, err := r.targets.Add(ctx, b, conn, session, in)
	if err != nil {
		return nil, mapTargetError(ctx, err)
	}
	return r.toModel(ctx, t, map[uuid.UUID]*models.Build{b.BuildID: b}), nil
}

func (r *minionTargetResolver) UpdateMinionTarget(ctx context.Context, id string, input model.MinionTargetInput) (*model.MinionTarget, error) {
	t, err := r.authorizeTarget(ctx, id, models.OperationRoleOperator, true)
	if err != nil {
		return nil, err
	}
	in, err := targetInput(ctx, input)
	if err != nil {
		return nil, err
	}
	if t, err = r.targets.Update(ctx, t.TargetID, in); err != nil {
		return nil, mapTargetError(ctx, err)
	}
	return r.toModel(ctx, t, nil), nil
}

func (r *minionTargetResolver) CheckMinionTarget(ctx context.Context, id string) (*model.MinionTarget, error) {
	t, err := r.authorizeTarget(ctx, id, models.OperationRoleOperator, true)
	if err != nil {
		return nil, err
	}
	if t, err = r.targets.Check(ctx, t.TargetID); err != nil {
		return nil, mapTargetError(ctx, err)
	}
	return r.toModel(ctx, t, nil), nil
}

func (r *minionTargetResolver) ConnectMinionTarget(ctx context.Context, id string) (*model.MinionTarget, error) {
	t, err := r.authorizeTarget(ctx, id, models.OperationRoleOperator, true)
	if err != nil {
		return nil, err
	}
	if t, err = r.targets.Connect(ctx, t.TargetID); err != nil {
		return nil, mapTargetError(ctx, err)
	}
	return r.toModel(ctx, t, nil), nil
}

func (r *minionTargetResolver) DisconnectMinionTarget(ctx context.Context, id string) (*model.MinionTarget, error) {
	t, err := r.authorizeTarget(ctx, id, models.OperationRoleOperator, true)
	if err != nil {
		return nil, err
	}
	if t, err = r.targets.Disconnect(ctx, t.TargetID); err != nil {
		return nil, mapTargetError(ctx, err)
	}
	return r.toModel(ctx, t, nil), nil
}

func (r *minionTargetResolver) RemoveMinionTarget(ctx context.Context, id string) (bool, error) {
	t, err := r.authorizeTarget(ctx, id, models.OperationRoleOperator, true)
	if err != nil {
		return false, err
	}
	if err := r.targets.Remove(ctx, t.TargetID); err != nil {
		return false, mapTargetError(ctx, err)
	}
	return true, nil
}

func (r *minionTargetResolver) BindConnections(ctx context.Context, b *model.Build) ([]*model.BindConnection, error) {
	bid, err := uuid.Parse(b.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid build ID: %w", err)
	}
	build, err := r.builds.FindByBuildID(ctx, bid)
	if err != nil {
		if repository.IsNotFound(err) {
			return []*model.BindConnection{}, nil
		}
		return nil, err
	}
	conns, err := r.targets.BindConnections(ctx, build)
	if err != nil {
		return nil, err
	}
	out := make([]*model.BindConnection, len(conns))
	for i, c := range conns {
		out[i] = &model.BindConnection{Place: c.Place, Name: c.Name, ModuleName: c.ModuleName, Kind: c.Kind,
			Version: c.Version, DelegatedOnly: c.DelegatedOnly, Agentless: c.Agentless,
			Fields: fieldModels(c.Fields), Problem: strPtr(c.Problem)}
	}
	return out, nil
}

func (r *minionTargetResolver) BindTarget(ctx context.Context, s *model.MinionSession) (*model.MinionTarget, error) {
	sid, err := uuid.Parse(s.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}
	t, err := r.targets.OfSession(ctx, sid)
	if err != nil || t == nil {
		return nil, err
	}
	if t.OperationID.String() != s.OperationID {
		// A target is its operation's: one linked to another's session
		// (core links only its own build's) is not shown there.
		return nil, nil
	}
	return r.toModel(ctx, t, nil), nil
}

func (r *minionTargetResolver) Load(ctx context.Context, id string) *model.MinionTarget {
	tid, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	t, err := r.targets.Get(ctx, tid)
	if err != nil {
		return nil
	}
	return r.toModel(ctx, t, nil)
}

// toModel maps a target, with its build's description and connection name
// (builds caches the builds read, nil for none).
func (r *minionTargetResolver) toModel(ctx context.Context, t *models.MinionTarget, builds map[uuid.UUID]*models.Build) *model.MinionTarget {
	values := map[string]any{}
	for k, v := range t.Values {
		values[k] = v
	}
	out := &model.MinionTarget{
		ID: t.TargetID.String(), OperationID: t.OperationID.String(), BuildID: t.BuildID.String(),
		Conn: t.Conn, ModuleName: t.ModuleName, Kind: t.Kind, Values: values, Name: t.Name, Retries: t.Retries, Policy: strPtr(t.Policy),
		State: t.State, StateDetail: strPtr(t.StateDetail), StateAt: t.StateAt.Format(time.RFC3339),
		Failures: t.Failures, UnreachableSince: timePtr(t.UnreachableSince), NextCallAt: timePtr(t.NextCallAt),
		LastCallAt: timePtr(t.LastCallAt), LastOkAt: timePtr(t.LastOKAt), Calls: int(t.Calls),
		CreatedBy: toActorModel(t.CreatedBy), CreatedAt: t.CreatedAt.Format(time.RFC3339), UpdatedAt: t.UpdatedAt.Format(time.RFC3339),
	}
	if t.SessionID != nil {
		out.SessionID = strPtr(t.SessionID.String())
	}
	if t.DelegateID != nil {
		out.DelegateID = strPtr(t.DelegateID.String())
	}
	out.Problems = []*model.ChannelEndpointProblem{}
	if problems, err := r.targets.Problems(ctx, t); err == nil {
		out.Problems = problemModels(problems)
	}
	b, ok := builds[t.BuildID]
	if !ok {
		if found, err := r.builds.FindByBuildID(ctx, t.BuildID); err == nil {
			b = found
		}
		if builds != nil {
			builds[t.BuildID] = b
		}
	}
	if b != nil {
		out.BuildDescription = strPtr(b.Description)
		if t.Conn >= 1 && t.Conn <= len(b.Connections.Items) {
			out.ConnName = strPtr(b.Connections.Items[t.Conn-1].Endpoint.Name)
		}
	}
	return out
}

// problemModels are a target's problems, by field key in order; the
// target's own (key "") with a null key.
func problemModels(problems map[string]string) []*model.ChannelEndpointProblem {
	out := make([]*model.ChannelEndpointProblem, 0, len(problems))
	for _, k := range protocol.SortedProblemKeys(problems) {
		out = append(out, &model.ChannelEndpointProblem{Key: strPtr(k), Message: problems[k]})
	}
	return out
}

// targetInput decodes the input; the caller is a user.
func targetInput(ctx context.Context, input model.MinionTargetInput) (miniontargets.Input, error) {
	values, ok := input.Values.(map[string]any)
	if !ok {
		return miniontargets.Input{}, moduleGQLError(ctx, errCodeBadUserInput, "values must be an object")
	}
	in := miniontargets.Input{Values: values, Retries: true, Actor: managementActor(gqlctx.AuthFromContext(ctx))}
	if input.Retries != nil {
		in.Retries = *input.Retries
	}
	if input.Policy != nil {
		in.Policy = *input.Policy
	}
	if input.DelegateID != nil && *input.DelegateID != "" {
		id, err := uuid.Parse(*input.DelegateID)
		if err != nil {
			return miniontargets.Input{}, moduleGQLError(ctx, errCodeBadUserInput, "delegateId is a session id")
		}
		in.Delegate = &id
	}
	return in, nil
}

// mapTargetError turns service errors into coded GraphQL errors. Field
// problems travel in extensions.problems as {key, message} pairs.
func mapTargetError(ctx context.Context, err error) error {
	var invalid *miniontargets.ValidationError
	switch {
	case errors.As(err, &invalid):
		ext := map[string]any{"code": errCodeBadUserInput}
		if len(invalid.Problems) > 0 {
			ext["problems"] = problemList(invalid.Problems)
		}
		return &gqlerror.Error{Message: invalid.Message, Path: graphql.GetPath(ctx), Extensions: ext}
	case errors.Is(err, miniontargets.ErrNotFound):
		return moduleGQLError(ctx, errCodeNotFound, "target not found")
	}
	return err
}
