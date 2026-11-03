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
	"github.com/logoscore/logos-core/core/pkg/taskdata"
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

// takeAck records the tasks an ack says the minion received; one with a
// transfer is the minion asking for a chunk of a file core sends it
// (uploads.go), answered once the reply's other items are in.
func (s *Service) takeAck(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, *pull, error) {
	var ack protocol.AckItem
	if protocol.UnmarshalCBOR(raw, &ack) != nil {
		out, err := errItem(protocol.ErrItemBadMessage, "the ack does not read", i)
		return out, nil, err
	}
	if err := ack.Validate(); err != nil {
		out, err := errItem(protocol.ErrItemBadMessage, err.Error(), i)
		return out, nil, err
	}
	ids := make([]uuid.UUID, 0, len(ack.Tasks))
	for _, t := range ack.Tasks {
		id, err := taskID(t)
		if err != nil {
			out, err := errItem(protocol.ErrItemBadMessage, "an acked task id does not read", i)
			return out, nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		if err := s.tasks.Ack(ctx, session.SessionID, ids, s.now()); err != nil {
			return nil, nil, fmt.Errorf("ack tasks: %w", err)
		}
	}
	if ack.Xfer != nil {
		return nil, &pull{xfer: ack.Xfer, next: *ack.Next, ctx: ack.Ctx, i: i}, nil
	}
	return nil, nil, nil
}

// takeResult keeps one part of a task's output. A part that says where
// the minion stands is answered with its resume when resume is set (a call
// carries the outbox's resumes instead).
func (s *Service) takeResult(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int, resume bool) ([]cbor.RawMessage, error) {
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
	// Listings as core keeps them: names, and modes read by the host's OS.
	ls := make([]protocol.ListingPiece, len(res.Ls))
	for j, p := range res.Ls {
		ls[j] = p.Named(session.Hello.OS)
	}
	task, fresh, err := s.tasks.AddResult(ctx, session.SessionID, id, repository.MinionTaskResult{
		Seq: res.Seq, Done: res.Done, OK: res.OK, Exit: res.Exit, Err: res.Err, Out: res.Out, Data: res.Data, At: s.now(), Ctx: res.Ctx,
		Named: func(result string, data []byte) ([]byte, error) {
			return protocol.NamedResultData(result, data)
		},
		Check:  taskdata.Check,
		Latest: taskdata.Latest,
		// A dir_listing command's data is its listing, made by core.
		Compose: func(result string) []byte {
			if result != protocol.ResultDirListing {
				return nil
			}
			return taskdata.ListingData(ls)
		},
	})
	if repository.IsNotFound(err) {
		return errItem(protocol.ErrItemUnknownTask, "no such task under this session", i)
	}
	// A part after the task ended: the minion delivered it, so no error;
	// it just changes nothing.
	if errors.Is(err, repository.ErrMinionTaskEnded) {
		s.logger.Info("minion task part after its end, ignored", zap.String("task_id", id.String()),
			zap.Uint64("seq", res.Seq), zap.String("session_id", session.SessionID.String()))
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keep a result: %w", err)
	}
	if fresh {
		// What it saw of its machine's files, into the snapshot as it comes
		// (any command's): before the task is told changed, so a listing
		// shown from it is there.
		if s.fileBrowser != nil && len(ls) > 0 {
			s.fileBrowser.Listings(ctx, task, session, ls, s.now())
		}
		s.publishTask(task)
		if res.Done {
			s.logger.Info("minion task done", zap.String("task_id", id.String()), zap.String("state", task.State),
				zap.String("session_id", session.SessionID.String()))
			// A file still coming for it stops with it; one going to it
			// is checked, and let go.
			s.endFile(ctx, task)
			s.endUpload(ctx, task, res)
			// The file browser takes what it says of its machine's files,
			// after this exchange (a listing may be large).
			if s.fileBrowser != nil {
				ended := *task
				go s.fileBrowser.TaskEnded(context.WithoutCancel(ctx), &ended)
			}
		}
	}
	// A part that says where the minion stands is answered with a resume
	// giving it back (a sent-again part too: the resume may have been
	// lost): a minion that keeps nothing goes on with the task from there.
	// Not once an operator asked to stop it: the cancel carries the ctx.
	if resume && !res.Done && res.Ctx != nil && task.CancelRequestedAt == nil && !task.Finished() {
		return resumeItem(task.TaskID, res.Ctx)
	}
	return nil, nil
}

// resumeItem is a resume of the task from ctx.
func resumeItem(task uuid.UUID, ctx []byte) ([]cbor.RawMessage, error) {
	item, err := protocol.MarshalCBOR(protocol.ResumeItem{T: protocol.ItemResume, Task: task[:], Ctx: ctx})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

// resumes are resumes of the session's tasks under way whose newest part
// said a ctx: what every call core makes to a bind-call page carries
// (outbox), so a walk goes on after a call that failed.
func (s *Service) resumes(ctx context.Context, session *models.MinionSession) ([]cbor.RawMessage, error) {
	tasks, err := s.tasks.Resumable(ctx, session.SessionID)
	if err != nil {
		return nil, fmt.Errorf("tasks to resume: %w", err)
	}
	var out []cbor.RawMessage
	for _, t := range tasks {
		item, err := resumeItem(t.TaskID, t.ResumeCtx)
		if err != nil {
			return nil, err
		}
		out = append(out, item...)
	}
	return out, nil
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
		// With where the task stands, when the minion said: one that keeps
		// nothing ends the task from there.
		item, err := protocol.MarshalCBOR(protocol.CancelItem{T: protocol.ItemCancel, Task: t.TaskID[:], Ctx: cancelCtx(t)})
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// cancelCtx is the ctx a task's cancel carries: its parts' newest, else its
// transfer's (a file core sends, or a stream the minion sends), where a
// minion that keeps nothing finds what it holds for the task (a page, its
// partial upload).
func cancelCtx(t models.MinionTask) []byte {
	switch {
	case t.ResumeCtx != nil:
		return t.ResumeCtx
	case t.Upload != nil && t.Upload.Ctx != nil:
		return t.Upload.Ctx
	case t.Stream != nil:
		return t.Stream.Ctx
	}
	return nil
}

// endUnknown ends a task the minion says it does not have (an err
// unknown_task naming it, as an answer to a cancel): cancelled when it was
// asked to stop, failed otherwise. A task that ended already is left.
func (s *Service) endUnknown(ctx context.Context, session *models.MinionSession, e protocol.ErrItem) error {
	if s.tasks == nil || len(e.Task) != 16 {
		return nil
	}
	id := uuid.UUID(e.Task)
	why := "the minion does not have the task"
	if e.Msg != "" {
		why += ": " + e.Msg
	}
	t, err := s.tasks.EndUnknown(ctx, session.SessionID, id, why, s.now())
	switch {
	case repository.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("end a task the minion does not have: %w", err)
	}
	s.logger.Info("minion task ended: the minion does not have it", zap.String("session_id", session.SessionID.String()),
		zap.String("task_id", id.String()), zap.String("state", t.State))
	s.publishTask(t)
	return nil
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
