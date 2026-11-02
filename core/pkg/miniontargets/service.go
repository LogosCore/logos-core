// Package miniontargets manages the targets of bind minions (docs:
// contracts/channel-core-connections/, bind-call minions: targets and
// sessions): where an operator put a page a factory built, with what the
// build connection's channel needs to call it. Adding one calls it at once;
// pkg/minionsync makes the calls. Authorization is the caller's.
package miniontargets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// ErrNotFound is a target that does not exist.
var ErrNotFound = errors.New("target not found")

// ValidationError is input the service refuses: Problems by field key ("" for
// the target as a whole).
type ValidationError struct {
	Message  string
	Problems map[string]string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Builds reads builds.
type Builds interface {
	FindByBuildID(ctx context.Context, buildID uuid.UUID) (*models.Build, error)
}

// Schemas reads a channel's current connection schema (nil when none of its
// instances declared one). Satisfied by *channelendpoints.Service.
type Schemas interface {
	Schema(ctx context.Context, moduleName string) (*protocol.ConnectionSchema, error)
}

// Caller calls targets, and opens and closes bind-persistent targets'
// connections. Satisfied by *minionsync.Service.
type Caller interface {
	CheckTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error)
	ConnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error)
	DisconnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error)
}

// Service manages targets.
type Service struct {
	targets  repository.IMinionTargetRepository
	sessions repository.IMinionSessionRepository
	builds   Builds
	schemas  Schemas
	caller   Caller
	bus      eventbus.IEventBus
	now      func() time.Time
}

// NewService is the targets service.
func NewService(targets repository.IMinionTargetRepository, sessions repository.IMinionSessionRepository, builds Builds,
	schemas Schemas, caller Caller, bus eventbus.IEventBus) *Service {
	return &Service{targets: targets, sessions: sessions, builds: builds, schemas: schemas, caller: caller, bus: bus,
		now: func() time.Time { return time.Now().UTC() }}
}

// Input is what an operator gives for a target.
type Input struct {
	Values  map[string]any
	Retries bool
	// Policy is who opens a bind-persistent target's connection
	// (models.PolicyOnDemand, ...); "" keeps a target's, on-demand for a new
	// one. A bind-call target has none.
	Policy string
	Actor  models.ModuleActor
}

// policyFor is the policy a target of kind takes from in (keeping had).
func policyFor(kind string, in Input, had string) (string, error) {
	if kind != protocol.KindBindPersistent {
		if in.Policy != "" {
			return "", invalid("a %s target has no policy: core calls it when there is something to send", kind)
		}
		return "", nil
	}
	switch {
	case in.Policy == "" && had != "":
		return had, nil
	case in.Policy == "":
		return models.PolicyOnDemand, nil
	case !models.KnownPolicy(in.Policy):
		return "", &ValidationError{Message: "policy is on-demand, manual or always", Problems: map[string]string{"policy": "on-demand, manual or always"}}
	}
	return in.Policy, nil
}

// BindConnection is a build connection targets can be added to: its
// place, name, channel and kind, and the channel's target fields for it.
type BindConnection struct {
	Place      int
	Name       string
	ModuleName string
	Kind       string
	Fields     []protocol.FieldSpec
	// Problem says why no target can be added to it now ("" when one can).
	Problem string
}

// BindConnections are b's connections of a bind kind.
func (s *Service) BindConnections(ctx context.Context, b *models.Build) ([]BindConnection, error) {
	var out []BindConnection
	for i, c := range b.Connections.Items {
		kind := c.ConnectionKind()
		if !protocol.IsBindKind(kind) {
			continue
		}
		bc := BindConnection{Place: i + 1, Name: c.Endpoint.Name, ModuleName: c.Endpoint.Channel.ModuleName, Kind: kind}
		schema, err := s.schemas.Schema(ctx, bc.ModuleName)
		if err != nil {
			return nil, err
		}
		switch {
		case kind != protocol.KindBindCall && kind != protocol.KindBindPersistent:
			bc.Problem = "core does not reach " + kind + " minions yet"
		case schema == nil || !hasKind(schema, kind):
			bc.Problem = fmt.Sprintf("no %s channel declares %s connections now", bc.ModuleName, kind)
		default:
			bc.Fields = schema.TargetFields(kind)
		}
		out = append(out, bc)
	}
	return out, nil
}

func hasKind(s *protocol.ConnectionSchema, kind string) bool {
	_, ok := s.Kinds[kind]
	return ok
}

// Add adds a target to connection conn (its place) of build b, and calls it
// at once, in the background: the target comes back new. With a session
// (one of b's whose target was removed) the target takes it over: a page
// keeps nothing, so core calls it under the session's id and the session
// goes on, instead of a rehello opening another.
func (s *Service) Add(ctx context.Context, b *models.Build, conn int, session *uuid.UUID, in Input) (*models.MinionTarget, error) {
	if b.State != models.BuildStateSucceeded {
		return nil, invalid("the build did not succeed: there is no page to reach")
	}
	if conn < 1 || conn > len(b.Connections.Items) {
		return nil, invalid("the build has no connection %d", conn)
	}
	c := b.Connections.Items[conn-1]
	kind, module := c.ConnectionKind(), c.Endpoint.Channel.ModuleName
	if session != nil {
		if err := s.canTakeOver(ctx, b, conn, kind, *session); err != nil {
			return nil, err
		}
	}
	values, err := s.check(ctx, module, kind, in.Values)
	if err != nil {
		return nil, err
	}
	policy, err := policyFor(kind, in, "")
	if err != nil {
		return nil, err
	}
	now := s.now()
	t := &models.MinionTarget{
		OperationID: b.OperationID, BuildID: b.BuildID, Conn: conn, ModuleName: module, Kind: kind, Policy: policy,
		Values: values, Name: targetName(values), Retries: in.Retries, SessionID: session,
		State: models.TargetNew, StateAt: now, CreatedBy: in.Actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.targets.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("store the target: %w", err)
	}
	s.publish(t, false)
	// A manual target waits for an operator's Connect.
	if t.Policy != models.PolicyManual {
		s.callSoon(t.TargetID)
	}
	return t, nil
}

// canTakeOver checks that a new target of b's connection conn may take
// session over: b's, open, the page's session through that connection (a
// minion that listens may be on another of its connections: it switches to
// this one), and no target's now.
func (s *Service) canTakeOver(ctx context.Context, b *models.Build, conn int, kind string, id uuid.UUID) error {
	sess, err := s.sessions.FindBySessionID(ctx, id)
	switch {
	case repository.IsNotFound(err):
		return invalid("no such session")
	case err != nil:
		return fmt.Errorf("load the session: %w", err)
	case sess.BuildID != b.BuildID || sess.OperationID != b.OperationID:
		return invalid("the session is not this build's")
	case sess.Closed(s.now()):
		return invalid("the session is closed")
	case sess.Conn != 0 && sess.Conn != conn && kind != protocol.KindBindPersistent:
		return invalid("the session's page answers connection %d, not %d", sess.Conn, conn)
	}
	other, err := s.targets.FindBySessionID(ctx, id)
	switch {
	case err == nil:
		return invalid("the session has a target already: %s", other.Name)
	case !repository.IsNotFound(err):
		return fmt.Errorf("look for the session's target: %w", err)
	}
	return nil
}

// Update replaces a target's values and retries; one that is not connected
// is called again at once.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*models.MinionTarget, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	values, err := s.check(ctx, t.ModuleName, t.Kind, in.Values)
	if err != nil {
		return nil, err
	}
	policy, err := policyFor(t.Kind, in, t.Policy)
	if err != nil {
		return nil, err
	}
	if err := s.targets.SetValues(ctx, id, values, targetName(values), in.Retries, policy, s.now()); err != nil {
		return nil, fmt.Errorf("store the target: %w", err)
	}
	s.publish(t, false)
	if t.State != models.TargetConnected && policy != models.PolicyManual {
		s.callSoon(id)
	}
	return s.Get(ctx, id)
}

// Connect opens a bind-persistent target's connection now (an operator's
// Connect, whatever its policy) and returns the target as the open left it.
func (s *Service) Connect(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Kind != protocol.KindBindPersistent {
		return nil, invalid("a %s target has no connection to open: Check now calls it", t.Kind)
	}
	return s.caller.ConnectTarget(ctx, id)
}

// Disconnect closes a bind-persistent target's connection. One of policy
// always is opened again by core: set it manual to keep it closed.
func (s *Service) Disconnect(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Kind != protocol.KindBindPersistent {
		return nil, invalid("a %s target has no connection to close", t.Kind)
	}
	return s.caller.DisconnectTarget(ctx, id)
}

// Check calls a target now and returns it as the call left it.
func (s *Service) Check(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.caller.CheckTarget(ctx, id)
}

// Remove removes a target: core stops calling it. The session its page
// opened stays, with its history, until an operator archives or deletes it
// as any other; a target added to it later takes it over (Add).
func (s *Service) Remove(ctx context.Context, id uuid.UUID) error {
	t, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if t.Kind == protocol.KindBindPersistent && s.caller != nil {
		// Its connection closes with it.
		if _, err := s.caller.DisconnectTarget(ctx, id); err != nil {
			return fmt.Errorf("close the target's connection: %w", err)
		}
	}
	if _, err := s.targets.Delete(ctx, id); err != nil {
		return fmt.Errorf("remove the target: %w", err)
	}
	s.publish(t, true)
	if t.SessionID != nil {
		// Its connection shows no target now.
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(eventbus.MinionSessionEventPayload{
			SessionID: t.SessionID.String(), OperationID: t.OperationID.String()}))
	}
	return nil
}

// Get is one target.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	t, err := s.targets.FindByTargetID(ctx, id)
	if repository.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return t, err
}

// OfSession is the target whose page a session is; nil for a session no
// target opened.
func (s *Service) OfSession(ctx context.Context, sessionID uuid.UUID) (*models.MinionTarget, error) {
	t, err := s.targets.FindBySessionID(ctx, sessionID)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	return t, err
}

// List is a page of an operation's targets, newest first.
func (s *Service) List(ctx context.Context, operationID uuid.UUID, f repository.MinionTargetFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionTarget, error) {
	return s.targets.FindByOperationIDWithCursor(ctx, operationID, f, cursor, limit, forward)
}

// Count counts an operation's targets f lets through.
func (s *Service) Count(ctx context.Context, operationID uuid.UUID, f repository.MinionTargetFilter) (int64, error) {
	return s.targets.CountByOperationID(ctx, operationID, f)
}

// Problems are why t's values no longer fit its channel's current target
// fields, by field key ("" for the target as a whole); none when they fit.
// A channel may change its target fields without a new schema version:
// nothing is built from them, so a target is checked when it is read and
// before each call (models.TargetNeedsEditing).
func (s *Service) Problems(ctx context.Context, t *models.MinionTarget) (map[string]string, error) {
	schema, err := s.schemas.Schema(ctx, t.ModuleName)
	if err != nil {
		return nil, err
	}
	if schema == nil || !hasKind(schema, t.Kind) {
		return map[string]string{"": fmt.Sprintf("no %s channel declares %s connections now", t.ModuleName, t.Kind)}, nil
	}
	values := map[string]any{}
	for k, v := range t.Values {
		if !empty(v) {
			values[k] = v
		}
	}
	return protocol.ValidateTargetValues(*schema, t.Kind, values), nil
}

// ProblemsText is problems as one line, for a target's state detail.
func ProblemsText(problems map[string]string) string {
	parts := make([]string, 0, len(problems))
	for _, k := range protocol.SortedProblemKeys(problems) {
		if k == "" {
			parts = append(parts, problems[k])
		} else {
			parts = append(parts, k+": "+problems[k])
		}
	}
	return strings.Join(parts, "; ")
}

// check validates values against the channel's target fields for kind and
// returns them over the fields' defaults.
func (s *Service) check(ctx context.Context, module, kind string, values map[string]any) (models.JSONMap, error) {
	if kind != protocol.KindBindCall && kind != protocol.KindBindPersistent {
		return nil, invalid("core does not reach %s minions yet", kind)
	}
	schema, err := s.schemas.Schema(ctx, module)
	if err != nil {
		return nil, err
	}
	if schema == nil || !hasKind(schema, kind) {
		return nil, invalid("no %s channel declares %s connections now", module, kind)
	}
	clean := map[string]any{}
	for k, v := range values {
		if !empty(v) {
			clean[k] = v
		}
	}
	if problems := protocol.ValidateTargetValues(*schema, kind, clean); len(problems) > 0 {
		return nil, &ValidationError{Message: "the target's values do not fit the channel's target fields", Problems: problems}
	}
	return models.JSONMap(protocol.ResolveTargetValues(*schema, kind, clean)), nil
}

// empty reports a value an operator left blank: its default applies.
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// targetName is what a target is shown as: its URL, else its host, else
// its first text value.
func targetName(values map[string]any) string {
	for _, k := range []string{"url", "host", "address"} {
		if v, ok := values[k].(string); ok && v != "" {
			return v
		}
	}
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "target"
}

// callSoon calls a target in the background, as its first call or after a
// change.
func (s *Service) callSoon(id uuid.UUID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, _ = s.caller.CheckTarget(ctx, id)
	}()
}

func (s *Service) publish(t *models.MinionTarget, deleted bool) {
	s.bus.Publish(eventbus.NewMinionTargetChangedEvent(eventbus.MinionTargetEventPayload{
		TargetID: t.TargetID.String(), OperationID: t.OperationID.String(), Deleted: deleted}))
}
