package resolver

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// StartScheduled as the repository does it: a SCHEDULED task on in state,
// its time dropped when drop; IsNotFound for one not SCHEDULED.
func (f *compositeTasks) StartScheduled(_ context.Context, id uuid.UUID, state string, drop bool) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || t.State != models.MinionTaskScheduled {
		return nil, repository.ErrNotFound
	}
	t.State = state
	if drop {
		t.ScheduledAt = nil
	}
	c := *t
	return &c, nil
}

// RequestCancel as the repository does it for a task nothing of which went
// out: cancelled now.
func (f *compositeTasks) RequestCancel(_ context.Context, id uuid.UUID, at time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || (t.State != models.MinionTaskScheduled && t.State != models.MinionTaskQueued) {
		return nil, repository.ErrNotFound
	}
	t.State, t.DoneAt, t.CancelRequestedAt = models.MinionTaskCancelled, &at, &at
	c := *t
	return &c, nil
}

// CountOpenSubtasks: a composite task's model counts them; these have none.
func (f *compositeTasks) CountOpenSubtasks(context.Context, uuid.UUID) (int, error) { return 0, nil }

// fakeComposites records the composite tasks started.
type fakeComposites struct{ started []uuid.UUID }

func (f *fakeComposites) Started(t *models.MinionTask) { f.started = append(f.started, t.TaskID) }

func (f *fakeComposites) StopTask(_ context.Context, t *models.MinionTask) (*models.MinionTask, error) {
	return t, nil
}

// A task queued with a time waits SCHEDULED, nothing of it started: a
// minion's, a composite's (its factory's code not called) and a factory
// command's (offered to none). At its time each goes on in the state it
// would have been queued in, and then starts (docs: Tasks and Results,
// Scheduled tasks).
func TestScheduledTasks(t *testing.T) {
	g, runner := newFactoryRig(t)
	composites := &fakeComposites{}
	g.r.composites = composites
	ctx, session := factoryAdminCtx(), g.session.SessionID.String()
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	when := at.Format(time.RFC3339)

	queued := map[string]*models.MinionTask{}
	for command, args := range map[string]map[string]any{
		"ls":      {"path": "/tmp"},
		"bundle":  {"path": "/x"},
		"whois":   {"address": "203.0.113.7"},
		"archive": {"path": "/x"},
	} {
		m, err := g.r.QueueMinionTask(ctx, session, command, args, &when)
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		task := g.tasks.rows[uuid.MustParse(m.ID)]
		if task.State != models.MinionTaskScheduled || task.ScheduledAt == nil || !task.ScheduledAt.Equal(at) ||
			m.ScheduledAt == nil || *m.ScheduledAt != when {
			t.Fatalf("%s: %s at %v (%v)", command, task.State, task.ScheduledAt, m.ScheduledAt)
		}
		queued[command] = task
	}
	if len(composites.started) != 0 || len(runner.queued) != 0 {
		t.Fatalf("started before their time: composites %v, factory %v", composites.started, runner.queued)
	}

	for command, want := range map[string]string{
		"ls": models.MinionTaskQueued, "bundle": models.MinionTaskComposing, "whois": models.MinionTaskFactory,
	} {
		if err := g.r.StartScheduled(context.Background(), queued[command]); err != nil {
			t.Fatal(err)
		}
		task := g.tasks.rows[queued[command].TaskID]
		if task.State != want || task.ScheduledAt == nil {
			t.Errorf("%s at its time: %s, at %v", command, task.State, task.ScheduledAt)
		}
	}
	if !slices.Equal(composites.started, []uuid.UUID{queued["bundle"].TaskID}) ||
		!slices.Equal(runner.queued, []uuid.UUID{queued["whois"].TaskID}) {
		t.Fatalf("started: composites %v, factory %v", composites.started, runner.queued)
	}
	// Taken on once: a second look leaves it alone.
	if err := g.r.StartScheduled(context.Background(), queued["bundle"]); err != nil || len(composites.started) != 1 {
		t.Fatalf("again: %v, %v", err, composites.started)
	}

	// Run now drops its time; one gone on is not scheduled.
	m, err := g.r.RunMinionTaskNow(ctx, queued["archive"].TaskID.String())
	if err != nil || m.State != models.MinionTaskComposing || m.ScheduledAt != nil {
		t.Fatalf("run now: %+v %v", m, err)
	}
	if _, err := g.r.RunMinionTaskNow(ctx, queued["ls"].TaskID.String()); err == nil || !strings.Contains(err.Error(), "not scheduled") {
		t.Fatalf("run now, gone on: %v", err)
	}
}

// Stop cancels a scheduled task at once, whatever its command.
func TestStopScheduledTask(t *testing.T) {
	g, runner := newFactoryRig(t)
	when := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	m, err := g.r.QueueMinionTask(factoryAdminCtx(), g.session.SessionID.String(), "whois", map[string]any{"address": "x"}, &when)
	if err != nil {
		t.Fatal(err)
	}
	if m, err = g.r.CancelMinionTask(factoryAdminCtx(), m.ID); err != nil || m.State != models.MinionTaskCancelled {
		t.Fatalf("stopped: %+v %v", m, err)
	}
	if len(runner.queued) != 0 {
		t.Fatalf("offered: %v", runner.queued)
	}
}

// A time is a date and time after now; core's own commands take none.
func TestScheduleRefused(t *testing.T) {
	g, _ := newFactoryRig(t)
	ctx, session := factoryAdminCtx(), g.session.SessionID.String()
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	later := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, c := range []struct {
		command, at, want string
	}{
		{"ls", past, "has passed"},
		{"ls", "tomorrow at ten", "RFC 3339"},
		{"help", later, "core runs help itself, at once"},
	} {
		if _, err := g.r.QueueMinionTask(ctx, session, c.command, map[string]any{}, &c.at); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s at %q: %v, want %q", c.command, c.at, err, c.want)
		}
	}
	blank := " "
	m, err := g.r.QueueMinionTask(ctx, session, "ls", map[string]any{"path": "/"}, &blank)
	if err != nil || m.State != models.MinionTaskQueued || m.ScheduledAt != nil {
		t.Fatalf("a blank time is none: %+v %v", m, err)
	}
}
