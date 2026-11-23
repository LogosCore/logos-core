// Package scheduledtasks takes on the tasks whose time came (docs:
// contracts/minion-messages/tasks-and-results, Scheduled tasks). A task
// queued with a time waits in core, SCHEDULED, nothing of it gone anywhere;
// once a second core looks for those whose time came, and the task resolver
// takes each on as one queued then would be. Taking one on is a conditional
// update, so two looks, or two cores, never take one on twice
// (Architecture, One core instance).
package scheduledtasks

import (
	"context"
	"sync"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

// Tick is how often core looks for the tasks whose time came: a task goes
// on within a second of it.
const Tick = time.Second

// batch is the most tasks one look takes on; the rest wait for the next.
const batch = 256

// Tasks finds the tasks whose time came (repository.IMinionTaskRepository).
type Tasks interface {
	ScheduledDue(ctx context.Context, now time.Time, limit int) ([]models.MinionTask, error)
}

// Starter takes a SCHEDULED task on as queued now: the task resolver, which
// does what queueing does once a task is kept (a minion's queued and pushed,
// a composite's factory's code called, a factory command offered). One not
// SCHEDULED any more (stopped, run now) it leaves alone.
type Starter interface {
	StartScheduled(ctx context.Context, t *models.MinionTask) error
}

// Service looks for the tasks whose time came, once a Tick.
type Service struct {
	tasks  Tasks
	logger *zap.Logger
	now    func() time.Time

	mu      sync.Mutex
	starter Starter

	stopped  chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New is a Service over tasks; it takes nothing on until WithStarter.
func New(tasks Tasks, logger *zap.Logger) *Service {
	return &Service{tasks: tasks, logger: logger, now: func() time.Time { return time.Now().UTC() }, stopped: make(chan struct{})}
}

// WithStarter sets what takes the tasks on, once the resolvers are made.
func (s *Service) WithStarter(st Starter) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starter = st
	return s
}

// Start looks once a Tick until Stop.
func (s *Service) Start() {
	s.wg.Add(1)
	go s.run()
}

// Stop ends the looks, and waits for the one under way.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
	s.wg.Wait()
}

func (s *Service) run() {
	defer s.wg.Done()
	t := time.NewTicker(Tick)
	defer t.Stop()
	for {
		s.Look(context.Background())
		select {
		case <-s.stopped:
			return
		case <-t.C:
		}
	}
}

// Look takes on the tasks whose time came by now, the earliest first.
func (s *Service) Look(ctx context.Context) {
	s.mu.Lock()
	st := s.starter
	s.mu.Unlock()
	if st == nil {
		return
	}
	due, err := s.tasks.ScheduledDue(ctx, s.now(), batch)
	if err != nil {
		s.logger.Error("scheduled tasks: the tasks whose time came", zap.Error(err))
		return
	}
	for i := range due {
		if err := st.StartScheduled(ctx, &due[i]); err != nil {
			s.logger.Error("scheduled tasks: take a task on", zap.String("task_id", due[i].TaskID.String()), zap.Error(err))
		}
	}
}
