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
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Minion builds over GraphQL (builds.graphql): factory kinds, an operation's
// build files, builds and minions. The logic is builds.Service; this
// resolver authorizes by operation role (viewer reads, operator writes; agent
// keys read only) and maps the results.

// BuildManager is the build surface the resolver needs. Satisfied by
// *builds.Service.
type BuildManager interface {
	Kinds(ctx context.Context) ([]builds.Kind, error)
	Files(ctx context.Context, op uuid.UUID) ([]models.BuildFile, error)
	File(ctx context.Context, id string) (*models.BuildFile, error)
	DeleteFile(ctx context.Context, id string) error
	Builds(ctx context.Context, op uuid.UUID, filter repository.BuildFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Build, error)
	CountBuilds(ctx context.Context, op uuid.UUID, filter repository.BuildFilter) (int64, error)
	Build(ctx context.Context, id string) (*models.Build, error)
	CreateBuild(ctx context.Context, op uuid.UUID, in builds.BuildInput) (*models.Build, error)
	CancelBuild(ctx context.Context, id string, actor models.ModuleActor) (*models.Build, error)
	DeleteArtifact(ctx context.Context, id string) (*models.Build, error)
	Minions(ctx context.Context, op uuid.UUID) ([]models.Minion, error)
}

// IBuildResolver is the business logic behind builds.graphql.
type IBuildResolver interface {
	ModuleBuild(ctx context.Context, obj *models.Module) (*model.BuildSchema, error)
	FactoryKinds(ctx context.Context) ([]*model.FactoryKind, error)
	BuildFiles(ctx context.Context, operationID string) ([]*model.BuildFile, error)
	DeleteBuildFile(ctx context.Context, id string) (bool, error)
	Builds(ctx context.Context, operationID string, search *string, states []string, moduleNames []string, first *int, after *string, last *int, before *string) (*model.BuildConnection, error)
	Build(ctx context.Context, id string) (*model.Build, error)
	CreateBuild(ctx context.Context, operationID string, input model.BuildInput) (*model.Build, error)
	CancelBuild(ctx context.Context, id string) (*model.Build, error)
	DeleteBuildArtifact(ctx context.Context, id string) (*model.Build, error)
	Minions(ctx context.Context, operationID string) ([]*model.Minion, error)
}

type buildResolver struct {
	builds        BuildManager
	operationRepo repository.IOperationRepository
}

func NewBuildResolver(b BuildManager, operationRepo repository.IOperationRepository) IBuildResolver {
	return &buildResolver{builds: b, operationRepo: operationRepo}
}

// authorize enforces a minimum operation role. Writes pass write=true: agent
// keys may read builds but never change them.
func (r *buildResolver) authorize(ctx context.Context, operationID uuid.UUID, minRole models.OperationRole, write bool) error {
	if write && gqlctx.AuthFromContext(ctx).Agent != nil {
		return moduleGQLError(ctx, errCodeForbidden, "forbidden")
	}
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, operationID)
	if err != nil {
		return fmt.Errorf("operation not found: %w", err)
	}
	return authorization.AuthorizeOperationRole(ctx, &op, minRole)
}

func (r *buildResolver) authorizeID(ctx context.Context, operationID string, minRole models.OperationRole, write bool) (uuid.UUID, error) {
	op, err := uuid.Parse(operationID)
	if err != nil {
		return op, moduleGQLError(ctx, errCodeBadUserInput, "invalid operation ID")
	}
	return op, r.authorize(ctx, op, minRole, write)
}

func (r *buildResolver) ModuleBuild(_ context.Context, obj *models.Module) (*model.BuildSchema, error) {
	if obj.Build == nil {
		return nil, nil
	}
	return toBuildSchemaModel(&obj.Build.BuildSchema), nil
}

func (r *buildResolver) FactoryKinds(ctx context.Context) ([]*model.FactoryKind, error) {
	kinds, err := r.builds.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.FactoryKind, len(kinds))
	for i, k := range kinds {
		out[i] = &model.FactoryKind{ModuleName: k.ModuleName, Schema: toBuildSchemaModel(&k.Schema), Instances: k.Instances, Icon: k.Icon, IconLight: k.IconLight}
	}
	return out, nil
}

// --- files ---

func (r *buildResolver) BuildFiles(ctx context.Context, operationID string) ([]*model.BuildFile, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleViewer, false)
	if err != nil {
		return nil, err
	}
	files, err := r.builds.Files(ctx, op)
	if err != nil {
		return nil, err
	}
	out := make([]*model.BuildFile, len(files))
	for i := range files {
		out[i] = toBuildFileModel(&files[i])
	}
	return out, nil
}

func (r *buildResolver) DeleteBuildFile(ctx context.Context, id string) (bool, error) {
	f, err := r.builds.File(ctx, id)
	if err != nil {
		return false, mapBuildError(ctx, err)
	}
	if err := r.authorize(ctx, f.OperationID, models.OperationRoleOperator, true); err != nil {
		return false, err
	}
	if err := r.builds.DeleteFile(ctx, id); err != nil {
		return false, mapBuildError(ctx, err)
	}
	return true, nil
}

// --- builds ---

func (r *buildResolver) Builds(ctx context.Context, operationID string, search *string, states []string, moduleNames []string, first *int, after *string, last *int, before *string) (*model.BuildConnection, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleViewer, false)
	if err != nil {
		return nil, err
	}
	args, err := pagination.ParseArgs(first, after, last, before)
	if err != nil {
		return nil, fmt.Errorf("invalid pagination args: %w", err)
	}
	filter := repository.BuildFilter{Search: strings.TrimSpace(deref(search)), States: states, ModuleNames: moduleNames}
	total, err := r.builds.CountBuilds(ctx, op, filter)
	if err != nil {
		return nil, fmt.Errorf("count builds: %w", err)
	}
	bs, err := r.builds.Builds(ctx, op, filter, args.Cursor, args.Limit+1, args.Forward)
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}
	edges, pageInfo := pagination.BuildEdges(bs, args, repository.BuildCursor,
		func(b *models.Build, cursor string) *model.BuildEdge {
			return &model.BuildEdge{Node: toBuildModel(b), Cursor: cursor}
		})
	return &model.BuildConnection{Edges: edges, PageInfo: &pageInfo, TotalCount: int(total)}, nil
}

func (r *buildResolver) Build(ctx context.Context, id string) (*model.Build, error) {
	b, err := r.builds.Build(ctx, id)
	if errors.Is(err, builds.ErrBuildNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.authorize(ctx, b.OperationID, models.OperationRoleViewer, false); err != nil {
		return nil, err
	}
	return toBuildModel(b), nil
}

func (r *buildResolver) CreateBuild(ctx context.Context, operationID string, input model.BuildInput) (*model.Build, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleOperator, true)
	if err != nil {
		return nil, err
	}
	values, ok := input.Values.(map[string]any)
	if !ok {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "values must be an object")
	}
	conns, err := connectionInputs(ctx, input.Connections)
	if err != nil {
		return nil, err
	}
	in := builds.BuildInput{
		Description: input.Description, ModuleName: input.ModuleName, Target: input.Target, Platform: input.Platform,
		Values: values, Commands: input.Commands, Connections: conns, Actor: managementActor(gqlctx.AuthFromContext(ctx)),
	}
	b, err := r.builds.CreateBuild(ctx, op, in)
	if err != nil {
		return nil, mapBuildError(ctx, err)
	}
	return toBuildModel(b), nil
}

// changeBuild authorizes an operator on a build's operation and applies fn.
func (r *buildResolver) changeBuild(ctx context.Context, id string, fn func() (*models.Build, error)) (*model.Build, error) {
	b, err := r.builds.Build(ctx, id)
	if err != nil {
		return nil, mapBuildError(ctx, err)
	}
	if err := r.authorize(ctx, b.OperationID, models.OperationRoleOperator, true); err != nil {
		return nil, err
	}
	if b, err = fn(); err != nil {
		return nil, mapBuildError(ctx, err)
	}
	return toBuildModel(b), nil
}

func (r *buildResolver) CancelBuild(ctx context.Context, id string) (*model.Build, error) {
	return r.changeBuild(ctx, id, func() (*models.Build, error) {
		return r.builds.CancelBuild(ctx, id, managementActor(gqlctx.AuthFromContext(ctx)))
	})
}

func (r *buildResolver) DeleteBuildArtifact(ctx context.Context, id string) (*model.Build, error) {
	return r.changeBuild(ctx, id, func() (*models.Build, error) { return r.builds.DeleteArtifact(ctx, id) })
}

func (r *buildResolver) Minions(ctx context.Context, operationID string) ([]*model.Minion, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleViewer, false)
	if err != nil {
		return nil, err
	}
	ms, err := r.builds.Minions(ctx, op)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Minion, len(ms))
	for i, m := range ms {
		out[i] = &model.Minion{
			ID: m.MinionID.String(), OperationID: m.OperationID.String(), ModuleName: m.ModuleName,
			Target: m.Target, Platform: m.Platform, Commands: orEmpty(m.Commands),
			CreatedBy: toActorModel(m.CreatedBy), CreatedAt: m.CreatedAt.Format(time.RFC3339),
		}
	}
	return out, nil
}

// --- inputs and errors ---

func connectionInputs(ctx context.Context, in []*model.BuildConnectionInput) ([]builds.ConnectionInput, error) {
	out := make([]builds.ConnectionInput, len(in))
	for i, c := range in {
		out[i].ModuleName = c.ModuleName
		if c.Name != nil {
			out[i].Name = *c.Name
		}
		if c.Description != nil {
			out[i].Description = *c.Description
		}
		var ok bool
		if out[i].Settings, ok = optionalObject(c.Settings); !ok {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "connection settings must be an object")
		}
		if out[i].Values, ok = optionalObject(c.Values); !ok {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "connection values must be an object")
		}
	}
	return out, nil
}

// optionalObject is a JSON input that may be null or an object.
func optionalObject(v any) (map[string]any, bool) {
	if v == nil {
		return nil, true
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// mapBuildError turns service errors into coded GraphQL errors. Problems
// travel in extensions.problems as {key, message} pairs.
func mapBuildError(ctx context.Context, err error) error {
	var invalid *builds.ValidationError
	switch {
	case errors.As(err, &invalid):
		ext := map[string]any{"code": errCodeBadUserInput}
		if len(invalid.Problems) > 0 {
			ext["problems"] = problemList(invalid.Problems)
		}
		return &gqlerror.Error{Message: invalid.Message, Path: graphql.GetPath(ctx), Extensions: ext}
	case errors.Is(err, builds.ErrBuildNotFound), errors.Is(err, builds.ErrFileNotFound):
		return moduleGQLError(ctx, errCodeNotFound, err.Error())
	}
	return err
}

// --- mapping ---

func toBuildSchemaModel(s *protocol.BuildSchema) *model.BuildSchema {
	out := &model.BuildSchema{Version: s.Version, Crypto: strPtr(s.Crypto), Fields: toBuildFieldModels(s.Fields)}
	for _, c := range s.Channels {
		out.Channels = append(out.Channels, &model.BuildChannel{
			ModuleName: c.ModuleName, Versions: c.Versions, Fields: toBuildFieldModels(c.Fields),
		})
	}
	for _, t := range s.Targets {
		mt := &model.BuildTarget{
			ID: t.ID, Label: orKey(t.Label, t.ID), Description: strPtr(t.Description), Format: t.Format,
			Platforms: t.Platforms, Fields: toBuildFieldModels(t.Fields),
		}
		for _, c := range s.TargetChannels(t) {
			mt.Channels = append(mt.Channels, &model.BuildChannelVersions{ModuleName: c.ModuleName, Versions: c.Versions})
		}
		out.Targets = append(out.Targets, mt)
	}
	out.Commands = make([]*model.BuildCommand, len(s.Commands))
	for i, c := range s.Commands {
		mc := &model.BuildCommand{
			ID: c.ID, Label: orKey(c.Label, c.ID), Description: strPtr(c.Description),
			Targets: []string{}, Default: c.Default, Required: c.Required, Params: toBuildFieldModels(c.Params),
		}
		for _, t := range s.Targets {
			if c.For(t.ID) {
				mc.Targets = append(mc.Targets, t.ID)
			}
		}
		out.Commands[i] = mc
	}
	return out
}

// toBuildFieldModels maps build fields. Build values are operation data
// shown in clear, so a secret field's default is too.
func toBuildFieldModels(fields []protocol.FieldSpec) []*model.ModuleField {
	out := make([]*model.ModuleField, len(fields))
	for i, f := range fields {
		out[i] = toFieldModel(f)
		if f.Type == protocol.FieldSecret {
			out[i].Default = f.Default
		}
	}
	return out
}

func toBuildFileModel(f *models.BuildFile) *model.BuildFile {
	return &model.BuildFile{
		ID: f.FileID.String(), OperationID: f.OperationID.String(), Name: f.Name, Size: int(f.Size),
		Sha256: f.SHA256, MediaType: strPtr(f.MediaType), UploadedBy: toActorModel(f.UploadedBy),
		CreatedAt: f.CreatedAt.Format(time.RFC3339),
	}
}

func toBuildModel(b *models.Build) *model.Build {
	out := &model.Build{
		ID: b.MinionID.String(), OperationID: b.OperationID.String(), Description: b.Description, ModuleName: b.ModuleName, Target: b.Target,
		Platform: b.Platform, SchemaVersion: b.SchemaVersion, Values: copyMap(b.Values),
		Connections: b.Connections.Items, State: b.State,
		Instance: strPtr(b.Instance), FactoryVersion: strPtr(b.FactoryVersion),
		Commands: orEmpty(b.Commands), Stages: []*model.BuildStage{}, Warnings: orEmpty(b.Warnings),
		Log: strPtr(b.Log), CreatedBy: toActorModel(b.CreatedBy),
		CreatedAt: b.CreatedAt.Format(time.RFC3339), StartedAt: timePtr(b.StartedAt), FinishedAt: timePtr(b.FinishedAt),
		Deadline: b.Deadline.Format(time.RFC3339),
	}
	if b.Crypto != nil {
		out.Crypto = b.Crypto
	}
	if out.Connections == nil {
		out.Connections = []protocol.BuildConnection{}
	}
	out.ConnectionDescriptions = make([]string, len(b.Connections.Items))
	copy(out.ConnectionDescriptions, b.ConnectionDescriptions)
	for _, st := range builds.Pipeline(b) {
		out.Stages = append(out.Stages, &model.BuildStage{
			ID: st.ID, Label: st.Label, Core: st.Core, State: st.State, Message: strPtr(st.Message),
			StartedAt: stageTime(st.StartedAt), FinishedAt: stageTime(st.FinishedAt),
		})
	}
	if a := b.Artifact; a != nil && (b.State == models.BuildStateSucceeded || a.Deleted) {
		out.Artifact = &model.BuildArtifact{Filename: a.Filename, Size: int(a.Size), Sha256: a.SHA256, MediaType: strPtr(a.MediaType), Deleted: a.Deleted}
	}
	if b.Error != nil {
		out.Error = &model.BuildError{Code: b.Error.Code, Message: b.Error.Message}
	}
	if b.CancelledBy != nil {
		out.CancelledBy = toActorModel(*b.CancelledBy)
	}
	return out
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// stageTime keeps a stage's time to the millisecond core stores: a step can
// take less than a second.
func stageTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func timePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
