package minionsync

import (
	"context"
	"fmt"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Max tasks (docs: contracts/minion-messages/tasks-and-results, Max tasks):
// the operator's limit of tasks a session's minion runs at once, set from
// the session's Tasks or with core's max_tasks command. Core keeps it on
// the session, the outbox sends it until the minion says it has it, and
// the max_tasks commands waiting for it end with what came of it: done when
// the minion says that limit, failed when it does not take it or a newer
// limit replaces theirs.

// SetMaxTasks sets the operator's limit for the session: kept, sent with
// core's next message to the minion (a nudge: at once where it can go at
// once), and the max_tasks commands waiting for another limit ended,
// replaced; those waiting for this one ended done when the minion has it
// already. session is updated in place.
func (s *Service) SetMaxTasks(ctx context.Context, session *models.MinionSession, max int) error {
	if err := session.CanSetMaxTasks(max, s.now()); err != nil {
		return err
	}
	if err := s.sessions.SetMaxTasksWanted(ctx, session.SessionID, max); err != nil {
		return fmt.Errorf("set max tasks: %w", err)
	}
	session.MaxTasksSet, session.MaxTasksSends = &max, 0
	s.endMaxTasksCommands(ctx, session)
	s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
	s.Nudge(session.SessionID, false)
	return nil
}

// endMaxTasksCommands ends the session's max_tasks commands that wait no
// more: done, the minion having said their limit; failed, another limit
// set since, or the minion not taking it (models.MaxTasksTries messages
// unanswered). Errors are logged: the commands end the next time.
func (s *Service) endMaxTasksCommands(ctx context.Context, session *models.MinionSession) {
	if s.tasks == nil {
		return
	}
	open, err := s.tasks.OpenOfCommand(ctx, session.SessionID, protocol.CommandMaxTasks)
	if err != nil {
		s.logger.Warn("max_tasks commands", zap.String("session_id", session.SessionID.String()), zap.Error(err))
		return
	}
	for _, t := range open {
		want, ok := maxOfCommand(t)
		var done bool
		var out, why string
		switch {
		case !ok:
			why = "its limit does not read"
		case session.MaxTasksSet != nil && *session.MaxTasksSet != want:
			why = fmt.Sprintf("replaced by max tasks %d before the minion had it", *session.MaxTasksSet)
		case session.MaxTasks != nil && *session.MaxTasks == want:
			done, out = true, fmt.Sprintf("the minion runs %d at once now%s\n", want, pausedNote(want))
		case session.MaxTasksNotTaken() != nil:
			why = fmt.Sprintf("the minion did not take it: core sent it with %d messages, and it never said it has it", models.MaxTasksTries)
		default:
			continue // still on its way
		}
		after, _, err := s.tasks.AddResult(ctx, session.SessionID, t.TaskID, repository.MinionTaskResult{
			Seq: 1, Done: true, OK: &done, Out: []byte(out), Err: why, At: s.now(),
		})
		if err != nil {
			s.logger.Warn("end a max_tasks command", zap.String("task_id", t.TaskID.String()), zap.Error(err))
			continue
		}
		if after != nil {
			s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
				TaskID: after.TaskID.String(), SessionID: after.SessionID.String(), OperationID: after.OperationID.String(),
			}))
		}
	}
}

// maxOfCommand is the limit a max_tasks command asked for.
func maxOfCommand(t models.MinionTask) (int, bool) {
	n, ok := protocol.AsInt64(t.Args["max"])
	return int(n), ok && n >= 0 && n <= protocol.MaxTasksLimit
}

// pausedNote says what a limit of 0 means.
func pausedNote(max int) string {
	if max == 0 {
		return ": paused, it takes no new tasks"
	}
	return ""
}
