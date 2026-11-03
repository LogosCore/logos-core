package resolver

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// max_tasks: core's own command in the console of a session whose build's
// factory declares max tasks (protocol.CommandMaxTasks): it sets how many
// tasks the minion runs at once, as the session's Tasks do. Core keeps it
// as a task, running until the minion says it has the limit (done), or does
// not take it, or a newer limit replaces it (failed): minionsync ends it.
// Nothing goes to the minion as a task: a task would wait for a free slot,
// the very limit it changes.

// maxTasksCommand is max_tasks as a command of a session. What the limit is
// now is the session's (its Tasks): the console keeps its commands a while.
func maxTasksCommand() protocol.BuildCommand {
	return protocol.BuildCommand{
		ID:    protocol.CommandMaxTasks,
		Label: "Set max tasks",
		Description: fmt.Sprintf("Sets how many tasks the minion runs at once, 0 to %d; 0 pauses it: no new tasks, "+
			"what it runs goes on. It goes with core's next message to the minion, whatever the minion runs, and the task ends "+
			"when the minion says it has it. Core runs it: no task goes to the minion.", protocol.MaxTasksLimit),
		Params: []protocol.FieldSpec{{
			Key: "max", Type: protocol.FieldInt, Label: "Max tasks", Required: true, Min: 0, Max: protocol.MaxTasksLimit,
			Description: "How many tasks the minion runs at once; 0 pauses it.",
		}},
	}
}

// offersMaxTasks reports whether a session's console has max_tasks: its
// build's factory declares max tasks, and its minion said its limit.
func (r *minionTaskResolver) offersMaxTasks(s *models.MinionSession) bool {
	return r.limits != nil && s.MaxTasksSettable && s.MaxTasks != nil
}

// runMaxTasks keeps a max_tasks as a task, running, and sets the session's
// limit behind it: the task ends when the minion says it has it, at once
// when it has it already.
func (r *minionTaskResolver) runMaxTasks(ctx context.Context, s *models.MinionSession, uid uuid.UUID, values, wire map[string]any) (*model.MinionTask, error) {
	n, _ := protocol.ArgInt(wire, "max")
	max := int(n)
	if err := s.CanSetMaxTasks(max, time.Now().UTC()); err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
	}
	now := time.Now().UTC()
	values["max"] = max
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: protocol.CommandMaxTasks,
		Args: values, IssuedBy: uid, State: models.MinionTaskRunning, QueuedAt: now, AckedAt: &now, StartedAt: &now}
	if err := r.tasks.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("keep the max_tasks task: %w", err)
	}
	from := "?"
	if s.MaxTasks != nil {
		from = fmt.Sprint(*s.MaxTasks)
	}
	if _, _, err := r.tasks.AddResult(ctx, s.SessionID, t.TaskID, repository.MinionTaskResult{
		Seq: 0, Out: fmt.Appendf(nil, "max tasks %s → %d: on its way to the minion\n", from, max), At: now,
	}); err != nil {
		return nil, fmt.Errorf("start the max_tasks task: %w", err)
	}
	if err := r.limits.SetMaxTasks(ctx, s, max); err != nil {
		ok := false
		_, _, _ = r.tasks.AddResult(ctx, s.SessionID, t.TaskID, repository.MinionTaskResult{
			Seq: 1, Done: true, OK: &ok, Err: err.Error(), At: time.Now().UTC(),
		})
		if models.IsMaxTasksRefusal(err) {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
		return nil, err
	}
	// As it stands now: ended already when the minion had the limit.
	after, err := r.tasks.FindByTaskID(ctx, t.TaskID)
	if err != nil {
		return nil, fmt.Errorf("read the max_tasks task: %w", err)
	}
	r.publish(after)
	return r.toModel(ctx, after, r.names(ctx, []models.MinionTask{*after}))
}
