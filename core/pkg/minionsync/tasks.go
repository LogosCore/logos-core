package minionsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Tasks under a session (docs: contracts/minion-messages.md, Asking for
// tasks): a get_tasks takes the tasks waiting, those sent before and not
// yet acked first, at most its max; an ack or a result for a task stops it
// being sent again; a result's parts are kept, once each; a cancel goes
// with every reply while an operator's stop waits for the task's end.

// WithTasks gives the service the tasks operators queue. Without it, a
// get_tasks gets nothing and task items are answered as unknown.
func (s *Service) WithTasks(tasks repository.IMinionTaskRepository) *Service {
	s.tasks = tasks
	return s
}

// taskID reads a task id: 16 raw bytes of a UUID.
func taskID(raw []byte) (uuid.UUID, error) {
	return uuid.FromBytes(raw)
}

// takeAck records the tasks an ack says the minion received.
func (s *Service) takeAck(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	var ack protocol.AckItem
	if protocol.UnmarshalCBOR(raw, &ack) != nil {
		return errItem(protocol.ErrItemBadMessage, "the ack does not read", i)
	}
	if err := ack.Validate(); err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), i)
	}
	ids := make([]uuid.UUID, 0, len(ack.Tasks))
	for _, t := range ack.Tasks {
		id, err := taskID(t)
		if err != nil {
			return errItem(protocol.ErrItemBadMessage, "an acked task id does not read", i)
		}
		ids = append(ids, id)
	}
	if err := s.tasks.Ack(ctx, session.SessionID, ids, s.now()); err != nil {
		return nil, fmt.Errorf("ack tasks: %w", err)
	}
	return nil, nil
}

// takeResult keeps one part of a task's output.
func (s *Service) takeResult(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	var res protocol.ResultItem
	if protocol.UnmarshalCBOR(raw, &res) != nil {
		return errItem(protocol.ErrItemBadMessage, "the result does not read", i)
	}
	if err := res.Validate(); err != nil {
		return errItem(protocol.ErrItemBadMessage, err.Error(), i)
	}
	id, err := taskID(res.Task)
	if err != nil {
		return errItem(protocol.ErrItemBadMessage, "the result's task id does not read", i)
	}
	task, fresh, err := s.tasks.AddResult(ctx, session.SessionID, id, repository.MinionTaskResult{
		Seq: res.Seq, Done: res.Done, OK: res.OK, Exit: res.Exit, Err: res.Err, Out: res.Out, Data: res.Data, At: s.now(),
	})
	if repository.IsNotFound(err) {
		return errItem(protocol.ErrItemUnknownTask, "no such task under this session", i)
	}
	if err != nil {
		return nil, fmt.Errorf("keep a result: %w", err)
	}
	if fresh {
		s.publishTask(task)
		if res.Done {
			s.logger.Info("minion task done", zap.String("task_id", id.String()), zap.String("state", task.State),
				zap.String("session_id", session.SessionID.String()))
		}
	}
	return nil, nil
}

// deliver answers a get_tasks asking for at most max (nil: no limit of the
// minion's): the tasks waiting, marked sent.
func (s *Service) deliver(ctx context.Context, session *models.MinionSession, max *uint64) ([]cbor.RawMessage, error) {
	limit := models.MaxMinionTasksPerReply
	if max != nil && *max < uint64(limit) {
		limit = int(*max)
	}
	waiting, err := s.tasks.ForDelivery(ctx, session.SessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("tasks to deliver: %w", err)
	}
	if len(waiting) == 0 {
		return nil, nil
	}
	items := make([]cbor.RawMessage, 0, len(waiting))
	ids := make([]uuid.UUID, 0, len(waiting))
	for _, t := range waiting {
		var args map[string]any
		if len(t.WireArgs) > 0 {
			if err := protocol.UnmarshalCBOR(t.WireArgs, &args); err != nil {
				return nil, fmt.Errorf("the args of task %s: %w", t.TaskID, err)
			}
		}
		item, err := protocol.MarshalCBOR(protocol.TaskItem{T: protocol.ItemTask, Task: t.TaskID[:], Cmd: t.Command, Args: args})
		if err != nil {
			return nil, fmt.Errorf("a task item: %w", err)
		}
		items = append(items, item)
		ids = append(ids, t.TaskID)
	}
	if err := s.tasks.MarkSent(ctx, ids, s.now()); err != nil {
		return nil, fmt.Errorf("mark tasks sent: %w", err)
	}
	for i := range waiting {
		s.publishTask(&waiting[i])
	}
	return items, nil
}

// cancels are the cancel items a reply carries: one for each task an
// operator asked to stop that is not over.
func (s *Service) cancels(ctx context.Context, session *models.MinionSession) ([]cbor.RawMessage, error) {
	send, gaveUp, err := s.tasks.CancelsToSend(ctx, session.SessionID, s.now())
	if err != nil {
		return nil, fmt.Errorf("cancels to send: %w", err)
	}
	for i := range gaveUp {
		s.publishTask(&gaveUp[i])
	}
	items := make([]cbor.RawMessage, 0, len(send))
	for _, t := range send {
		item, err := protocol.MarshalCBOR(protocol.CancelItem{T: protocol.ItemCancel, Task: t.TaskID[:]})
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// readGetTasks reads and checks a get_tasks.
func readGetTasks(raw cbor.RawMessage) (protocol.GetTasksItem, error) {
	var get protocol.GetTasksItem
	if protocol.UnmarshalCBOR(raw, &get) != nil {
		return get, errors.New("the get_tasks does not read")
	}
	return get, get.Validate()
}

func (s *Service) publishTask(t *models.MinionTask) {
	s.bus.Publish(eventbus.NewMinionTaskChangedEvent(eventbus.MinionTaskEventPayload{
		TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), OperationID: t.OperationID.String(),
	}))
}
