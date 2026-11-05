package miniontargets

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

type fakeTargets struct {
	repository.IMinionTargetRepository
	mu   sync.Mutex
	rows map[uuid.UUID]models.MinionTarget
}

func (f *fakeTargets) Insert(_ context.Context, t *models.MinionTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.TargetID = uuid.New()
	f.rows[t.TargetID] = *t
	return nil
}

func (f *fakeTargets) FindByTargetID(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	return &t, nil
}

func (f *fakeTargets) SetValues(_ context.Context, id uuid.UUID, v models.JSONMap, creds []uuid.UUID, name string, retries bool, policy string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.rows[id]
	t.Values, t.CredentialIDs, t.Name, t.Retries = v, creds, name, retries
	if policy != "" {
		t.Policy = policy
	}
	f.rows[id] = t
	return nil
}

func (f *fakeTargets) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.rows[id]
	delete(f.rows, id)
	return ok, nil
}

func (f *fakeTargets) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.rows {
		if t.SessionID != nil && *t.SessionID == id {
			return &t, nil
		}
	}
	return nil, qmgo.ErrNoSuchDocuments
}

// fakeSessions are sessions by id; closing one is recorded, which no
// target change should do.
type fakeSessions struct {
	repository.IMinionSessionRepository
	rows   map[uuid.UUID]*models.MinionSession
	closed []uuid.UUID
}

func (f *fakeSessions) FindBySessionID(_ context.Context, id uuid.UUID) (*models.MinionSession, error) {
	if s, ok := f.rows[id]; ok {
		return s, nil
	}
	return nil, qmgo.ErrNoSuchDocuments
}

func (f *fakeSessions) Close(_ context.Context, id uuid.UUID, _ string, _ time.Time) error {
	f.closed = append(f.closed, id)
	return nil
}

type fakeBuilds map[uuid.UUID]*models.Build

func (f fakeBuilds) FindByBuildID(_ context.Context, id uuid.UUID) (*models.Build, error) {
	if b, ok := f[id]; ok {
		return b, nil
	}
	return nil, qmgo.ErrNoSuchDocuments
}

type schemas map[string]*protocol.ConnectionSchema

func (s schemas) Schema(_ context.Context, m string) (*protocol.ConnectionSchema, error) {
	return s[m], nil
}

type caller struct{ calls, disconnects chan uuid.UUID }

func (c caller) CheckTarget(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	c.calls <- id
	return nil, nil
}

func (c caller) ConnectTarget(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	c.calls <- id
	return nil, nil
}

func (c caller) DisconnectTarget(_ context.Context, id uuid.UUID) (*models.MinionTarget, error) {
	c.disconnects <- id
	return nil, nil
}

func setup() (*Service, *fakeTargets, *fakeSessions, *models.Build, caller) {
	dumbo := &protocol.ConnectionSchema{SchemaVersion: protocol.ConnectionSchemaV2, Version: 1, Kinds: map[string]protocol.ConnectionKind{
		protocol.KindBindCall: {
			Fields: []protocol.FieldSpec{{Key: "method", Type: protocol.FieldString, Label: "Method"}},
			Target: []protocol.FieldSpec{
				{Key: "url", Type: protocol.FieldURL, Label: "URL", Required: true},
				{Key: "tls_verify", Type: protocol.FieldBool, Label: "Verify", Default: false},
			},
		},
	}}
	b := &models.Build{BuildID: uuid.New(), OperationID: uuid.New(), State: models.BuildStateSucceeded,
		Connections: models.BuildConnections{Items: []protocol.BuildConnection{
			{Endpoint: protocol.ChannelEndpointExport{Name: "http", Kind: protocol.KindReverseCall, Channel: protocol.ExportChannel{ModuleName: "http"}}},
			{Endpoint: protocol.ChannelEndpointExport{Name: "page", Kind: protocol.KindBindCall, Channel: protocol.ExportChannel{ModuleName: "dumbo"}}},
		}}}
	targets := &fakeTargets{rows: map[uuid.UUID]models.MinionTarget{}}
	sessions := &fakeSessions{rows: map[uuid.UUID]*models.MinionSession{}}
	c := caller{calls: make(chan uuid.UUID, 4), disconnects: make(chan uuid.UUID, 4)}
	return NewService(targets, sessions, fakeBuilds{b.BuildID: b}, schemas{"dumbo": dumbo}, c, eventbus.NopEventBus{}), targets, sessions, b, c
}

func TestAdd(t *testing.T) {
	s, targets, _, b, c := setup()
	ctx := context.Background()
	conns, err := s.BindConnections(ctx, b)
	if err != nil || len(conns) != 1 || conns[0].Place != 2 || len(conns[0].Fields) != 2 || conns[0].Problem != "" {
		t.Fatalf("bind connections %+v %v", conns, err)
	}
	tg, err := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h/a.php", "tls_verify": ""}, Retries: true})
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetNew || tg.ModuleName != "dumbo" || tg.Kind != protocol.KindBindCall || tg.Name != "https://h/a.php" ||
		tg.Values["tls_verify"] != false {
		t.Fatalf("%+v", tg)
	}
	select {
	case id := <-c.calls:
		if id != tg.TargetID {
			t.Fatal("another target called")
		}
	case <-time.After(time.Second):
		t.Fatal("not called at once")
	}
	if len(targets.rows) != 1 {
		t.Fatal("not stored")
	}
	// Refused: a reverse connection, no such connection, values that do not
	// fit, a build that did not succeed.
	var invalid *ValidationError
	if _, err := s.Add(ctx, b, 1, nil, Input{Values: map[string]any{"url": "https://h"}}); !errors.As(err, &invalid) {
		t.Errorf("a reverse connection: %v", err)
	}
	if _, err := s.Add(ctx, b, 3, nil, Input{Values: map[string]any{"url": "https://h"}}); !errors.As(err, &invalid) {
		t.Errorf("no connection 3: %v", err)
	}
	if _, err := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"method": "GET"}}); !errors.As(err, &invalid) || invalid.Problems["url"] == "" ||
		invalid.Problems["method"] == "" {
		t.Errorf("bad values: %v %+v", err, invalid)
	}
	b.State = models.BuildStateFailed
	if _, err := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h"}}); !errors.As(err, &invalid) {
		t.Errorf("a failed build: %v", err)
	}
}

func TestUpdateAndRemove(t *testing.T) {
	s, targets, sessions, b, c := setup()
	ctx := context.Background()
	tg, _ := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h/a.php"}, Retries: true})
	<-c.calls
	if _, err := s.Update(ctx, tg.TargetID, Input{Values: map[string]any{"url": "https://h/b.php"}}); err != nil {
		t.Fatal(err)
	}
	<-c.calls
	if got := targets.rows[tg.TargetID]; got.Name != "https://h/b.php" || got.Retries {
		t.Fatalf("%+v", got)
	}
	sid := uuid.New()
	row := targets.rows[tg.TargetID]
	row.SessionID = &sid
	targets.rows[tg.TargetID] = row
	if err := s.Remove(ctx, tg.TargetID); err != nil {
		t.Fatal(err)
	}
	// The target goes; its session stays, history and all.
	if len(targets.rows) != 0 || len(sessions.closed) != 0 {
		t.Fatalf("removed: %d targets, closed %v", len(targets.rows), sessions.closed)
	}
	if err := s.Remove(ctx, tg.TargetID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("again: %v", err)
	}
	if len(c.disconnects) != 0 {
		t.Fatal("a bind-call target disconnected")
	}
	// A bind-persistent target's connection closes with it.
	tg, _ = s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h/a.php"}})
	<-c.calls
	row = targets.rows[tg.TargetID]
	row.Kind = protocol.KindBindPersistent
	targets.rows[tg.TargetID] = row
	if err := s.Remove(ctx, tg.TargetID); err != nil || len(c.disconnects) != 1 || <-c.disconnects != tg.TargetID {
		t.Fatalf("a persistent target removed: %v", err)
	}
}

// A target added to a session whose target was removed takes it over: the
// session's own build and connection, open, with no target now.
func TestTakeOver(t *testing.T) {
	s, targets, sessions, b, c := setup()
	ctx := context.Background()
	sid := uuid.New()
	sessions.rows[sid] = &models.MinionSession{SessionID: sid, BuildID: b.BuildID, OperationID: b.OperationID, Conn: 2, CreatedAt: time.Now()}
	in := Input{Values: map[string]any{"url": "https://h/moved.php"}, Retries: true}
	tg, err := s.Add(ctx, b, 2, &sid, in)
	if err != nil {
		t.Fatal(err)
	}
	<-c.calls
	if tg.SessionID == nil || *tg.SessionID != sid || targets.rows[tg.TargetID].SessionID == nil {
		t.Fatalf("not linked: %+v", tg)
	}

	var invalid *ValidationError
	refused := func(name string, session uuid.UUID, conn int) {
		t.Helper()
		if _, err := s.Add(ctx, b, conn, &session, in); !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	refused("a session with a target", sid, 2)
	other := uuid.New()
	refused("no such session", other, 2)
	sessions.rows[other] = &models.MinionSession{SessionID: other, BuildID: uuid.New(), OperationID: b.OperationID, Conn: 2, CreatedAt: time.Now()}
	refused("another build's session", other, 2)
	sessions.rows[other] = &models.MinionSession{SessionID: other, BuildID: b.BuildID, OperationID: b.OperationID, Conn: 1, CreatedAt: time.Now()}
	refused("another connection's", other, 2)
	closed := time.Now().Add(-time.Minute)
	sessions.rows[other] = &models.MinionSession{SessionID: other, BuildID: b.BuildID, OperationID: b.OperationID, Conn: 2, ClosedAt: &closed}
	refused("a closed session", other, 2)
}

// A target's values are checked against the channel's target fields as
// they are now: a field it now requires, one it dropped, no channel.
func TestProblems(t *testing.T) {
	s, _, _, b, c := setup()
	ctx := context.Background()
	tg, err := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h/a.php"}, Retries: true})
	if err != nil {
		t.Fatal(err)
	}
	<-c.calls
	if p, err := s.Problems(ctx, tg); err != nil || len(p) != 0 {
		t.Fatalf("fits: %v %v", p, err)
	}
	k := s.schemas.(schemas)["dumbo"].Kinds[protocol.KindBindCall]
	k.Target = append(k.Target, protocol.FieldSpec{Key: "token", Type: protocol.FieldString, Label: "Token", Required: true})
	s.schemas.(schemas)["dumbo"].Kinds[protocol.KindBindCall] = k
	if p, _ := s.Problems(ctx, tg); p["token"] == "" {
		t.Fatalf("a newly required field: %v", p)
	}
	k.Target = k.Target[:1] // url only: tls_verify, set by its default, is gone
	s.schemas.(schemas)["dumbo"].Kinds[protocol.KindBindCall] = k
	if p, _ := s.Problems(ctx, tg); p["tls_verify"] == "" {
		t.Fatalf("a dropped field: %v", p)
	}
	delete(s.schemas.(schemas), "dumbo")
	if p, _ := s.Problems(ctx, tg); p[""] == "" {
		t.Fatalf("no channel: %v", p)
	}
	if got := ProblemsText(map[string]string{"": "whole", "b": "two", "a": "one"}); got != "whole; a: one; b: two" {
		t.Fatalf("text %q", got)
	}
}

// fakeCreds says one credential is the operation's, of type KEY.
type fakeCreds struct{ ok uuid.UUID }

func (f fakeCreds) Check(_ context.Context, _ uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]string, error) {
	for _, field := range fields {
		for _, id := range protocol.FieldCredentialIDs(field, values[field.Key]) {
			if id != f.ok.String() {
				return map[string]string{field.Key: "credential " + id + " is not one of the operation's"}, nil
			}
		}
	}
	return nil, nil
}

// A target's credentials are checked when it is saved, and before each call
// (Problems): one deleted since leaves it needing an edit.
func TestTargetCredentials(t *testing.T) {
	s, targets, _, b, _ := setup()
	key := uuid.New()
	schema := s.schemas.(schemas)["dumbo"]
	k := schema.Kinds[protocol.KindBindCall]
	k.Target = append(k.Target, protocol.FieldSpec{Key: "logins", Type: protocol.FieldCredentialList, Label: "Logins"})
	schema.Kinds[protocol.KindBindCall] = k
	s.WithCredentials(fakeCreds{ok: key})
	ctx := context.Background()

	var invalid *ValidationError
	other := uuid.New().String()
	if _, err := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h/a.php", "logins": []any{other}}}); !errors.As(err, &invalid) ||
		invalid.Problems["logins"] == "" {
		t.Fatalf("another operation's credential: %v", err)
	}
	tg, err := s.Add(ctx, b, 2, nil, Input{Values: map[string]any{"url": "https://h/a.php", "logins": []any{key.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	if row := targets.rows[tg.TargetID]; len(row.CredentialIDs) != 1 || row.CredentialIDs[0] != key {
		t.Fatalf("credential ids %v", row.CredentialIDs)
	}
	if _, err := s.Update(ctx, tg.TargetID, Input{Values: map[string]any{"url": "https://h/a.php"}}); err != nil {
		t.Fatal(err)
	}
	if row := targets.rows[tg.TargetID]; len(row.CredentialIDs) != 0 {
		t.Fatalf("credential ids after none: %v", row.CredentialIDs)
	}

	row := targets.rows[tg.TargetID]
	row.Values = models.JSONMap{"url": "https://h/a.php", "logins": []any{other}}
	if p, err := s.Problems(ctx, &row); err != nil || p["logins"] == "" {
		t.Fatalf("a credential gone since: %v %v", p, err)
	}
}
