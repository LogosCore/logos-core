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
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Minion builds over GraphQL (builds.graphql): factory kinds, an operation's
// build files, presets, builds and minions. The logic is builds.Service; this
// resolver authorizes by operation role (viewer reads, operator writes; agent
// keys read only) and maps the results.

// BuildManager is the build surface the resolver needs. Satisfied by
// *builds.Service.
type BuildManager interface {
	Kinds(ctx context.Context) ([]builds.Kind, error)
	Files(ctx context.Context, op uuid.UUID) ([]models.BuildFile, error)
	File(ctx context.Context, id string) (*models.BuildFile, error)
	DeleteFile(ctx context.Context, id string) error
	Presets(ctx context.Context, op uuid.UUID, moduleName string) ([]builds.PresetView, error)
	Preset(ctx context.Context, id string) (builds.PresetView, error)
	CreatePreset(ctx context.Context, op uuid.UUID, moduleName string, in builds.PresetInput) (builds.PresetView, error)
	UpdatePreset(ctx context.Context, id string, expectedRevision int64, in builds.PresetInput) (builds.PresetView, error)
	DeletePreset(ctx context.Context, id string) error
	Builds(ctx context.Context, op uuid.UUID, limit int64) ([]models.Build, error)
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
	BuildPresets(ctx context.Context, operationID string, moduleName *string) ([]*model.BuildPreset, error)
	BuildPreset(ctx context.Context, id string) (*model.BuildPreset, error)
	CreateBuildPreset(ctx context.Context, operationID string, moduleName string, input model.BuildPresetInput) (*model.BuildPreset, error)
	UpdateBuildPreset(ctx context.Context, id string, input model.BuildPresetInput, expectedRevision int) (*model.BuildPreset, error)
	DeleteBuildPreset(ctx context.Context, id string) (bool, error)
	Builds(ctx context.Context, operationID string, limit *int) ([]*model.Build, error)
	Build(ctx context.Context, id string) (*model.Build, error)
	CreateBuild(ctx context.Context, operationID string, input model.BuildInput) (*model.Build, error)
	CancelBuild(ctx context.Context, id string) (*model.Build, error)
	DeleteBuildArtifact(ctx context.Context, id string) (*model.Build, error)
	Minions(ctx context.Context, operationID string) ([]*model.Minion, error)
}

// Default and largest number of builds one builds query returns.
const (
	defaultBuildsLimit = 50
	maxBuildsLimit     = 500
)

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
		out[i] = &model.FactoryKind{ModuleName: k.ModuleName, Schema: toBuildSchemaModel(&k.Schema), Instances: k.Instances, Icon: k.Icon}
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

// --- presets ---

func (r *buildResolver) BuildPresets(ctx context.Context, operationID string, moduleName *string) ([]*model.BuildPreset, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleViewer, false)
	if err != nil {
		return nil, err
	}
	name := ""
	if moduleName != nil {
		name = *moduleName
	}
	views, err := r.builds.Presets(ctx, op, name)
	if err != nil {
		return nil, err
	}
	out := make([]*model.BuildPreset, len(views))
	for i := range views {
		out[i] = toPresetModel(views[i])
	}
	return out, nil
}

func (r *buildResolver) BuildPreset(ctx context.Context, id string) (*model.BuildPreset, error) {
	v, err := r.builds.Preset(ctx, id)
	if errors.Is(err, builds.ErrPresetNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.authorize(ctx, v.OperationID, models.OperationRoleViewer, false); err != nil {
		return nil, err
	}
	return toPresetModel(v), nil
}

func (r *buildResolver) CreateBuildPreset(ctx context.Context, operationID string, moduleName string, input model.BuildPresetInput) (*model.BuildPreset, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleOperator, true)
	if err != nil {
		return nil, err
	}
	in, err := presetInput(ctx, input)
	if err != nil {
		return nil, err
	}
	v, err := r.builds.CreatePreset(ctx, op, moduleName, in)
	if err != nil {
		return nil, mapBuildError(ctx, err)
	}
	return toPresetModel(v), nil
}

func (r *buildResolver) UpdateBuildPreset(ctx context.Context, id string, input model.BuildPresetInput, expectedRevision int) (*model.BuildPreset, error) {
	v, err := r.builds.Preset(ctx, id)
	if err != nil {
		return nil, mapBuildError(ctx, err)
	}
	if err := r.authorize(ctx, v.OperationID, models.OperationRoleOperator, true); err != nil {
		return nil, err
	}
	in, err := presetInput(ctx, input)
	if err != nil {
		return nil, err
	}
	v, err = r.builds.UpdatePreset(ctx, id, int64(expectedRevision), in)
	if err != nil {
		return nil, mapBuildError(ctx, err)
	}
	return toPresetModel(v), nil
}

func (r *buildResolver) DeleteBuildPreset(ctx context.Context, id string) (bool, error) {
	v, err := r.builds.Preset(ctx, id)
	if err != nil {
		return false, mapBuildError(ctx, err)
	}
	if err := r.authorize(ctx, v.OperationID, models.OperationRoleOperator, true); err != nil {
		return false, err
	}
	if err := r.builds.DeletePreset(ctx, id); err != nil {
		return false, mapBuildError(ctx, err)
	}
	return true, nil
}

// --- builds ---

func (r *buildResolver) Builds(ctx context.Context, operationID string, limit *int) ([]*model.Build, error) {
	op, err := r.authorizeID(ctx, operationID, models.OperationRoleViewer, false)
	if err != nil {
		return nil, err
	}
	n := defaultBuildsLimit
	if limit != nil && *limit > 0 {
		n = min(*limit, maxBuildsLimit)
	}
	bs, err := r.builds.Builds(ctx, op, int64(n))
	if err != nil {
		return nil, err
	}
	out := make([]*model.Build, len(bs))
	for i := range bs {
		out[i] = toBuildModel(&bs[i])
	}
	return out, nil
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
		ModuleName: input.ModuleName, Target: input.Target, Platform: input.Platform,
		Values: values, Connections: conns, Actor: managementActor(gqlctx.AuthFromContext(ctx)),
	}
	if input.PresetID != nil {
		in.PresetID = *input.PresetID
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
			ID: m.MinionID, OperationID: m.OperationID.String(), BuildID: m.BuildID, ModuleName: m.ModuleName,
			Target: m.Target, Platform: m.Platform, Capabilities: orEmpty(m.Capabilities),
			CreatedBy: toActorModel(m.CreatedBy), CreatedAt: m.CreatedAt.Format(time.RFC3339),
		}
	}
	return out, nil
}

// --- inputs and errors ---

func presetInput(ctx context.Context, input model.BuildPresetInput) (builds.PresetInput, error) {
	values, ok := input.Values.(map[string]any)
	if !ok {
		return builds.PresetInput{}, moduleGQLError(ctx, errCodeBadUserInput, "values must be an object")
	}
	conns, err := connectionInputs(ctx, input.Connections)
	if err != nil {
		return builds.PresetInput{}, err
	}
	in := builds.PresetInput{Name: input.Name, Target: input.Target, Values: values, Connections: conns,
		Actor: managementActor(gqlctx.AuthFromContext(ctx))}
	if input.Description != nil {
		in.Description = strings.TrimSpace(*input.Description)
	}
	if input.Platform != nil {
		in.Platform = *input.Platform
	}
	return in, nil
}

func connectionInputs(ctx context.Context, in []*model.BuildConnectionInput) ([]builds.ConnectionInput, error) {
	out := make([]builds.ConnectionInput, len(in))
	for i, c := range in {
		out[i].ModuleName = c.ModuleName
		if c.Name != nil {
			out[i].Name = *c.Name
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
	var conflict *builds.ConflictError
	var invalid *builds.ValidationError
	switch {
	case errors.As(err, &conflict):
		return moduleGQLError(ctx, errCodeConflict, conflict.Error())
	case errors.As(err, &invalid):
		ext := map[string]any{"code": errCodeBadUserInput}
		if len(invalid.Problems) > 0 {
			ext["problems"] = problemList(invalid.Problems)
		}
		return &gqlerror.Error{Message: invalid.Message, Path: graphql.GetPath(ctx), Extensions: ext}
	case errors.Is(err, builds.ErrBuildNotFound), errors.Is(err, builds.ErrPresetNotFound), errors.Is(err, builds.ErrFileNotFound):
		return moduleGQLError(ctx, errCodeNotFound, err.Error())
	}
	return err
}

// --- mapping ---

func toBuildSchemaModel(s *protocol.BuildSchema) *model.BuildSchema {
	out := &model.BuildSchema{Version: s.Version, Crypto: orEmpty(s.Crypto), Fields: toBuildFieldModels(s.Fields)}
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

func toPresetModel(v builds.PresetView) *model.BuildPreset {
	out := &model.BuildPreset{
		ID: v.Id.Hex(), OperationID: v.OperationID.String(), Name: v.Name, Description: strPtr(v.Description),
		ModuleName: v.ModuleName, Target: v.Target, Platform: strPtr(v.Platform), Values: copyMap(v.Values),
		SchemaVersion: v.SchemaVersion, SchemaChanged: v.SchemaChanged(), Revision: int(v.Revision),
		Connections: []*model.BuildPresetConnection{}, Problems: []*model.BuildProblem{},
		CreatedBy: toActorModel(v.CreatedBy), UpdatedBy: toActorModel(v.UpdatedBy),
		CreatedAt: v.CreatedAt.Format(time.RFC3339), UpdatedAt: v.UpdatedAt.Format(time.RFC3339),
	}
	for i, c := range v.Connections {
		out.Connections = append(out.Connections, &model.BuildPresetConnection{
			ModuleName: c.ModuleName, Name: strPtr(c.Name), Settings: copyMap(c.Settings),
			SchemaVersion: c.SchemaVersion, SchemaChanged: v.ConnectionSchemaChanged(i), Values: copyMap(c.Values),
		})
	}
	for _, k := range protocol.SortedProblemKeys(v.Problems) {
		out.Problems = append(out.Problems, &model.BuildProblem{Key: strPtr(k), Message: v.Problems[k]})
	}
	return out
}

func toBuildModel(b *models.Build) *model.Build {
	out := &model.Build{
		ID: b.BuildID, OperationID: b.OperationID.String(), ModuleName: b.ModuleName, Target: b.Target,
		Platform: b.Platform, SchemaVersion: b.SchemaVersion, Values: copyMap(b.Values),
		Connections: b.Connections.Items, MinionID: b.MinionID, State: b.State,
		Instance: strPtr(b.Instance), FactoryVersion: strPtr(b.FactoryVersion),
		Steps: []*model.BuildStep{}, Capabilities: orEmpty(b.Capabilities), Warnings: orEmpty(b.Warnings),
		Log: strPtr(b.Log), CreatedBy: toActorModel(b.CreatedBy),
		CreatedAt: b.CreatedAt.Format(time.RFC3339), StartedAt: timePtr(b.StartedAt), FinishedAt: timePtr(b.FinishedAt),
		Deadline: b.Deadline.Format(time.RFC3339),
	}
	if out.Connections == nil {
		out.Connections = []protocol.BuildConnection{}
	}
	for _, s := range b.Steps {
		out.Steps = append(out.Steps, &model.BuildStep{Step: s.Step, Message: strPtr(s.Message), Percent: s.Percent, At: s.At.Format(time.RFC3339)})
	}
	if a := b.Artifact; a != nil && (b.State == models.BuildStateSucceeded || a.Deleted) {
		out.Artifact = &model.BuildArtifact{Filename: a.Filename, Size: int(a.Size), Sha256: a.SHA256, MediaType: strPtr(a.MediaType), Deleted: a.Deleted}
	}
	if b.Error != nil {
		out.Error = &model.BuildError{Code: b.Error.Code, Message: b.Error.Message}
	}
	if b.PresetID != nil {
		out.PresetID = strPtr(b.PresetID.Hex())
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

func timePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
