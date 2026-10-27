package builds

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// presetNamePattern: presets are picked by name, so it is a plain slug.
var presetNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// MaxDescriptionLength bounds a preset's description.
const MaxDescriptionLength = 500

// PresetInput is what an operator submits for a preset.
type PresetInput struct {
	Name        string
	Description string
	Target      string
	Platform    string
	Values      map[string]any
	Connections []ConnectionInput
	Actor       models.ModuleActor
}

// PresetView is a preset with its standing against the factory kind's
// current schema.
type PresetView struct {
	models.BuildPreset
	// Schema is the kind's current build schema; nil when no instance of the
	// kind declared one.
	Schema *protocol.BuildSchema
	// Problems are why the preset does not fit now, keyed like
	// ValidationError.Problems. Empty when it does.
	Problems map[string]string
	// Channels are the connections' current channel schemas, by connection
	// index; nil where no instance of the channel declared one.
	Channels []*protocol.ConnectionSchema
}

// SchemaChanged reports whether the kind now declares another schema version
// than the one the preset was saved against.
func (v PresetView) SchemaChanged() bool {
	return v.Schema != nil && v.Schema.Version != v.SchemaVersion
}

// ConnectionSchemaChanged reports whether connection i's channel now declares
// another connection schema version than its settings were saved against.
func (v PresetView) ConnectionSchemaChanged(i int) bool {
	return i < len(v.Channels) && v.Channels[i] != nil && v.Channels[i].Version != v.Connections[i].SchemaVersion
}

// Presets returns an operation's presets, all kinds or one.
func (s *Service) Presets(ctx context.Context, op uuid.UUID, moduleName string) ([]PresetView, error) {
	ps, err := s.repos.Presets.List(ctx, op, strings.TrimSpace(moduleName))
	if err != nil {
		return nil, fmt.Errorf("list build presets: %w", err)
	}
	out := make([]PresetView, len(ps))
	for i := range ps {
		if out[i], err = s.presetView(ctx, ps[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Preset returns one preset.
func (s *Service) Preset(ctx context.Context, id string) (PresetView, error) {
	p, err := s.findPreset(ctx, id)
	if err != nil {
		return PresetView{}, err
	}
	return s.presetView(ctx, *p)
}

// CreatePreset stores a new preset of a factory kind in an operation.
func (s *Service) CreatePreset(ctx context.Context, op uuid.UUID, moduleName string, in PresetInput) (PresetView, error) {
	moduleName = strings.TrimSpace(moduleName)
	p := &models.BuildPreset{OperationID: op, ModuleName: moduleName, CreatedBy: in.Actor, CreatedAt: s.now()}
	if err := s.applyPresetInput(ctx, p, in); err != nil {
		return PresetView{}, err
	}
	if err := s.repos.Presets.Insert(ctx, p); err != nil {
		return PresetView{}, presetWriteErr(err, p.Name)
	}
	return s.presetView(ctx, *p)
}

// UpdatePreset replaces a preset's settings if it is still at
// expectedRevision.
func (s *Service) UpdatePreset(ctx context.Context, id string, expectedRevision int64, in PresetInput) (PresetView, error) {
	p, err := s.findPreset(ctx, id)
	if err != nil {
		return PresetView{}, err
	}
	if p.Revision != expectedRevision {
		return PresetView{}, &ConflictError{}
	}
	if err := s.applyPresetInput(ctx, p, in); err != nil {
		return PresetView{}, err
	}
	if err := s.repos.Presets.Update(ctx, p); err != nil {
		return PresetView{}, presetWriteErr(err, p.Name)
	}
	return s.presetView(ctx, *p)
}

// DeletePreset removes a preset.
func (s *Service) DeletePreset(ctx context.Context, id string) error {
	p, err := s.findPreset(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.repos.Presets.Delete(ctx, p.Id); err != nil {
		return fmt.Errorf("delete build preset: %w", err)
	}
	return nil
}

// applyPresetInput validates in against the kind's current schema and sets it
// on p. A preset is partial: platform, connections and required values may be
// left for the build form, but what it holds must fit.
func (s *Service) applyPresetInput(ctx context.Context, p *models.BuildPreset, in PresetInput) error {
	name, desc := strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if !presetNamePattern.MatchString(name) {
		return invalid("name must match %s", presetNamePattern)
	}
	if utf8.RuneCountInString(desc) > MaxDescriptionLength {
		return invalid("description longer than %d characters", MaxDescriptionLength)
	}
	schema, err := s.schema(ctx, p.ModuleName)
	if err != nil {
		return err
	}
	if schema == nil {
		return invalid("no %q factory has declared a build schema", p.ModuleName)
	}
	values := compact(in.Values)
	conns := compactConnections(in.Connections)
	c, probs, err := s.check(ctx, p.OperationID, *schema, settings{Target: in.Target, Platform: in.Platform, Values: values, Connections: conns}, true)
	if err != nil {
		return err
	}
	if err := probs.err("the preset does not fit the factory's build schema"); err != nil {
		return err
	}
	p.Name, p.Description, p.Target, p.Platform = name, desc, in.Target, in.Platform
	p.SchemaVersion, p.Values, p.FileIDs = schema.Version, values, c.fileIDs
	p.Connections = make([]models.BuildPresetConnection, len(conns))
	for i, conn := range conns {
		p.Connections[i] = models.BuildPresetConnection{
			ModuleName: conn.ModuleName, Name: conn.Name, Description: conn.Description,
			SchemaVersion: c.channels[i].Version, Settings: conn.Settings, Values: conn.Values,
		}
	}
	p.UpdatedBy, p.UpdatedAt = in.Actor, s.now()
	return nil
}

// presetView checks a stored preset against the kind's and its channels'
// current schemas: a channel or factory release can leave it unfit.
func (s *Service) presetView(ctx context.Context, p models.BuildPreset) (PresetView, error) {
	v := PresetView{BuildPreset: p}
	schema, err := s.schema(ctx, p.ModuleName)
	if err != nil {
		return v, err
	}
	v.Schema = schema
	if schema == nil {
		v.Problems = map[string]string{"": fmt.Sprintf("no %q factory has declared a build schema", p.ModuleName)}
		return v, nil
	}
	conns := make([]ConnectionInput, len(p.Connections))
	for i, c := range p.Connections {
		conns[i] = ConnectionInput{ModuleName: c.ModuleName, Name: c.Name, Description: c.Description, Settings: c.Settings, Values: c.Values}
	}
	c, probs, err := s.check(ctx, p.OperationID, *schema, settings{Target: p.Target, Platform: p.Platform, Values: p.Values, Connections: conns}, true)
	if err != nil {
		return v, err
	}
	v.Channels = c.channels
	if len(probs) > 0 {
		v.Problems = probs
	}
	return v, nil
}

func (s *Service) findPreset(ctx context.Context, id string) (*models.BuildPreset, error) {
	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return nil, ErrPresetNotFound
	}
	p, err := s.repos.Presets.FindByID(ctx, oid)
	if err != nil {
		return nil, notFound(err, ErrPresetNotFound)
	}
	return p, nil
}

func presetWriteErr(err error, name string) error {
	switch {
	case errors.Is(err, repository.ErrBuildPresetNameTaken):
		return invalid("a preset named %q already exists in this operation", name)
	case errors.Is(err, repository.ErrBuildPresetConflict):
		return &ConflictError{}
	}
	return fmt.Errorf("store build preset: %w", err)
}

// compact drops values that leave a field unset: null, "" and [].
func compact(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		switch x := v.(type) {
		case nil:
			continue
		case string:
			if x == "" {
				continue
			}
		case []any:
			if len(x) == 0 {
				continue
			}
		}
		out[k] = v
	}
	return out
}

// notFound maps a repository miss to sentinel.
func notFound(err, sentinel error) error {
	if repository.IsNotFound(err) {
		return sentinel
	}
	return err
}
