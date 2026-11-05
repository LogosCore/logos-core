// Package credentialrefs resolves the credentials a module's values name
// (docs: contracts/credentials.md). A credential field (protocol.
// FieldCredential, FieldCredentialList) keeps credential ids wherever core
// stores values: a target's, an endpoint's, a build's, a task's. Core checks
// them when they are saved (each one of the operation's, of a type its field
// takes) and gives the module the credentials themselves when it uses them:
// a call, an open, a build, a task.
package credentialrefs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Store reads and marks credentials. Satisfied by
// repository.ICredentialRepository.
type Store interface {
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Credential, error)
	// MarkValid sets a credential of the operation VALID unless it is, and
	// reports whether it changed.
	MarkValid(ctx context.Context, operationID, credentialID uuid.UUID) (bool, error)
}

// Unusable is a credential that cannot go to the module: gone, another
// operation's, or not of a type its field takes (converted since the values
// were saved). The values need editing; trying again changes nothing. Any
// other error from a resolve is core's own (a store), worth retrying.
type Unusable struct{ Why string }

func (e *Unusable) Error() string { return e.Why }

func unusable(format string, args ...any) error { return &Unusable{Why: fmt.Sprintf(format, args...)} }

// Resolver checks and resolves credential references.
type Resolver struct {
	store Store
	bus   eventbus.IEventBus
	usage *UsageRepos
}

// New is a resolver over store; bus carries the update a login makes.
func New(store Store, bus eventbus.IEventBus) *Resolver {
	if bus == nil {
		bus = eventbus.NewNopEventBus()
	}
	return &Resolver{store: store, bus: bus}
}

// IDs are the credential ids fields' values name, as UUIDs, each once. An
// id that is not a UUID names no credential: it is left out, and Check says
// so.
func IDs(fields []protocol.FieldSpec, values map[string]any) []uuid.UUID {
	var out []uuid.UUID
	for _, s := range protocol.CredentialIDs(fields, values) {
		if id, err := uuid.Parse(s); err == nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Check is what is wrong with the credentials values name, by field key
// (nil when nothing is): each must be one of the operation's credentials, of
// a type its field accepts. The values passed the schema's own checks.
func (r *Resolver) Check(ctx context.Context, operationID uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]string, error) {
	found, err := r.load(ctx, operationID, IDs(fields, values))
	if err != nil {
		return nil, err
	}
	var problems map[string]string
	for _, f := range fields {
		for _, s := range protocol.FieldCredentialIDs(f, values[f.Key]) {
			c, ok := found[s]
			var reason string
			switch {
			case !ok:
				reason = fmt.Sprintf("credential %s is not one of the operation's", s)
			case len(f.Accept) > 0 && !slices.Contains(f.Accept, string(c.Type)):
				reason = fmt.Sprintf("credential %q is a %s; the field takes %s", c.Name, c.Type, strings.Join(f.Accept, ", "))
			default:
				continue
			}
			if problems == nil {
				problems = map[string]string{}
			}
			if _, taken := problems[f.Key]; !taken {
				problems[f.Key] = reason
			}
		}
	}
	return problems, nil
}

// Resolve are the credentials ids name as a module gets them (Wire), by id,
// and the revision each was at. Each must be one of the operation's: an id
// that names none is Unusable, saying it.
func (r *Resolver) Resolve(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	found, err := r.load(ctx, operationID, ids)
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]protocol.Credential, len(ids))
	revs := make(map[string]int64, len(ids))
	for _, id := range ids {
		c, ok := found[id.String()]
		if !ok {
			return nil, nil, unusable("credential %s is gone, or not one of the operation's", id)
		}
		w := Wire(c)
		if err := w.Validate(); err != nil {
			return nil, nil, unusable("credential %q: %v", c.Name, err)
		}
		out[w.ID], revs[w.ID] = w, c.Rev
	}
	return out, revs, nil
}

// ResolveFields is Resolve for the credentials fields' values name, each
// checked against its field's types (Unusable when one is not): the
// credential may have been converted since the values were saved.
func (r *Resolver) ResolveFields(ctx context.Context, operationID uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]protocol.Credential, error) {
	given, _, err := r.Resolve(ctx, operationID, IDs(fields, values))
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if _, err := protocol.FieldCredentials(f, values[f.Key], given); err != nil {
			return nil, &Unusable{Why: err.Error()}
		}
	}
	return given, nil
}

func (r *Resolver) load(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID) (map[string]models.Credential, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	creds, err := r.store.FindByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	out := make(map[string]models.Credential, len(creds))
	for _, c := range creds {
		if c.OperationID == operationID {
			out[c.CredentialID.String()] = c
		}
	}
	return out, nil
}

// Wire is a credential as a module gets it: its id, name, type and the
// fields its type takes that hold a value. Its properties, tags, validity and
// comments are the operators' notes, and stay in core.
func Wire(c models.Credential) protocol.Credential {
	w := protocol.Credential{ID: c.CredentialID.String(), Name: c.Name, Type: string(c.Type)}
	spec, _ := models.CredentialTypeSpecFor(c.Type)
	for _, f := range spec.Fields {
		switch f.Field {
		case models.CredentialFieldUsername:
			w.Username = c.Username
		case models.CredentialFieldRealm:
			w.Realm = c.Realm
		case models.CredentialFieldPassword:
			w.Password = c.Password
		case models.CredentialFieldKey:
			w.Key = c.Key
		case models.CredentialFieldPassphrase:
			w.Passphrase = c.Passphrase
		case models.CredentialFieldCertificate:
			w.Certificate = c.Certificate
		case models.CredentialFieldPrivateKey:
			w.PrivateKey = c.PrivateKey
		case models.CredentialFieldTicket:
			w.Ticket = c.Ticket
		case models.CredentialFieldService:
			w.Service = c.Service
		case models.CredentialFieldExpiresAt:
			if c.ExpiresAt != nil {
				w.ExpiresAt = c.ExpiresAt.UTC().Format(time.RFC3339)
			}
		case models.CredentialFieldHash:
			w.Hash = c.Hash
		case models.CredentialFieldHex:
			w.Hex = c.Hex
		case models.CredentialFieldAlgorithm:
			w.Algorithm = c.Algorithm
		case models.CredentialFieldKeys:
			for _, k := range c.Keys {
				w.Keys = append(w.Keys, protocol.CredentialKey{Name: k.Name, Content: k.Content})
			}
		}
	}
	return w
}

// LoginWorked records that a channel logged in with a credential: it works,
// so it is VALID from now on (it may have been untested, or marked not
// working before it was fixed). The update goes out as any credential's, from
// the system: no operator marked it.
func (r *Resolver) LoginWorked(ctx context.Context, operationID, credentialID uuid.UUID) error {
	changed, err := r.store.MarkValid(ctx, operationID, credentialID)
	if err != nil || !changed {
		return err
	}
	r.bus.Publish(eventbus.NewCredentialUpdatedEvent(eventbus.SystemActor(), eventbus.CredentialEventPayload{
		CredentialID: credentialID.String(), OperationID: operationID.String(),
	}))
	return nil
}

// UsageRepos are what usage reads: the records whose values name
// credentials, and the channels' schemas their fields are by.
type UsageRepos struct {
	Targets interface {
		FindByCredentialID(ctx context.Context, credentialID uuid.UUID, limit int64) ([]models.MinionTarget, error)
	}
	Endpoints interface {
		FindByCredentialID(ctx context.Context, credentialID uuid.UUID, limit int64) ([]models.ChannelEndpoint, error)
	}
	Builds interface {
		CountByCredentialID(ctx context.Context, credentialID uuid.UUID) (int64, error)
	}
	Tasks interface {
		CountByCredentialID(ctx context.Context, credentialID uuid.UUID) (int64, error)
	}
	Schemas interface {
		Schema(ctx context.Context, moduleName string) (*protocol.ConnectionSchema, error)
	}
}

// WithUsage lets the resolver say where credentials are used.
func (r *Resolver) WithUsage(u UsageRepos) *Resolver {
	r.usage = &u
	return r
}

// MaxUsageListed bounds the targets and the endpoints Usage lists.
const MaxUsageListed = 200

// Usage is where a credential is used: the targets and endpoints whose
// values name it (what goes to their channels at the next call), and how
// many builds it went into and tasks carried it (history: an artifact keeps
// what it was built with).
type Usage struct {
	Targets       []TargetUse
	Endpoints     []EndpointUse
	Builds, Tasks int64
}

// TargetUse is a target naming the credential: the credential types its
// fields naming it take (empty: any), and how the credential last fared
// logging in to it (nil: not tried).
type TargetUse struct {
	Target models.MinionTarget
	Accept []string
	Login  *models.TargetLogin
}

// EndpointUse is an endpoint naming the credential, with the types its
// fields naming it take (empty: any).
type EndpointUse struct {
	Endpoint models.ChannelEndpoint
	Accept   []string
}

// Usage says where credential id is used.
func (r *Resolver) Usage(ctx context.Context, id uuid.UUID) (Usage, error) {
	var out Usage
	if r.usage == nil {
		return out, nil
	}
	u := r.usage
	targets, err := u.Targets.FindByCredentialID(ctx, id, MaxUsageListed)
	if err != nil {
		return out, fmt.Errorf("the targets naming it: %w", err)
	}
	for _, t := range targets {
		var fields []protocol.FieldSpec
		if s, err := u.Schemas.Schema(ctx, t.ModuleName); err == nil && s != nil {
			fields = s.TargetFields(t.Kind)
		}
		use := TargetUse{Target: t, Accept: acceptOf(fields, t.Values, id)}
		if l, ok := t.Logins[id.String()]; ok {
			use.Login = &l
		}
		out.Targets = append(out.Targets, use)
	}
	endpoints, err := u.Endpoints.FindByCredentialID(ctx, id, MaxUsageListed)
	if err != nil {
		return out, fmt.Errorf("the endpoints naming it: %w", err)
	}
	for _, ep := range endpoints {
		var fields []protocol.FieldSpec
		if s, err := u.Schemas.Schema(ctx, ep.ModuleName); err == nil && s != nil {
			fields = s.Kinds[ep.Kind].Fields
		}
		out.Endpoints = append(out.Endpoints, EndpointUse{Endpoint: ep, Accept: acceptOf(fields, ep.Values, id)})
	}
	if out.Builds, err = u.Builds.CountByCredentialID(ctx, id); err != nil {
		return out, fmt.Errorf("the builds it went into: %w", err)
	}
	if out.Tasks, err = u.Tasks.CountByCredentialID(ctx, id); err != nil {
		return out, fmt.Errorf("the tasks that carried it: %w", err)
	}
	return out, nil
}

// acceptOf is the credential types the fields naming id in values take, in
// the protocol's order; empty when one of them takes any.
func acceptOf(fields []protocol.FieldSpec, values map[string]any, id uuid.UUID) []string {
	take := map[string]bool{}
	for _, f := range fields {
		if !slices.Contains(protocol.FieldCredentialIDs(f, values[f.Key]), id.String()) {
			continue
		}
		if len(f.Accept) == 0 {
			return nil
		}
		for _, t := range f.Accept {
			take[t] = true
		}
	}
	var out []string
	for _, t := range protocol.CredentialTypes() {
		if take[t] {
			out = append(out, t)
		}
	}
	return out
}
