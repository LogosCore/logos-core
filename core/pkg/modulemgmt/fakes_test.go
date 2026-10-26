package modulemgmt

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	"go.uber.org/zap"
)

// --- registry ---

type fakeRegistry struct {
	mu        sync.Mutex
	rows      map[string]*models.Module
	applied   []protocol.ConfigApplyReply
	applyErrs []string
}

func newFakeRegistry(mods ...models.Module) *fakeRegistry {
	r := &fakeRegistry{rows: map[string]*models.Module{}}
	for i := range mods {
		m := mods[i]
		r.rows[m.Instance] = &m
	}
	return r
}

func (r *fakeRegistry) FindByInstance(_ context.Context, instance string) (models.Module, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.rows[instance]
	if !ok {
		return models.Module{}, qmgo.ErrNoSuchDocuments
	}
	return *m, nil
}

func (r *fakeRegistry) RecordApplyResult(_ context.Context, instance string, reply protocol.ConfigApplyReply, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied = append(r.applied, reply)
	if m, ok := r.rows[instance]; ok {
		m.AppliedRevision = reply.AppliedRevision
		m.Rejected = reply.Rejected
		m.RestartRequired = reply.RestartRequired
		m.LastApplyError = ""
		m.LastApplyAt = &at
	}
	return nil
}

func (r *fakeRegistry) RecordApplyError(_ context.Context, instance, msg string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyErrs = append(r.applyErrs, msg)
	if m, ok := r.rows[instance]; ok {
		m.LastApplyError = msg
		m.LastApplyAt = &at
	}
	return nil
}

func (r *fakeRegistry) RecordStatus(_ context.Context, instance string, status map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.rows[instance]; ok {
		m.LastMetrics = status
	}
	return nil
}

func (r *fakeRegistry) row(instance string) models.Module {
	r.mu.Lock()
	defer r.mu.Unlock()
	return *r.rows[instance]
}

// --- config store, with the repository's compare-and-set semantics ---

type fakeConfigs struct {
	mu   sync.Mutex
	docs map[string]*models.ModuleConfig // by instance
	// raceOnApply simulates another writer winning between read and write.
	raceOnApply bool
	applies     int
	raises      int
}

func newFakeConfigs() *fakeConfigs { return &fakeConfigs{docs: map[string]*models.ModuleConfig{}} }

func (f *fakeConfigs) put(instance string, rev int64, values map[string]any) {
	f.docs[instance] = &models.ModuleConfig{ModuleName: "http", Instance: instance, Revision: rev, Values: values}
}

func copyConfig(d *models.ModuleConfig) *models.ModuleConfig {
	if d == nil {
		return nil
	}
	c := *d
	c.Values = models.JSONMap{}
	for k, v := range d.Values {
		c.Values[k] = v
	}
	return &c
}

func (f *fakeConfigs) doc(instance string) *models.ModuleConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyConfig(f.docs[instance])
}

func (f *fakeConfigs) FindByInstance(_ context.Context, instance string) (*models.ModuleConfig, error) {
	return f.doc(instance), nil
}

func (f *fakeConfigs) Apply(_ context.Context, name, instance string, expected int64,
	set map[string]any, unset []string, actor models.ModuleActor, at time.Time,
) (*models.ModuleConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applies++
	if f.raceOnApply {
		return nil, repository.ErrModuleConfigConflict
	}
	d := f.docs[instance]
	var cur int64
	if d != nil {
		cur = d.Revision
	}
	if cur != expected {
		return nil, repository.ErrModuleConfigConflict
	}
	if d == nil {
		d = &models.ModuleConfig{Instance: instance, Values: models.JSONMap{}}
		f.docs[instance] = d
	}
	d.ModuleName = name
	for kk, v := range set {
		d.Values[kk] = v
	}
	for _, kk := range unset {
		delete(d.Values, kk)
	}
	d.Revision++
	d.UpdatedBy, d.UpdatedAt = actor, at
	return copyConfig(d), nil
}

func (f *fakeConfigs) RaiseRevision(_ context.Context, name, instance string, minRevision int64, _ time.Time) (*models.ModuleConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raises++
	d := f.docs[instance]
	if d == nil {
		d = &models.ModuleConfig{ModuleName: name, Instance: instance, Values: models.JSONMap{}}
		f.docs[instance] = d
	}
	if d.Revision < minRevision {
		d.Revision = minRevision
	}
	return copyConfig(d), nil
}

type fakeChanges struct {
	mu   sync.Mutex
	rows []models.ModuleConfigChange
}

func (f *fakeChanges) Insert(_ context.Context, c *models.ModuleConfigChange) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, *c)
	return nil
}

func (f *fakeChanges) ListByInstance(context.Context, string, int64) ([]models.ModuleConfigChange, error) {
	return f.rows, nil
}

type fakeInvocations struct {
	mu       sync.Mutex
	inserted []models.ModuleActionInvocation
	finished map[string]string // invocation id -> status
}

func (f *fakeInvocations) Insert(_ context.Context, inv *models.ModuleActionInvocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, *inv)
	return nil
}

func (f *fakeInvocations) Finish(_ context.Context, id, status, _, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finished == nil {
		f.finished = map[string]string{}
	}
	f.finished[id] = status
	return nil
}

func (f *fakeInvocations) ListByInstance(context.Context, string, int64) ([]models.ModuleActionInvocation, error) {
	return f.inserted, nil
}

// --- RPC ---

type rpcCall struct {
	queue, opType string
	payload       json.RawMessage
	timeout       time.Duration
}

// fakeRPC answers calls with handler (default: apply everything at the
// requested revision).
type fakeRPC struct {
	mu      sync.Mutex
	calls   []rpcCall
	handler func(c rpcCall) (messaging.ReplyEnvelope, error)
}

func (f *fakeRPC) Call(_ context.Context, queue, opType string, payload any, timeout time.Duration) (messaging.ReplyEnvelope, error) {
	raw, _ := json.Marshal(payload)
	c := rpcCall{queue: queue, opType: opType, payload: raw, timeout: timeout}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		return h(c)
	}
	var req protocol.ConfigApplyRequest
	_ = json.Unmarshal(raw, &req)
	return okReply(protocol.ConfigApplyReply{AppliedRevision: req.Revision})
}

func (f *fakeRPC) callsOf(opType string) []rpcCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []rpcCall
	for _, c := range f.calls {
		if c.opType == opType {
			out = append(out, c)
		}
	}
	return out
}

func okReply(payload any) (messaging.ReplyEnvelope, error) {
	r, err := protocol.NewReply(protocol.Envelope{Version: protocol.EnvelopeVersionV1}, protocol.Source{}, payload)
	return r, err
}

func errReply(code, msg string) messaging.ReplyEnvelope {
	return protocol.NewErrorReply(protocol.Envelope{Version: protocol.EnvelopeVersionV1}, protocol.Source{}, code, msg)
}

type fakeBus struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (b *fakeBus) Publish(e eventbus.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *fakeBus) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

// --- fixtures ---

func testManifest() *models.ModuleManifest {
	return &models.ModuleManifest{ManagementManifest: protocol.ManagementManifest{
		SchemaVersion: protocol.ManagementSchemaV1,
		Properties: []protocol.PropertySpec{
			{FieldSpec: protocol.FieldSpec{Key: "limit", Type: protocol.FieldInt, Default: float64(10), Min: float64(1), Max: float64(100)}, Apply: protocol.ApplyLive},
			{FieldSpec: protocol.FieldSpec{Key: "mode", Type: protocol.FieldEnum, Default: "fast", Options: []protocol.EnumOption{{Value: "fast"}, {Value: "safe"}}}, Apply: protocol.ApplyRestart},
			{FieldSpec: protocol.FieldSpec{Key: "token", Type: protocol.FieldSecret}, Apply: protocol.ApplyLive},
		},
		Actions: []protocol.ActionSpec{
			{Name: "flush", Risk: protocol.RiskLow, Params: []protocol.FieldSpec{
				{Key: "count", Type: protocol.FieldInt, Required: true},
				{Key: "password", Type: protocol.FieldSecret},
			}},
			{Name: "wipe", Risk: protocol.RiskHigh, TimeoutSeconds: 5},
		},
	}}
}

func registered(instance string) models.Module {
	return models.Module{
		Type: "channel", Name: "http", Instance: instance, RPCQueue: "q." + instance,
		Status: models.ModuleStatusRegistered, Manifest: testManifest(),
	}
}

type harness struct {
	svc     *Service
	reg     *fakeRegistry
	configs *fakeConfigs
	changes *fakeChanges
	invs    *fakeInvocations
	rpc     *fakeRPC
	bus     *fakeBus
}

func newHarness(mods ...models.Module) *harness {
	h := &harness{
		reg:     newFakeRegistry(mods...),
		configs: newFakeConfigs(),
		changes: &fakeChanges{},
		invs:    &fakeInvocations{},
		rpc:     &fakeRPC{},
		bus:     &fakeBus{},
	}
	h.svc = NewService(h.reg, h.configs, h.changes, h.invs, h.rpc, h.bus, Config{
		ApplyAttempts: 3, RetryBackoff: time.Millisecond, DriftCheckInterval: time.Hour,
	}, zap.NewNop())
	h.svc.sleep = func(context.Context, time.Duration) bool { return true }
	return h
}

var alice = models.ModuleActor{Type: "user", ID: "u-1", Name: "alice"}
