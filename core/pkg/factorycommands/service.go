// Package factorycommands runs factory commands' tasks (docs:
// contracts/factory-commands): commands a factory runs itself, in the
// factory module, never on a minion, which every session of its builds has.
// Their tasks are the session's (state FACTORY until they end), kept with its
// other tasks.
//
// Core offers a queued task to the instances of the session's factory kind
// whose schema declares the command (factory.command), with everything it
// keeps of the session and of its build, as it offers builds: in turn, a busy
// or unreachable instance passing it on, and again every Tick while none
// takes it. The instance that takes it reports what the command prints
// (factory.command.output) and how it ended (factory.command.result); core
// stops it with factory.command.cancel. A task under way on an instance that
// is no longer registered, or that sent no report for
// protocol.FactoryCommandNoReport, ends failed.
package factorycommands

import (
	"context"
	"errors"
	"fmt"
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
	repository.IMinionFactoryRepository
}

// Sessions finds a task's session.
type Sessions interface {
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error)
}

// Builds finds a session's build.
type Builds interface {
	FindByBuildID(ctx context.Context, buildID uuid.UUID) (*models.Build, error)
}

// BuildFiles reads a build's files and its artifact (builds.Service).
type BuildFiles interface {
	// File is a build file's record.
	File(ctx context.Context, id string) (*models.BuildFile, error)
	// OpenFile opens one of a build's files, whatever its state.
	OpenFile(ctx context.Context, b *models.Build, file uuid.UUID) (io.ReadCloser, *models.BuildFile, error)
	// OpenArtifact opens a succeeded build's artifact.
	OpenArtifact(ctx context.Context, id string) (io.ReadCloser, *models.Build, error)
}

// Modules lists the registered module instances: the factory instances a
// task may go to.
type Modules interface {
	ListActive(ctx context.Context) ([]models.Module, error)
}

// Users names who queued a task, for its call's actor.
type Users interface {
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.User, error)
}

// Credentials resolves the credentials a task's args name
// (credentialrefs.Resolver).
type Credentials interface {
	Resolve(ctx context.Context, operationID uuid.UUID, ids []uuid.UUID) (map[string]protocol.Credential, map[string]int64, error)
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

// Files are task files: those a task's args name, and those its factory puts
// for it (minionfiles.Service).
type Files interface {
	FileByID(ctx context.Context, fileID uuid.UUID) (*models.MinionFile, error)
	Open(ctx context.Context, fileID uuid.UUID) (io.ReadCloser, *models.MinionFile, error)
	// PutFactoryFile keeps a file the factory puts for a task.
	PutFactoryFile(ctx context.Context, operationID, forTask, by uuid.UUID, name string, body io.Reader) (*models.MinionFile, error)
	// DropTaskFile drops a task file now, bytes and all.
	DropTaskFile(ctx context.Context, fileID uuid.UUID) error
	// ReleaseTaskFile drops a task file once no task that has not ended
	// names it.
	ReleaseTaskFile(ctx context.Context, fileID uuid.UUID) error
	// AdoptFactoryFiles makes files the factory put for a task its own.
	AdoptFactoryFiles(ctx context.Context, task *models.MinionTask, ids []uuid.UUID) error
}

// Config tunes the service.
type Config struct {
	// CallTimeout is how long core waits for an instance's answer to an
	// offer: the control plane's default.
	CallTimeout time.Duration
	// Tick is how often core offers the tasks that wait, and looks for those
	// it no longer waits for.
	Tick time.Duration
	// Lease is how long a core process holds a task it offers.
	Lease time.Duration
	// Offers bounds the tasks one tick offers.
	Offers int
	// LinkTTL is how long the links a call carries are signed for: they are
	// good only while the task runs, besides.
	LinkTTL time.Duration
	// MaxFileSize is the largest file the factory may put for a task
	// (MINION_FILE_MAX_SIZE, as an operator's task file).
	MaxFileSize int64
}

// DefaultConfig is the service's configuration but for MaxFileSize.
func DefaultConfig() Config {
	return Config{CallTimeout: 10 * time.Second, Tick: 2 * time.Second, Lease: time.Minute, Offers: 20, LinkTTL: 24 * time.Hour,
		MaxFileSize: 2 << 30}
}

// Service runs factory commands' tasks.
type Service struct {
	cfg        Config
	tasks      Tasks
	sessions   Sessions
	builds     Builds
	buildFiles BuildFiles
	modules    Modules
	users      Users
	creds      Credentials
	rpc        Caller
	links      Links
	files      Files
	bus        eventbus.IEventBus
	logger     *zap.Logger
	// id names this core process in a task's claim.
	id  string
	now func() time.Time

	kick    chan struct{}
	stopped chan struct{}
	stop    sync.Once
	wg      sync.WaitGroup
}

// New is a service; Start runs it.
func New(cfg Config, tasks Tasks, sessions Sessions, builds Builds, buildFiles BuildFiles, modules Modules, users Users,
	creds Credentials, rpc Caller, links Links, files Files, bus eventbus.IEventBus, logger *zap.Logger) *Service {
	def := DefaultConfig()
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = def.CallTimeout
	}
	if cfg.Tick <= 0 {
		cfg.Tick = def.Tick
	}
	if cfg.Lease <= 0 {
		cfg.Lease = def.Lease
	}
	if cfg.Offers <= 0 {
		cfg.Offers = def.Offers
	}
	if cfg.LinkTTL <= 0 {
		cfg.LinkTTL = def.LinkTTL
	}
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = def.MaxFileSize
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{cfg: cfg, tasks: tasks, sessions: sessions, builds: builds, buildFiles: buildFiles, modules: modules, users: users,
		creds: creds, rpc: rpc, links: links, files: files, bus: bus, logger: logger, id: uuid.NewString(),
		now: func() time.Time { return time.Now().UTC() }, kick: make(chan struct{}, 1), stopped: make(chan struct{})}
}

// Start runs the loop: a tick every Tick, and one as soon as a task is
// queued. It offers every task due at once: a restart of core goes on where
// it was.
func (s *Service) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(s.cfg.Tick)
		defer t.Stop()
		for {
			s.Tick(context.Background())
			select {
			case <-s.stopped:
				return
			case <-t.C:
			case <-s.kick:
			}
		}
	}()
}

// Stop stops the loop, and waits for the cancels on their way.
func (s *Service) Stop() {
	s.stop.Do(func() { close(s.stopped) })
	s.wg.Wait()
}

// Queued: a factory command's task was queued; it is offered now.
func (s *Service) Queued(*models.MinionTask) {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// ErrNotInFactory is a stop of a task that is no factory command's, or has
// ended.
var ErrNotInFactory = errors.New("not a factory command's task that runs")

// StopTask stops a factory command's task, as an operator does (docs:
// Stopping): it ends cancelled at once, and the instance it runs on, if any,
// is told to stop it. What it printed until then stays with it.
func (s *Service) StopTask(ctx context.Context, t *models.MinionTask) (*models.MinionTask, error) {
	if !t.InFactory() {
		return nil, ErrNotInFactory
	}
	after, err := s.tasks.EndFactory(ctx, t.TaskID, models.MinionTaskCancelled, "", s.now())
	if repository.IsNotFound(err) {
		// It ended meanwhile: as it is.
		return s.tasks.FindByTaskID(ctx, t.TaskID)
	}
	if err != nil {
		return nil, err
	}
	s.logger.Info("factory command's task stopped", zap.String("task_id", t.TaskID.String()), zap.String("instance", after.Factory.Instance))
	s.ended(ctx, after, true)
	return after, nil
}

// fail ends a task failed on core's word, saying why; cancel tells the
// instance it runs on to stop it.
func (s *Service) fail(ctx context.Context, t *models.MinionTask, why string, cancel bool) {
	after, err := s.tasks.EndFactory(ctx, t.TaskID, models.MinionTaskFailed, why, s.now())
	if repository.IsNotFound(err) {
		return
	}
	if err != nil {
		s.logger.Warn("factory commands: fail a task", zap.String("task_id", t.TaskID.String()), zap.Error(err))
		return
	}
	s.logger.Info("factory command's task failed", zap.String("task_id", t.TaskID.String()), zap.String("why", why))
	s.ended(ctx, after, cancel)
}

// ended lets go of what a task that ended held: the task files its args
// name, once no other task needs them; and, with cancel, the command on its
// instance.
func (s *Service) ended(ctx context.Context, t *models.MinionTask, cancel bool) {
	s.publish(t)
	if t.Factory == nil {
		return
	}
	if s.files != nil {
		for _, id := range t.Factory.Files {
			if err := s.files.ReleaseTaskFile(ctx, id); err != nil {
				s.logger.Warn("factory commands: release a task file", zap.String("file_id", id.String()), zap.Error(err))
			}
		}
	}
	if cancel && t.Factory.Instance != "" {
		s.cancelAt(t.Factory.Instance, t.TaskID)
	}
}

// cancelAt tells instance to stop task, in the background: best effort. An
// instance that does not hear it stops the command at its next report,
// which core answers not_found.
func (s *Service) cancelAt(instance string, task uuid.UUID) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, done := context.WithTimeout(context.Background(), 2*s.cfg.CallTimeout)
		defer done()
		queue, err := s.queueOf(ctx, instance)
		if err != nil || queue == "" {
			return
		}
		rep, err := s.rpc.Call(ctx, queue, protocol.TypeFactoryCommandCancel, protocol.FactoryCommandCancelRequest{TaskID: task.String()},
			s.cfg.CallTimeout)
		if err == nil && rep.Error != nil {
			err = fmt.Errorf("%s: %s", rep.Error.Code, rep.Error.Message)
		}
		if err != nil {
			s.logger.Info("factory commands: a cancel did not reach the instance", zap.String("instance", instance),
				zap.String("task_id", task.String()), zap.Error(err))
		}
	}()
}

// queueOf is a registered instance's rpc_queue: "" for one not registered.
func (s *Service) queueOf(ctx context.Context, instance string) (string, error) {
	mods, err := s.modules.ListActive(ctx)
	if err != nil {
		return "", err
	}
	for _, m := range mods {
		if m.Instance == instance {
			return m.RPCQueue, nil
		}
	}
	return "", nil
}

func (s *Service) publish(t *models.MinionTask) {
	if s.bus == nil || t == nil {
		return
	}
	s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(),
	}))
}
