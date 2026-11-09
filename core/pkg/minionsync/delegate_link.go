package minionsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// A bind target that names a delegate (models.MinionTarget.DelegateID) is
// called and opened through it: its calls and opens go to the delegate's
// task that serves the target's kind as delegate items, and their answers
// come back the same way (docs: contracts/minion-messages/delegation/).
// delegateLink is that way, as Linker is a channel module's.

// delegateLink calls and opens target t through its delegate. version is
// the channel's schema version t's build connection was built against.
type delegateLink struct {
	s       *Service
	t       *models.MinionTarget
	version int
	// via is the way the latest call's answer came: the delegate's task,
	// then the delegates the delegate's own messages come through.
	via []models.MinionSessionVia
}

// linkerFor is how t is reached: through its delegate when it names one,
// else the channels' control links (nil: core keeps none).
func (s *Service) linkerFor(t *models.MinionTarget, conn *protocol.BuildConnection) Linker {
	if t.DelegateID == nil {
		if s.link == nil {
			return nil
		}
		return s.link
	}
	l := &delegateLink{s: s, t: t}
	if conn != nil {
		l.version = conn.Endpoint.Channel.SchemaVersion
	}
	return l
}

// delegateFor is the task of session's that serves module's kind at
// version, running, its place in its command's serves, and the session.
// With none
// running it queues one when none waits either, and says
// ErrNoDelegateTask; ErrNotADelegate for a session that cannot be the
// delegate.
func (s *Service) delegateFor(ctx context.Context, sessionID, operation uuid.UUID, module, kind string,
	version int) (*delegateTask, uint64, *models.MinionSession, error) {
	session, serving, err := s.servingCommands(ctx, sessionID, operation, module, kind, version)
	if err != nil {
		return nil, 0, nil, err
	}
	open, err := s.tasks.OpenWithAction(ctx, sessionID, protocol.ActionDelegate)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("the delegate's tasks: %w", err)
	}
	waiting := false
	for _, t := range open {
		i := slices.IndexFunc(serving, func(c protocol.BuildCommand) bool { return c.ID == t.Command })
		if i < 0 || t.CancelRequestedAt != nil {
			continue
		}
		if t.AckedAt == nil {
			waiting = true
			continue
		}
		dt, err := s.delegateTaskOf(ctx, session, t.TaskID)
		if err != nil {
			return nil, 0, nil, err
		}
		if dt != nil {
			return dt, uint64(serving[i].Serving(module, kind, version)), session, nil
		}
	}
	if !waiting {
		if err := s.queueDelegate(ctx, session, serving[0]); err != nil {
			return nil, 0, nil, err
		}
	}
	return nil, 0, nil, fmt.Errorf("%w: waiting for session #%d to take its delegate task", ErrNoDelegateTask, session.Number)
}

// servingCommands are the delegate commands of session sessionID's build
// that serve module's kind at version, with the session: ErrNotADelegate
// when it cannot be a delegate of operation's for them.
func (s *Service) servingCommands(ctx context.Context, sessionID, operation uuid.UUID, module, kind string,
	version int) (*models.MinionSession, []protocol.BuildCommand, error) {
	if s.tasks == nil || s.delegateSchemas == nil {
		return nil, nil, fmt.Errorf("%w: core carries no delegates", ErrNotADelegate)
	}
	session, err := s.sessions.FindBySessionID(ctx, sessionID)
	switch {
	case repository.IsNotFound(err):
		return nil, nil, fmt.Errorf("%w: no such session", ErrNotADelegate)
	case err != nil:
		return nil, nil, fmt.Errorf("load the delegate's session: %w", err)
	case session.OperationID != operation:
		return nil, nil, fmt.Errorf("%w: session #%d is of another operation", ErrNotADelegate, session.Number)
	case session.Closed(s.now()):
		return nil, nil, fmt.Errorf("%w: session #%d is closed", ErrNotADelegate, session.Number)
	}
	cmds, err := s.delegateCommands(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	serving := slices.DeleteFunc(slices.Clone(cmds), func(c protocol.BuildCommand) bool {
		return c.Action != protocol.ActionDelegate || c.Serving(module, kind, version) == 0
	})
	if len(serving) == 0 {
		return nil, nil, fmt.Errorf("%w: session #%d's minion serves no %s %s connections (version %d)", ErrNotADelegate,
			session.Number, module, kind, version)
	}
	return session, serving, nil
}

// CheckDelegate says why session sessionID cannot be the delegate of a
// target of operation's of module's kind at version: "" when it can.
func (s *Service) CheckDelegate(ctx context.Context, sessionID, operation uuid.UUID, module, kind string, version int) (string, error) {
	_, _, err := s.servingCommands(ctx, sessionID, operation, module, kind, version)
	switch {
	case errors.Is(err, ErrNotADelegate):
		return strings.TrimPrefix(err.Error(), ErrNotADelegate.Error()+": "), nil
	case err != nil:
		return "", err
	}
	return "", nil
}

// queueDelegate queues a task of cmd, a delegate command, for session's
// minion, as core's own: its params take their defaults, stop_idle the
// action's. The targets that wait are called once its minion has it.
func (s *Service) queueDelegate(ctx context.Context, session *models.MinionSession, cmd protocol.BuildCommand) error {
	params := append(slices.Clone(cmd.Params), protocol.TunnelCoreParams(cmd.Action)...)
	wire, problems := protocol.TaskArgs(params, map[string]any{})
	if problems != nil {
		return fmt.Errorf("%w: its %s command takes args core cannot give (%v)", ErrNotADelegate, cmd.ID, problems)
	}
	var wireBytes []byte
	if numbered := protocol.WireArgs(cmd.Params, wire); len(numbered) > 0 {
		var err error
		if wireBytes, err = protocol.MarshalCBOR(numbered); err != nil {
			return fmt.Errorf("encode a delegate task's args: %w", err)
		}
	}
	// Its stop_idle as an operator types one, as a task's args keep it.
	values := map[string]any{}
	for _, p := range protocol.TunnelCoreParams(cmd.Action) {
		if p.Key == protocol.ParamStopIdle && p.Default != nil {
			values[protocol.ParamStopIdle] = p.Default
		}
	}
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: session.OperationID, SessionID: session.SessionID, Command: cmd.ID,
		Op: cmd.Op, Args: values, WireArgs: wireBytes, Result: cmd.Result, Action: cmd.Action, State: models.MinionTaskQueued,
		QueuedAt: s.now()}
	if err := s.tasks.Insert(ctx, t); err != nil {
		return fmt.Errorf("queue a delegate task: %w", err)
	}
	s.dlg.mu.Lock()
	s.dlg.awaited[t.TaskID] = session.SessionID
	s.dlg.mu.Unlock()
	s.logger.Info("delegates: core queued a delegate task", zap.String("session_id", session.SessionID.String()),
		zap.String("task_id", t.TaskID.String()), zap.String("command", cmd.ID))
	s.publishTask(t)
	return nil
}

// ask sends a call or an open through dt, and waits for its answer: the
// delegate's, or a failure core gives when none can come (its delegate
// late, the deadline gone, its task ended). n is the step's number, given
// when 0.
func (s *Service) ask(ctx context.Context, dt *delegateTask, d protocol.DelegateItem, deadline time.Duration, openOf string) protocol.DelegateItem {
	st := &delegateStep{deadline: deadline, answer: make(chan protocol.DelegateItem, 1), openOf: openOf}
	s.dlg.mu.Lock()
	if d.N == 0 {
		dt.nextN++
		d.N = dt.nextN
	}
	st.pieces = d.Pieces(pieceRoom(dt.limit, d))
	dt.steps[d.N] = st
	session := dt.session
	s.dlg.mu.Unlock()
	s.wake(session)
	select {
	case a := <-st.answer:
		return a
	case <-ctx.Done():
		s.dlg.mu.Lock()
		delete(dt.steps, d.N)
		s.dlg.mu.Unlock()
		return protocol.DelegateItem{Failure: &protocol.LinkFailure{Kind: protocol.LinkFailOther, Msg: "core gave up on it"}}
	}
}

// nextStep is the number of dt's next step.
func (s *Service) nextStep(dt *delegateTask) uint64 {
	s.dlg.mu.Lock()
	defer s.dlg.mu.Unlock()
	dt.nextN++
	return dt.nextN
}

// Call is a call to the target through its delegate, answered as the
// control link's.
func (l *delegateLink) Call(ctx context.Context, module string, c channellink.Call) (protocol.LinkFrame, error) {
	dt, serve, session, err := l.s.delegateFor(ctx, *l.t.DelegateID, l.t.OperationID, module, l.t.Kind, l.version)
	if err != nil {
		return protocol.LinkFrame{}, err
	}
	// The answer comes in the delegate's messages, the way they come.
	l.via = viaThrough(session, dt.id, session.Via)
	a := l.s.ask(ctx, dt, protocol.DelegateItem{T: protocol.ItemDelegate, Task: dt.id[:], Step: protocol.DelegateCall, Serve: serve,
		Values: c.Values, Credentials: c.Credentials, ID: c.ID[:], Frame: c.Data, Deadline: uint64(c.Deadline.Milliseconds())}, c.Deadline, "")
	return linkAnswer(a), nil
}

// Open opens the target's connection through its delegate, as the control
// link's open: sent says the open, and once the delegate answered that it
// opened, the connection is the open's as a socket naming it would be
// (made as the answer is taken).
func (l *delegateLink) Open(ctx context.Context, module string, values map[string]any, creds map[string]protocol.Credential,
	under [16]byte, _ *protocol.LinkKeys, _ *protocol.LinkActing, deadline time.Duration,
	sent func(instance string, request uint64)) (protocol.LinkFrame, error) {
	dt, serve, _, err := l.s.delegateFor(ctx, *l.t.DelegateID, l.t.OperationID, module, l.t.Kind, l.version)
	if err != nil {
		return protocol.LinkFrame{}, err
	}
	n := l.s.nextStep(dt)
	instance := delegateInstance(dt.id)
	sent(instance, n)
	a := l.s.ask(ctx, dt, protocol.DelegateItem{T: protocol.ItemDelegate, Task: dt.id[:], Step: protocol.DelegateOpen, N: n, Serve: serve,
		Values: values, Credentials: creds, Under: under[:], Deadline: uint64(deadline.Milliseconds())}, deadline, module)
	// One that opened is the open's connection already (delegateAnswer).
	return linkAnswer(a), nil
}

// linkAnswer is a delegate's answer as the control link's.
func linkAnswer(a protocol.DelegateItem) protocol.LinkFrame {
	return protocol.LinkFrame{Type: protocol.LinkAnswer, Request: a.N, ID: a.ID, Data: a.Frame, Status: a.Status, Failure: a.Failure,
		Excerpt: a.Head, Peer: peerOf(a.Peer), Logins: a.Logins}
}
