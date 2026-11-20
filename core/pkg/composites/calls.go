package composites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// outcome is how a call went.
type outcome int

const (
	// answered: an instance decided.
	answered outcome = iota
	// unanswered: no instance took the call, or none answered in time: it
	// goes again, with its seq.
	unanswered
	// refused: its factory's code could not decide (an error), or its
	// answer does not read: the task fails.
	refused
)

// call makes the task's next call about event (again, with its seq, when
// resend) and takes its answer.
func (s *Service) call(ctx context.Context, lk *look, event protocol.ComposeEvent, resend bool) (bool, error) {
	t, c := lk.t, lk.t.Composite
	seq := c.Seq
	if !resend {
		if seq++; seq > protocol.MaxComposeCalls {
			if event == protocol.ComposeStopped {
				return false, s.end(ctx, lk, models.MinionTaskCancelled, nil, "")
			}
			return true, s.fail(ctx, t, fmt.Sprintf("its factory's code made %d calls, a task's most", protocol.MaxComposeCalls))
		}
	}
	now := s.now()
	deadline := now.Add(s.cfg.CallTimeout)
	// The timer stops at the next call, whatever brings it.
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{IfSeq: &c.Seq, Unset: []string{"timer_at"},
		Set: map[string]any{"seq": seq, "call_event": string(event), "call_at": now, "call_deadline": deadline, "wake_at": deadline}})
	if repository.IsNotFound(err) {
		// Changed meanwhile (a stop): look again.
		return true, nil
	}
	if err != nil {
		return false, err
	}
	lk.t = after
	d, req, out, why := s.send(ctx, lk, event, seq)
	switch {
	case out == answered && event == protocol.ComposeStopped:
		return true, s.applyStopped(ctx, lk, req, d)
	case out == answered:
		return true, s.apply(ctx, lk, req, d)
	case out == refused && event == protocol.ComposeStopped:
		s.logger.Info("composite task's stopped call refused: cancelled, nothing cleaned up", zap.String("task_id", t.TaskID.String()),
			zap.String("why", why))
		return false, s.end(ctx, lk, models.MinionTaskCancelled, nil, "")
	case out == refused:
		return true, s.fail(ctx, lk.t, why)
	}
	// Again shortly, with the same seq, to any instance that can take it.
	s.logger.Info("composite task's call unanswered", zap.String("task_id", t.TaskID.String()), zap.Int("seq", seq),
		zap.String("event", string(event)), zap.String("why", why))
	next := s.now().Add(s.cfg.Retry)
	if event == protocol.ComposeStopped && c.StoppedCallAt != nil {
		if limit := c.StoppedCallAt.Add(protocol.ComposeStoppedWait); limit.Before(next) {
			next = limit
		}
	}
	u := repository.ComposeUpdate{IfSeq: &seq, Set: map[string]any{"wake_at": next}, Unset: []string{"call_at", "call_deadline"}}
	if c.SilentSince == nil {
		u.Set["silent_since"] = now
	}
	after, err = s.tasks.UpdateComposite(ctx, t.TaskID, u)
	if err == nil {
		s.publish(after)
	}
	return false, ignoreNotFound(err)
}

// send makes a call to the instances that can take it, the one with the
// fewest calls waiting first: an instance whose queue is gone, one busy, and
// one that cannot take the call (it predates composites) pass it on; a
// timeout leaves it for later. why says what went wrong.
func (s *Service) send(ctx context.Context, lk *look, event protocol.ComposeEvent, seq int) (protocol.Decision, protocol.ComposeRequest, outcome, string) {
	var d protocol.Decision
	var req protocol.ComposeRequest
	insts, err := s.instances(ctx, lk)
	if err != nil {
		return d, req, unanswered, err.Error()
	}
	if len(insts) == 0 {
		return d, req, unanswered, fmt.Sprintf("no registered %q factory declares %q a composite command", lk.build.ModuleName, lk.t.Command)
	}
	why := ""
	for _, inst := range insts {
		deadline := s.now().Add(s.cfg.CallTimeout)
		req, err = s.request(ctx, lk, event, seq, deadline)
		if err == nil {
			err = req.Validate()
		}
		if err != nil {
			return d, req, refused, "core could not make the call: " + err.Error()
		}
		s.callOut(inst.Instance, lk.t.SessionID, lk.t.TaskID)
		rep, err := s.rpc.Call(ctx, inst.RPCQueue, protocol.TypeFactoryCompose, req, s.cfg.CallTimeout)
		s.callIn(inst.Instance, lk.t.SessionID, lk.t.TaskID)
		switch {
		case errors.Is(err, messaging.ErrRPCTimeout):
			return d, req, unanswered, fmt.Sprintf("%s did not answer in time", inst.Instance)
		case err != nil:
			why = fmt.Sprintf("%s: %v", inst.Instance, err)
			continue
		case rep.Status == protocol.StatusError || rep.Error != nil:
			code, msg := protocol.CodeInternalError, ""
			if rep.Error != nil {
				code, msg = rep.Error.Code, rep.Error.Message
			}
			switch code {
			case protocol.CodeBusy, protocol.CodeValidationFailed, protocol.CodeUnsupportedVersion:
				why = fmt.Sprintf("%s: %s: %s", inst.Instance, code, msg)
				continue
			}
			if msg == "" {
				msg = code
			}
			return d, req, refused, "its factory's code could not decide: " + msg
		}
		if err := json.Unmarshal(rep.Payload, &d); err != nil {
			return d, req, refused, "its factory's answer does not read: " + err.Error()
		}
		return d, req, answered, ""
	}
	return d, req, unanswered, why
}

// instances are the registered instances of the build's factory kind whose
// schema declares the command composite, whatever version the build was
// made with: the one with the fewest calls waiting first.
func (s *Service) instances(ctx context.Context, lk *look) ([]models.Module, error) {
	mods, err := s.modules.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("the factory's instances: %w", err)
	}
	var out []models.Module
	for _, m := range mods {
		if m.Type != protocol.ServiceFactory || m.Name != lk.build.ModuleName || m.RPCQueue == "" || !m.Build.Current() {
			continue
		}
		if c, ok := m.Build.Command(lk.t.Command); ok && c.Composite {
			out = append(out, m)
		}
	}
	s.mu.Lock()
	waiting := make(map[string]int, len(out))
	for _, m := range out {
		waiting[m.Instance] = s.waiting[m.Instance]
	}
	s.mu.Unlock()
	slices.SortStableFunc(out, func(a, b models.Module) int {
		if d := waiting[a.Instance] - waiting[b.Instance]; d != 0 {
			return d
		}
		return strings.Compare(a.Instance, b.Instance)
	})
	return out, nil
}

// request is the call's payload: its links signed until a minute past its
// deadline.
func (s *Service) request(ctx context.Context, lk *look, event protocol.ComposeEvent, seq int, deadline time.Time) (protocol.ComposeRequest, error) {
	t, c := lk.t, lk.t.Composite
	expires := deadline.Add(time.Minute)
	open, err := s.tasks.CountOpenSubtasks(ctx, t.TaskID)
	if err != nil {
		return protocol.ComposeRequest{}, err
	}
	under := open + len(c.Waiting)
	req := protocol.ComposeRequest{TaskID: t.TaskID.String(), SessionID: t.SessionID.String(), BuildID: lk.build.BuildID.String(),
		Seq: seq, Event: event, Deadline: deadline, Command: t.Command, Args: s.args(lk, t.Command, t.Args), Actor: s.actor(ctx, t),
		Platform: lk.build.Platform, OS: lk.session.Hello.OS, Commands: slices.Clone(lk.build.Commands), Ctx: c.Ctx,
		SubtasksUnderWay: under, SubtasksEnded: max(c.Subtasks-under, 0), UploadURL: s.links.Sign(UploadPath(t.TaskID), expires)}
	if req.Commands == nil {
		req.Commands = []string{}
	}
	if lk.cmd.HasOp() {
		req.OwnOp = protocol.OwnOpState(c.OwnOp)
	}
	switch event {
	case protocol.ComposeEnded:
		if len(c.Pending) == 0 {
			return req, errors.New("an ended call with no end to tell")
		}
		e, err := s.endOf(ctx, lk, c.Pending[0], expires)
		if err != nil {
			return req, err
		}
		req.End = &e
	case protocol.ComposeStopped:
		for _, p := range c.Pending {
			e, err := s.endOf(ctx, lk, p, expires)
			if err != nil {
				return req, err
			}
			req.Ends = append(req.Ends, e)
		}
	}
	return req, nil
}

// args are a command's args by param key, its defaults filled in, as an
// operator gives them (a duration "30s").
func (s *Service) args(lk *look, command string, values models.JSONMap) map[string]any {
	c, ok := lk.schema.Command(command)
	if !ok {
		if len(values) == 0 {
			return nil
		}
		return map[string]any(values)
	}
	out := protocol.ResolveFieldValues(c.Params, map[string]any(values))
	if len(out) == 0 {
		return nil
	}
	return out
}

// actor is who queued the task; nil for one core queued itself.
func (s *Service) actor(ctx context.Context, t *models.MinionTask) *protocol.Actor {
	if t.IssuedBy == uuid.Nil {
		return nil
	}
	a := &protocol.Actor{Type: "user", ID: t.IssuedBy.String()}
	if s.users != nil {
		if users, err := s.users.FindByIDs(ctx, []uuid.UUID{t.IssuedBy}); err == nil && len(users) > 0 {
			a.Name = users[0].Username
		}
	}
	return a
}

// apply takes the answer to the call req: kept with the call before
// anything is done of it (what it stops, its subtasks and its own op are
// done from the task's record, by the next rounds), so an answer that came
// counts once. An invalid one fails the task. An answer after a stop counts
// for its ctx alone.
func (s *Service) apply(ctx context.Context, lk *look, req protocol.ComposeRequest, d protocol.Decision) error {
	t, c := lk.t, lk.t.Composite
	if err := d.ValidateFor(req); err != nil {
		return s.fail(ctx, t, "its factory's decision is invalid: "+err.Error())
	}
	if d.Finish != nil {
		if why := s.checkFinish(ctx, lk, d.Finish); why != "" {
			return s.fail(ctx, t, "its factory's decision is invalid: finish: "+why)
		}
	}
	now := s.now()
	seq, running := req.Seq, ""
	u := repository.ComposeUpdate{IfSeq: &seq, IfPhase: &running, PopEnd: req.Event == protocol.ComposeEnded,
		Set:   map[string]any{"answered": seq, "wake_at": now},
		Unset: []string{"call_at", "call_deadline", "silent_since"}}
	ctxStatus(&u, d)
	if f := d.Finish; f != nil {
		u.Set["phase"] = models.ComposeFinishing
		u.Set["finish"] = models.MinionComposeFinish{OK: f.OK, Message: f.Message, Err: f.Err, Out: f.Out, OutFile: f.OutFile,
			Data: f.Data, DataFile: f.DataFile, ResultOf: f.ResultOf, At: now}
		u.Unset = append(u.Unset, "timer_at")
	} else {
		if d.Cancel != nil {
			u.Set["cancel"] = models.MinionComposeCancel{Keys: d.Cancel.Keys, OwnOp: d.Cancel.OwnOp}
		}
		waiting := slices.Clone(c.Waiting)
		asked := c.Subtasks
		for _, sub := range d.Run {
			if asked >= protocol.MaxSubtasks {
				u.Ends = append(u.Ends, models.MinionComposeEnd{Command: sub.Command, Args: sub.Args, Key: sub.Key, As: sub.As,
					State: string(protocol.EndRefused), Err: fmt.Sprintf("a task runs %d subtasks at most", protocol.MaxSubtasks), At: now})
				continue
			}
			asked++
			waiting = append(waiting, models.MinionSubtask{TaskID: uuid.New(), Command: sub.Command, Args: sub.Args, Key: sub.Key, As: sub.As})
		}
		u.Waiting, u.Subtasks = &waiting, asked-c.Subtasks
		if d.OwnOp != nil {
			u.Set["send_own"] = models.MinionOwnOpRun{Args: d.OwnOp.Args}
		}
		if after, _ := d.Timer(); after > 0 {
			u.Set["timer_at"] = now.Add(after)
		} else {
			u.Unset = append(u.Unset, "timer_at")
		}
	}
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, u)
	if repository.IsNotFound(err) {
		// Stopped meanwhile: it counts for its ctx alone.
		cu := repository.ComposeUpdate{IfSeq: &seq, PopEnd: req.Event == protocol.ComposeEnded,
			Set: map[string]any{"answered": seq, "wake_at": now}, Unset: []string{"call_at", "call_deadline", "silent_since"}}
		if d.Ctx != nil {
			ctxStatus(&cu, protocol.Decision{Ctx: d.Ctx})
		}
		_, err = s.tasks.UpdateComposite(ctx, t.TaskID, cu)
		return ignoreNotFound(err)
	}
	if err != nil {
		return err
	}
	s.publish(after)
	return nil
}

// ctxStatus puts a decision's ctx and status in u: nil keeps them, empty
// clears them.
func ctxStatus(u *repository.ComposeUpdate, d protocol.Decision) {
	if d.Ctx != nil {
		if len(*d.Ctx) == 0 {
			u.Unset = append(u.Unset, "ctx")
		} else {
			u.Set["ctx"] = *d.Ctx
		}
	}
	if d.Status != nil {
		if *d.Status == "" {
			u.Unset = append(u.Unset, "status")
		} else {
			u.Set["status"] = *d.Status
		}
	}
}

// applyStopped takes the answer to the stopped call: its subtasks alone,
// which clean up; their ends call the factory no more.
func (s *Service) applyStopped(ctx context.Context, lk *look, req protocol.ComposeRequest, d protocol.Decision) error {
	t, c := lk.t, lk.t.Composite
	if err := d.ValidateFor(req); err != nil {
		s.logger.Info("composite task's answer to its stop is invalid: cancelled, nothing cleaned up",
			zap.String("task_id", t.TaskID.String()), zap.Error(err))
		return s.end(ctx, lk, models.MinionTaskCancelled, nil, "")
	}
	now := s.now()
	seq := req.Seq
	var cleanup []models.MinionSubtask
	asked := c.Subtasks
	for _, sub := range d.Run {
		if asked >= protocol.MaxSubtasks {
			break
		}
		asked++
		cleanup = append(cleanup, models.MinionSubtask{TaskID: uuid.New(), Command: sub.Command, Args: sub.Args, Key: sub.Key, As: sub.As, Cleanup: true})
	}
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{IfSeq: &seq, DropEnds: true, Waiting: &cleanup,
		Subtasks: asked - c.Subtasks,
		Set:      map[string]any{"answered": seq, "phase": models.ComposeCleanup, "wake_at": now},
		Unset:    []string{"call_at", "call_deadline", "silent_since"}})
	if err == nil {
		s.publish(after)
	}
	return ignoreNotFound(err)
}

// checkFinish checks what only core knows of a finish: the subtask its
// result_of names (one of the task's, that ended done, whose command has
// the composite's result) and the files out_file and data_file name (put
// for the task). It says what is wrong; "" for nothing.
func (s *Service) checkFinish(ctx context.Context, lk *look, f *protocol.Finish) string {
	t := lk.t
	if f.ResultOf != "" {
		id, err := uuid.Parse(f.ResultOf)
		if err != nil {
			return "result_of names no subtask of the task"
		}
		sub, err := s.tasks.Subtask(ctx, t.TaskID, id)
		switch {
		case err != nil:
			return "result_of names no subtask of the task"
		case sub.State != models.MinionTaskDone:
			return fmt.Sprintf("result_of: subtask %s did not end done", f.ResultOf)
		case sub.Result != lk.cmd.Result:
			return fmt.Sprintf("result_of: subtask %s's command %q has result %q, the command's is %q", f.ResultOf, sub.Command, sub.Result, lk.cmd.Result)
		}
	}
	for name, id := range map[string]string{"out_file": f.OutFile, "data_file": f.DataFile} {
		if id == "" {
			continue
		}
		if _, err := s.putFile(ctx, t, id); err != nil {
			return fmt.Sprintf("%s: %v", name, err)
		}
	}
	return ""
}
