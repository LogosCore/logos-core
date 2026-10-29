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
	"unicode"
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

// MaxNameLength bounds an endpoint's name: any text, in characters.
const MaxNameLength = 120

// slugPattern is an endpoint's slug: a plain identifier, safe in a build's
// connection, an export and a file name.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

const maxSlugLength = 63

// fallbackSlug is the slug of a name with nothing a slug can keep.
const fallbackSlug = "endpoint"

// Slugify makes a slug of any text: lowercased, every run of characters a
// slug cannot hold one '-', at most maxSlugLength long, and fallbackSlug
// when nothing is left. "Edge EU (CDN)" is "edge-eu-cdn", "localhost:8090"
// "localhost-8090".
func Slugify(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			if r == '-' && (dash || b.Len() == 0) {
				continue
			}
			if b.Len() == 0 && (r == '.' || r == '_') {
				continue
			}
			b.WriteRune(r)
			dash = r == '-'
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	slug := b.String()
	if len(slug) > maxSlugLength {
		slug = slug[:maxSlugLength]
	}
	slug = strings.TrimRight(slug, "-")
	if slug == "" {
		return fallbackSlug
	}
	return slug
}

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
	Name string
	// Slug is the endpoint's identifier; empty makes one from Name on
	// create, and keeps the stored one on update.
	Slug        string
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

// maxFreeSlugTries bounds FreeSlug's search for a free suffix.
const maxFreeSlugTries = 1000

// FreeSlug is the first of slug+tag, slug+tag-2, -3… that no endpoint of the
// operation has, where slug is Slugify(text), shortened to fit: what the
// forms suggest for a new endpoint, a copy, an import, or a connection saved
// as an endpoint. tag is "" or starts with "-".
func (s *Service) FreeSlug(ctx context.Context, operationID uuid.UUID, text, tag string) (string, error) {
	base := Slugify(text)
	for n := 1; n <= maxFreeSlugTries; n++ {
		suffix := tag
		if n > 1 {
			suffix = fmt.Sprintf("%s-%d", tag, n)
		}
		room := max(0, maxSlugLength-len(suffix))
		candidate := strings.TrimRight(base[:min(len(base), room)], "-") + suffix
		taken, err := s.repo.SlugTaken(ctx, operationID, candidate)
		if err != nil {
			return "", fmt.Errorf("check endpoint slug: %w", err)
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", invalid("no free slug like %q", base)
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
	slug, err := s.slugFor(ctx, operationID, in.Slug, name)
	if err != nil {
		return View{}, err
	}
	values, err := checkValues(*schema, in.Values)
	if err != nil {
		return View{}, err
	}
	now := s.now()
	ep := &models.ChannelEndpoint{
		OperationID: operationID, Name: name, Slug: slug, Description: desc, ModuleName: moduleName,
		SchemaVersion: schema.Version, Values: values,
		CreatedBy: in.Actor, UpdatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Insert(ctx, ep); err != nil {
		return View{}, s.writeErr(err, slug)
	}
	return view(*ep, schema), nil
}

// Update replaces an endpoint's name, description and values, and its slug
// when Input.Slug is set. expectedRevision
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
	slug := ep.Slug
	if strings.TrimSpace(in.Slug) != "" || slug == "" {
		if slug, err = s.slugFor(ctx, ep.OperationID, in.Slug, name); err != nil {
			return View{}, err
		}
	}
	values, err := checkValues(*schema, in.Values)
	if err != nil {
		return View{}, err
	}
	ep.Name, ep.Slug, ep.Description, ep.Values, ep.SchemaVersion = name, slug, desc, values, schema.Version
	ep.UpdatedBy, ep.UpdatedAt = in.Actor, s.now()
	if err := s.repo.Update(ctx, ep); err != nil {
		return View{}, s.writeErr(err, slug)
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
		"name":   v.Slug,
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

// slugFor is the slug an endpoint is saved with: the one given, checked, or
// a free one made from name.
func (s *Service) slugFor(ctx context.Context, operationID uuid.UUID, given, name string) (string, error) {
	if given = strings.TrimSpace(given); given != "" {
		if !slugPattern.MatchString(given) {
			return "", invalid("slug must be 1-63 lowercase letters, digits, '.', '_' or '-', starting with a letter or digit")
		}
		return given, nil
	}
	return s.FreeSlug(ctx, operationID, name, "")
}

func (s *Service) writeErr(err error, slug string) error {
	switch {
	case errors.Is(err, repository.ErrChannelEndpointSlugTaken):
		return invalid("this operation already has an endpoint with the slug %q", slug)
	case errors.Is(err, repository.ErrChannelEndpointConflict):
		return &ConflictError{}
	}
	return fmt.Errorf("save channel endpoint: %w", err)
}

func checkMeta(in Input) (name, desc string, err error) {
	name = strings.TrimSpace(in.Name)
	desc = strings.TrimSpace(in.Description)
	switch {
	case name == "":
		return "", "", invalid("a name is required")
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", "", invalid("name longer than %d characters", MaxNameLength)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", "", invalid("name has a control character (a line break, a tab…)")
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
