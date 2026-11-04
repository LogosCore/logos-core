package resolver

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// connect and disconnect: core's own commands in the console of a session
// whose connection is bind-persistent (a minion that listens). They open
// and close its target's connection, as the target's Connect and
// Disconnect do, and are kept as tasks so the session's history says who
// connected when. Nothing goes to the minion: one not connected could not
// get a task.

// TaskConnector opens and closes a bind-persistent target's connection
// (minionsync.Service), and says whether one is open now.
type TaskConnector interface {
	ConnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error)
	DisconnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error)
	TargetConnected(targetID uuid.UUID) bool
}

// connectCommands are core's connect and disconnect.
func connectCommands() []protocol.BuildCommand {
	return []protocol.BuildCommand{
		{
			ID: protocol.CommandConnect, Label: "Connect",
			Description: "Opens the connection to this session's minion now: core asks the channel to dial it, " +
				"whatever its target's policy. Core runs it: nothing goes to the minion.",
		},
		{
			ID: protocol.CommandDisconnect, Label: "Disconnect",
			Description: "Closes the connection to this session's minion; the minion keeps running and listening. " +
				"A target of policy always is opened again in a few seconds. Core runs it: nothing goes to the minion.",
		},
	}
}

// offersConnect reports whether a session's console has connect and
// disconnect: its connection is bind-persistent, and core can open one.
func (r *minionTaskResolver) offersConnect(s *models.MinionSession) bool {
	return r.connector != nil && r.targets != nil && s.ConnKind == protocol.KindBindPersistent
}

// runConnect keeps a connect or disconnect as a task, running, and opens or
// closes the session's target's connection behind it (an open waits for the
// channel's dial, up to its deadline): the task ends with what came of it.
func (r *minionTaskResolver) runConnect(ctx context.Context, s *models.MinionSession, uid uuid.UUID, values map[string]any, command string) (*model.MinionTask, error) {
	target, err := r.targets.FindBySessionID(ctx, s.SessionID)
	switch {
	case repository.IsNotFound(err):
		return nil, moduleGQLError(ctx, errCodeBadUserInput, fmt.Sprintf(
			"session #%d has no target: core cannot reach its minion. Add a target first (the session's Connections, or its build's Targets)", s.Number))
	case err != nil:
		return nil, fmt.Errorf("look for the session's target: %w", err)
	}
	now := time.Now().UTC()
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: command,
		Args: values, IssuedBy: uid, State: models.MinionTaskRunning, QueuedAt: now, AckedAt: &now, StartedAt: &now}
	if err := r.tasks.Insert(ctx, t); err != nil {
		return nil, fmt.Errorf("keep the %s task: %w", command, err)
	}
	r.publish(t)
	log := logger.From(ctx)
	go func(ctx context.Context) {
		ok, out, why := r.connectOutcome(ctx, command, target)
		after, _, err := r.tasks.AddResult(ctx, s.SessionID, t.TaskID, repository.MinionTaskResult{
			Seq: 1, Done: true, OK: &ok, Out: []byte(out), Err: why, At: time.Now().UTC(),
		})
		if err != nil {
			log.Warn("end a connect task", zap.String("task_id", t.TaskID.String()), zap.Error(err))
			return
		}
		r.publish(after)
	}(context.WithoutCancel(ctx))
	return r.toModel(ctx, t, r.names(ctx, []models.MinionTask{*t}))
}

// connectOutcome opens or closes target's connection: whether it worked,
// the task's output, and its error when it did not. Whether the target is
// connected is whether a connection is open now, not its stored state,
// which may be behind on it.
func (r *minionTaskResolver) connectOutcome(ctx context.Context, command string, target *models.MinionTarget) (bool, string, string) {
	connected := r.connector.TargetConnected(target.TargetID)
	if command == protocol.CommandConnect {
		if connected {
			return true, fmt.Sprintf("already connected (%s)\n", target.Name), ""
		}
		after, err := r.connector.ConnectTarget(ctx, target.TargetID)
		if err != nil {
			return false, "", "the open failed: " + err.Error()
		}
		if r.connector.TargetConnected(target.TargetID) {
			return true, fmt.Sprintf("connected (%s)\n", after.Name), ""
		}
		return false, "", stateText(after)
	}
	if !connected {
		return true, fmt.Sprintf("not connected (%s)\n", target.Name), ""
	}
	after, err := r.connector.DisconnectTarget(ctx, target.TargetID)
	if err != nil {
		return false, "", "the close failed: " + err.Error()
	}
	if r.connector.TargetConnected(target.TargetID) {
		return false, "", "still connected"
	}
	out := fmt.Sprintf("disconnected (%s)\n", after.Name)
	if after.PolicyOf() == models.PolicyAlways {
		out += "Its target's policy is always: core opens it again in a few seconds.\n"
	}
	return true, out, ""
}

// stateText is a target's state and its detail: "unreachable: dial tcp …".
func stateText(t *models.MinionTarget) string {
	if t.StateDetail == "" {
		return t.State
	}
	return t.State + ": " + t.StateDetail
}
