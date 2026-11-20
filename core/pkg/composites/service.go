// Package composites runs composite commands' tasks (docs:
// contracts/task-outsourcing): a factory's code, called over the control
// plane (factory.compose), decides one call at a time which of the session's
// commands core runs as the task's subtasks, and when its own op goes to the
// minion; core runs them as ordinary tasks of the session, and tells the
// factory how each ended.
//
// Everything a task needs between calls is in its record
// (models.MinionComposite): the factory's ctx, the ends it has not heard of,
// the subtasks waiting in it, its timer, the call out. A decision is kept
// with its answer before core acts on it, and what it asks for (a cancel, a
// subtask, its own op) is done from there, once: a restart of core, or a
// call sent again, queues nothing twice. Core looks at a task when an event
// says something of it changed (its subtask's end, its own op's) and when
// its record says its time came (WakeAt: a timer, a call's deadline); a
// process claims a task while it looks at it, so one makes its calls at a
// time.
package composites

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Tasks is what the service reads and writes of tasks
// (repository.IMinionTaskRepository).
type Tasks interface {
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionTask, error)
	Parts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error)
	OutParts(ctx context.Context, taskID uuid.UUID) ([]models.MinionTaskPart, error)
	RequestCancel(ctx context.Context, taskID uuid.UUID, at time.Time) (*models.MinionTask, error)
	repository.IMinionCompositeRepository
}

// Sessions finds a task's session.
type Sessions interface {
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error)
}

// Builds finds a session's build.
type Builds interface {
	FindByBuildID(ctx context.Context, buildID uuid.UUID) (*models.Build, error)
}

// Schemas finds a factory kind's latest build schema: its commands' params.
type Schemas interface {
	LatestBuildSchema(ctx context.Context, moduleName string) (*models.ModuleBuildSchema, error)
}

// Modules lists the registered module instances: the factory instances a
// call may go to.
type Modules interface {
	ListActive(ctx context.Context) ([]models.Module, error)
}

// Users names who queued a task, for its calls' actor.
type Users interface {
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.User, error)
}

// Caller makes a call over the control plane (messaging.RPCClient).
type Caller interface {
	Call(ctx context.Context, queue, opType string, payload any, timeout time.Duration) (protocol.ReplyEnvelope, error)
}

// Links signs the links a call carries, and checks them when the factory
// follows one (builds.Links: the same key).
type Links interface {
	Sign(path string, expires time.Time) string
	Check(path, expires, sig string, now time.Time) error
}

// Files are the files tasks got and the factory puts (minionfiles.Service).
type Files interface {
	// File is a task's file (one it got), nil for none.
	File(ctx context.Context, taskID uuid.UUID) (*models.MinionFile, error)
	// FileByID is a file, nil for none.
	FileByID(ctx context.Context, fileID uuid.UUID) (*models.MinionFile, error)
	// Open is a kept file's bytes.
	Open(ctx context.Context, fileID uuid.UUID) (io.ReadCloser, *models.MinionFile, error)
	// PutFactoryFile keeps a file the factory put for a composite task.
	PutFactoryFile(ctx context.Context, operationID, forTask, by uuid.UUID, name string, body io.Reader) (*models.MinionFile, error)
	// DropTaskFile drops a task file, bytes and all.
	DropTaskFile(ctx context.Context, fileID uuid.UUID) error
	// TakeFiles makes the files task from got to's as well (a finish that
	// takes a subtask's result).
	TakeFiles(ctx context.Context, from, to uuid.UUID) error
}

// Queuer queues a composite task's subtasks and sends its own op, each
// checked as an operator's task is (the task resolver). A check that fails
// returns a *Refusal, and queues nothing.
type Queuer interface {
	// QueueSubtask queues sub, one of parent's subtasks, under its task id,
	// issued by whoever queued parent. A task that has the id already (it was
	// queued before core stopped) comes back as it is.
	QueueSubtask(ctx context.Context, parent *models.MinionTask, sub models.MinionSubtask) (*models.MinionTask, error)
	// SendOwnOp sends t's own op with args, t's own when nil.
	SendOwnOp(ctx context.Context, t *models.MinionTask, args map[string]any) (*models.MinionTask, error)
}

// Refusal is a subtask, or an own op, that failed core's checks: it ends
// refused, saying why.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

// Nudger tells a session's minion core has something new for it
// (minionsync.Service.Nudge).
type Nudger interface {
	Nudge(session uuid.UUID, streams bool)
}

// Config tunes the service.
type Config struct {
	// CallTimeout is how long core waits for a factory's answer to a call:
	// the control plane's default.
	CallTimeout time.Duration
	// Tick is how often core looks for tasks whose time came (a timer, a
	// call's deadline) or that an event it missed concerned.
	Tick time.Duration
	// Workers is how many tasks core looks at at once.
	Workers int
	// Retry is how soon a call no instance took goes again.
	Retry time.Duration
	// MaxFileSize is the largest file the factory may put for a task
	// (MINION_FILE_MAX_SIZE, as an operator's task file).
	MaxFileSize int64
}

// DefaultConfig is the service's configuration but for MaxFileSize.
func DefaultConfig() Config {
	return Config{CallTimeout: 10 * time.Second, Tick: 2 * time.Second, Workers: 8, Retry: time.Second, MaxFileSize: 2 << 30}
}

// Service runs composite commands' tasks.
type Service struct {
	cfg      Config
	tasks    Tasks
	sessions Sessions
	builds   Builds
	schemas  Schemas
	modules  Modules
	users    Users
	rpc      Caller
	links    Links
	files    Files
	queuer   Queuer
	nudger   Nudger
	bus      eventbus.IEventBus
	logger   *zap.Logger
	// id names this core process in a task's claim.
	id  string
	now func() time.Time

	work chan uuid.UUID
	mu   sync.Mutex
	// queued are the tasks waiting for a worker.
	queued map[uuid.UUID]bool
	// locks: one worker looks at a task at a time.
	locks map[uuid.UUID]*taskLock
	// watched are the tasks whose events concern a composite task: its
	// subtasks, and itself (its own op), by their composite task.
	watched map[uuid.UUID]uuid.UUID
	// out are the calls out, by session and task: a reverse-call minion's
	// replies say come back soon while one is (Deciding).
	out map[uuid.UUID]map[uuid.UUID]bool
	// waiting counts the calls out by factory instance: core calls the one
	// with the fewest.
	waiting map[string]int

	stopped chan struct{}
	wg      sync.WaitGroup
	unsub   func()
}

type taskLock struct {
	mu   sync.Mutex
	refs int
}

// New is a service; Start runs it.
func New(cfg Config, tasks Tasks, sessions Sessions, builds Builds, schemas Schemas, modules Modules, users Users, rpc Caller,
	links Links, files Files, bus eventbus.IEventBus, logger *zap.Logger) *Service {
	def := DefaultConfig()
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = def.CallTimeout
	}
	if cfg.Tick <= 0 {
		cfg.Tick = def.Tick
	}
	if cfg.Workers <= 0 {
		cfg.Workers = def.Workers
	}
	if cfg.Retry <= 0 {
		cfg.Retry = def.Retry
	}
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = def.MaxFileSize
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{cfg: cfg, tasks: tasks, sessions: sessions, builds: builds, schemas: schemas, modules: modules, users: users,
		rpc: rpc, links: links, files: files, bus: bus, logger: logger, id: uuid.NewString(), now: func() time.Time { return time.Now().UTC() },
		work: make(chan uuid.UUID, 4096), queued: map[uuid.UUID]bool{}, locks: map[uuid.UUID]*taskLock{},
		watched: map[uuid.UUID]uuid.UUID{}, out: map[uuid.UUID]map[uuid.UUID]bool{}, waiting: map[string]int{},
		stopped: make(chan struct{})}
}

// WithQueuer gives the service what queues subtasks and own ops (the task
// resolver, built after it).
func (s *Service) WithQueuer(q Queuer) *Service {
	s.queuer = q
	return s
}

// WithNudger gives it what tells a minion of a task queued.
func (s *Service) WithNudger(n Nudger) *Service {
	s.nudger = n
	return s
}

// Start runs the workers, the tick, and the watch of tasks' events. It
// looks at every task whose time came at once: a restart of core goes on
// where it was.
func (s *Service) Start() {
	if s.bus != nil {
		s.unsub = s.bus.Subscribe([]eventbus.Topic{eventbus.TopicMinionTaskChanged}, func(_ context.Context, e eventbus.Event) {
			p, ok := e.Payload.(eventbus.MinionTaskEventPayload)
			if !ok || p.Sent {
				return
			}
			id, err := uuid.Parse(p.TaskID)
			if err != nil {
				return
			}
			s.mu.Lock()
			parent, ok := s.watched[id]
			s.mu.Unlock()
			if ok {
				s.Nudge(parent)
			}
		})
	}
	for range s.cfg.Workers {
		s.wg.Add(1)
		go s.worker()
	}
	s.wg.Add(1)
	go s.ticker()
}

// Stop stops the workers once the tasks they look at are done with.
func (s *Service) Stop() {
	if s.unsub != nil {
		s.unsub()
	}
	close(s.stopped)
	s.wg.Wait()
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopped:
			return
		case id := <-s.work:
			s.mu.Lock()
			delete(s.queued, id)
			s.mu.Unlock()
			s.step(context.Background(), id)
		}
	}
}

func (s *Service) ticker() {
	defer s.wg.Done()
	t := time.NewTicker(s.cfg.Tick)
	defer t.Stop()
	for {
		s.tick(context.Background())
		select {
		case <-s.stopped:
			return
		case <-t.C:
		}
	}
}

// tick looks at the tasks whose time came, and at those with a subtask that
// ended unheard (an event lost, or one before core restarted).
func (s *Service) tick(ctx context.Context) {
	due, err := s.tasks.ComposeDue(ctx, s.now(), 256)
	if err != nil {
		s.logger.Error("composites: the tasks due", zap.Error(err))
	}
	unheard, err := s.tasks.UnheardParents(ctx, 256)
	if err != nil {
		s.logger.Error("composites: the tasks with an end unheard", zap.Error(err))
	}
	for _, id := range append(due, unheard...) {
		s.Nudge(id)
	}
}

// Nudge has core look at a composite task soon: something of it changed.
func (s *Service) Nudge(task uuid.UUID) {
	s.mu.Lock()
	if s.queued[task] {
		s.mu.Unlock()
		return
	}
	s.queued[task] = true
	s.watched[task] = task
	s.mu.Unlock()
	select {
	case s.work <- task:
	default:
		// The queue is full: the tick looks at it again (its WakeAt), or
		// the next event.
		s.mu.Lock()
		delete(s.queued, task)
		s.mu.Unlock()
	}
}

// Started: a composite task was queued (its start call goes now).
func (s *Service) Started(t *models.MinionTask) {
	s.Nudge(t.TaskID)
}

// watch has events of task concern parent: one of its subtasks.
func (s *Service) watch(task, parent uuid.UUID) {
	s.mu.Lock()
	s.watched[task] = parent
	s.mu.Unlock()
}

// forget lets go of a task that ended: its events, and its subtasks', no
// longer concern it.
func (s *Service) forget(task uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, parent := range s.watched {
		if parent == task {
			delete(s.watched, id)
		}
	}
}

func (s *Service) lock(task uuid.UUID) *taskLock {
	s.mu.Lock()
	l, ok := s.locks[task]
	if !ok {
		l = &taskLock{}
		s.locks[task] = l
	}
	l.refs++
	s.mu.Unlock()
	l.mu.Lock()
	return l
}

func (s *Service) unlock(task uuid.UUID, l *taskLock) {
	l.mu.Unlock()
	s.mu.Lock()
	l.refs--
	if l.refs == 0 {
		delete(s.locks, task)
	}
	s.mu.Unlock()
}

// Hold looks at tasks now, in the caller's stead, and says whether their
// factory's decisions all came within d: a reverse-call minion's message
// that brought their ends, and asks for tasks, then gets what the
// decisions queued in its reply (docs: Mechanics by connection kind). Past
// d the look goes on, and the reply goes without it.
func (s *Service) Hold(ctx context.Context, tasks []uuid.UUID, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, id := range tasks {
			s.step(context.WithoutCancel(ctx), id)
		}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// Deciding reports whether a call about one of the session's composite
// tasks is out: a reverse-call minion is told to come back soon, for what
// its decision queues.
func (s *Service) Deciding(session uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.out[session]) > 0
}

// callOut and callIn count a call to an instance about a session's task.
func (s *Service) callOut(instance string, session, task uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiting[instance]++
	if s.out[session] == nil {
		s.out[session] = map[uuid.UUID]bool{}
	}
	s.out[session][task] = true
}

func (s *Service) callIn(instance string, session, task uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting[instance]--; s.waiting[instance] <= 0 {
		delete(s.waiting, instance)
	}
	delete(s.out[session], task)
	if len(s.out[session]) == 0 {
		delete(s.out, session)
	}
}

// ErrNotComposite is a stop of a task that is no composite command's, or
// has ended.
var ErrNotComposite = errors.New("not a composite command's task that runs")

// StopTask stops a composite task, as an operator does (docs: Stopping).
// The first stop stops what is under way; once that has ended, its factory
// hears of it, and may run subtasks to clean up; the task then ends
// cancelled. A second stop ends it cancelled at once, its cleanup stopped.
func (s *Service) StopTask(ctx context.Context, t *models.MinionTask) (*models.MinionTask, error) {
	if !t.Composing() {
		return nil, ErrNotComposite
	}
	now := s.now()
	switch t.Composite.Phase {
	case models.ComposeStopping, models.ComposeCleanup, models.ComposeClosed:
		after, err := s.tasks.EndComposite(ctx, t.TaskID, models.MinionTaskCancelled, nil, "", now)
		if err != nil {
			return nil, err
		}
		if _, _, err := s.stopUnderWay(ctx, after, false, true); err != nil {
			s.logger.Warn("composites: stop a stopped task's cleanup", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		}
		s.publish(after)
		s.forget(after.TaskID)
		s.logger.Info("composite task stopped again: cancelled", zap.String("task_id", t.TaskID.String()))
		return after, nil
	}
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{
		Set:   map[string]any{"phase": models.ComposeStopping, "stopped_at": now, "wake_at": now},
		Unset: []string{"timer_at", "finish", "cancel", "send_own"},
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info("composite task stopped", zap.String("task_id", t.TaskID.String()))
	s.publish(after)
	s.Nudge(t.TaskID)
	return after, nil
}

func (s *Service) publish(t *models.MinionTask) {
	if s.bus == nil || t == nil {
		return
	}
	s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(),
	}))
}
