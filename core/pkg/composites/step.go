package composites

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

const (
	// maxRounds is how much core does for one task in one look before it
	// lets others have the worker: a round is a call, or one thing a
	// decision asked for.
	maxRounds = 64
	// fileWait is how soon core looks again at a subtask whose file is
	// still being joined: its end goes with its file.
	fileWait = time.Second
	// backstop is how soon core looks again at a task that waits for an end:
	// the end's event wakes it before, unless the event was lost.
	backstop = 30 * time.Second
)

// look is a task as one round reads it, with what its calls need.
type look struct {
	t       *models.MinionTask
	session *models.MinionSession
	build   *models.Build
	schema  protocol.BuildSchema
	cmd     protocol.BuildCommand
}

// step looks at a composite task and does what is due, round by round,
// until it waits for something: an end, its timer, an instance to take a
// call. A process claims the task while it looks; another's claim makes it
// leave the task to that process.
func (s *Service) step(ctx context.Context, id uuid.UUID) {
	l := s.lock(id)
	defer s.unlock(id, l)
	for range maxRounds {
		now := s.now()
		t, err := s.tasks.ClaimComposite(ctx, id, s.id, now.Add(s.lease()), now)
		if repository.IsNotFound(err) {
			if cur, ferr := s.tasks.FindByTaskID(ctx, id); ferr != nil || !cur.Composing() {
				s.forget(id)
			}
			return
		}
		if err != nil {
			s.logger.Error("composites: claim a task", zap.String("task_id", id.String()), zap.Error(err))
			return
		}
		more, err := s.round(ctx, t)
		if err != nil {
			s.logger.Error("composites: a task's round", zap.String("task_id", id.String()), zap.Error(err))
			s.setWake(ctx, id, now.Add(s.cfg.Retry))
		}
		if err != nil || !more {
			if err := s.tasks.ReleaseComposite(ctx, id, s.id); err != nil {
				s.logger.Warn("composites: release a task", zap.String("task_id", id.String()), zap.Error(err))
			}
			return
		}
	}
	_ = s.tasks.ReleaseComposite(ctx, id, s.id)
	s.Nudge(id)
}

// lease is how long a claim lasts: a call's attempts, and some.
func (s *Service) lease() time.Duration { return 3*s.cfg.CallTimeout + 30*time.Second }

// round does one thing for the task, if one is due: it says whether to go
// on (something changed).
func (s *Service) round(ctx context.Context, t *models.MinionTask) (bool, error) {
	if !t.Composing() {
		s.forget(t.TaskID)
		return false, nil
	}
	if t.Composite.Phase == models.ComposeClosed {
		return false, nil
	}
	lk, gone, err := s.read(ctx, t)
	if err != nil {
		return false, err
	}
	if gone != "" {
		return true, s.fail(ctx, t, gone)
	}
	now := s.now()
	if lk.session.Closed(now) {
		// A closed session takes its tasks with it: core calls the factory
		// no more about them.
		s.logger.Info("composite task's session closed: its factory is called no more", zap.String("task_id", t.TaskID.String()))
		after, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{
			Set: map[string]any{"phase": models.ComposeClosed}, Unset: []string{"wake_at", "timer_at"}})
		if err == nil {
			s.publish(after)
			s.forget(t.TaskID)
		}
		return false, ignoreNotFound(err)
	}
	heard, joining, err := s.hear(ctx, t)
	if err != nil || heard {
		return heard, err
	}
	switch t.Composite.Phase {
	case "":
		return s.running(ctx, lk, joining)
	case models.ComposeFinishing:
		return s.finishing(ctx, lk)
	case models.ComposeStopping:
		return s.stopping(ctx, lk)
	case models.ComposeCleanup:
		return s.cleaningUp(ctx, lk)
	}
	return false, nil
}

// read is a task's look: its session, its build, its factory's latest
// schema and its command in it. gone says why the task cannot go on (its
// build or command is gone): it fails.
func (s *Service) read(ctx context.Context, t *models.MinionTask) (*look, string, error) {
	session, err := s.sessions.FindBySessionID(ctx, t.SessionID)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, "its session is gone", nil
		}
		return nil, "", fmt.Errorf("the task's session: %w", err)
	}
	b, err := s.builds.FindByBuildID(ctx, session.BuildID)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, "its session's build is gone", nil
		}
		return nil, "", fmt.Errorf("the session's build: %w", err)
	}
	sc, err := s.schemas.LatestBuildSchema(ctx, b.ModuleName)
	if err != nil {
		return nil, "", fmt.Errorf("the factory's build schema: %w", err)
	}
	if sc == nil {
		return nil, fmt.Sprintf("no %q factory declares a build schema", b.ModuleName), nil
	}
	cmd, ok := sc.Command(t.Command)
	if !ok || !cmd.Composite {
		return nil, fmt.Sprintf("its factory no longer declares %q a composite command", t.Command), nil
	}
	return &look{t: t, session: session, build: b, schema: sc.BuildSchema, cmd: cmd}, "", nil
}

// hear takes the ends of the task's subtasks that ended, in order: each
// waits for its call. A cleanup subtask's end calls nothing. A subtask
// whose file is still being joined is left for a moment (joining): its end
// lists the file.
func (s *Service) hear(ctx context.Context, t *models.MinionTask) (heard, joining bool, err error) {
	subs, err := s.tasks.UnheardSubtasks(ctx, t.TaskID, 64)
	if err != nil {
		return false, false, fmt.Errorf("the subtasks that ended: %w", err)
	}
	var ends []models.MinionComposeEnd
	var ids []uuid.UUID
	for _, sub := range subs {
		if !sub.Cleanup {
			if sub.State == models.MinionTaskDone && s.fileJoining(ctx, sub.TaskID) {
				joining = true
				continue
			}
			at := sub.QueuedAt
			if sub.DoneAt != nil {
				at = *sub.DoneAt
			}
			id := sub.TaskID
			ends = append(ends, models.MinionComposeEnd{TaskID: &id, At: at})
		}
		ids = append(ids, sub.TaskID)
	}
	if len(ends) > 0 {
		if _, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{Ends: ends,
			Set: map[string]any{"wake_at": s.now()}}); err != nil {
			return false, joining, ignoreNotFound(err)
		}
	}
	for _, id := range ids {
		if err := s.tasks.MarkHeard(ctx, id); err != nil {
			return false, joining, err
		}
	}
	return len(ids) > 0, joining, nil
}

// fileJoining reports whether a task's file is still coming or being
// joined.
func (s *Service) fileJoining(ctx context.Context, task uuid.UUID) bool {
	if s.files == nil {
		return false
	}
	f, err := s.files.File(ctx, task)
	return err == nil && f != nil && !f.Ended()
}

// running is the task while its factory's code runs it: what the latest
// decision asked for first (what it stops, its subtasks, its own op, in
// that order), then the next call: one not answered again, the start, the
// next end, or the timer.
func (s *Service) running(ctx context.Context, lk *look, joining bool) (bool, error) {
	t, c := lk.t, lk.t.Composite
	now := s.now()
	if c.Cancel != nil {
		return true, s.cancel(ctx, lk)
	}
	if len(c.Waiting) > 0 {
		queued, err := s.queueWaiting(ctx, lk)
		if err != nil || queued {
			return queued, err
		}
	}
	if c.SendOwn != nil {
		return true, s.sendOwn(ctx, lk)
	}
	if c.SilentSince != nil && now.Sub(*c.SilentSince) >= protocol.ComposeNoAnswer {
		return true, s.fail(ctx, t, fmt.Sprintf("its factory did not answer for %s", protocol.ComposeNoAnswer))
	}
	switch {
	case c.Answered < c.Seq:
		// The call out went unanswered (or core stopped meanwhile): again,
		// with its seq.
		return s.call(ctx, lk, protocol.ComposeEvent(c.CallEvent), true)
	case c.Seq == 0:
		return s.call(ctx, lk, protocol.ComposeStart, false)
	case len(c.Pending) > 0:
		return s.call(ctx, lk, protocol.ComposeEnded, false)
	case c.TimerAt != nil && !now.Before(*c.TimerAt):
		return s.call(ctx, lk, protocol.ComposeTimer, false)
	}
	// Nothing to tell its factory: it waits for an end, or its timer.
	open, err := s.tasks.CountOpenSubtasks(ctx, t.TaskID)
	if err != nil {
		return false, err
	}
	if open == 0 && len(c.Waiting) == 0 && !t.OwnOpOut() && !joining && c.TimerAt == nil {
		return true, s.fail(ctx, t, "its factory's code waits for nothing: nothing is under way, and no timer is set")
	}
	next := now.Add(backstop)
	if c.TimerAt != nil && c.TimerAt.Before(next) {
		next = *c.TimerAt
	}
	if joining && now.Add(fileWait).Before(next) {
		next = now.Add(fileWait)
	}
	s.setWake(ctx, t.TaskID, next)
	return false, nil
}

// cancel stops what the latest decision named: its subtasks with one of its
// keys (those waiting in the task end cancelled at once) and its own op.
func (s *Service) cancel(ctx context.Context, lk *look) error {
	t, c := lk.t, lk.t.Composite
	now := s.now()
	u := repository.ComposeUpdate{Unset: []string{"cancel"}, Set: map[string]any{"wake_at": now}}
	if len(c.Cancel.Keys) > 0 {
		subs, err := s.tasks.OpenSubtasks(ctx, t.TaskID, c.Cancel.Keys)
		if err != nil {
			return err
		}
		for _, sub := range subs {
			s.cancelTask(ctx, sub)
		}
		keep := []models.MinionSubtask{}
		for _, w := range c.Waiting {
			if slices.Contains(c.Cancel.Keys, w.Key) {
				u.Ends = append(u.Ends, models.MinionComposeEnd{Command: w.Command, Args: w.Args, Key: w.Key, As: w.As,
					State: string(protocol.EndCancelled), At: now})
				continue
			}
			keep = append(keep, w)
		}
		u.Waiting = &keep
	}
	if c.Cancel.OwnOp && t.OwnOpOut() {
		if err := s.cancelOwnOp(ctx, t); err != nil {
			return err
		}
	}
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, u)
	if err == nil {
		s.publish(after)
	}
	return ignoreNotFound(err)
}

// cancelTask stops a subtask as an operator's stop does.
func (s *Service) cancelTask(ctx context.Context, sub models.MinionTask) {
	after, err := s.tasks.RequestCancel(ctx, sub.TaskID, s.now())
	if err != nil {
		s.logger.Warn("composites: stop a subtask", zap.String("task_id", sub.TaskID.String()), zap.Error(err))
		return
	}
	s.publish(after)
}

// cancelOwnOp stops the task's own op: one queued ends cancelled now, one
// sent is told to stop (its last part ends it).
func (s *Service) cancelOwnOp(ctx context.Context, t *models.MinionTask) error {
	now := s.now()
	var after *models.MinionTask
	var err error
	if t.State == models.MinionTaskQueued {
		after, err = s.tasks.EndOwnOp(ctx, t.TaskID, models.MinionComposeOwnEnd{State: string(protocol.EndCancelled), At: now})
	} else if t.CancelRequestedAt == nil {
		after, err = s.tasks.CancelOwnOp(ctx, t.TaskID, now)
	}
	if repository.IsNotFound(err) {
		// Gone out meanwhile, or ended: a cancel for one sent is asked once
		// it is, by the next look.
		return nil
	}
	if err == nil && after != nil {
		s.publish(after)
	}
	return err
}

// queueWaiting queues the subtasks waiting in the task, in order, as many
// as may be under way at once: one that fails core's checks ends refused
// (a cleanup subtask's refusal calls nothing). It says whether it queued
// one.
func (s *Service) queueWaiting(ctx context.Context, lk *look) (bool, error) {
	t, c := lk.t, lk.t.Composite
	if s.queuer == nil {
		return false, errors.New("core queues no subtasks here")
	}
	open, err := s.tasks.CountOpenSubtasks(ctx, t.TaskID)
	if err != nil {
		return false, err
	}
	room := protocol.MaxSubtasksUnderWay - open
	if room <= 0 {
		return false, nil
	}
	n := min(room, len(c.Waiting))
	now := s.now()
	u := repository.ComposeUpdate{Set: map[string]any{"wake_at": now}}
	for _, sub := range c.Waiting[:n] {
		q, err := s.queuer.QueueSubtask(ctx, t, sub)
		var refused *Refusal
		switch {
		case errors.As(err, &refused):
			s.logger.Info("composite task's subtask refused", zap.String("task_id", t.TaskID.String()),
				zap.String("command", sub.Command), zap.String("why", refused.Reason))
			if !sub.Cleanup {
				u.Ends = append(u.Ends, models.MinionComposeEnd{Command: sub.Command, Args: sub.Args, Key: sub.Key, As: sub.As,
					State: string(protocol.EndRefused), Err: refused.Reason, At: now})
			}
		case err != nil:
			// Again by the next look: its task id makes a subtask queued now
			// the same one then.
			return false, fmt.Errorf("queue a subtask: %w", err)
		default:
			s.watch(q.TaskID, t.TaskID)
		}
	}
	rest := slices.Clone(c.Waiting[n:])
	u.Waiting = &rest
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, u)
	if err == nil {
		s.publish(after)
	}
	return true, ignoreNotFound(err)
}

// sendOwn sends the task's own op, as the latest decision asked: one that
// fails core's checks ends refused.
func (s *Service) sendOwn(ctx context.Context, lk *look) error {
	t, c := lk.t, lk.t.Composite
	if s.queuer == nil {
		return errors.New("core sends no own ops here")
	}
	now := s.now()
	_, err := s.queuer.SendOwnOp(ctx, t, c.SendOwn.Args)
	var refused *Refusal
	u := repository.ComposeUpdate{Unset: []string{"send_own"}, Set: map[string]any{"wake_at": now}}
	switch {
	case errors.As(err, &refused):
		s.logger.Info("composite task's own op refused", zap.String("task_id", t.TaskID.String()), zap.String("why", refused.Reason))
		id := t.TaskID
		u.Set["own_op"] = string(protocol.OwnOpEnded)
		u.Set["own_end"] = models.MinionComposeOwnEnd{State: string(protocol.EndRefused), Err: refused.Reason, At: now}
		u.Ends = []models.MinionComposeEnd{{TaskID: &id, OwnOp: true, At: now}}
	case err != nil:
		return fmt.Errorf("send the own op: %w", err)
	default:
		if s.nudger != nil {
			s.nudger.Nudge(t.SessionID, false)
		}
	}
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, u)
	if err == nil {
		s.publish(after)
	}
	return ignoreNotFound(err)
}

// finishing is the task once its factory finished it: what is under way
// stops, and once it has ended, the task ends as the finish says.
func (s *Service) finishing(ctx context.Context, lk *look) (bool, error) {
	under, changed, err := s.stopUnderWay(ctx, lk.t, false, false)
	if err != nil || changed {
		return changed, err
	}
	if under {
		s.setWake(ctx, lk.t.TaskID, s.now().Add(backstop))
		return false, nil
	}
	return false, s.finish(ctx, lk)
}

// stopping is the task once an operator stopped it: what is under way
// stops; once it has ended, its factory hears of it (the stopped call,
// with every end it had not heard of), and its answer's subtasks clean up.
// Without an answer within a minute, the task ends cancelled.
func (s *Service) stopping(ctx context.Context, lk *look) (bool, error) {
	t, c := lk.t, lk.t.Composite
	under, changed, err := s.stopUnderWay(ctx, t, true, false)
	if err != nil || changed {
		// The ends of those dropped are in its record now: look again.
		return changed, err
	}
	if under {
		s.setWake(ctx, t.TaskID, s.now().Add(backstop))
		return false, nil
	}
	now := s.now()
	if c.StoppedCallAt != nil && now.Sub(*c.StoppedCallAt) >= protocol.ComposeStoppedWait {
		s.logger.Info("composite task's stopped call unanswered: cancelled, nothing cleaned up", zap.String("task_id", t.TaskID.String()))
		return false, s.end(ctx, lk, models.MinionTaskCancelled, nil, "")
	}
	if c.StoppedCallAt == nil {
		after, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{Set: map[string]any{"stopped_call_at": now}})
		if err != nil {
			return false, ignoreNotFound(err)
		}
		lk.t = after
	}
	return s.call(ctx, lk, protocol.ComposeStopped, c.Answered < c.Seq && c.CallEvent == string(protocol.ComposeStopped))
}

// cleaningUp is the task once its factory answered the stopped call: it
// ends cancelled once its cleanup subtasks have.
func (s *Service) cleaningUp(ctx context.Context, lk *look) (bool, error) {
	t, c := lk.t, lk.t.Composite
	if len(c.Waiting) > 0 {
		queued, err := s.queueWaiting(ctx, lk)
		if err != nil || queued {
			return queued, err
		}
	}
	open, err := s.tasks.CountOpenSubtasks(ctx, t.TaskID)
	if err != nil {
		return false, err
	}
	if open == 0 && len(c.Waiting) == 0 {
		return false, s.end(ctx, lk, models.MinionTaskCancelled, nil, "")
	}
	s.setWake(ctx, t.TaskID, s.now().Add(backstop))
	return false, nil
}

// stopUnderWay stops what the task has under way: its subtasks queued,
// sent or running (with all, its cleanup ones too) and its own op when it
// is out; the subtasks waiting in it are dropped (ending cancelled, for its
// factory to hear, when told). It says whether something has not ended
// yet, and whether its record changed (the waiting dropped).
func (s *Service) stopUnderWay(ctx context.Context, t *models.MinionTask, told, all bool) (under, changed bool, err error) {
	subs, err := s.tasks.OpenSubtasks(ctx, t.TaskID, nil)
	if err != nil {
		return false, false, err
	}
	for _, sub := range subs {
		if sub.Cleanup && !all {
			continue
		}
		under = true
		if sub.CancelRequestedAt == nil {
			s.cancelTask(ctx, sub)
		}
	}
	if t.OwnOpOut() {
		under = true
		if err := s.cancelOwnOp(ctx, t); err != nil {
			return false, false, err
		}
	}
	if c := t.Composite; len(c.Waiting) > 0 && !t.Finished() {
		now := s.now()
		u := repository.ComposeUpdate{Waiting: &[]models.MinionSubtask{}}
		for _, w := range c.Waiting {
			if told && !w.Cleanup {
				u.Ends = append(u.Ends, models.MinionComposeEnd{Command: w.Command, Args: w.Args, Key: w.Key, As: w.As,
					State: string(protocol.EndCancelled), At: now})
			}
		}
		if _, err := s.tasks.UpdateComposite(ctx, t.TaskID, u); err != nil {
			return false, false, ignoreNotFound(err)
		}
		changed = true
	}
	if under {
		// The queued ones ended at once: count again.
		open, err := s.tasks.OpenSubtasks(ctx, t.TaskID, nil)
		if err != nil {
			return false, changed, err
		}
		cur, err := s.tasks.FindByTaskID(ctx, t.TaskID)
		if err != nil {
			return false, changed, err
		}
		under = cur.OwnOpOut()
		for _, sub := range open {
			under = under || all || !sub.Cleanup
		}
	}
	return under, changed, nil
}

// fail ends the task failed, saying why, once what is under way has ended:
// an invalid decision, an error its factory's code answered, a factory
// that does not answer.
func (s *Service) fail(ctx context.Context, t *models.MinionTask, why string) error {
	s.logger.Info("composite task fails", zap.String("task_id", t.TaskID.String()), zap.String("why", why))
	now := s.now()
	after, err := s.tasks.UpdateComposite(ctx, t.TaskID, repository.ComposeUpdate{
		Set: map[string]any{"phase": models.ComposeFinishing, "wake_at": now,
			"finish": models.MinionComposeFinish{OK: false, Err: cutText(why, protocol.MaxComposeText), At: now}},
		Unset: []string{"timer_at", "cancel", "send_own", "call_at", "call_deadline"},
	})
	if err == nil {
		s.publish(after)
	}
	return ignoreNotFound(err)
}

// end ends the task: state, with ok and err (nil and "": none).
func (s *Service) end(ctx context.Context, lk *look, state string, ok *bool, err string) error {
	after, e := s.tasks.EndComposite(ctx, lk.t.TaskID, state, ok, err, s.now())
	if e != nil {
		return ignoreNotFound(e)
	}
	s.ended(after)
	return nil
}

// ended: the task ended; its events, and its subtasks', concern nothing now.
func (s *Service) ended(t *models.MinionTask) {
	s.logger.Info("composite task ended", zap.String("task_id", t.TaskID.String()), zap.String("state", t.State))
	s.publish(t)
	s.forget(t.TaskID)
}

// setWake has the task looked at by at, at the latest.
func (s *Service) setWake(ctx context.Context, task uuid.UUID, at time.Time) {
	if _, err := s.tasks.UpdateComposite(ctx, task, repository.ComposeUpdate{Set: map[string]any{"wake_at": at}}); err != nil &&
		!repository.IsNotFound(err) {
		s.logger.Warn("composites: set a task's wake", zap.String("task_id", task.String()), zap.Error(err))
	}
}

// cutText is s at most max bytes, cut on a rune.
func cutText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func ignoreNotFound(err error) error {
	if repository.IsNotFound(err) {
		return nil
	}
	return err
}
