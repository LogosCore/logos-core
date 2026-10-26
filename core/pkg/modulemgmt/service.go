package modulemgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Errors the GraphQL layer maps to client errors.
var (
	ErrModuleNotFound = errors.New("module not found")
	ErrForbidden      = errors.New("forbidden")
)

// ValidationError is a client error: the request does not fit the manifest.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ConflictError means expectedRevision was not the instance's current config
// revision.
type ConflictError struct {
	CurrentRevision int64 // -1 when unknown (lost a race at write time)
}

func (e *ConflictError) Error() string {
	if e.CurrentRevision < 0 {
		return "config was changed concurrently; reload and retry"
	}
	return fmt.Sprintf("config is at revision %d; reload and retry", e.CurrentRevision)
}

// Registry is the slice of the module registry the service uses.
type Registry interface {
	FindByInstance(ctx context.Context, instance string) (models.Module, error)
	RecordApplyResult(ctx context.Context, instance string, reply protocol.ConfigApplyReply, at time.Time) error
	RecordApplyError(ctx context.Context, instance, message string, at time.Time) error
	RecordStatus(ctx context.Context, instance string, status map[string]any) error
}

// EventPublisher fans management changes out to GraphQL subscribers.
// Satisfied by eventbus.IEventBus.
type EventPublisher interface {
	Publish(event eventbus.Event)
}

// Config tunes pushes and action calls. Zero values take the defaults.
type Config struct {
	// ApplyTimeout bounds one mgmt.config.apply call. Default 10s.
	ApplyTimeout time.Duration
	// ApplyAttempts is how many times a push is tried before last_apply_error
	// is recorded. Default 3.
	ApplyAttempts int
	// RetryBackoff is the wait after the first failed attempt, doubled after
	// each further one. Default 1s.
	RetryBackoff time.Duration
	// DriftCheckInterval is the minimum gap between heartbeat-triggered drift
	// checks of one instance. Default 10s.
	DriftCheckInterval time.Duration
}

// Action timeouts (protocol.MaxActionTimeout is the manifest's cap).
const (
	DefaultActionTimeout = 30 * time.Second
	MaxActionTimeout     = time.Duration(protocol.MaxActionTimeout) * time.Second
)

// systemActor attributes core-initiated pushes (drift repair).
var systemActor = models.ModuleActor{Type: "system", ID: protocol.ServiceCore}

// Service is the module-management business logic, shared by the lifecycle
// RPC handlers (register reply config, heartbeat drift) and the GraphQL
// resolver.
type Service struct {
	registry    Registry
	configs     repository.IModuleConfigRepository
	changes     repository.IModuleConfigChangeRepository
	invocations repository.IModuleActionInvocationRepository
	rpc         messaging.RPCCaller
	bus         EventPublisher
	logger      *zap.Logger
	cfg         Config

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) bool

	mu     sync.Mutex
	pushes map[string]*pushState
	wg     sync.WaitGroup // in-flight push goroutines (tests wait on it)

	// ctx bounds background pushes; Stop cancels it so shutdown neither waits
	// out retries nor records the closing RPC client as an apply error.
	ctx    context.Context
	cancel context.CancelFunc
}

// maxDriftBackoffShift caps the drift-check back-off at
// DriftCheckInterval << 6 (about 10 minutes at the 10s default).
const maxDriftBackoffShift = 6

// pushState makes pushes single-flight per instance: while one runs, further
// requests only mark it to run once more, with the newest actor.
type pushState struct {
	running   bool
	again     bool
	actor     models.ModuleActor
	lastCheck time.Time // last heartbeat-triggered check
	// failures counts consecutive failed pushes. Drift checks back off
	// exponentially with it, so a module that keeps refusing a config is not
	// re-pushed every heartbeat; a successful or unneeded push, or a new
	// config write, resets it.
	failures int
}

// NewService wires the service. bus may be nil (no subscriber fan-out).
func NewService(
	registry Registry,
	configs repository.IModuleConfigRepository,
	changes repository.IModuleConfigChangeRepository,
	invocations repository.IModuleActionInvocationRepository,
	rpc messaging.RPCCaller,
	bus EventPublisher,
	cfg Config,
	logger *zap.Logger,
) *Service {
	if cfg.ApplyTimeout <= 0 {
		cfg.ApplyTimeout = 10 * time.Second
	}
	if cfg.ApplyAttempts <= 0 {
		cfg.ApplyAttempts = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Second
	}
	if cfg.DriftCheckInterval <= 0 {
		cfg.DriftCheckInterval = 10 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		ctx:         ctx,
		cancel:      cancel,
		registry:    registry,
		configs:     configs,
		changes:     changes,
		invocations: invocations,
		rpc:         rpc,
		bus:         bus,
		logger:      logger.With(zap.String("component", "module-mgmt")),
		cfg:         cfg,
		now:         func() time.Time { return time.Now().UTC() },
		sleep: func(ctx context.Context, d time.Duration) bool {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return true
			case <-ctx.Done():
				return false
			}
		},
		pushes: make(map[string]*pushState),
	}
}

// --- reads ---

// Effective loads the desired config of a module row.
func (s *Service) Effective(ctx context.Context, mod models.Module) (EffectiveConfig, error) {
	cfg, err := s.configs.FindByInstance(ctx, mod.Instance)
	if err != nil {
		return EffectiveConfig{}, fmt.Errorf("load module config: %w", err)
	}
	return ComputeEffective(manifestOf(mod), cfg), nil
}

// ManagementConfigFor is the config for the module.register reply.
// appliedRevision is the revision the registering process reports applied; if
// that is ahead of the stored revision, the revision is raised first (see
// catchUp) so the process accepts the config instead of ignoring it as stale.
func (s *Service) ManagementConfigFor(ctx context.Context, moduleName, instance string, manifest *protocol.ManagementManifest, appliedRevision int64) (protocol.ManagementConfig, error) {
	cfg, err := s.configs.FindByInstance(ctx, instance)
	if err != nil {
		return protocol.ManagementConfig{}, fmt.Errorf("load module config: %w", err)
	}
	cfg, err = s.catchUp(ctx, moduleName, instance, appliedRevision, cfg)
	if err != nil {
		return protocol.ManagementConfig{}, err
	}
	return ComputeEffective(manifest, cfg).Wire(), nil
}

// catchUp handles an instance that reports an applied revision ahead of the
// stored desired revision — the config store was reset or migrated while the
// module kept running. Modules ignore an apply whose revision is below what
// they already applied, so the desired revision is raised to applied+1 (values
// unchanged, no audit entry: nothing an operator set has changed). It returns
// the config to use; cfg itself when no raise was needed.
func (s *Service) catchUp(ctx context.Context, moduleName, instance string, appliedRevision int64, cfg *models.ModuleConfig) (*models.ModuleConfig, error) {
	var rev int64
	if cfg != nil {
		rev = cfg.Revision
	}
	if appliedRevision <= rev {
		return cfg, nil
	}
	raised, err := s.configs.RaiseRevision(ctx, moduleName, instance, appliedRevision+1, s.now())
	if err != nil {
		return nil, fmt.Errorf("raise config revision: %w", err)
	}
	s.logger.Info("instance applied revision is ahead of desired config; raised revision",
		zap.String("instance", instance), zap.Int64("applied_revision", appliedRevision),
		zap.Int64("old_revision", rev), zap.Int64("revision", raised.Revision))
	return raised, nil
}

func manifestOf(mod models.Module) *protocol.ManagementManifest {
	if mod.Manifest == nil {
		return nil
	}
	return &mod.Manifest.ManagementManifest
}

// ConfigChanges lists an instance's config audit entries, newest first.
func (s *Service) ConfigChanges(ctx context.Context, instance string, limit int64) ([]models.ModuleConfigChange, error) {
	return s.changes.ListByInstance(ctx, instance, limit)
}

// Invocations lists an instance's action invocations, newest first.
func (s *Service) Invocations(ctx context.Context, instance string, limit int64) ([]models.ModuleActionInvocation, error) {
	return s.invocations.ListByInstance(ctx, instance, limit)
}

// --- config writes ---

// KeyValue is one property assignment; a nil Value clears the key (the module
// falls back to its default).
type KeyValue struct {
	Key   string
	Value any
}

// UpdateRequest is one updateModuleConfig call.
type UpdateRequest struct {
	Instance         string
	Values           []KeyValue
	ExpectedRevision int64
	Actor            models.ModuleActor
}

// UpdateConfig validates and stores property values for one instance, then
// pushes its new config to it in the background. It returns the (re-read)
// module row.
func (s *Service) UpdateConfig(ctx context.Context, req UpdateRequest) (*models.Module, error) {
	mod, err := s.findModule(ctx, req.Instance)
	if err != nil {
		return nil, err
	}
	if mod.Manifest == nil {
		return nil, invalid("module %q declares no management manifest", req.Instance)
	}
	if len(req.Values) == 0 {
		return nil, invalid("no values given")
	}

	specs := make(map[string]protocol.PropertySpec, len(mod.Manifest.Properties))
	for _, p := range mod.Manifest.Properties {
		specs[p.Key] = p
	}
	set := map[string]any{}
	var unset []string
	seen := map[string]bool{}
	for _, kv := range req.Values {
		spec, ok := specs[kv.Key]
		if !ok {
			return nil, invalid("%q is not a declared property of %s", kv.Key, mod.Name)
		}
		if seen[kv.Key] {
			return nil, invalid("%q given more than once", kv.Key)
		}
		seen[kv.Key] = true
		if kv.Value == nil {
			unset = append(unset, kv.Key)
			continue
		}
		v := normalizeValue(kv.Value)
		if err := protocol.ValidateValue(spec.FieldSpec, v); err != nil {
			return nil, invalid("%s: %v", kv.Key, err)
		}
		set[kv.Key] = v
	}

	current, err := s.configs.FindByInstance(ctx, mod.Instance)
	if err != nil {
		return nil, fmt.Errorf("load module config: %w", err)
	}
	var currentRev int64
	var currentVals map[string]any
	if current != nil {
		currentRev, currentVals = current.Revision, current.Values
	}
	if currentRev != req.ExpectedRevision {
		return nil, &ConflictError{CurrentRevision: currentRev}
	}

	// The audit diff is exact: it is computed against the revision the write
	// below is conditioned on.
	var changes models.ModuleConfigKeyChanges
	for _, kv := range req.Values {
		old, hadOld := currentVals[kv.Key]
		if !hadOld {
			old = nil
		}
		next := set[kv.Key] // nil for an unset
		if valuesEqual(old, next) {
			continue
		}
		spec := specs[kv.Key].FieldSpec
		changes = append(changes, models.ModuleConfigKeyChange{Key: kv.Key, Old: redact(spec, old), New: redact(spec, next)})
	}
	if len(changes) == 0 {
		// Nothing changes: no new revision, nothing to push.
		return &mod, nil
	}

	now := s.now()
	doc, err := s.configs.Apply(ctx, mod.Name, mod.Instance, req.ExpectedRevision, set, unset, req.Actor, now)
	if errors.Is(err, repository.ErrModuleConfigConflict) {
		return nil, &ConflictError{CurrentRevision: -1}
	}
	if err != nil {
		return nil, fmt.Errorf("store module config: %w", err)
	}

	change := &models.ModuleConfigChange{
		Instance:   mod.Instance,
		ModuleName: mod.Name,
		Revision:   doc.Revision,
		Changes:    changes,
		Actor:      req.Actor,
		At:         now,
	}
	// The config is committed: record it even if the client has gone away.
	ctx = context.WithoutCancel(ctx)
	if err := s.changes.Insert(ctx, change); err != nil {
		// The write itself succeeded; losing its audit entry must not turn it
		// into a client error that invites a retry at a stale revision.
		s.logger.Error("failed to record module config change", zap.String("module", mod.Name), zap.Error(err))
	}

	if mod.Status == models.ModuleStatusRegistered {
		s.RequestPush(mod.Instance, req.Actor)
	}
	s.publishUpdated(mod)

	updated, err := s.registry.FindByInstance(ctx, mod.Instance)
	if err != nil {
		return &mod, nil //nolint:nilerr // the write succeeded; serve the pre-write row rather than fail it
	}
	return &updated, nil
}

// --- pushing ---

// RequestPush schedules a background push of the instance's effective config.
// Single-flight per instance: a request during a running push makes it run
// once more afterwards (with the newest config and actor) instead of starting
// a second, concurrent one.
func (s *Service) RequestPush(instance string, actor models.ModuleActor) {
	s.mu.Lock()
	st := s.state(instance)
	st.actor = actor
	st.failures = 0 // a new config deserves a prompt drift check again
	if st.running {
		st.again = true
		s.mu.Unlock()
		return
	}
	st.running = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runPushes(instance, false)
}

// HeartbeatApplied is the heartbeat hook: given the revision the instance
// reports applied, it pushes in the background if that differs from the
// desired revision (behind: drift; ahead: the config store was reset, see
// catchUp). Rate-limited per instance (DriftCheckInterval) and skipped while a
// push is already running, so a module that keeps failing to apply is not
// hammered on every heartbeat.
func (s *Service) HeartbeatApplied(instance string, _ int64) {
	s.mu.Lock()
	st := s.state(instance)
	now := s.now()
	interval := s.cfg.DriftCheckInterval << min(st.failures, maxDriftBackoffShift)
	if st.running || now.Sub(st.lastCheck) < interval {
		s.mu.Unlock()
		return
	}
	st.lastCheck = now
	st.running = true
	st.actor = systemActor
	s.wg.Add(1)
	s.mu.Unlock()
	// The drift check re-reads the row, which TouchHeartbeat just updated.
	go s.runPushes(instance, true)
}

func (s *Service) state(instance string) *pushState {
	st, ok := s.pushes[instance]
	if !ok {
		st = &pushState{}
		s.pushes[instance] = st
	}
	return st
}

// Wait blocks until background pushes finish. For tests and shutdown.
func (s *Service) Wait() { s.wg.Wait() }

// Stop cancels background pushes and waits for them. Call it before closing
// the RPC client.
func (s *Service) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *Service) runPushes(instance string, onlyIfDrifted bool) {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		st := s.state(instance)
		actor := st.actor
		st.again = false
		s.mu.Unlock()

		ok := s.pushWithRetries(instance, actor, onlyIfDrifted)
		onlyIfDrifted = false // a re-run was requested by a config write

		s.mu.Lock()
		if ok {
			st.failures = 0
		} else {
			st.failures++
		}
		if !st.again {
			st.running = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// errNotPushable means there is nothing to push to (not registered, no
// manifest, or already in sync for a drift check). Not an apply failure.
var errNotPushable = errors.New("not pushable")

// errTerminal wraps a failure retrying cannot fix (the module answered with an
// error reply).
type errTerminal struct{ err error }

func (e errTerminal) Error() string { return e.err.Error() }
func (e errTerminal) Unwrap() error { return e.err }

// pushWithRetries pushes the config, retrying transient failures. It reports
// whether the instance ended up with the config (or needed none).
func (s *Service) pushWithRetries(instance string, actor models.ModuleActor, onlyIfDrifted bool) bool {
	ctx := s.ctx
	var lastErr error
	var mod models.Module
	backoff := s.cfg.RetryBackoff
	for attempt := 1; attempt <= s.cfg.ApplyAttempts; attempt++ {
		var err error
		mod, err = s.pushOnce(ctx, instance, actor, onlyIfDrifted)
		if err == nil {
			s.publishUpdated(mod)
			return true
		}
		if errors.Is(err, errNotPushable) {
			return true
		}
		lastErr = err
		var term errTerminal
		if errors.As(err, &term) || attempt == s.cfg.ApplyAttempts {
			break
		}
		s.logger.Debug("config push failed; retrying", zap.String("instance", instance),
			zap.Int("attempt", attempt), zap.Error(err))
		if !s.sleep(ctx, backoff) {
			break
		}
		backoff *= 2
	}
	if ctx.Err() != nil {
		return false // shutting down: not the module's failure
	}
	s.logger.Warn("config push failed", zap.String("instance", instance), zap.Error(lastErr))
	if err := s.registry.RecordApplyError(ctx, instance, lastErr.Error(), s.now()); err != nil {
		s.logger.Error("failed to record apply error", zap.String("instance", instance), zap.Error(err))
	}
	if mod.Instance == "" {
		mod.Instance = instance
	}
	s.publishUpdated(mod)
	return false
}

// pushOnce sends the instance's current config once and records the reply.
func (s *Service) pushOnce(ctx context.Context, instance string, actor models.ModuleActor, onlyIfDrifted bool) (models.Module, error) {
	mod, err := s.registry.FindByInstance(ctx, instance)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return mod, errNotPushable
	}
	if err != nil {
		return mod, fmt.Errorf("load module: %w", err)
	}
	if mod.Status != models.ModuleStatusRegistered || mod.Manifest == nil || mod.RPCQueue == "" {
		return mod, errNotPushable
	}
	cfg, err := s.configs.FindByInstance(ctx, instance)
	if err != nil {
		return mod, fmt.Errorf("load module config: %w", err)
	}
	if cfg, err = s.catchUp(ctx, mod.Name, instance, mod.AppliedRevision, cfg); err != nil {
		return mod, err
	}
	eff := ComputeEffective(manifestOf(mod), cfg)
	if onlyIfDrifted && mod.AppliedRevision == eff.Revision {
		return mod, errNotPushable
	}

	req := protocol.ConfigApplyRequest{ManagementConfig: eff.Wire(), Actor: actor.Protocol()}
	reply, err := s.rpc.Call(ctx, mod.RPCQueue, protocol.TypeMgmtConfigApply, req, s.cfg.ApplyTimeout)
	if err != nil {
		return mod, err
	}
	if reply.Status != protocol.StatusOK {
		return mod, errTerminal{fmt.Errorf("module refused config revision %d: %s", eff.Revision, replyErrorText(reply))}
	}
	var res protocol.ConfigApplyReply
	if err := json.Unmarshal(reply.Payload, &res); err != nil {
		return mod, errTerminal{fmt.Errorf("malformed mgmt.config.apply reply: %w", err)}
	}
	if err := s.registry.RecordApplyResult(ctx, instance, res, s.now()); err != nil {
		return mod, fmt.Errorf("record apply result: %w", err)
	}
	s.logger.Info("config applied", zap.String("instance", instance),
		zap.Int64("revision", eff.Revision), zap.Int64("applied_revision", res.AppliedRevision),
		zap.Int("rejected", len(res.Rejected)), zap.Strings("restart_required", res.RestartRequired))
	return mod, nil
}

func replyErrorText(r messaging.ReplyEnvelope) string {
	if r.Error == nil {
		return "status " + r.Status
	}
	return r.Error.Code + ": " + r.Error.Message
}

// --- actions ---

// InvokeRequest is one invokeModuleAction call.
type InvokeRequest struct {
	Instance string
	Action   string
	Params   any // decoded JSON object or nil
	Actor    models.ModuleActor
	// AllowHighRisk is whether the caller holds module:action:invoke:high.
	AllowHighRisk bool
}

// InvokeAction runs a declared action and waits for its outcome (bounded by
// the action's timeout). A failed or timed-out action is a normal return with
// that status; an error means the invocation was refused or never recorded.
func (s *Service) InvokeAction(ctx context.Context, req InvokeRequest) (*models.ModuleActionInvocation, error) {
	mod, err := s.findModule(ctx, req.Instance)
	if err != nil {
		return nil, err
	}
	if mod.Status != models.ModuleStatusRegistered {
		return nil, invalid("module %q is not registered", req.Instance)
	}
	if mod.Manifest == nil {
		return nil, invalid("module %q declares no management manifest", req.Instance)
	}
	var action *protocol.ActionSpec
	for i := range mod.Manifest.Actions {
		if mod.Manifest.Actions[i].Name == req.Action {
			action = &mod.Manifest.Actions[i]
			break
		}
	}
	if action == nil {
		return nil, invalid("%q is not a declared action of %s", req.Action, mod.Name)
	}
	if action.Risk == protocol.RiskHigh && !req.AllowHighRisk {
		return nil, ErrForbidden
	}
	params, stored, err := validateParams(*action, req.Params)
	if err != nil {
		return nil, err
	}

	timeout := DefaultActionTimeout
	if action.TimeoutSeconds > 0 {
		timeout = time.Duration(action.TimeoutSeconds) * time.Second
	}
	if timeout > MaxActionTimeout {
		timeout = MaxActionTimeout
	}

	inv := &models.ModuleActionInvocation{
		InvocationID: protocol.NewULID(),
		Instance:     mod.Instance,
		ModuleName:   mod.Name,
		Action:       action.Name,
		Params:       stored,
		Status:       models.ModuleActionRunning,
		Actor:        req.Actor,
		StartedAt:    s.now(),
	}
	if err := s.invocations.Insert(ctx, inv); err != nil {
		return nil, fmt.Errorf("record invocation: %w", err)
	}
	s.publishUpdated(mod)

	// The call outlives a client that disconnects: the invocation is recorded
	// and must reach a terminal state.
	callCtx := context.WithoutCancel(ctx)
	reply, callErr := s.rpc.Call(callCtx, mod.RPCQueue, protocol.TypeMgmtActionInvoke, protocol.ActionInvokeRequest{
		InvocationID: inv.InvocationID,
		Action:       action.Name,
		Params:       params,
		Actor:        req.Actor.Protocol(),
	}, timeout)
	status, resultJSON, errMsg := InvocationOutcome(reply, callErr, timeout)

	finished := s.now()
	inv.Status, inv.ResultJSON, inv.Error, inv.FinishedAt = status, resultJSON, errMsg, &finished
	if err := s.invocations.Finish(callCtx, inv.InvocationID, status, resultJSON, errMsg, finished); err != nil {
		s.logger.Error("failed to record invocation outcome", zap.String("invocation_id", inv.InvocationID), zap.Error(err))
	}
	s.logger.Info("module action finished", zap.String("instance", mod.Instance), zap.String("action", action.Name),
		zap.String("status", status), zap.String("actor", req.Actor.ID))
	if callErr == nil {
		s.refreshStatus(callCtx, mod)
	}
	s.publishUpdated(mod)
	return inv, nil
}

// StatusRefreshTimeout bounds the mgmt.state.get core sends after an action.
const StatusRefreshTimeout = 3 * time.Second

// refreshStatus reads a module's status values right after one of its
// actions answered, so the action's effect, and which actions are available
// now, show without waiting for the next heartbeat. Best effort: on any
// failure the heartbeat catches up.
func (s *Service) refreshStatus(ctx context.Context, mod models.Module) {
	reply, err := s.rpc.Call(ctx, mod.RPCQueue, protocol.TypeMgmtStateGet, struct{}{}, StatusRefreshTimeout)
	if err == nil && reply.Status != protocol.StatusOK {
		err = errors.New(replyErrorText(reply))
	}
	var st protocol.StateGetReply
	if err == nil {
		err = json.Unmarshal(reply.Payload, &st)
	}
	if err == nil && st.Status != nil {
		err = s.registry.RecordStatus(ctx, mod.Instance, st.Status)
	}
	if err != nil {
		s.logger.Warn("status not refreshed after an action", zap.String("instance", mod.Instance), zap.Error(err))
	}
}

// validateParams checks params against an action's declared params. It
// returns the params to send and the copy to store (secrets redacted).
func validateParams(action protocol.ActionSpec, raw any) (send map[string]any, stored models.JSONMap, err error) {
	in := map[string]any{}
	if raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, invalid("params must be a JSON object")
		}
		in = m
	}
	declared := make(map[string]protocol.FieldSpec, len(action.Params))
	for _, p := range action.Params {
		declared[p.Key] = p
	}
	for k := range in {
		if _, ok := declared[k]; !ok {
			return nil, nil, invalid("%q is not a parameter of action %s", k, action.Name)
		}
	}
	send = map[string]any{}
	stored = models.JSONMap{}
	for _, p := range action.Params {
		v, ok := in[p.Key]
		if ok {
			v = normalizeValue(v)
		}
		if err := protocol.ValidateValue(p, v); err != nil {
			return nil, nil, invalid("param %s: %v", p.Key, err)
		}
		if ok && v != nil {
			send[p.Key] = v
			stored[p.Key] = redact(p, v)
		}
	}
	return send, stored, nil
}

// InvocationOutcome maps an action RPC result to a terminal invocation state:
// no reply in time → TIMED_OUT; any other transport failure or an error reply
// (action_failed / validation_failed / not_found ...) → FAILED; an ok reply →
// SUCCEEDED with its result as JSON.
func InvocationOutcome(reply messaging.ReplyEnvelope, callErr error, timeout time.Duration) (status, resultJSON, errMsg string) {
	if callErr != nil {
		if errors.Is(callErr, messaging.ErrRPCTimeout) {
			return models.ModuleActionTimedOut, "", fmt.Sprintf("no reply within %s", timeout)
		}
		return models.ModuleActionFailed, "", callErr.Error()
	}
	if reply.Status != protocol.StatusOK {
		return models.ModuleActionFailed, "", replyErrorText(reply)
	}
	var res protocol.ActionInvokeReply
	if len(reply.Payload) > 0 {
		if err := json.Unmarshal(reply.Payload, &res); err != nil {
			return models.ModuleActionFailed, "", "malformed mgmt.action.invoke reply: " + err.Error()
		}
	}
	if res.Result != nil {
		b, err := json.Marshal(res.Result)
		if err == nil {
			resultJSON = string(b)
		}
	}
	return models.ModuleActionSucceeded, resultJSON, ""
}

// --- helpers ---

func (s *Service) findModule(ctx context.Context, instance string) (models.Module, error) {
	mod, err := s.registry.FindByInstance(ctx, instance)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return mod, ErrModuleNotFound
	}
	if err != nil {
		return mod, fmt.Errorf("load module: %w", err)
	}
	return mod, nil
}

// publishUpdated emits module.updated so the moduleChanged subscription
// refetches the row (and its management view).
func (s *Service) publishUpdated(mod models.Module) {
	if s.bus == nil || mod.Instance == "" {
		return
	}
	s.bus.Publish(eventbus.NewModuleUpdatedEvent(eventbus.SystemActor(), eventbus.ModuleEventPayload{
		Instance: mod.Instance, Type: mod.Type, Status: mod.Status,
	}))
}
