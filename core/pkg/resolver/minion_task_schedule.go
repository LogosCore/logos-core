package resolver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// Scheduled tasks (docs: contracts/minion-messages/tasks-and-results,
// Scheduled tasks): a task queued with a time waits SCHEDULED, nothing of it
// gone anywhere, and goes on at that time (pkg/scheduledtasks calls
// StartScheduled) or at once (Run now) as one queued then.

// scheduleOf reads an operator's time for a task (queueMinionTask's at): an
// RFC 3339 date and time, after now. None, or a blank one: the task goes on
// at once.
func scheduleOf(ctx context.Context, at *string, now time.Time) (*time.Time, error) {
	if at == nil || strings.TrimSpace(*at) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*at))
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "at: a date and time, RFC 3339 (2026-11-24T10:00:00Z)")
	}
	if !t.After(now) {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf("at: %s has passed", t.UTC().Format(time.RFC3339)))
	}
	u := t.UTC()
	return &u, nil
}

// StartScheduled takes t, SCHEDULED, on at its time (scheduledtasks.Starter):
// as queued then, its time kept. One not SCHEDULED any more is left alone.
func (r *minionTaskResolver) StartScheduled(ctx context.Context, t *models.MinionTask) error {
	_, err := r.startScheduled(ctx, t, false)
	return err
}

// startScheduled takes t, SCHEDULED, on in its StartState, its time dropped
// when drop (Run now), and does what queueing does once a task is kept: a
// composite command's factory's code called, a factory command offered to
// its factory, and the task published, so a minion's goes out where its
// minion keeps a connection. One not SCHEDULED any more comes back nil.
func (r *minionTaskResolver) startScheduled(ctx context.Context, t *models.MinionTask, drop bool) (*models.MinionTask, error) {
	after, err := r.tasks.StartScheduled(ctx, t.TaskID, t.StartState(), drop)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("take the scheduled task on: %w", err)
	}
	switch {
	case after.Composite != nil && r.composites != nil:
		r.composites.Started(after)
	case after.Factory != nil && r.factory != nil:
		r.factory.Queued(after)
	}
	r.publish(after)
	return after, nil
}

// RunMinionTaskNow takes a SCHEDULED task on at once, its time dropped
// (docs: Scheduled tasks, Run now).
func (r *minionTaskResolver) RunMinionTaskNow(ctx context.Context, id string) (*model.MinionTask, error) {
	tid, err := uuid.Parse(id)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "task not found")
		}
		return nil, err
	}
	if _, err := r.session(ctx, t.SessionID.String(), models.OperationRoleOperator); err != nil {
		return nil, err
	}
	notScheduled := moduleGQLError(ctx, errCodeBadUserInput, "the task is not scheduled: it went on already")
	if t.State != models.MinionTaskScheduled {
		return nil, notScheduled
	}
	after, err := r.startScheduled(ctx, t, true)
	if err != nil {
		return nil, err
	}
	if after == nil {
		return nil, notScheduled
	}
	return r.toModel(ctx, after, r.names(ctx, []models.MinionTask{*after}))
}
