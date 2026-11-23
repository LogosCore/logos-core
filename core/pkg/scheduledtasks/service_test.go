package scheduledtasks

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

type fakeTasks struct {
	rows []models.MinionTask
	err  error
	at   time.Time
}

func (f *fakeTasks) ScheduledDue(_ context.Context, now time.Time, limit int) ([]models.MinionTask, error) {
	f.at = now
	if f.err != nil {
		return nil, f.err
	}
	var out []models.MinionTask
	for _, t := range f.rows {
		if t.State == models.MinionTaskScheduled && !t.ScheduledAt.After(now) && len(out) < limit {
			out = append(out, t)
		}
	}
	return out, nil
}

type fakeStarter struct {
	started []uuid.UUID
	fail    uuid.UUID
}

func (f *fakeStarter) StartScheduled(_ context.Context, t *models.MinionTask) error {
	if t.TaskID == f.fail {
		return errors.New("gone")
	}
	f.started = append(f.started, t.TaskID)
	return nil
}

// A look takes on the tasks whose time came, each once a look, and goes on
// past one that fails; with nothing to take them on, it looks at none.
func TestLook(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := now.Add(d); return &x }
	due, later, failing := uuid.New(), uuid.New(), uuid.New()
	tasks := &fakeTasks{rows: []models.MinionTask{
		{TaskID: due, State: models.MinionTaskScheduled, ScheduledAt: at(-time.Second)},
		{TaskID: failing, State: models.MinionTaskScheduled, ScheduledAt: at(0)},
		{TaskID: later, State: models.MinionTaskScheduled, ScheduledAt: at(time.Minute)},
	}}
	s := New(tasks, zap.NewNop())
	s.now = func() time.Time { return now }
	s.Look(context.Background())
	if !tasks.at.IsZero() {
		t.Fatal("looked with nothing to take the tasks on")
	}
	starter := &fakeStarter{fail: failing}
	s.WithStarter(starter).Look(context.Background())
	if !slices.Equal(starter.started, []uuid.UUID{due}) || !tasks.at.Equal(now) {
		t.Fatalf("started %v at %v", starter.started, tasks.at)
	}
	tasks.err = errors.New("down")
	s.Look(context.Background())
	if len(starter.started) != 1 {
		t.Fatalf("started %v with the store down", starter.started)
	}
}

// Start looks at once and once a Tick; Stop ends it.
func TestStartStop(t *testing.T) {
	now := time.Now().UTC()
	id := uuid.New()
	tasks := &fakeTasks{rows: []models.MinionTask{{TaskID: id, State: models.MinionTaskScheduled, ScheduledAt: &now}}}
	starter := &fakeStarter{}
	s := New(tasks, zap.NewNop()).WithStarter(starter)
	s.Start()
	s.Stop()
	if len(starter.started) == 0 || starter.started[0] != id {
		t.Fatalf("started %v", starter.started)
	}
	s.Stop() // a second Stop is none
}
