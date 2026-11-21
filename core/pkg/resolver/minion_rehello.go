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

// rehello: core's own command in every session's console
// (models.CommandRehello): it asks the minion for its hello again, its host
// and process as they are now. Core keeps it as a task, running until a
// hello comes (done, with what changed) or until it went with
// models.RehelloTries messages and none came (failed): minionsync ends it.
// Nothing goes to the minion as a task: the rehello item goes with core's
// next message, whatever the minion runs.

// rehelloCommand is rehello as a command of a session.
func rehelloCommand() protocol.BuildCommand {
	return protocol.BuildCommand{
		ID:    models.CommandRehello,
		Label: "Ask for a hello",
		Description: "Asks the minion for its hello again: its host and process as they are now (its user, addresses, " +
			"version). It goes with core's next message to the minion, whatever the minion runs, and the task ends when " +
			"the hello comes, saying what changed. Core runs it: no task goes to the minion.",
	}
}

// runRehello keeps a rehello as a task, running, and asks the session's
// minion for its hello behind it.
func (r *minionTaskResolver) runRehello(ctx context.Context, s *models.MinionSession, uid uuid.UUID, values map[string]any) (*model.MinionTask, error) {
	now := time.Now().UTC()
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: models.CommandRehello,
		Args: values, IssuedBy: uid, State: models.MinionTaskRunning, QueuedAt: now, AckedAt: &now, StartedAt: &now}
	if err := r.tasks.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("keep the rehello task: %w", err)
	}
	if _, _, err := r.tasks.AddResult(ctx, s.SessionID, t.TaskID, repository.MinionTaskResult{
		Seq: 0, Out: []byte("asked the minion for its hello: on its way with core's next message\n"), At: now,
	}); err != nil {
		return nil, fmt.Errorf("start the rehello task: %w", err)
	}
	if err := r.limits.AskRehello(ctx, s); err != nil {
		ok := false
		_, _, _ = r.tasks.AddResult(ctx, s.SessionID, t.TaskID, repository.MinionTaskResult{
			Seq: 1, Done: true, OK: &ok, Err: err.Error(), At: time.Now().UTC(),
		})
		return nil, err
	}
	after, err := r.tasks.FindByTaskID(ctx, t.TaskID)
	if err != nil {
		return nil, fmt.Errorf("read the rehello task: %w", err)
	}
	r.publish(after)
	return r.toModel(ctx, after, r.names(ctx, []models.MinionTask{*after}))
}
