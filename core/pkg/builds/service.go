// Package builds runs minion builds (contracts/minion-build.md): factory kinds
// and their build schemas, an operation's build files, and its builds. A build is offered to a live instance of its factory kind over the
// control plane (factory.build), followed through progress and result, its
// artifact checked, and a successful one creates the minion.
//
// Authorization (operation membership and role) is the caller's job; the
// service validates, stores, and talks to factories.
package builds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Registry is the slice of the module registry the service reads.
type Registry interface {
	LatestBuildSchema(ctx context.Context, moduleName string) (*models.ModuleBuildSchema, error)
	LatestBuildSchemas(ctx context.Context) (map[string]*models.ModuleBuildSchema, error)
	LatestIcons(ctx context.Context, moduleType string) (map[string]string, error)
	ListActive(ctx context.Context) ([]models.Module, error)
	FindByInstance(ctx context.Context, instance string) (models.Module, error)
}

// Channels gives a channel kind's current minion connection schema (nil when
// no instance declared one): what a build's connection settings follow.
type Channels interface {
	Schema(ctx context.Context, moduleName string) (*protocol.ConnectionSchema, error)
}

// Caller calls a module's rpc_queue (messaging.RPCClient).
type Caller interface {
	Call(ctx context.Context, queue, opType string, payload any, timeout time.Duration) (protocol.ReplyEnvelope, error)
}

// Store holds build files and artifacts.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

// Signer signs the URLs factories download files and upload artifacts with.
type Signer interface {
	GetURL(ctx context.Context, key string, expiry time.Duration) (string, error)
	PutURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// Repos are the stores the service writes.
type Repos struct {
	Files   repository.IBuildFileRepository
	Builds  repository.IBuildRepository
	Minions repository.IMinionRepository
}

// Config tunes the service. Zero values take the defaults.
type Config struct {
	// Timeout bounds the time a build spends queued, and then building.
	Timeout time.Duration
	// FileMaxSize caps one uploaded build file; ArtifactMaxSize is the
	// max_size factories are given.
	FileMaxSize     int64
	ArtifactMaxSize int64
	// FileGrace is how long an upload nothing refers to is kept.
	FileGrace time.Duration
	// TickInterval paces the scheduler: dispatching queued builds, failing
	// the ones that ran out of time or lost their factory.
	TickInterval time.Duration
	// SweepInterval paces the removal of unreferenced uploads.
	SweepInterval time.Duration
	// CallTimeout bounds one factory.build or factory.build.cancel call.
	CallTimeout time.Duration
	// Lease is how long a core instance holds a queued build it offers to
	// factories.
	Lease time.Duration
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.Timeout, 30*time.Minute)
	def(&c.FileGrace, 24*time.Hour)
	def(&c.TickInterval, 2*time.Second)
	def(&c.SweepInterval, time.Hour)
	def(&c.CallTimeout, 10*time.Second)
	// Offering calls instances one after another, CallTimeout each.
	def(&c.Lease, 2*time.Minute)
	if c.FileMaxSize <= 0 {
		c.FileMaxSize = 64 << 20
	}
	if c.ArtifactMaxSize <= 0 {
		c.ArtifactMaxSize = 256 << 20
	}
	return c
}

// Errors the API layer maps to client errors.
var (
	ErrFileNotFound  = errors.New("build file not found")
	ErrBuildNotFound = errors.New("build not found")
)

// ValidationError is a client error. Problems holds reasons by where they
// are: "description", "target", "platform", "values.<key>", "connections",
// "connections[<i>]", "connections[<i>].name", "connections[<i>].description",
// "connections[<i>].settings.<key>", "connections[<i>].values.<key>", or ""
// when the problem is not one place's.
type ValidationError struct {
	Message  string
	Problems map[string]string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Service runs builds.
type Service struct {
	repos    Repos
	registry Registry
	channels Channels
	rpc      Caller
	store    Store
	signer   Signer
	cfg      Config
	logger   *zap.Logger
	now      func() time.Time

	kick      chan struct{}
	stop      chan struct{}
	wg        sync.WaitGroup
	next      atomic.Uint64 // rotates the instances builds are offered to
	verifying sync.Map      // build ids whose artifact this process checks
}

func NewService(repos Repos, registry Registry, channels Channels, rpc Caller, store Store, signer Signer, cfg Config, logger *zap.Logger) *Service {
	return &Service{
		repos: repos, registry: registry, channels: channels, rpc: rpc, store: store, signer: signer,
		cfg:    cfg.withDefaults(),
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
		kick:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
	}
}

// Start runs the scheduler and the upload sweeper until Stop.
func (s *Service) Start() {
	s.wg.Add(1)
	go s.loop()
}

// Stop ends the scheduler, waiting for an offer or check in progress.
func (s *Service) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
}

func (s *Service) loop() {
	defer s.wg.Done()
	tick := time.NewTicker(s.cfg.TickInterval)
	defer tick.Stop()
	sweep := time.NewTicker(s.cfg.SweepInterval)
	defer sweep.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-s.stop
		cancel()
	}()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
		case <-s.kick:
		case <-sweep.C:
			s.sweepFiles(ctx)
			continue
		}
		s.Tick(ctx)
	}
}

// wake makes the scheduler run now, e.g. for a build just queued.
func (s *Service) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// notFound maps a repository miss to sentinel.
func notFound(err, sentinel error) error {
	if repository.IsNotFound(err) {
		return sentinel
	}
	return err
}
