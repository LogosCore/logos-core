package credentialrefs

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

type fakeStore struct {
	mu    sync.Mutex
	rows  map[uuid.UUID]models.Credential
	valid []uuid.UUID
}

func (f *fakeStore) FindByIDs(_ context.Context, ids []uuid.UUID) ([]models.Credential, error) {
	var out []models.Credential
	for _, id := range ids {
		if c, ok := f.rows[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) MarkValid(_ context.Context, op, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.rows[id]
	if !ok || c.OperationID != op || c.Validity == models.CredentialValidityValid {
		return false, nil
	}
	c.Validity = models.CredentialValidityValid
	f.rows[id] = c
	f.valid = append(f.valid, id)
	return true, nil
}

// recordingBus keeps what is published.
type recordingBus struct {
	eventbus.IEventBus
	mu  sync.Mutex
	got []eventbus.Event
}

func (b *recordingBus) Publish(e eventbus.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, e)
}

type rig struct {
	store          *fakeStore
	r              *Resolver
	op, other      uuid.UUID
	key, pw, alien uuid.UUID
	events         *recordingBus
}

func newRig(t *testing.T) rig {
	t.Helper()
	g := rig{op: uuid.New(), other: uuid.New(), key: uuid.New(), pw: uuid.New(), alien: uuid.New()}
	g.store = &fakeStore{rows: map[uuid.UUID]models.Credential{
		g.key: {CredentialID: g.key, OperationID: g.op, Name: "root key", Type: models.CredentialTypeKey, Username: "root", Key: "KEY",
			Rev: 3, Validity: models.CredentialValidityUnknown,
			// Notes for people: not the module's.
			Properties: []models.CredentialProperty{{Name: "found on", Value: "web-01"}}, Tags: []string{"web"}},
		g.pw: {CredentialID: g.pw, OperationID: g.op, Name: "admin", Type: models.CredentialTypePlaintext, Username: "admin",
			Realm: "CORP", Password: "pw", Rev: 1, Validity: models.CredentialValidityValid,
			// A value in a field the type has not (edits crossing a change of type).
			Hash: "stray"},
		g.alien: {CredentialID: g.alien, OperationID: g.other, Name: "theirs", Type: models.CredentialTypeKey, Key: "K"},
	}}
	g.events = &recordingBus{IEventBus: eventbus.NewNopEventBus()}
	g.r = New(g.store, g.events)
	return g
}

var loginField = protocol.FieldSpec{Key: "credentials", Type: protocol.FieldCredentialList,
	Accept: []string{protocol.CredentialTypeKey}}

func TestCheck(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	fields := []protocol.FieldSpec{loginField}
	if p, err := g.r.Check(ctx, g.op, fields, map[string]any{"credentials": []any{g.key.String()}}); err != nil || p != nil {
		t.Fatalf("an operation's key: %v %v", p, err)
	}
	p, _ := g.r.Check(ctx, g.op, fields, map[string]any{"credentials": []any{g.alien.String()}})
	if !strings.Contains(p["credentials"], "not one of the operation's") {
		t.Errorf("another operation's: %v", p)
	}
	p, _ = g.r.Check(ctx, g.op, fields, map[string]any{"credentials": []any{g.key.String(), g.pw.String()}})
	if p["credentials"] != `credential "admin" is a PLAINTEXT; the field takes KEY` {
		t.Errorf("a type the field does not take: %v", p)
	}
	p, _ = g.r.Check(ctx, g.op, fields, map[string]any{"credentials": []any{"not-a-uuid"}})
	if !strings.Contains(p["credentials"], "not one of the operation's") {
		t.Errorf("not an id: %v", p)
	}
}

// What a module gets: the type's fields holding a value, no notes, nothing
// left in a field the type has not.
func TestResolveAndWire(t *testing.T) {
	g := newRig(t)
	got, revs, err := g.r.Resolve(context.Background(), g.op, []uuid.UUID{g.key, g.pw})
	if err != nil {
		t.Fatal(err)
	}
	k, p := got[g.key.String()], got[g.pw.String()]
	if !reflect.DeepEqual(k, protocol.Credential{ID: g.key.String(), Name: "root key", Type: "KEY", Username: "root", Key: "KEY"}) ||
		revs[g.key.String()] != 3 {
		t.Errorf("the key: %+v, rev %d", k, revs[g.key.String()])
	}
	if p.Hash != "" || p.Realm != "CORP" || p.Password != "pw" {
		t.Errorf("the password: %+v", p)
	}
	if _, _, err := g.r.Resolve(context.Background(), g.op, []uuid.UUID{g.alien}); err == nil {
		t.Error("another operation's credential resolved")
	}
	at := time.Date(2026, 11, 5, 18, 0, 0, 0, time.FixedZone("x", 3600))
	w := Wire(models.Credential{CredentialID: uuid.New(), Type: models.CredentialTypeTicket, Ticket: "doIF", ExpiresAt: &at})
	if w.ExpiresAt != "2026-11-05T17:00:00Z" || w.Validate() != nil {
		t.Errorf("a ticket: %+v %v", w, w.Validate())
	}
}

// ResolveFields checks each against its field: a credential converted since
// the values were saved is refused.
func TestResolveFields(t *testing.T) {
	g := newRig(t)
	values := map[string]any{"credentials": []any{g.pw.String()}}
	if _, err := g.r.ResolveFields(context.Background(), g.op, []protocol.FieldSpec{loginField}, values); err == nil ||
		!strings.Contains(err.Error(), "takes KEY") {
		t.Fatalf("converted: %v", err)
	}
	anyType := protocol.FieldSpec{Key: "credentials", Type: protocol.FieldCredentialList}
	if got, err := g.r.ResolveFields(context.Background(), g.op, []protocol.FieldSpec{anyType}, values); err != nil || len(got) != 1 {
		t.Fatalf("any type: %v %v", got, err)
	}
}

// A login that worked marks the credential VALID once, and says so as any
// credential update does; one VALID already, or another operation's, is no
// change.
func TestLoginWorked(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	for _, id := range []uuid.UUID{g.key, g.key, g.pw} {
		if err := g.r.LoginWorked(ctx, g.op, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.r.LoginWorked(ctx, g.op, g.alien); err != nil {
		t.Fatal(err)
	}
	got := g.events.got
	if len(g.store.valid) != 1 || g.store.valid[0] != g.key || len(got) != 1 || got[0].Actor.Type != eventbus.ActorSystem ||
		got[0].Topic != eventbus.TopicCredentialUpdated {
		t.Fatalf("marked %v, events %+v", g.store.valid, got)
	}
}

type fakeUsage struct {
	targets   []models.MinionTarget
	endpoints []models.ChannelEndpoint
	schemas   map[string]*protocol.ConnectionSchema
}

type targetsOf fakeUsage

func (f targetsOf) FindByCredentialID(context.Context, uuid.UUID, int64) ([]models.MinionTarget, error) {
	return f.targets, nil
}

type endpointsOf fakeUsage

func (f endpointsOf) FindByCredentialID(context.Context, uuid.UUID, int64) ([]models.ChannelEndpoint, error) {
	return f.endpoints, nil
}

type counted int64

func (c counted) CountByCredentialID(context.Context, uuid.UUID) (int64, error) { return int64(c), nil }

type schemasOf fakeUsage

func (f schemasOf) Schema(_ context.Context, module string) (*protocol.ConnectionSchema, error) {
	return f.schemas[module], nil
}

// Usage: each target and endpoint naming the credential with the types its
// fields take, the target's last login with it, and the history's counts.
func TestUsage(t *testing.T) {
	g := newRig(t)
	at := time.Now().UTC()
	f := fakeUsage{
		targets: []models.MinionTarget{{TargetID: uuid.New(), Name: "10.0.0.7", ModuleName: "ssh", Kind: protocol.KindBindCall,
			Values: models.JSONMap{"credentials": []any{g.key.String()}},
			Logins: map[string]models.TargetLogin{g.key.String(): {Accepted: true, At: at}}}},
		endpoints: []models.ChannelEndpoint{{EndpointID: uuid.New(), Name: "edge", ModuleName: "http", Kind: protocol.KindReverseCall,
			Values: models.JSONMap{"proxy_login": g.key.String()}}},
		schemas: map[string]*protocol.ConnectionSchema{
			"ssh": {Kinds: map[string]protocol.ConnectionKind{protocol.KindBindCall: {Target: []protocol.FieldSpec{
				{Key: "credentials", Type: protocol.FieldCredentialList, Accept: []string{protocol.CredentialTypePlaintext, protocol.CredentialTypeKey}}}}}},
			"http": {Kinds: map[string]protocol.ConnectionKind{protocol.KindReverseCall: {Fields: []protocol.FieldSpec{
				{Key: "proxy_login", Type: protocol.FieldCredential}}}}},
		},
	}
	g.r.WithUsage(UsageRepos{Targets: targetsOf(f), Endpoints: endpointsOf(f), Builds: counted(2), Tasks: counted(5), Schemas: schemasOf(f)})
	u, err := g.r.Usage(context.Background(), g.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Targets) != 1 || strings.Join(u.Targets[0].Accept, ",") != "PLAINTEXT,KEY" || u.Targets[0].Login == nil || !u.Targets[0].Login.Accepted {
		t.Errorf("targets %+v", u.Targets)
	}
	if len(u.Endpoints) != 1 || u.Endpoints[0].Accept != nil {
		t.Errorf("endpoints %+v: a field that takes any", u.Endpoints)
	}
	if u.Builds != 2 || u.Tasks != 5 {
		t.Errorf("builds %d, tasks %d", u.Builds, u.Tasks)
	}
}
