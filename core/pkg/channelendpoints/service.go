// Package channelendpoints manages channel endpoints: an operation's
// ready-to-use values for a channel kind's minion connection schema (e.g. "EU
// CDN" for the HTTP channels), which its operators reuse when creating
// minions. The values are copied into the minion, so deleting or changing an
// endpoint affects nothing already built, and channels never see endpoints.
//
// Channels declare the schema at module.register and core stores it with each
// instance's registration. An endpoint belongs to one operation and one
// channel kind (module name), and is validated against the kind's latest
// schema (the highest version any instance declared): when saved, and again
// whenever it is read or exported.
//
// Authorization (operation membership and role) is the caller's job; the
// service only stores and validates.
package channelendpoints

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// ExportFormat identifies the export document's layout.
const ExportFormat = "logos.channel-endpoint.v1"

// MaxDescriptionLength bounds an endpoint's description.
const MaxDescriptionLength = 500

// namePattern: endpoints are picked by name, so it is a plain slug of at
// most maxNameLength characters.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

const maxNameLength = 63

// ErrNotFound is mapped to a client error by the GraphQL layer.
var ErrNotFound = errors.New("channel endpoint not found")

// ValidationError is a client error. Problems holds per-field reasons, keyed
// by connection schema field (empty when the problem is not a field's).
type ValidationError struct {
	Message  string
	Problems map[string]string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ConflictError means the endpoint changed since the caller read it.
type ConflictError struct{}

func (e *ConflictError) Error() string {
	return "the endpoint was changed concurrently; reload and retry"
}

// Registry is the slice of the module registry the service reads.
type Registry interface {
	LatestConnectionSchema(ctx context.Context, moduleName string) (*models.ModuleConnectionSchema, error)
	LatestConnectionSchemas(ctx context.Context) (map[string]*models.ModuleConnectionSchema, error)
	LatestIcons(ctx context.Context, moduleType string) (map[string]models.ModuleIcon, error)
}

// Kind is a channel kind, its current connection schema, and its icon (empty
// when none of its instances sent one).
type Kind struct {
	ModuleName string
	Schema     protocol.ConnectionSchema
	Icon       string
	IconLight  string // the icon on light themes; empty when Icon serves both
}

// Service validates and stores channel endpoints.
type Service struct {
	repo     repository.IChannelEndpointRepository
	registry Registry
	now      func() time.Time
}

func NewService(repo repository.IChannelEndpointRepository, registry Registry) *Service {
	return &Service{repo: repo, registry: registry, now: func() time.Time { return time.Now().UTC() }}
}

// View is an endpoint with its standing against the kind's current schema.
type View struct {
	models.ChannelEndpoint
	// Schema is the kind's current connection schema; nil when no instance
	// of the kind declared one.
	Schema *protocol.ConnectionSchema
	// Problems are reasons the endpoint cannot be used now, keyed like
	// ValidationError.Problems. Empty when it can.
	Problems map[string]string
}

// SchemaChanged reports whether the kind now declares another schema version
// than the one the values were saved against.
func (v View) SchemaChanged() bool {
	return v.Schema != nil && v.Schema.Version != v.SchemaVersion
}

// Input is what an operator submits for an endpoint.
type Input struct {
	Name        string
	Description string
	// Values by schema field key, replacing the stored ones. Null, "" and []
	// leave a field unset (its default applies).
	Values map[string]any
	Actor  models.ModuleActor
}

// Schema returns a channel kind's current connection schema; nil when no
// instance of it declared one.
func (s *Service) Schema(ctx context.Context, moduleName string) (*protocol.ConnectionSchema, error) {
	c, err := s.registry.LatestConnectionSchema(ctx, strings.TrimSpace(moduleName))
	if err != nil {
		return nil, fmt.Errorf("load connection schema: %w", err)
	}
	if c == nil {
		return nil, nil
	}
	return &c.ConnectionSchema, nil
}

// Kinds returns every channel kind that declared a connection schema, by
// name, with its latest schema.
func (s *Service) Kinds(ctx context.Context) ([]Kind, error) {
	schemas, err := s.registry.LatestConnectionSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("load connection schemas: %w", err)
	}
	icons, err := s.registry.LatestIcons(ctx, protocol.ServiceChannel)
	if err != nil {
		return nil, fmt.Errorf("load channel icons: %w", err)
	}
	out := make([]Kind, 0, len(schemas))
	for name, c := range schemas {
		out = append(out, Kind{ModuleName: name, Schema: c.ConnectionSchema, Icon: icons[name].Icon, IconLight: icons[name].IconLight})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleName < out[j].ModuleName })
	return out, nil
}

// List returns a page of an operation's endpoints by name: those filter lets
// through, past cursor, at most limit; each checked against its kind's
// current schema.
func (s *Service) List(ctx context.Context, operationID uuid.UUID, filter repository.ChannelEndpointFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]View, error) {
	eps, err := s.repo.FindByOperationIDWithCursor(ctx, operationID, filter, cursor, limit, forward)
	if err != nil {
		return nil, fmt.Errorf("list channel endpoints: %w", err)
	}
	schemas := map[string]*protocol.ConnectionSchema{}
	out := make([]View, len(eps))
	for i, ep := range eps {
		schema, ok := schemas[ep.ModuleName]
		if !ok {
			if schema, err = s.Schema(ctx, ep.ModuleName); err != nil {
				return nil, err
			}
			schemas[ep.ModuleName] = schema
		}
		out[i] = view(ep, schema)
	}
	return out, nil
}

// Count counts an operation's endpoints filter lets through.
func (s *Service) Count(ctx context.Context, operationID uuid.UUID, filter repository.ChannelEndpointFilter) (int64, error) {
	n, err := s.repo.CountByOperationID(ctx, operationID, filter)
	if err != nil {
		return 0, fmt.Errorf("count channel endpoints: %w", err)
	}
	return n, nil
}

// maxFreeNameTries bounds FreeName's search for a free suffix.
const maxFreeNameTries = 1000

// FreeName is the first of name+tag, name+tag-2, -3… that no endpoint of the
// operation is called, the base shortened to fit a name: what the forms
// suggest for a copy, an import, or a connection saved as an endpoint. tag
// is "" or starts with "-".
func (s *Service) FreeName(ctx context.Context, operationID uuid.UUID, name, tag string) (string, error) {
	base := []rune(strings.TrimSpace(name))
	for n := 1; n <= maxFreeNameTries; n++ {
		suffix := tag
		if n > 1 {
			suffix = fmt.Sprintf("%s-%d", tag, n)
		}
		room := max(0, maxNameLength-utf8.RuneCountInString(suffix))
		candidate := string(base[:min(len(base), room)]) + suffix
		taken, err := s.repo.NameTaken(ctx, operationID, candidate)
		if err != nil {
			return "", fmt.Errorf("check endpoint name: %w", err)
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", invalid("no free name like %q", name)
}

// Get returns one endpoint.
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	ep, err := s.find(ctx, id)
	if err != nil {
		return View{}, err
	}
	schema, err := s.Schema(ctx, ep.ModuleName)
	if err != nil {
		return View{}, err
	}
	return view(*ep, schema), nil
}

// Create stores a new endpoint of a channel kind in an operation.
func (s *Service) Create(ctx context.Context, operationID uuid.UUID, moduleName string, in Input) (View, error) {
	moduleName = strings.TrimSpace(moduleName)
	schema, err := s.requireSchema(ctx, moduleName)
	if err != nil {
		return View{}, err
	}
	name, desc, err := checkMeta(in)
	if err != nil {
		return View{}, err
	}
	values, err := checkValues(*schema, in.Values)
	if err != nil {
		return View{}, err
	}
	now := s.now()
	ep := &models.ChannelEndpoint{
		OperationID: operationID, Name: name, Description: desc, ModuleName: moduleName,
		SchemaVersion: schema.Version, Values: values,
		CreatedBy: in.Actor, UpdatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Insert(ctx, ep); err != nil {
		return View{}, s.writeErr(err, name)
	}
	return view(*ep, schema), nil
}

// Update replaces an endpoint's name, description and values. expectedRevision
// is the revision the caller read; a mismatch is a ConflictError.
func (s *Service) Update(ctx context.Context, id string, expectedRevision int64, in Input) (View, error) {
	ep, err := s.find(ctx, id)
	if err != nil {
		return View{}, err
	}
	if ep.Revision != expectedRevision {
		return View{}, &ConflictError{}
	}
	schema, err := s.requireSchema(ctx, ep.ModuleName)
	if err != nil {
		return View{}, err
	}
	name, desc, err := checkMeta(in)
	if err != nil {
		return View{}, err
	}
	values, err := checkValues(*schema, in.Values)
	if err != nil {
		return View{}, err
	}
	ep.Name, ep.Description, ep.Values, ep.SchemaVersion = name, desc, values, schema.Version
	ep.UpdatedBy, ep.UpdatedAt = in.Actor, s.now()
	if err := s.repo.Update(ctx, ep); err != nil {
		return View{}, s.writeErr(err, name)
	}
	return view(*ep, schema), nil
}

// Delete removes an endpoint.
func (s *Service) Delete(ctx context.Context, id string) error {
	endpointID, err := uuid.Parse(id)
	if err != nil {
		return ErrNotFound
	}
	ok, err := s.repo.Delete(ctx, endpointID)
	if err != nil {
		return fmt.Errorf("delete channel endpoint: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Export is the endpoint as a minion's connection settings: its values over
// the schema's defaults. An endpoint with problems is
// refused, so values the channel's schema rejects never reach a minion.
func (s *Service) Export(ctx context.Context, id string) (map[string]any, error) {
	v, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(v.Problems) > 0 {
		return nil, &ValidationError{Message: "the endpoint does not fit the channel's connection schema", Problems: v.Problems}
	}
	return map[string]any{
		"format": ExportFormat,
		"name":   v.Name,
		"channel": map[string]any{
			"module_name":    v.ModuleName,
			"schema_version": v.Schema.Version,
		},
		"params": protocol.ResolveConnectionValues(*v.Schema, v.Values),
	}, nil
}

// --- helpers ---

func view(ep models.ChannelEndpoint, schema *protocol.ConnectionSchema) View {
	v := View{ChannelEndpoint: ep, Schema: schema}
	if schema == nil {
		v.Problems = map[string]string{"": fmt.Sprintf("no %q channel has declared a minion connection schema", ep.ModuleName)}
		return v
	}
	v.Problems = protocol.ValidateConnectionValues(*schema, ep.Values)
	return v
}

func (s *Service) requireSchema(ctx context.Context, moduleName string) (*protocol.ConnectionSchema, error) {
	schema, err := s.Schema(ctx, moduleName)
	if err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, invalid("no %q channel has declared a minion connection schema", moduleName)
	}
	return schema, nil
}

func (s *Service) find(ctx context.Context, id string) (*models.ChannelEndpoint, error) {
	endpointID, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrNotFound
	}
	ep, err := s.repo.FindByEndpointID(ctx, endpointID)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load channel endpoint: %w", err)
	}
	return ep, nil
}

func (s *Service) writeErr(err error, name string) error {
	switch {
	case errors.Is(err, repository.ErrChannelEndpointNameTaken):
		return invalid("this operation already has an endpoint named %q", name)
	case errors.Is(err, repository.ErrChannelEndpointConflict):
		return &ConflictError{}
	}
	return fmt.Errorf("save channel endpoint: %w", err)
}

func checkMeta(in Input) (name, desc string, err error) {
	name = strings.TrimSpace(in.Name)
	desc = strings.TrimSpace(in.Description)
	if !namePattern.MatchString(name) {
		return "", "", invalid("name must be 1-63 lowercase letters, digits, '.', '_' or '-', starting with a letter or digit")
	}
	if utf8.RuneCountInString(desc) > MaxDescriptionLength {
		return "", "", invalid("description longer than %d characters", MaxDescriptionLength)
	}
	return name, desc, nil
}

// checkValues normalizes and validates submitted values.
func checkValues(schema protocol.ConnectionSchema, submitted map[string]any) (models.JSONMap, error) {
	values := models.JSONMap{}
	for k, v := range submitted {
		if !empty(v) {
			values[k] = v
		}
	}
	if problems := protocol.ValidateConnectionValues(schema, values); problems != nil {
		keys := protocol.SortedProblemKeys(problems)
		return nil, &ValidationError{
			Message:  fmt.Sprintf("%s: %s", keys[0], problems[keys[0]]),
			Problems: problems,
		}
	}
	return values, nil
}

func empty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}
