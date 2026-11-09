package minionsync

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Delegation (docs: contracts/minion-messages/delegation/): a delegate is a
// session whose minion serves a channel kind's transport on its host while
// a task of its runs (protocol.ActionDelegate, ActionReverseDelegate), and
// carries what a channel module would say to core in delegate items: a
// sync's request and reply, a call's or an open's and its answer, a
// connection's frames and close. Core takes each frame as a channel's
// (Exchange): a delegated connection is a Conn whose frames go out as
// delegate items to the delegate, a delegated sync a sync call's. A
// delegate's state is in memory, as a tunnel's streams are: a restart
// loses it, its connections close, and the delegate's items about them
// get a close back.

// DelegateSchemas reads factories' build schemas: what a delegate command
// serves. Satisfied by the module registry.
type DelegateSchemas interface {
	LatestBuildSchema(ctx context.Context, moduleName string) (*models.ModuleBuildSchema, error)
}

// WithDelegates has the service take delegates' items, with the build
// schemas that say what their commands serve. Without it a delegate item
// is one core does not know.
func (s *Service) WithDelegates(schemas DelegateSchemas) *Service {
	s.delegateSchemas = schemas
	return s
}

// Errors a call or an open through a delegate meets.
var (
	// ErrNoDelegateTask: the delegate runs no task serving the target's
	// kind yet; core queued one if none was waiting.
	ErrNoDelegateTask = errors.New("the delegate runs no task serving the kind yet")
	// ErrNotADelegate: the session named cannot be the target's delegate
	// (closed, of another operation, or its build serves no such kind).
	ErrNotADelegate = errors.New("the session cannot carry the target's calls")
)

const (
	// delegateResend is how soon a call, an open or a close core sent a
	// delegate goes again while unanswered.
	delegateResend = 5 * time.Second
	// delegateGrace is how long past a step's deadline core waits for its
	// answer.
	delegateGrace = 30 * time.Second
	// delegatePieceAge is how long pieces of a frame wait for the rest: a
	// lost piece is never sent again.
	delegatePieceAge = 2 * time.Minute
	// delegateTick is how often core looks at its delegates: their
	// presence, steps overdue, stop_idle.
	delegateTick = 5 * time.Second
	// delegateKeepAlive is how often a delegated connection checks its
	// session in, as an open socket does.
	delegateKeepAlive = 30 * time.Second
	// delegateHeld caps the frames being joined from a delegate task's
	// pieces; delegateQueued what waits to go to it (past it, frames are
	// dropped: they are messages, which their minions send again).
	delegateHeld   = 8 << 20
	delegateQueued = 16 << 20
)

// delegates are the delegate tasks core knows of now, by task id.
type delegates struct {
	mu    sync.Mutex
	tasks map[uuid.UUID]*delegateTask
	// awaited are delegate tasks core queued itself: once their minion has
	// one, the targets that wait for it are called.
	awaited map[uuid.UUID]uuid.UUID
}

func newDelegates() *delegates {
	return &delegates{tasks: map[uuid.UUID]*delegateTask{}, awaited: map[uuid.UUID]uuid.UUID{}}
}

// delegateTask is a delegate's task and what goes through it.
type delegateTask struct {
	id, session, operation uuid.UUID
	action, command        string
	serves                 []protocol.ChannelVersions
	stopIdle               time.Duration
	// limit is the largest message the delegate's connection carries:
	// pieces are cut for it.
	limit int
	// usedAt is when an item last went through, either way.
	usedAt time.Time
	// nextN numbers what core starts (a delegate task's calls and opens).
	nextN uint64
	// steps are calls and opens waiting for their answers, by n; closing
	// the connections core asked closed, waiting for the delegate's close.
	steps   map[uint64]*delegateStep
	closing map[uint64]*delegateStep
	// out are frames and replies to go, once each, in order; queued their
	// bytes.
	out    []protocol.DelegateItem
	queued int
	join   protocol.DelegateJoiner
	// conns are the connections through it, by n; seq the next number of
	// each one's frames from core.
	conns    map[uint64]*delegateConn
	seq      map[uint64]uint64
	carried  uint64
	stopping bool
}

// delegateConn is a connection through a delegate.
type delegateConn struct {
	conn     *Conn
	module   string
	kind     string
	openedAt time.Time
	keptAt   time.Time
}

// delegateStep is a call, an open or a close core waits to have answered.
type delegateStep struct {
	pieces    []protocol.DelegateItem
	sentAt    time.Time
	firstSent time.Time
	deadline  time.Duration
	answer    chan protocol.DelegateItem
	// openOf is an open's channel kind: the connection its answer opens is
	// made as the answer is taken, before any frame over it.
	openOf string
}

// delegateInstance is the instance a delegate task's connections and opens
// are of, as a channel instance's are.
func delegateInstance(task uuid.UUID) string { return "delegate:" + task.String() }

// delegated is what a context says of a delegated message: the delegates on
// its way, nearest first (the session's path, models.MinionSession.Via),
// and their operation.
type delegated struct {
	via       []models.MinionSessionVia
	operation uuid.UUID
}

type delegatedKey struct{}

// throughDelegate is ctx for the frames session's delegate task dt
// carries: the delegates ctx's own message came through, behind it.
func throughDelegate(ctx context.Context, session *models.MinionSession, dt *delegateTask) context.Context {
	d, _ := ctx.Value(delegatedKey{}).(delegated)
	return withVia(ctx, viaThrough(session, dt.id, d.via), dt.operation)
}

// viaThrough is the path through session's delegate task task, the
// delegates behind it (the way session's own messages come) after.
func viaThrough(session *models.MinionSession, task uuid.UUID, behind []models.MinionSessionVia) []models.MinionSessionVia {
	return append([]models.MinionSessionVia{{SessionID: session.SessionID, Number: session.Number, TaskID: task}}, behind...)
}

// withVia is ctx for a message that came through the delegates via, of
// operation's.
func withVia(ctx context.Context, via []models.MinionSessionVia, operation uuid.UUID) context.Context {
	return context.WithValue(ctx, delegatedKey{}, delegated{via: via, operation: operation})
}

// delegatedOf is what ctx says of a delegated message; ok false for one a
// channel brought.
func delegatedOf(ctx context.Context) (delegated, bool) {
	d, ok := ctx.Value(delegatedKey{}).(delegated)
	return d, ok
}

// operation is the operation the sender is of.
func (f *sender) operation() uuid.UUID {
	switch {
	case f.session != nil:
		return f.session.OperationID
	case f.staging != nil:
		return f.staging.OperationID
	case f.build != nil:
		return f.build.OperationID
	}
	return uuid.Nil
}

// delegatedRefusal says why a delegated message from from is not taken: of
// another operation, too many delegates deep, or one of the delegates on its
// way (a loop); "" when it is.
func delegatedRefusal(ctx context.Context, from *sender) string {
	d, ok := delegatedOf(ctx)
	switch {
	case !ok:
		return ""
	case from.operation() != d.operation:
		return "a minion of another operation"
	case len(d.via) > protocol.MaxDelegateDepth:
		return fmt.Sprintf("more than %d delegates deep", protocol.MaxDelegateDepth)
	case from.session != nil && slices.ContainsFunc(d.via, func(v models.MinionSessionVia) bool { return v.SessionID == from.session.SessionID }):
		return "a delegate on its own way (a loop)"
	}
	return ""
}

// pathOf is the way ctx's message of was came (its delegates; none for a
// channel's), as its check-in records it: since now, unless was's messages
// came the same way before.
func (s *Service) pathOf(ctx context.Context, was *models.MinionSession) ([]models.MinionSessionVia, time.Time) {
	d, _ := delegatedOf(ctx)
	if was != nil && was.ViaSince != nil && len(d.via) > 0 && models.SameVia(was.Via, d.via) {
		return d.via, *was.ViaSince
	}
	return d.via, s.now()
}

// withPath is c, a message's check-in of was's, with the way it came.
func (s *Service) withPath(ctx context.Context, c repository.MinionSessionCheckIn, was *models.MinionSession) repository.MinionSessionCheckIn {
	c.Path = true
	c.Via, c.ViaSince = s.pathOf(ctx, was)
	return c
}

// delegateTaskOf is the delegate task id of session, loaded when core
// does not hold it yet; nil when it is none (not the session's, not a
// delegate's, ended or ending).
func (s *Service) delegateTaskOf(ctx context.Context, session *models.MinionSession, id uuid.UUID) (*delegateTask, error) {
	s.dlg.mu.Lock()
	dt := s.dlg.tasks[id]
	s.dlg.mu.Unlock()
	if dt != nil {
		if dt.session != session.SessionID || dt.stopping {
			return nil, nil
		}
		return dt, nil
	}
	if s.tasks == nil || s.delegateSchemas == nil {
		return nil, nil
	}
	t, err := s.tasks.FindByTaskID(ctx, id)
	switch {
	case repository.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load a delegate task: %w", err)
	case t.SessionID != session.SessionID || !protocol.IsDelegateAction(t.Action) || t.Finished() || t.CancelRequestedAt != nil:
		return nil, nil
	}
	cmd, err := s.delegateCommand(ctx, session, t.Command)
	if err != nil || cmd == nil {
		return nil, err
	}
	stopIdle := delegateStopIdle(t.Action, t.Args[protocol.ParamStopIdle])
	now := s.now()
	dt = &delegateTask{id: id, session: session.SessionID, operation: session.OperationID, action: t.Action, command: t.Command,
		serves: cmd.Serves, stopIdle: stopIdle, limit: planLimit(session), usedAt: now,
		steps: map[uint64]*delegateStep{}, closing: map[uint64]*delegateStep{}, conns: map[uint64]*delegateConn{},
		seq: map[uint64]uint64{}, join: protocol.DelegateJoiner{MaxHeld: delegateHeld}}
	s.dlg.mu.Lock()
	defer s.dlg.mu.Unlock()
	if had := s.dlg.tasks[id]; had != nil {
		return had, nil
	}
	s.dlg.tasks[id] = dt
	return dt, nil
}

// delegateStopIdle is a delegate task's stop_idle as its args say it, the
// action's default when they do not.
func delegateStopIdle(action string, v any) time.Duration {
	switch x := v.(type) {
	case string:
		if d, err := time.ParseDuration(x); err == nil {
			return d
		}
	case int64:
		return time.Duration(x) * time.Millisecond
	case uint64:
		return time.Duration(x) * time.Millisecond
	case float64:
		return time.Duration(x) * time.Millisecond
	}
	for _, p := range protocol.TunnelCoreParams(action) {
		if p.Key == protocol.ParamStopIdle {
			if str, ok := p.Default.(string); ok {
				d, _ := time.ParseDuration(str)
				return d
			}
		}
	}
	return 0
}

// delegateCommand is the command id of session's build as its factory's
// latest schema has it; nil when it has none such.
func (s *Service) delegateCommand(ctx context.Context, session *models.MinionSession, id string) (*protocol.BuildCommand, error) {
	cmds, err := s.delegateCommands(ctx, session)
	if err != nil {
		return nil, err
	}
	for i := range cmds {
		if cmds[i].ID == id {
			return &cmds[i], nil
		}
	}
	return nil, nil
}

// delegateCommands are the delegate commands session's build has, as its
// factory's latest schema declares them.
func (s *Service) delegateCommands(ctx context.Context, session *models.MinionSession) ([]protocol.BuildCommand, error) {
	if s.delegateSchemas == nil {
		return nil, nil
	}
	b, err := s.builds.FindByBuildID(ctx, session.BuildID)
	if repository.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load the delegate's build: %w", err)
	}
	sc, err := s.delegateSchemas.LatestBuildSchema(ctx, b.ModuleName)
	if err != nil {
		return nil, fmt.Errorf("load the delegate's build schema: %w", err)
	}
	if sc == nil {
		return nil, nil
	}
	var out []protocol.BuildCommand
	for _, c := range sc.Commands {
		if protocol.IsDelegateAction(c.Action) && slices.Contains(b.Commands, c.ID) {
			out = append(out, c)
		}
	}
	return out, nil
}

// served is the task's command's serves at place (from 1), with the kinds
// of its action: ok false for none.
func (dt *delegateTask) served(place uint64, kind string) (protocol.ChannelVersions, bool) {
	if place < 1 || place > uint64(len(dt.serves)) {
		return protocol.ChannelVersions{}, false
	}
	sv := dt.serves[place-1]
	return sv, slices.Contains(sv.Kinds, kind)
}

// takeDelegate takes one of a delegate's items, under its session.
func (s *Service) takeDelegate(ctx context.Context, session *models.MinionSession, raw cbor.RawMessage, i int) ([]cbor.RawMessage, error) {
	if s.delegateSchemas == nil {
		return errItem(protocol.ErrItemUnknownItem, "core carries no delegates", i)
	}
	d, rErr := protocol.ReadDelegate(raw, true)
	if rErr != nil {
		s.logger.Info("delegates: an item that does not read", zap.String("session_id", session.SessionID.String()), zap.Error(rErr))
		return errItem(protocol.ErrItemBadMessage, rErr.Error(), i)
	}
	id, _ := uuid.FromBytes(d.Task)
	dt, err := s.delegateTaskOf(ctx, session, id)
	if err != nil {
		return nil, err
	}
	if dt == nil {
		return delegateCloseOf(d, protocol.DelegateTaskEnded, "no such delegate task")
	}
	s.dlg.mu.Lock()
	dt.usedAt, dt.limit = s.now(), planLimit(session)
	if d.Step == protocol.DelegateSync || d.Step == protocol.DelegateFrame {
		dt.carried++
	}
	s.dlg.mu.Unlock()
	reverse := dt.action == protocol.ActionReverseDelegate
	switch d.Step {
	case protocol.DelegateSync:
		if !reverse {
			return errItem(protocol.ErrItemBadMessage, "a sync is a reverse_delegate task's", i)
		}
		return s.delegateSync(ctx, session, dt, d, i)
	case protocol.DelegateFrame:
		return s.delegateFrame(ctx, session, dt, d, i)
	case protocol.DelegateAnswer:
		if reverse {
			return errItem(protocol.ErrItemBadMessage, "an answer is a delegate task's", i)
		}
		s.delegateAnswer(dt, d)
	case protocol.DelegateClose:
		s.delegateClosed(ctx, dt, d.N)
	}
	return nil, nil
}

// delegateCloseOf answers d, about what core does not have, with a close.
func delegateCloseOf(d protocol.DelegateItem, reason int, msg string) ([]cbor.RawMessage, error) {
	if d.Step == protocol.DelegateClose {
		return nil, nil // a close needs no answer
	}
	item, err := protocol.MarshalCBOR(protocol.DelegateItem{T: protocol.ItemDelegate, Task: d.Task, Step: protocol.DelegateClose,
		N: d.N, Reason: reason, Msg: msg})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

// joined is d's frame once whole: done false while pieces of it are
// missing.
func (s *Service) joined(dt *delegateTask, d protocol.DelegateItem) ([]byte, bool) {
	s.dlg.mu.Lock()
	defer s.dlg.mu.Unlock()
	frame, done, err := dt.join.Add(d, s.now())
	if err != nil {
		s.logger.Info("delegates: a piece not taken", zap.String("task_id", dt.id.String()), zap.Error(err))
		return nil, false
	}
	return frame, done
}

// delegateSync takes a sync: the frame as the sync call's, its reply
// queued for the delegate (in the reply to the message that carried it,
// when it fits).
func (s *Service) delegateSync(ctx context.Context, session *models.MinionSession, dt *delegateTask, d protocol.DelegateItem, i int) ([]cbor.RawMessage, error) {
	sv, ok := dt.served(d.Serve, protocol.KindReverseCall)
	if !ok {
		return errItem(protocol.ErrItemBadMessage, fmt.Sprintf("serve %d is no reverse-call kind of the task's command", d.Serve), i)
	}
	frame, done := s.joined(dt, d)
	if !done {
		return nil, nil
	}
	id, _ := uuid.FromBytes(d.ID)
	dctx := ViaChannel(throughDelegate(ctx, session, dt), sv.ModuleName)
	reply, err := s.Exchange(dctx, nil, id.String(), peerOf(d.Peer), frame)
	r := protocol.DelegateItem{T: protocol.ItemDelegate, Task: d.Task, Step: protocol.DelegateReply, N: d.N}
	switch {
	case errors.Is(err, ErrUnopened):
		r.Status = 422
	case errors.Is(err, ErrReplayed):
		r.Status = 409
	case err != nil:
		s.logger.Error("delegates: a delegated sync", zap.String("task_id", dt.id.String()), zap.Error(err))
		r.Status = 500
	case reply == nil:
		r.Status = protocol.ReplyNoContent
	default:
		r.Status, r.ID, r.Frame = protocol.ReplyOK, d.ID, reply
	}
	s.delegateQueue(dt, r)
	return nil, nil
}

// delegateFrame takes a frame over a connection through the delegate: a
// reverse one's first opens it.
func (s *Service) delegateFrame(ctx context.Context, session *models.MinionSession, dt *delegateTask, d protocol.DelegateItem, i int) ([]cbor.RawMessage, error) {
	s.dlg.mu.Lock()
	dc := dt.conns[d.N]
	s.dlg.mu.Unlock()
	if dc == nil {
		if dt.action != protocol.ActionReverseDelegate || d.Serve == 0 {
			return delegateCloseOf(d, protocol.DelegateNoConnection, "no such connection")
		}
		sv, ok := dt.served(d.Serve, protocol.KindReversePersistent)
		if !ok {
			return errItem(protocol.ErrItemBadMessage, fmt.Sprintf("serve %d is no reverse-persistent kind of the task's command", d.Serve), i)
		}
		dc = s.newDelegateConn(dt, d.N, sv.ModuleName, protocol.KindReversePersistent, peerOf(d.Peer))
	}
	frame, done := s.joined(dt, d)
	if !done {
		return nil, nil
	}
	id, _ := uuid.FromBytes(d.ID)
	dctx := ViaChannel(throughDelegate(ctx, session, dt), dc.module)
	reply, err := s.Exchange(dctx, dc.conn, id.String(), dc.conn.Peer, frame)
	switch {
	case errors.Is(err, ErrUnopened), errors.Is(err, ErrReplayed):
		// As over a socket: nothing to tell the minion.
		s.logger.Debug("delegates: a frame not taken", zap.String("task_id", dt.id.String()), zap.Error(err))
	case err != nil:
		s.logger.Error("delegates: a delegated frame", zap.String("task_id", dt.id.String()), zap.Error(err))
	case reply != nil:
		if err := dc.conn.send(ctx, protocol.ConnFrame{ID: d.ID, Data: reply}); err != nil {
			s.logger.Debug("delegates: a reply that cannot go", zap.String("task_id", dt.id.String()), zap.Error(err))
		}
	}
	return nil, nil
}

// newDelegateConn is connection n through dt, of a channel kind it serves,
// from peer: a Conn whose frames go to the delegate as delegate items, and
// whose close asks the delegate to close it.
func (s *Service) newDelegateConn(dt *delegateTask, n uint64, module, kind, peer string) *delegateConn {
	task := dt.id
	var c *Conn
	c = NewConn(fmt.Sprintf("delegate:%s:%d", task, n), kind, delegateInstance(task), peer,
		func(_ context.Context, f protocol.ConnFrame) error { return s.delegateSend(task, n, f) },
		func() { s.closeDelegateConn(task, n, c) })
	now := s.now()
	dc := &delegateConn{conn: c, module: module, kind: kind, openedAt: now, keptAt: now}
	s.dlg.mu.Lock()
	dt.conns[n] = dc
	s.dlg.mu.Unlock()
	s.logger.Info("delegates: a connection opened through a delegate", zap.String("task_id", task.String()), zap.Uint64("n", n),
		zap.String("module", module), zap.String("kind", kind))
	return dc
}

// delegateSend queues a frame of connection n of task for its delegate.
func (s *Service) delegateSend(task uuid.UUID, n uint64, f protocol.ConnFrame) error {
	s.dlg.mu.Lock()
	dt := s.dlg.tasks[task]
	if dt == nil || dt.conns[n] == nil {
		s.dlg.mu.Unlock()
		return errors.New("the delegated connection is gone")
	}
	seq := dt.seq[n]
	dt.seq[n] = seq + 1
	s.dlg.mu.Unlock()
	s.delegateQueue(dt, protocol.DelegateItem{T: protocol.ItemDelegate, Task: task[:], Step: protocol.DelegateFrame, N: n, Seq: seq,
		ID: f.ID, Frame: f.Data})
	return nil
}

// closeDelegateConn is core closing connection n of task: the delegate is
// asked to close it (until it says it did), and core forgets it.
func (s *Service) closeDelegateConn(task uuid.UUID, n uint64, c *Conn) {
	s.dlg.mu.Lock()
	dt := s.dlg.tasks[task]
	if dt == nil || dt.conns[n] == nil || dt.conns[n].conn != c {
		s.dlg.mu.Unlock()
		return
	}
	delete(dt.conns, n)
	delete(dt.seq, n)
	dt.join.Drop(n)
	item := protocol.DelegateItem{T: protocol.ItemDelegate, Task: task[:], Step: protocol.DelegateClose, N: n, Reason: protocol.DelegateClosed}
	dt.closing[n] = &delegateStep{pieces: []protocol.DelegateItem{item}}
	session := dt.session
	s.dlg.mu.Unlock()
	s.wake(session)
	go s.Closed(context.Background(), c)
}

// delegateClosed is the delegate's word that connection n ended (or its
// answer to core's close).
func (s *Service) delegateClosed(ctx context.Context, dt *delegateTask, n uint64) {
	s.dlg.mu.Lock()
	dc := dt.conns[n]
	delete(dt.conns, n)
	delete(dt.seq, n)
	delete(dt.closing, n)
	dt.join.Drop(n)
	s.dlg.mu.Unlock()
	if dc != nil {
		s.logger.Info("delegates: a connection through a delegate ended", zap.String("task_id", dt.id.String()), zap.Uint64("n", n))
		dc.conn.end()
		s.Closed(ctx, dc.conn)
	}
}

// delegateAnswer gives a call's or an open's answer to what waits for it.
func (s *Service) delegateAnswer(dt *delegateTask, d protocol.DelegateItem) {
	frame, done := s.joined(dt, d)
	if !done {
		return
	}
	d.Frame, d.Off, d.Size = frame, 0, 0
	s.dlg.mu.Lock()
	st := dt.steps[d.N]
	delete(dt.steps, d.N)
	s.dlg.mu.Unlock()
	if st != nil && st.openOf != "" && d.Failure == nil {
		// It opened: the connection is the open's now, as a socket naming
		// it would be, so the minion's first frame over it, in this message
		// or the next, finds it.
		instance := delegateInstance(dt.id)
		dc := s.newDelegateConn(dt, d.N, st.openOf, protocol.KindBindPersistent, peerOf(d.Peer))
		if _, _, ok := s.OpenedFor(instance, d.N); !ok || !s.ForTarget(dc.conn, instance, d.N) {
			dc.conn.closeBecause("its open no longer waits for it")
		}
	}
	if st != nil && st.answer != nil {
		select {
		case st.answer <- d:
		default:
		}
	}
}

// delegateQueue queues d for dt's delegate, in pieces its messages carry,
// and wakes it.
func (s *Service) delegateQueue(dt *delegateTask, d protocol.DelegateItem) {
	s.dlg.mu.Lock()
	if dt.queued+len(d.Frame) > delegateQueued {
		s.dlg.mu.Unlock()
		s.logger.Info("delegates: too much waits for a delegate, a frame dropped", zap.String("task_id", dt.id.String()))
		return
	}
	pieces := d.Pieces(pieceRoom(dt.limit, d))
	dt.out = append(dt.out, pieces...)
	dt.queued += len(d.Frame)
	session := dt.session
	s.dlg.mu.Unlock()
	s.wake(session)
}

// pieceRoom is how many bytes of a frame an item like d carries in a
// message of limit: what is left once the item's other keys and the
// message's own are in.
func pieceRoom(limit int, d protocol.DelegateItem) int {
	d.Frame = nil
	head, err := protocol.MarshalCBOR(d)
	if err != nil {
		head = make([]byte, protocol.DelegateItemOverhead)
	}
	return max(limit-messageOverhead-itemOverhead-len(head)-protocol.DelegateItemOverhead, 64)
}

// wake tells the session core has something for it: pushed at once over a
// persistent connection, in a call to a page, with its next message
// otherwise.
func (s *Service) wake(session uuid.UUID) {
	if s.reachable(session) {
		s.pushAfter(session, false, tunnelDelay)
		return
	}
	s.Nudge(session, false)
}

// delegateItems adds what goes to session's delegate tasks through add,
// within most items: core's closes and steps (again while unanswered),
// then frames and replies in order. polls says something waits for the
// delegate that a polling one should come back for soon.
func (s *Service) delegateItems(session uuid.UUID, most int, add func(cbor.RawMessage) bool) (added int, polls bool) {
	s.dlg.mu.Lock()
	defer s.dlg.mu.Unlock()
	now := s.now()
	var tasks []*delegateTask
	for _, dt := range s.dlg.tasks {
		if dt.session == session {
			tasks = append(tasks, dt)
		}
	}
	slices.SortFunc(tasks, func(a, b *delegateTask) int { return strings.Compare(a.id.String(), b.id.String()) })
	put := func(d protocol.DelegateItem) bool {
		if added >= most {
			return false
		}
		item, err := protocol.MarshalCBOR(d)
		if err != nil || !add(item) {
			return false
		}
		added++
		return true
	}
	// due sends st's pieces when it was never sent, or not lately.
	due := func(st *delegateStep) bool {
		if !st.sentAt.IsZero() && now.Sub(st.sentAt) < delegateResend {
			return true
		}
		for _, p := range st.pieces {
			if !put(p) {
				return false
			}
		}
		st.sentAt = now
		if st.firstSent.IsZero() {
			st.firstSent = now
		}
		return true
	}
	full := false
	for _, dt := range tasks {
		for _, n := range sortedKeys(dt.closing) {
			if full = !due(dt.closing[n]); full {
				break
			}
		}
		for _, n := range sortedKeys(dt.steps) {
			if full || !due(dt.steps[n]) {
				full = true
				break
			}
		}
		for !full && len(dt.out) > 0 {
			if !put(dt.out[0]) {
				full = true
				break
			}
			dt.queued -= len(dt.out[0].Frame)
			dt.out = dt.out[1:]
		}
		polls = polls || len(dt.out) > 0 || len(dt.steps) > 0 || len(dt.closing) > 0
	}
	return added, polls
}

func sortedKeys[V any](m map[uint64]V) []uint64 {
	out := make([]uint64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// peerOf is a delegate's peer as core keeps a channel's: an IP address, ""
// for none.
func peerOf(b []byte) string {
	if len(b) != net.IPv4len && len(b) != net.IPv6len {
		return ""
	}
	return peerAddress(net.IP(b).String())
}

// WatchDelegates follows delegate tasks' changes: one that ends, or is to,
// takes what it carried with it; one core queued that its minion now has
// calls the targets waiting for it.
func (s *Service) WatchDelegates() func() {
	return s.bus.Subscribe([]eventbus.Topic{eventbus.TopicMinionTaskChanged}, func(ctx context.Context, e eventbus.Event) {
		p, ok := e.Payload.(eventbus.MinionTaskEventPayload)
		if !ok {
			return
		}
		id, err := uuid.Parse(p.TaskID)
		if err != nil {
			return
		}
		s.dlg.mu.Lock()
		_, ours := s.dlg.tasks[id]
		_, awaited := s.dlg.awaited[id]
		s.dlg.mu.Unlock()
		if ours || awaited {
			go s.checkDelegateTask(context.WithoutCancel(ctx), id)
		}
	})
}

// checkDelegateTask reads a delegate task again: one that ended, or is to,
// ends what it carries; one core queued that its minion took calls the
// targets that wait for it.
func (s *Service) checkDelegateTask(ctx context.Context, id uuid.UUID) {
	t, err := s.tasks.FindByTaskID(ctx, id)
	switch {
	case repository.IsNotFound(err):
		s.endDelegateTask(ctx, id, "the task is gone")
		return
	case err != nil:
		s.logger.Warn("delegates: read a delegate task", zap.String("task_id", id.String()), zap.Error(err))
		return
	case t.Finished() || t.CancelRequestedAt != nil:
		s.endDelegateTask(ctx, id, "the delegate's task ended")
		return
	}
	if t.AckedAt == nil {
		return
	}
	s.dlg.mu.Lock()
	session, awaited := s.dlg.awaited[id]
	delete(s.dlg.awaited, id)
	s.dlg.mu.Unlock()
	if awaited {
		s.callDelegated(ctx, session)
	}
}

// callDelegated calls the targets session's minion is the delegate of: a
// task of its that serves them runs now.
func (s *Service) callDelegated(ctx context.Context, session uuid.UUID) {
	if s.targets == nil {
		return
	}
	ts, err := s.targets.FindByDelegateID(ctx, session)
	if err != nil {
		s.logger.Warn("delegates: the targets a delegate carries", zap.String("session_id", session.String()), zap.Error(err))
		return
	}
	for _, t := range ts {
		if t.Kind == protocol.KindBindPersistent && t.PolicyOf() == models.PolicyManual && t.State != models.TargetConnecting {
			continue
		}
		s.callSoon(t.TargetID, bindCheck, 0)
	}
}

// endDelegateTask forgets a delegate task: its connections close, and what
// waits for its delegate's answer fails.
func (s *Service) endDelegateTask(ctx context.Context, id uuid.UUID, why string) {
	s.dlg.mu.Lock()
	dt := s.dlg.tasks[id]
	delete(s.dlg.tasks, id)
	delete(s.dlg.awaited, id)
	s.dlg.mu.Unlock()
	if dt == nil {
		return
	}
	s.logger.Info("delegates: a delegate task ended", zap.String("task_id", id.String()), zap.String("why", why),
		zap.Int("connections", len(dt.conns)))
	for _, st := range dt.steps {
		failStep(st, protocol.LinkFailOther, why)
	}
	for _, dc := range dt.conns {
		dc.conn.end()
		s.Closed(ctx, dc.conn)
	}
}

// failStep answers a step that waits with a failure.
func failStep(st *delegateStep, kind int, why string) {
	if st.answer == nil {
		return
	}
	select {
	case st.answer <- protocol.DelegateItem{Failure: &protocol.LinkFailure{Kind: kind, Msg: why}}:
	default:
	}
}

// RunDelegates looks at the delegates now and then, until ctx ends: their
// pieces that never completed, the steps no answer came for, a delegate
// gone lost (its connections close), its connections' check-ins, and a task
// idle past its stop_idle.
func (s *Service) RunDelegates(ctx context.Context) {
	if s.delegateSchemas == nil {
		return
	}
	t := time.NewTicker(delegateTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.lookAtDelegates(ctx)
	}
}

// lookAtDelegates is one look of RunDelegates.
func (s *Service) lookAtDelegates(ctx context.Context) {
	now := s.now()
	s.dlg.mu.Lock()
	tasks := make([]*delegateTask, 0, len(s.dlg.tasks))
	for _, dt := range s.dlg.tasks {
		dt.join.Expire(now.Add(-delegatePieceAge))
		tasks = append(tasks, dt)
	}
	s.dlg.mu.Unlock()
	for _, dt := range tasks {
		session, err := s.sessions.FindBySessionID(ctx, dt.session)
		switch {
		case repository.IsNotFound(err) || (err == nil && session.Closed(now)):
			s.endDelegateTask(ctx, dt.id, "the delegate's session closed")
			continue
		case err != nil:
			continue
		}
		reached := s.reachable(dt.session) || session.LateAt == nil || now.Before(*session.LateAt)
		lost := !s.reachable(dt.session) && session.LostAt != nil && !now.Before(*session.LostAt)
		var gone, keep []*delegateConn
		var idle bool
		s.dlg.mu.Lock()
		for n, st := range dt.steps {
			switch {
			case st.firstSent.IsZero() && !reached:
				delete(dt.steps, n)
				failStep(st, protocol.LinkFailUnreachable, fmt.Sprintf("the delegate, session #%d, is late", session.Number))
			case !st.firstSent.IsZero() && now.Sub(st.firstSent) > st.deadline+delegateGrace:
				delete(dt.steps, n)
				failStep(st, protocol.LinkFailTimeout, "the delegate did not answer in time")
			}
		}
		for n, dc := range dt.conns {
			switch {
			case lost:
				delete(dt.conns, n)
				gone = append(gone, dc)
			case reached && now.Sub(dc.keptAt) >= delegateKeepAlive:
				dc.keptAt = now
				keep = append(keep, dc)
			}
		}
		if dt.stopIdle > 0 && !dt.stopping && len(dt.conns) == 0 && len(dt.steps) == 0 && now.Sub(dt.usedAt) >= dt.stopIdle {
			dt.stopping, idle = true, true
		}
		s.dlg.mu.Unlock()
		for _, dc := range gone {
			dc.conn.end()
			s.Closed(ctx, dc.conn)
		}
		for _, dc := range keep {
			s.KeepAlive(ctx, dc.conn)
		}
		if idle {
			s.stopDelegate(ctx, dt)
		}
	}
}

// stopDelegate stops a delegate task nothing went through for its
// stop_idle, as Stop does.
func (s *Service) stopDelegate(ctx context.Context, dt *delegateTask) {
	s.logger.Info("delegates: stopping a delegate nothing used", zap.String("task_id", dt.id.String()), zap.Duration("stop_idle", dt.stopIdle))
	t, err := s.tasks.RequestCancelWhy(ctx, dt.id, s.now(), fmt.Sprintf("stopped by core: nothing went through it for %s (stop_idle)", dt.stopIdle))
	if err != nil {
		s.logger.Warn("delegates: stop a delegate", zap.String("task_id", dt.id.String()), zap.Error(err))
		return
	}
	s.publishTask(t)
}

// DelegateSummary is what a delegate task carries now.
type DelegateSummary struct {
	Serves      []protocol.ChannelVersions
	StopIdle    time.Duration
	UsedAt      time.Time
	Carried     uint64
	Waiting     int
	Connections []DelegateConnection
}

// DelegateConnection is a connection through a delegate.
type DelegateConnection struct {
	N          uint64
	ModuleName string
	Kind       string
	Peer       string
	OpenedAt   time.Time
	// Session is the session over it once its minion spoke.
	Session uuid.UUID
}

// Delegate is what the delegate task id carries now; ok false when core
// holds nothing of it (it carried nothing since core started, or ended).
func (s *Service) Delegate(id uuid.UUID) (DelegateSummary, bool) {
	s.dlg.mu.Lock()
	dt := s.dlg.tasks[id]
	if dt == nil {
		s.dlg.mu.Unlock()
		return DelegateSummary{}, false
	}
	out := DelegateSummary{Serves: slices.Clone(dt.serves), StopIdle: dt.stopIdle, UsedAt: dt.usedAt, Carried: dt.carried,
		Waiting: len(dt.out) + len(dt.steps)}
	conns := make([]*delegateConn, 0, len(dt.conns))
	ns := sortedKeys(dt.conns)
	for _, n := range ns {
		conns = append(conns, dt.conns[n])
	}
	s.dlg.mu.Unlock()
	for i, dc := range conns {
		c := DelegateConnection{N: ns[i], ModuleName: dc.module, Kind: dc.kind, Peer: dc.conn.Peer, OpenedAt: dc.openedAt}
		c.Session = dc.conn.ownerOf()
		out.Connections = append(out.Connections, c)
	}
	return out, true
}
