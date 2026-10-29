package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.uber.org/zap"
)

// fakeRepo is an in-memory IModuleRegistryRepository for handler tests.
type fakeRepo struct {
	rows map[string]*models.Module

	upsertErr    error
	heartbeatErr error
	deregErr     error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[string]*models.Module)}
}

func (f *fakeRepo) Upsert(_ context.Context, reg *models.Module) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	if row, ok := f.rows[reg.Instance]; ok && row.Status == models.ModuleStatusBlocked {
		return repository.ErrInstanceBlocked
	}
	reg.Status = models.ModuleStatusRegistered
	reg.DeregisteredAt = nil
	reg.DeclaredDeadAt = nil
	cp := *reg
	f.rows[reg.Instance] = &cp
	return nil
}

func (f *fakeRepo) RecordStatus(context.Context, string, map[string]any) error { return nil }

func (f *fakeRepo) Block(_ context.Context, instance, reason string, by models.ModuleActor, at time.Time) (bool, error) {
	row, ok := f.rows[instance]
	if !ok || row.Status == models.ModuleStatusBlocked {
		return false, nil
	}
	row.Status, row.BlockReason, row.BlockedBy, row.BlockedAt = models.ModuleStatusBlocked, reason, &by, &at
	return true, nil
}

func (f *fakeRepo) Unblock(_ context.Context, instance string, at time.Time) (bool, error) {
	row, ok := f.rows[instance]
	if !ok || row.Status != models.ModuleStatusBlocked {
		return false, nil
	}
	row.Status, row.DeregisteredAt, row.DeregisterReason = models.ModuleStatusDeregistered, &at, "unblocked"
	row.BlockReason, row.BlockedBy, row.BlockedAt = "", nil, nil
	return true, nil
}

func (f *fakeRepo) Delete(_ context.Context, instance string) (bool, error) {
	row, ok := f.rows[instance]
	if !ok || (row.Status != models.ModuleStatusDeregistered && row.Status != models.ModuleStatusDead) {
		return false, nil
	}
	delete(f.rows, instance)
	return true, nil
}

func (f *fakeRepo) TouchHeartbeat(_ context.Context, instance, status string, metrics map[string]any, applied *int64, at time.Time) (bool, error) {
	if f.heartbeatErr != nil {
		return false, f.heartbeatErr
	}
	row, ok := f.rows[instance]
	if !ok || row.Status != models.ModuleStatusRegistered {
		return false, nil
	}
	row.LastStatus = status
	row.LastMetrics = metrics
	row.LastHeartbeatAt = &at
	if applied != nil {
		row.AppliedRevision = *applied
	}
	return true, nil
}

func (f *fakeRepo) MarkDeregistered(_ context.Context, instance, reason string, at time.Time) (bool, error) {
	if f.deregErr != nil {
		return false, f.deregErr
	}
	row, ok := f.rows[instance]
	if !ok || row.Status != models.ModuleStatusRegistered {
		return false, nil
	}
	row.Status = models.ModuleStatusDeregistered
	row.DeregisterReason = reason
	row.DeregisteredAt = &at
	return true, nil
}

func (f *fakeRepo) FindStaleRegistered(_ context.Context, cutoff time.Time, limit int64) ([]models.Module, error) {
	var out []models.Module
	for _, row := range f.rows {
		if row.Status != models.ModuleStatusRegistered {
			continue
		}
		last := row.RegisteredAt
		if row.LastHeartbeatAt != nil {
			last = *row.LastHeartbeatAt
		}
		if last.Before(cutoff) {
			out = append(out, *row)
		}
	}
	return out, nil
}

func (f *fakeRepo) MarkDead(_ context.Context, instances []string, at time.Time) error {
	for _, inst := range instances {
		if row, ok := f.rows[inst]; ok && row.Status == models.ModuleStatusRegistered {
			row.Status = models.ModuleStatusDead
			row.DeclaredDeadAt = &at
		}
	}
	return nil
}

func (f *fakeRepo) FindByInstance(_ context.Context, instance string) (models.Module, error) {
	row, ok := f.rows[instance]
	if !ok {
		return models.Module{}, qmgo.ErrNoSuchDocuments
	}
	return *row, nil
}

func (f *fakeRepo) ListActive(_ context.Context) ([]models.Module, error) {
	var out []models.Module
	for _, row := range f.rows {
		if row.Status == models.ModuleStatusRegistered {
			out = append(out, *row)
		}
	}
	return out, nil
}

func (f *fakeRepo) FindWithCursor(_ context.Context, filter repository.ModuleFilter, _ *pagination.Cursor, _ int64, _ bool) ([]models.Module, error) {
	want := make(map[string]struct{}, len(filter.Statuses))
	for _, s := range filter.Statuses {
		want[s] = struct{}{}
	}
	var out []models.Module
	for _, row := range f.rows {
		if len(want) > 0 {
			if _, ok := want[row.Status]; !ok {
				continue
			}
		}
		out = append(out, *row)
	}
	return out, nil
}

func (f *fakeRepo) Count(ctx context.Context, filter repository.ModuleFilter) (int64, error) {
	rows, err := f.FindWithCursor(ctx, filter, nil, 0, true)
	return int64(len(rows)), err
}

// fakeEmitter records published events.
type fakeEmitter struct {
	events []emitted
	err    error
}

type emitted struct {
	routingKey string
	env        messaging.Envelope
}

func (e *fakeEmitter) Publish(_ context.Context, routingKey string, env messaging.Envelope) error {
	if e.err != nil {
		return e.err
	}
	e.events = append(e.events, emitted{routingKey: routingKey, env: env})
	return nil
}

func newTestService() (*Service, *fakeRepo, *fakeEmitter) {
	repo := newFakeRepo()
	emitter := &fakeEmitter{}
	svc := NewService(repo, emitter, nil, nil, Config{
		HeartbeatInterval:    30 * time.Second,
		HeartbeatGraceMisses: 3,
	}, zap.NewNop())
	return svc, repo, emitter
}

// fakeInvalidator records gate cache-busting calls.
type fakeInvalidator struct {
	instances []string
}

func (f *fakeInvalidator) Invalidate(_ context.Context, instance string) {
	f.instances = append(f.instances, instance)
}

// fakePublisher records events fanned out to the in-process event bus.
type fakePublisher struct {
	events []eventbus.Event
}

func (p *fakePublisher) Publish(ev eventbus.Event) {
	p.events = append(p.events, ev)
}

func envFor(t *testing.T, opType string, payload any) messaging.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return messaging.Envelope{Type: opType, Version: "1.0", Payload: raw}
}

func TestHandleRegister_NewInstance(t *testing.T) {
	svc, repo, emitter := newTestService()

	out, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
		ModuleType: "channel",
		ModuleName: "http",
		Instance:   "http-1",
		Version:    "1.2.0",
		RPCQueue:   "logos.channel.rpc.http-1",
	}))
	if err != nil {
		t.Fatalf("HandleRegister error: %v", err)
	}

	reply := out.(registerReply)
	if !reply.Registered || reply.Instance != "http-1" {
		t.Errorf("reply = %+v", reply)
	}
	if row := repo.rows["http-1"]; row == nil || row.Name != "http" {
		t.Errorf("module_name not persisted: %+v", row)
	}
	if reply.HeartbeatIntervalSeconds != 30 || reply.HeartbeatGraceMisses != 3 {
		t.Errorf("bootstrap config = %d/%d, want 30/3", reply.HeartbeatIntervalSeconds, reply.HeartbeatGraceMisses)
	}
	if reply.Config.Policy == nil || reply.Config.FeatureFlags == nil {
		t.Error("policy/feature_flags must serialize as objects, not null")
	}
	if _, ok := repo.rows["http-1"]; !ok {
		t.Error("registration not persisted")
	}
	if len(emitter.events) != 1 || emitter.events[0].routingKey != "channel.http-1.registered" {
		t.Errorf("events = %+v, want one channel.http-1.registered", emitter.events)
	}
}

func TestHandleRegister_Icon(t *testing.T) {
	svc, repo, _ := newTestService()
	const icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/></svg>`
	register := func(instance, icon string) *models.Module {
		t.Helper()
		_, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
			ModuleType: "channel", ModuleName: "http", Instance: instance, RPCQueue: "q." + instance, Icon: icon,
		}))
		if err != nil {
			t.Fatalf("HandleRegister: %v", err)
		}
		return repo.rows[instance]
	}
	if row := register("http-1", icon); row.Icon != icon {
		t.Errorf("icon = %q, want the one sent", row.Icon)
	}
	// A broken icon does not cost the registration; the UI shows the default.
	if row := register("http-2", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`); row == nil || row.Icon != "" {
		t.Errorf("row = %+v, want registered without an icon", row)
	}
}

func TestHandleRegister_IconLight(t *testing.T) {
	svc, repo, _ := newTestService()
	const icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/></svg>`
	const light = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="5"/></svg>`
	const broken = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	register := func(instance, icon, light string) *models.Module {
		t.Helper()
		_, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
			ModuleType: "channel", ModuleName: "http", Instance: instance, RPCQueue: "q." + instance,
			Icon: icon, IconLight: light,
		}))
		if err != nil {
			t.Fatalf("HandleRegister: %v", err)
		}
		return repo.rows[instance]
	}
	if row := register("http-1", icon, light); row.Icon != icon || row.IconLight != light {
		t.Errorf("icons = %q / %q, want the ones sent", row.Icon, row.IconLight)
	}
	// Each refused alone; the registration and the other icon stand.
	if row := register("http-2", icon, broken); row == nil || row.Icon != icon || row.IconLight != "" {
		t.Errorf("row = %+v, want the icon without the broken light one", row)
	}
	// Without an icon, a light one would leave dark themes on the default.
	if row := register("http-3", "", light); row == nil || row.Icon != "" || row.IconLight != "" {
		t.Errorf("row = %+v, want registered without icons", row)
	}
	if row := register("http-4", broken, light); row == nil || row.Icon != "" || row.IconLight != "" {
		t.Errorf("row = %+v, want registered without icons", row)
	}
}

func TestHandleRegister_IdempotentTakeoverRevivesDead(t *testing.T) {
	svc, repo, _ := newTestService()
	dead := time.Now().Add(-time.Hour)
	repo.rows["http-1"] = &models.Module{
		Type: "channel", Instance: "http-1", Status: models.ModuleStatusDead,
		DeclaredDeadAt: &dead,
	}

	_, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
		ModuleType: "channel", ModuleName: "http", Instance: "http-1", RPCQueue: "q",
	}))
	if err != nil {
		t.Fatalf("takeover register error: %v", err)
	}
	row := repo.rows["http-1"]
	if row.Status != models.ModuleStatusRegistered {
		t.Errorf("status = %q, want registered after takeover", row.Status)
	}
	if row.DeclaredDeadAt != nil {
		t.Error("declared_dead_at should be cleared on takeover")
	}
}

func TestHandleRegister_ValidationFailed(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
		ModuleType: "channel", // missing instance + rpc_queue
	}))
	assertRPCCode(t, err, messaging.CodeValidationFailed)
}

func TestHandleHeartbeat_OK(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.rows["http-1"] = &models.Module{
		Instance: "http-1", Status: models.ModuleStatusRegistered, RegisteredAt: time.Now(),
	}

	out, err := svc.HandleHeartbeat(context.Background(), envFor(t, OpHeartbeat, heartbeatRequest{
		Instance: "http-1", Status: "healthy",
	}))
	if err != nil {
		t.Fatalf("heartbeat error: %v", err)
	}
	reply := out.(heartbeatReply)
	if !reply.Ack || reply.ConfigChanged {
		t.Errorf("reply = %+v", reply)
	}
	if repo.rows["http-1"].LastHeartbeatAt == nil {
		t.Error("last_heartbeat_at not updated")
	}
}

func TestHandleHeartbeat_UnknownInstance(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.HandleHeartbeat(context.Background(), envFor(t, OpHeartbeat, heartbeatRequest{Instance: "ghost"}))
	assertRPCCode(t, err, messaging.CodeUnknownInstance)
}

func TestHandleHeartbeat_DeadInstanceIsUnknown(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.rows["http-1"] = &models.Module{Instance: "http-1", Status: models.ModuleStatusDead}
	_, err := svc.HandleHeartbeat(context.Background(), envFor(t, OpHeartbeat, heartbeatRequest{Instance: "http-1"}))
	assertRPCCode(t, err, messaging.CodeUnknownInstance)
}

func TestHandleDeregister_OK(t *testing.T) {
	svc, repo, emitter := newTestService()
	repo.rows["http-1"] = &models.Module{
		Type: "channel", Instance: "http-1", Status: models.ModuleStatusRegistered,
	}

	out, err := svc.HandleDeregister(context.Background(), envFor(t, OpDeregister, deregisterRequest{
		Instance: "http-1", Reason: "shutdown",
	}))
	if err != nil {
		t.Fatalf("deregister error: %v", err)
	}
	reply := out.(deregisterReply)
	if !reply.Deregistered {
		t.Errorf("reply = %+v", reply)
	}
	if repo.rows["http-1"].Status != models.ModuleStatusDeregistered {
		t.Error("status not set to deregistered")
	}
	if len(emitter.events) != 1 || emitter.events[0].routingKey != "channel.http-1.deregistered" {
		t.Errorf("events = %+v, want channel.http-1.deregistered", emitter.events)
	}
}

func TestHandleDeregister_UnknownInstance(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.HandleDeregister(context.Background(), envFor(t, OpDeregister, deregisterRequest{Instance: "ghost"}))
	assertRPCCode(t, err, messaging.CodeUnknownInstance)
}

func TestHandleDeregister_InvalidatesGate(t *testing.T) {
	repo := newFakeRepo()
	inv := &fakeInvalidator{}
	svc := NewService(repo, &fakeEmitter{}, inv, nil, Config{}, zap.NewNop())
	repo.rows["http-1"] = &models.Module{
		Type: "channel", Instance: "http-1", Status: models.ModuleStatusRegistered,
	}

	if _, err := svc.HandleDeregister(context.Background(), envFor(t, OpDeregister, deregisterRequest{
		Instance: "http-1", Reason: "shutdown",
	})); err != nil {
		t.Fatalf("deregister error: %v", err)
	}
	if len(inv.instances) != 1 || inv.instances[0] != "http-1" {
		t.Errorf("invalidated = %v, want [http-1]", inv.instances)
	}

	// An unknown instance must not bust the cache.
	inv.instances = nil
	_, _ = svc.HandleDeregister(context.Background(), envFor(t, OpDeregister, deregisterRequest{Instance: "ghost"}))
	if len(inv.instances) != 0 {
		t.Errorf("invalidated on unknown instance = %v, want none", inv.instances)
	}
}

func TestHandleRegister_PublishesBusEvent(t *testing.T) {
	repo := newFakeRepo()
	pub := &fakePublisher{}
	svc := NewService(repo, &fakeEmitter{}, nil, pub, Config{}, zap.NewNop())

	if _, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
		ModuleType: "channel", ModuleName: "http", Instance: "http-1", RPCQueue: "q",
	})); err != nil {
		t.Fatalf("register error: %v", err)
	}

	if len(pub.events) != 1 {
		t.Fatalf("published %d bus events, want 1", len(pub.events))
	}
	ev := pub.events[0]
	if ev.Topic != eventbus.TopicModuleRegistered {
		t.Errorf("topic = %q, want %q", ev.Topic, eventbus.TopicModuleRegistered)
	}
	p, ok := ev.Payload.(eventbus.ModuleEventPayload)
	if !ok {
		t.Fatalf("payload type = %T, want ModuleEventPayload", ev.Payload)
	}
	if p.Instance != "http-1" || p.Type != "channel" || p.Status != models.ModuleStatusRegistered {
		t.Errorf("payload = %+v", p)
	}
}

func TestHandleHeartbeat_PublishesUpdatedEvent(t *testing.T) {
	repo := newFakeRepo()
	repo.rows["http-1"] = &models.Module{Instance: "http-1", Status: models.ModuleStatusRegistered}
	pub := &fakePublisher{}
	svc := NewService(repo, &fakeEmitter{}, nil, pub, Config{}, zap.NewNop())

	if _, err := svc.HandleHeartbeat(context.Background(), envFor(t, OpHeartbeat, heartbeatRequest{
		Instance: "http-1", Status: "healthy",
	})); err != nil {
		t.Fatalf("heartbeat error: %v", err)
	}
	if len(pub.events) != 1 || pub.events[0].Topic != eventbus.TopicModuleUpdated {
		t.Fatalf("events = %+v, want one %q", pub.events, eventbus.TopicModuleUpdated)
	}
	if p, ok := pub.events[0].Payload.(eventbus.ModuleEventPayload); !ok || p.Instance != "http-1" {
		t.Errorf("payload = %+v", pub.events[0].Payload)
	}

	// A rejected heartbeat (unknown instance) publishes nothing.
	pub.events = nil
	_, _ = svc.HandleHeartbeat(context.Background(), envFor(t, OpHeartbeat, heartbeatRequest{Instance: "ghost"}))
	if len(pub.events) != 0 {
		t.Errorf("unknown-instance heartbeat published %d events", len(pub.events))
	}
}

func TestHandleDeregister_PublishesBusEvent(t *testing.T) {
	repo := newFakeRepo()
	pub := &fakePublisher{}
	svc := NewService(repo, &fakeEmitter{}, nil, pub, Config{}, zap.NewNop())
	repo.rows["http-1"] = &models.Module{
		Type: "channel", Instance: "http-1", Status: models.ModuleStatusRegistered,
	}

	if _, err := svc.HandleDeregister(context.Background(), envFor(t, OpDeregister, deregisterRequest{
		Instance: "http-1", Reason: "shutdown",
	})); err != nil {
		t.Fatalf("deregister error: %v", err)
	}

	if len(pub.events) != 1 {
		t.Fatalf("published %d bus events, want 1", len(pub.events))
	}
	ev := pub.events[0]
	if ev.Topic != eventbus.TopicModuleDeregistered {
		t.Errorf("topic = %q, want %q", ev.Topic, eventbus.TopicModuleDeregistered)
	}
	p, _ := ev.Payload.(eventbus.ModuleEventPayload)
	if p.Instance != "http-1" || p.Status != models.ModuleStatusDeregistered {
		t.Errorf("payload = %+v", p)
	}

	// An unknown instance must not publish.
	pub.events = nil
	_, _ = svc.HandleDeregister(context.Background(), envFor(t, OpDeregister, deregisterRequest{Instance: "ghost"}))
	if len(pub.events) != 0 {
		t.Errorf("published on unknown instance = %+v, want none", pub.events)
	}
}

func TestDeregister_UsesProvidedActorAndBustsGate(t *testing.T) {
	repo := newFakeRepo()
	inv := &fakeInvalidator{}
	pub := &fakePublisher{}
	svc := NewService(repo, &fakeEmitter{}, inv, pub, Config{}, zap.NewNop())
	repo.rows["http-1"] = &models.Module{
		Type: "channel", Instance: "http-1", Status: models.ModuleStatusRegistered,
	}

	// The GraphQL removeModule path passes a user actor.
	found, err := svc.Deregister(context.Background(), "http-1", "removed by admin",
		eventbus.UserActor("admin-uid"))
	if err != nil || !found {
		t.Fatalf("Deregister found=%v err=%v, want true/nil", found, err)
	}
	if repo.rows["http-1"].Status != models.ModuleStatusDeregistered {
		t.Error("status not set to deregistered")
	}
	if len(inv.instances) != 1 || inv.instances[0] != "http-1" {
		t.Errorf("gate invalidated = %v, want [http-1]", inv.instances)
	}
	if len(pub.events) != 1 {
		t.Fatalf("published %d bus events, want 1", len(pub.events))
	}
	if pub.events[0].Actor.Type != eventbus.ActorUser || pub.events[0].Actor.ID != "admin-uid" {
		t.Errorf("event actor = %+v, want user/admin-uid", pub.events[0].Actor)
	}

	// Unknown instance → not found, no error, nothing fanned out.
	inv.instances, pub.events = nil, nil
	found, err = svc.Deregister(context.Background(), "ghost", "x", eventbus.SystemActor())
	if err != nil || found {
		t.Errorf("Deregister(ghost) found=%v err=%v, want false/nil", found, err)
	}
	if len(inv.instances) != 0 || len(pub.events) != 0 {
		t.Errorf("unknown instance fanned out: inv=%v pub=%v", inv.instances, pub.events)
	}
}

func TestHandleRegister_PersistErrorIsInternal(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.upsertErr = errors.New("mongo down")
	_, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
		ModuleType: "channel", ModuleName: "http", Instance: "http-1", RPCQueue: "q",
	}))
	// Not an RPCError → server maps to internal_error.
	var rpcErr *messaging.RPCError
	if errors.As(err, &rpcErr) {
		t.Fatalf("expected a plain error (→ internal_error), got RPCError %v", rpcErr)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func assertRPCCode(t *testing.T, err error, code string) {
	t.Helper()
	var rpcErr *messaging.RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *messaging.RPCError with code %q, got %v", code, err)
	}
	if rpcErr.Code != code {
		t.Fatalf("error code = %q, want %q", rpcErr.Code, code)
	}
}

func (f *fakeRepo) RecordApplyResult(context.Context, string, protocol.ConfigApplyReply, time.Time) error {
	return nil
}
func (f *fakeRepo) RecordApplyError(context.Context, string, string, time.Time) error { return nil }

func (f *fakeRepo) LatestConnectionSchema(context.Context, string) (*models.ModuleConnectionSchema, error) {
	return nil, nil
}

func (f *fakeRepo) LatestConnectionSchemas(context.Context) (map[string]*models.ModuleConnectionSchema, error) {
	return nil, nil
}

func (f *fakeRepo) LatestBuildSchema(context.Context, string) (*models.ModuleBuildSchema, error) {
	return nil, nil
}

func (f *fakeRepo) LatestIcons(context.Context, string) (map[string]models.ModuleIcon, error) {
	return nil, nil
}

func (f *fakeRepo) LatestBuildSchemas(context.Context) (map[string]*models.ModuleBuildSchema, error) {
	return nil, nil
}

func TestBlockUnblockDelete(t *testing.T) {
	repo := newFakeRepo()
	emitter := &fakeEmitter{}
	inv := &fakeInvalidator{}
	pub := &fakePublisher{}
	svc := NewService(repo, emitter, inv, pub, Config{HeartbeatInterval: 30 * time.Second, HeartbeatGraceMisses: 3}, zap.NewNop())
	ctx := context.Background()
	register := func() error {
		_, err := svc.HandleRegister(ctx, envFor(t, OpRegister, registerRequest{
			ModuleType: "channel", ModuleName: "http", Instance: "http-1", RPCQueue: "q.http-1",
		}))
		return err
	}
	if err := register(); err != nil {
		t.Fatal(err)
	}
	ana := models.ModuleActor{Type: "user", ID: "u-1", Name: "ana"}

	// Blocking a running instance cuts it off at once.
	if ok, err := svc.Block(ctx, "http-1", "not ours", ana, eventbus.UserActor("u-1")); !ok || err != nil {
		t.Fatalf("Block: %v %v", ok, err)
	}
	row := repo.rows["http-1"]
	if row.Status != models.ModuleStatusBlocked || row.BlockReason != "not ours" || row.BlockedBy == nil || row.BlockedBy.Name != "ana" || row.BlockedAt == nil {
		t.Fatalf("row = %+v", row)
	}
	if len(inv.instances) != 1 || inv.instances[0] != "http-1" {
		t.Errorf("gate not busted: %v", inv.instances)
	}
	if last := emitter.events[len(emitter.events)-1]; last.routingKey != "channel.http-1.blocked" {
		t.Errorf("audit event %q", last.routingKey)
	}
	if last := pub.events[len(pub.events)-1]; last.Topic != eventbus.TopicModuleBlocked {
		t.Errorf("bus event %s", last.Topic)
	}
	// It can neither come back nor heartbeat.
	assertRPCCode(t, register(), messaging.CodeInstanceBlocked)
	_, err := svc.HandleHeartbeat(ctx, envFor(t, OpHeartbeat, heartbeatRequest{Instance: "http-1"}))
	assertRPCCode(t, err, messaging.CodeUnknownInstance)
	if ok, _ := svc.Block(ctx, "http-1", "", ana, eventbus.UserActor("u-1")); ok {
		t.Error("blocked twice")
	}
	if ok, _ := svc.Delete(ctx, "http-1", eventbus.UserActor("u-1")); ok {
		t.Error("a blocked instance's row deleted")
	}

	// Unblocked, it registers again.
	if ok, err := svc.Unblock(ctx, "http-1", eventbus.UserActor("u-1")); !ok || err != nil {
		t.Fatalf("Unblock: %v %v", ok, err)
	}
	if row := repo.rows["http-1"]; row.Status != models.ModuleStatusDeregistered || row.BlockedBy != nil {
		t.Fatalf("row = %+v", row)
	}
	if last := pub.events[len(pub.events)-1]; last.Topic != eventbus.TopicModuleUnblocked {
		t.Errorf("bus event %s", last.Topic)
	}
	if err := register(); err != nil {
		t.Fatalf("register after unblock: %v", err)
	}
	if ok, _ := svc.Unblock(ctx, "http-1", eventbus.UserActor("u-1")); ok {
		t.Error("unblocked an instance that is not blocked")
	}

	// A registered instance's row stays; a gone one's goes.
	if ok, _ := svc.Delete(ctx, "http-1", eventbus.UserActor("u-1")); ok {
		t.Error("a registered instance's row deleted")
	}
	if _, err := svc.HandleDeregister(ctx, envFor(t, OpDeregister, deregisterRequest{Instance: "http-1"})); err != nil {
		t.Fatal(err)
	}
	if ok, err := svc.Delete(ctx, "http-1", eventbus.UserActor("u-1")); !ok || err != nil {
		t.Fatalf("Delete: %v %v", ok, err)
	}
	if _, ok := repo.rows["http-1"]; ok {
		t.Error("row still there")
	}
	if last := emitter.events[len(emitter.events)-1]; last.routingKey != "channel.http-1.deleted" {
		t.Errorf("audit event %q", last.routingKey)
	}
	if last := pub.events[len(pub.events)-1]; last.Topic != eventbus.TopicModuleDeleted {
		t.Errorf("bus event %s", last.Topic)
	}
	if ok, _ := svc.Block(ctx, "ghost", "", ana, eventbus.UserActor("u-1")); ok {
		t.Error("blocked an unknown instance")
	}
}

func TestHandleRegister_CryptoSuite(t *testing.T) {
	schema := func(suite string) *protocol.BuildSchema {
		return &protocol.BuildSchema{
			SchemaVersion: protocol.BuildSchemaV1, Version: 1, Crypto: suite,
			Channels: []protocol.BuildChannel{{ChannelVersions: protocol.ChannelVersions{ModuleName: "http", Versions: []int{2}}}},
			Targets:  []protocol.BuildTarget{{ID: "linux-elf", Label: "Linux binary", Format: "elf", Platforms: []string{"linux/amd64"}}},
		}
	}
	register := func(suite string) error {
		svc, _, _ := newTestService()
		_, err := svc.HandleRegister(context.Background(), envFor(t, OpRegister, registerRequest{
			ModuleType: "factory", ModuleName: "bob", Instance: "bob-1", Version: "1.0.0",
			RPCQueue: "logos.factory.rpc.bob-1", Build: schema(suite),
		}))
		return err
	}
	if err := register(protocol.CryptoAES256CBCHMACSHA256); err != nil {
		t.Fatalf("supported suite: %v", err)
	}
	if err := register(""); err != nil {
		t.Fatalf("no suite: %v", err)
	}
	assertRPCCode(t, register("x25519-xchacha20poly1305"), messaging.CodeValidationFailed)
}
