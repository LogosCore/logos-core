package minionsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// bind-persistent minions (docs: contracts/channel-core-connections/,
// bind-persistent connections): a process that listens, which core reaches
// by asking a channel, over its control link, to open a connection to its
// target. The channel dials, then opens the connection's socket naming the
// open (OpenedFor); from there the connection is any persistent one: the
// minion speaks first (a stage, or a check-in under its session), core
// pushes over it, and the session that speaks over it is the target's.
//
//   - The target is connected once its minion spoke: a message of the
//     target's build (or of the session it is linked to) that opens with
//     its id's keys, within the open's time. Another build's minion, or
//     nothing at all, is not the minion: the connection closes.
//   - Who opens it is the target's policy: on-demand (core, when the
//     session has something to send, and once when the target is added),
//     manual (an operator's Connect), always (core, and again whenever it
//     drops).
//   - It closes idle (models.IdleClose: 10 minutes on-demand, an hour
//     manual), but not while a task it carried runs; on Disconnect; or when
//     the minion, the channel or the network drops it: the target is then
//     not connected. One that drops soon after it opened counts as an open
//     that failed, so reopens back off.
//   - An open that fails leaves the target unreachable, retried as a
//     call's would be (but manual's).

const (
	// openDefault is how long an open may take when its kind says nothing
	// (ConnectionKind.MaxOpen): the channel's answer, and as long again for
	// the minion's first word once the connection's socket is up.
	openDefault = 30 * time.Second
	// openMax caps what a kind may ask for.
	openMax = 10 * time.Minute
	// alwaysReopen is how soon a connection of policy always is opened
	// again after it closed.
	alwaysReopen = 5 * time.Second
)

// steadyFor is how long a connection stays open after its minion spoke
// before a drop is no longer the open failing: one that closes sooner (a
// minion that hangs up at once, a channel that cannot keep it) counts one
// failure more, and is reopened after the target's retry wait.
var steadyFor = 30 * time.Second

// openWithin is an open's time: what its kind asks for (limit; 0 for
// nothing), within core's cap, else openDefault.
func openWithin(limit time.Duration) time.Duration {
	if limit <= 0 {
		return openDefault
	}
	return min(limit, openMax)
}

// openKey names an open as a connection's socket names it.
type openKey struct {
	instance string
	request  uint64
}

// opening is an open waiting for its socket: what it knows of its target
// (the Conn fields of the same names), taken once a socket named it
// (OpenedFor), and arrived closed once that socket is conn (ForTarget).
type opening struct {
	target, build, operation, session uuid.UUID
	policy                            string
	place                             int
	failures                          int
	downSince                         *time.Time
	taken                             bool
	arrived                           chan struct{}
	conn                              *Conn
}

// socketWait bounds how long an open the channel answered waits for the
// socket that took it to become its target's: the socket's handler does
// that right after the WebSocket's upgrade, which the channel's answer can
// outrun. Longer means the upgrade failed.
var socketWait = 5 * time.Second

// persistent are the opens waiting for their sockets, and the targets'
// open connections.
type persistent struct {
	mu    sync.Mutex
	opens map[openKey]*opening
	conns map[uuid.UUID][]*Conn
}

func newPersistent() *persistent {
	return &persistent{opens: map[openKey]*opening{}, conns: map[uuid.UUID][]*Conn{}}
}

// OpenedFor is the target and policy of the open a connection socket
// names (instance and request), taken: a second socket naming it is
// refused. ok false for an open core is not waiting for.
func (s *Service) OpenedFor(instance string, request uint64) (target uuid.UUID, policy string, ok bool) {
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	o := s.persist.opens[openKey{instance, request}]
	if o == nil || o.taken {
		return uuid.Nil, "", false
	}
	o.taken = true
	return o.target, o.policy, true
}

// ForTarget makes c the connection of the open its socket names (instance
// and request, taken by OpenedFor), and keeps it with its target's: its
// controller calls it once the socket is up, before any frame. The open,
// waiting for it, is told. false when that open no longer waits (it gave
// up on the socket, or its target's call ended): the socket is no one's,
// and its controller closes it.
func (s *Service) ForTarget(c *Conn, instance string, request uint64) bool {
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	o := s.persist.opens[openKey{instance, request}]
	if o == nil || !o.taken || o.conn != nil {
		return false
	}
	c.mu.Lock()
	c.target, c.policy, c.build, c.operation, c.session, c.place = o.target, o.policy, o.build, o.operation, o.session, o.place
	c.failures, c.downSince = o.failures, o.downSince
	c.mu.Unlock()
	o.conn = c
	s.persist.conns[o.target] = append(s.persist.conns[o.target], c)
	close(o.arrived)
	return true
}

// awaitSocket is the connection whose socket took the open key, once it is
// its target's (ForTarget), socketWait at most; nil when none came. An
// open no socket took is not waited for: it came to nothing. One given up
// on is forgotten at once, so a socket that comes after is refused.
func (s *Service) awaitSocket(ctx context.Context, key openKey) *Conn {
	s.persist.mu.Lock()
	o := s.persist.opens[key]
	s.persist.mu.Unlock()
	if o == nil || !o.taken {
		return nil
	}
	timer := time.NewTimer(socketWait)
	defer timer.Stop()
	select {
	case <-o.arrived:
	case <-timer.C:
	case <-ctx.Done():
	}
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	delete(s.persist.opens, key)
	return o.conn
}

// dropOpen forgets the open key, whatever came of it.
func (s *Service) dropOpen(key openKey) {
	s.persist.mu.Lock()
	delete(s.persist.opens, key)
	s.persist.mu.Unlock()
}

// targetConns are the target's open connections.
func (s *Service) targetConns(target uuid.UUID) []*Conn {
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	return append([]*Conn(nil), s.persist.conns[target]...)
}

// ConnectTarget opens a bind-persistent target's connection now (an
// operator's Connect, whatever its policy) and returns the target as the
// open left it. The open is core's own, as CheckTarget's call is.
func (s *Service) ConnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error) {
	if s.targets == nil {
		return nil, errors.New("targets are not served")
	}
	if err := await(ctx, func(c context.Context) { s.callTarget(c, targetID, bindConnect) }); err != nil {
		return nil, err
	}
	return s.targets.FindByTargetID(ctx, targetID)
}

// DisconnectTarget closes a bind-persistent target's connections and
// returns the target as that left it. One of policy always is opened
// again.
func (s *Service) DisconnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error) {
	if s.targets == nil {
		return nil, errors.New("targets are not served")
	}
	for _, c := range s.targetConns(targetID) {
		c.closeBecause("an operator disconnected it")
	}
	// The sockets' handlers finish the close (persistentClosed), which
	// drops the connections and then stores the state: wait a moment for
	// the state they leave.
	deadline := time.Now().Add(2 * time.Second)
	for {
		t, err := s.targets.FindByTargetID(ctx, targetID)
		if err != nil || time.Now().After(deadline) ||
			(len(s.targetConns(targetID)) == 0 && t.State != models.TargetConnected) {
			return t, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// closeTargetConns closes the connections of a target that is gone (it was
// removed during its open, or while one spoke).
func (s *Service) closeTargetConns(target uuid.UUID) {
	for _, c := range s.targetConns(target) {
		c.closeBecause("its target was removed")
	}
}

// closeBecause closes c, saying why for the target's state.
func (c *Conn) closeBecause(why string) {
	c.mu.Lock()
	if c.why == "" {
		c.why = why
	}
	c.mu.Unlock()
	if c.close != nil {
		c.close()
	}
}

// refuse closes c: a minion not its target's spoke over it, or nothing
// did. why says which, for the target's state.
func (c *Conn) refuse(why string) {
	c.mu.Lock()
	if c.why == "" {
		c.why, c.refused = why, true
	}
	c.mu.Unlock()
	if c.close != nil {
		c.close()
	}
}

// closeWhy is why core closed c ("" for the minion, the channel or the
// network), and whether that was a minion not the target's.
func (c *Conn) closeWhy() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.why, c.refused
}

// hear records the first message of the target's minion over c, at now.
func (c *Conn) hear(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.spokeAt.IsZero() {
		c.spokeAt = now
		close(c.heard)
	}
}

// spoke is when the target's minion first spoke over c; zero before.
func (c *Conn) spoke() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.spokeAt
}

// end records that c's socket closed.
func (c *Conn) end() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ended {
		c.ended = true
		close(c.gone)
	}
}

// follow makes session the one c's target is linked to: true when it was
// another.
func (c *Conn) follow(session uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == session {
		return false
	}
	c.session = session
	return true
}

// ofTarget reports whether from, who a message over c came under, is the
// minion c's target reaches: the session the target is linked to (which a
// later build may have claimed), a session, staging or build id of the
// target's build, or a later build's of the operation that claims the
// target's session (one built to persist it). Another build's, of this
// operation or another, is not.
func (c *Conn) ofTarget(from *sender) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case from.session != nil:
		return from.session.SessionID == c.session || from.session.BuildID == c.build
	case from.build == nil:
		return false
	case from.build.BuildID == c.build:
		return true
	}
	p := from.build.PersistedSessionID
	return c.session != uuid.Nil && p != nil && *p == c.session && from.build.OperationID == c.operation
}

// carry records the tasks items name as c's: a task core sends, a result
// or an ack the minion sends. A task's last result ends its carry.
func (c *Conn) carry(items []cbor.RawMessage) {
	if c.target == uuid.Nil {
		return
	}
	var add, drop []uuid.UUID
	for _, raw := range items {
		switch protocol.ItemType(raw) {
		case protocol.ItemTask, protocol.ItemResult:
			var it struct {
				Task []byte `cbor:"1,keyasint"`
				Done bool   `cbor:"3,keyasint,omitempty"`
			}
			if protocol.UnmarshalCBOR(raw, &it) != nil {
				continue
			}
			id, err := uuid.FromBytes(it.Task)
			switch {
			case err != nil:
			case protocol.ItemType(raw) == protocol.ItemResult && it.Done:
				drop = append(drop, id)
			default:
				add = append(add, id)
			}
		case protocol.ItemAck:
			var a struct {
				Tasks [][]byte `cbor:"1,keyasint,omitempty"`
			}
			if protocol.UnmarshalCBOR(raw, &a) != nil {
				continue
			}
			for _, t := range a.Tasks {
				if id, err := uuid.FromBytes(t); err == nil {
					add = append(add, id)
				}
			}
		}
	}
	if len(add) == 0 && len(drop) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range add {
		c.carried[id] = true
	}
	for _, id := range drop {
		delete(c.carried, id)
	}
}

// carriedRun reports whether a task c carried is still running: its
// minion has it, and says more of it over c. Those that ended are
// forgotten.
func (s *Service) carriedRun(ctx context.Context, c *Conn) bool {
	if s.tasks == nil {
		return false
	}
	c.mu.Lock()
	ids := make([]uuid.UUID, 0, len(c.carried))
	for id := range c.carried {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		task, err := s.tasks.FindByTaskID(ctx, id)
		switch {
		case err != nil && !repository.IsNotFound(err):
			return true // not known now: kept as running
		case err == nil && !task.Finished() && task.State != models.MinionTaskQueued:
			return true
		}
		c.mu.Lock()
		delete(c.carried, id)
		c.mu.Unlock()
	}
	return false
}

// openTarget opens a bind-persistent target's connection, when its policy
// takes why and none is open, and waits for its minion to speak over it.
func (s *Service) openTarget(ctx context.Context, t *models.MinionTarget, why bindTrigger) error {
	if len(s.targetConns(t.TargetID)) > 0 {
		return nil // open: core pushes over it
	}
	policy := t.PolicyOf()
	switch why {
	case bindWake:
		return nil // a stream of a session with no connection waits for one
	case bindNews, bindScheduled:
		if policy == models.PolicyManual {
			return nil // an operator opens it
		}
	}
	if (why == bindNews || why == bindScheduled) && policy != models.PolicyManual {
		// A minion with other connections may be on one of them: core
		// dials this one only when the minion is on it, is switching to it
		// (listening for the dial), or has gone quiet where it is.
		where, detail := s.minionOn(ctx, t)
		if where == minionElsewhere {
			if t.State == models.TargetNotConnected && t.StateDetail == detail && t.NextCallAt == nil {
				return nil
			}
			now := s.now()
			t.State, t.StateDetail, t.StateAt, t.NextCallAt = models.TargetNotConnected, detail, now, nil
			return s.saveCall(ctx, t)
		}
		if where == minionHere && why == bindNews && policy == models.PolicyOnDemand && !s.workWaits(ctx, t) {
			// News with nothing to send (a task ended, core's own connect
			// or disconnect kept): on demand opens for work only. A switch
			// to it opens it whatever waits.
			return nil
		}
	}
	b, err := s.builds.FindByBuildID(ctx, t.BuildID)
	if repository.IsNotFound(err) || (err == nil && buildConn(b, t) == nil) {
		return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "the build or its connection is gone"))
	}
	if err != nil {
		return fmt.Errorf("load the build: %w", err)
	}
	if s.link == nil {
		return s.saveCall(ctx, s.openFailed(t, models.TargetNoChannel, "core keeps no control links"))
	}
	kind, err := s.kindOf(ctx, t)
	if err != nil {
		return err
	}
	if err := s.saveCall(ctx, s.stateOnly(t, models.TargetConnecting, "")); err != nil {
		return err
	}
	// The minion over the connection is the target's: of its build, or the
	// session it is linked to.
	o := &opening{target: t.TargetID, build: t.BuildID, operation: t.OperationID, policy: policy, place: t.Conn,
		failures: t.Failures, downSince: t.UnreachableSince, arrived: make(chan struct{})}
	if t.SessionID != nil {
		o.session = *t.SessionID
	}
	var key openKey
	sent := func(instance string, request uint64) {
		key = openKey{instance, request}
		s.persist.mu.Lock()
		s.persist.opens[key] = o
		s.persist.mu.Unlock()
	}
	defer func() { s.dropOpen(key) }()
	// The minion speaks first, under its target's open session's id, else
	// its build's: one that keeps no ids of its own (a channel acting as
	// it) takes them from the open.
	under := b.BuildID
	var session *models.MinionSession
	if t.SessionID != nil {
		if found, err := s.sessions.FindBySessionID(ctx, *t.SessionID); err == nil && !found.Closed(s.now()) {
			under, session = found.SessionID, found
		}
	}
	// A channel that acts as the minion seals and opens the connection's
	// frames itself: the open gives it under's keys.
	var keys *protocol.LinkKeys
	if kind.agentless {
		from := &sender{session: session}
		switch {
		case session != nil:
			from.crypto = session.Crypto
		case b.Crypto != nil:
			from.build, from.crypto = b, *b.Crypto
		}
		if err := from.readKeys(); err != nil {
			return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "core does not implement the build's crypto"))
		}
		keys = from.linkKeys()
	}
	values := callValues(buildConn(b, t), t)
	creds, err := s.credentialsOf(ctx, t, kind, values)
	if why := refusedCredentials(err); why != "" {
		return s.saveCall(ctx, s.answered(t, models.TargetRefused, why))
	}
	if err != nil {
		return err
	}
	t.Calls++
	a, err := s.link.Open(ctx, t.ModuleName, values, creds, under, keys, kind.open, sent)
	if err == nil {
		s.noteLogins(ctx, t, creds, a.Logins)
	}
	switch {
	case errors.Is(err, channellink.ErrNoLink):
		return s.saveCall(ctx, s.openFailed(t, models.TargetNoChannel, fmt.Sprintf("no %s channel has its control link open", t.ModuleName)))
	case errors.Is(err, channellink.ErrLinkDropped):
		return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable, "the channel's control link dropped during the open"))
	case err != nil:
		return err
	case a.Failure != nil && refusal(a.Failure.Kind):
		// The target refused the channel (a login, a host key): opening it
		// again changes nothing until it is edited.
		return s.saveCall(ctx, s.answered(t, models.TargetRefused,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	case a.Failure != nil:
		return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	}
	// The socket comes before the answer and takes its open, but becomes
	// the target's only after its upgrade, which the answer can outrun:
	// wait for it.
	c := s.awaitSocket(ctx, key)
	if c == nil {
		// The channel said it opened, and no socket came.
		return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable, "the connection closed as soon as it opened"))
	}
	return s.firstWord(ctx, t, c, kind.open)
}

// firstWord waits, within at most, for the first word of the target's
// minion over c, its connection just opened, and stores what came of the
// open: connected once the minion spoke; not the minion when another
// minion spoke, or nothing did; unreachable when the connection closed
// first.
func (s *Service) firstWord(ctx context.Context, t *models.MinionTarget, c *Conn, within time.Duration) error {
	timer := time.NewTimer(within)
	defer timer.Stop()
	silent := false
	select {
	case <-c.heard:
	case <-c.gone:
	case <-timer.C:
		silent = true
	case <-ctx.Done():
		silent = true
	}
	now := s.now()
	if !c.spoke().IsZero() {
		t.State, t.StateDetail, t.StateAt = models.TargetConnected, "", now
		t.Failures, t.UnreachableSince = 0, nil
		t.LastCallAt, t.LastOKAt, t.NextCallAt = &now, &now, nil
		s.logger.Info("targets: a connection opened, its minion spoke", zap.String("target_id", t.TargetID.String()),
			zap.String("policy", t.PolicyOf()))
		return s.saveCall(ctx, t)
	}
	why, refused := c.closeWhy()
	switch {
	case refused:
		// Another minion spoke over it (Exchange closed it).
		return s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, why))
	case silent:
		// Something took the connection, and said nothing: not the
		// minion, which speaks first.
		why = fmt.Sprintf("nothing spoke over the connection within %s", within)
		c.refuse(why)
		return s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, why))
	case why != "":
		// Core closed it (an operator's Disconnect).
		return s.saveCall(ctx, s.stateOnly(t, models.TargetNotConnected, why))
	}
	return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable, "the connection closed before the minion spoke"))
}

// openFailed is t after an open that reached nothing: as a failed call,
// but a manual target is never retried.
func (s *Service) openFailed(t *models.MinionTarget, state, detail string) *models.MinionTarget {
	t = s.failed(t, state, detail)
	if t.PolicyOf() == models.PolicyManual {
		t.NextCallAt = nil
	}
	return t
}

// persistentClosed is told a target's connection closed: the target is
// not connected (once its last one went), and opened again as its policy
// says. A connection whose minion never spoke is its open's to tell.
func (s *Service) persistentClosed(ctx context.Context, c *Conn) {
	s.persist.mu.Lock()
	left := s.persist.conns[c.target][:0]
	for _, o := range s.persist.conns[c.target] {
		if o != c {
			left = append(left, o)
		}
	}
	if len(left) == 0 {
		delete(s.persist.conns, c.target)
	} else {
		s.persist.conns[c.target] = left
	}
	s.persist.mu.Unlock()
	spoke := c.spoke()
	if spoke.IsZero() || len(left) > 0 || s.targets == nil {
		return
	}
	// As the target's calls and opens: under its lock, so after the open
	// that brought c stored it connected.
	defer s.binds.lock(c.target)()
	t, err := s.targets.FindByTargetID(ctx, c.target)
	if err != nil {
		return // removed with it
	}
	now := s.now()
	why, refused := c.closeWhy()
	switch {
	case refused:
		t = s.answered(t, models.TargetNotTheMinion, why)
	case why == "" && now.Sub(spoke) < steadyFor:
		// Dropped soon after it opened: as an open that failed, counted on
		// from the failures its open found, so its reopens back off (a
		// minion that hangs up at once, a channel that cannot keep it).
		last := t.LastCallAt
		t.Failures, t.UnreachableSince = c.failures, c.downSince
		t = s.failed(t, models.TargetUnreachable, fmt.Sprintf("the connection closed %s after its minion spoke (the minion, the channel or the network)",
			now.Sub(spoke).Round(time.Second)))
		t.LastCallAt = last
		if !s.reopens(ctx, t, c) {
			t.NextCallAt = nil
		}
	default:
		if why == "" {
			why = "closed by the minion, the channel or the network"
		}
		t.State, t.StateDetail, t.StateAt, t.NextCallAt = models.TargetNotConnected, why, now, nil
		if s.reopens(ctx, t, c) {
			next := now.Add(alwaysReopen)
			t.NextCallAt = &next
		}
	}
	if err := s.saveCall(ctx, t); err != nil {
		s.logger.Warn("targets: a connection's close", zap.String("target_id", t.TargetID.String()), zap.Error(err))
	}
	s.logger.Info("targets: a connection closed", zap.String("target_id", t.TargetID.String()), zap.String("state", t.State),
		zap.String("why", t.StateDetail))
}

// reopens reports whether t's connection c, closed, is opened again: one
// of policy always is; one on demand when work waits for it, or a task it
// carried runs (its minion may hold the result); a manual one never.
func (s *Service) reopens(ctx context.Context, t *models.MinionTarget, c *Conn) bool {
	switch t.PolicyOf() {
	case models.PolicyAlways:
		return true
	case models.PolicyOnDemand:
		return s.workWaits(ctx, t) || s.carriedRun(ctx, c)
	}
	return false
}

// Where a target's minion is, for an open core starts on its own.
const (
	// minionHere: on this connection, quiet where it is, or not known (no
	// session yet): the open goes as its policy says.
	minionHere = iota
	// minionSwitching: a switch to this connection is under way: the
	// minion listens for the dial.
	minionSwitching
	// minionElsewhere: on another of its connections, checking in there.
	minionElsewhere
)

// minionOn is where the target's session's minion is, and for elsewhere
// what the target says about it.
func (s *Service) minionOn(ctx context.Context, t *models.MinionTarget) (int, string) {
	if t.SessionID == nil {
		return minionHere, ""
	}
	session, err := s.sessions.FindBySessionID(ctx, *t.SessionID)
	if err != nil || session.Closed(s.now()) || session.Conn == 0 || session.Conn == t.Conn {
		return minionHere, ""
	}
	if s.tasks != nil {
		if open, err := s.tasks.OpenWithAction(ctx, session.SessionID, protocol.ActionSwitchConnection); err == nil {
			for _, task := range open {
				if n, ok := protocol.AsInt64(task.Args["conn"]); ok && int(n) == t.Conn {
					return minionSwitching, ""
				}
			}
		}
	}
	if session.LateAt != nil && !s.now().Before(*session.LateAt) {
		// Late where it is: this connection may reach it.
		return minionHere, ""
	}
	return minionElsewhere, fmt.Sprintf("the minion uses connection %d: a switch to this one opens it", session.Conn)
}

// workWaits reports whether the target's session has something to send:
// tasks not sent, or sent and not acknowledged, or a cancel.
func (s *Service) workWaits(ctx context.Context, t *models.MinionTarget) bool {
	if t.SessionID == nil || s.tasks == nil {
		return false
	}
	if waiting, err := s.tasks.ForDelivery(ctx, *t.SessionID, 1); err == nil && len(waiting) > 0 {
		return true
	}
	n, err := s.tasks.CountCancelling(ctx, *t.SessionID)
	return err == nil && n > 0
}

// tasksRun reports whether the target's session has tasks its minion has
// and has not ended.
func (s *Service) tasksRun(ctx context.Context, t *models.MinionTarget) bool {
	if t.SessionID == nil || s.tasks == nil {
		return false
	}
	n, err := s.tasks.CountRunning(ctx, *t.SessionID)
	return err == nil && n > 0
}

// linkTarget links c's target to session, the one its minion speaks under
// over c now: from its stage, its claim on, or a check-in.
func (s *Service) linkTarget(ctx context.Context, c *Conn, session *models.MinionSession) {
	if s.targets == nil {
		return
	}
	// As the target's calls and opens save it: under its lock.
	defer s.binds.lock(c.target)()
	t, err := s.targets.FindByTargetID(ctx, c.target)
	if repository.IsNotFound(err) {
		c.closeBecause("its target was removed")
		return
	}
	if err != nil || (t.SessionID != nil && *t.SessionID == session.SessionID) {
		return
	}
	sid := session.SessionID
	t.SessionID = &sid
	if err := s.saveCall(ctx, t); err != nil {
		s.logger.Warn("targets: link a session", zap.String("target_id", c.target.String()), zap.Error(err))
		return
	}
	s.logger.Info("targets: a connection's minion is a session", zap.String("target_id", c.target.String()),
		zap.Int64("number", session.Number), zap.String("session_id", sid.String()))
}

// TargetConnected reports whether the bind-persistent target has a
// connection open now that its minion spoke over: what its stored state
// may be behind on.
func (s *Service) TargetConnected(target uuid.UUID) bool {
	for _, c := range s.targetConns(target) {
		if !c.spoke().IsZero() {
			return true
		}
	}
	return false
}

// resetPersistent, as core starts, says the bind-persistent targets left
// connected or connecting are not: their connections went with the process
// that held them (core keeps them in memory, as one instance). Those of
// policy always are opened again, and on demand ones whose session has
// work waiting or tasks running (their minions may hold the results).
func (s *Service) resetPersistent(ctx context.Context) {
	stale, err := s.targets.FindByKindAndStates(ctx, protocol.KindBindPersistent,
		[]string{models.TargetConnected, models.TargetConnecting})
	if err != nil {
		s.logger.Warn("targets: the connections core held before it started", zap.Error(err))
		return
	}
	now := s.now()
	for i := range stale {
		t := &stale[i]
		if len(s.targetConns(t.TargetID)) > 0 {
			continue // opened since
		}
		t.State, t.StateDetail, t.StateAt, t.NextCallAt = models.TargetNotConnected, "core restarted: its connection closed", now, nil
		if p := t.PolicyOf(); p == models.PolicyAlways || (p == models.PolicyOnDemand && (s.workWaits(ctx, t) || s.tasksRun(ctx, t))) {
			t.NextCallAt = &now
		}
		if err := s.saveCall(ctx, t); err != nil {
			s.logger.Warn("targets: reset a connection core held", zap.String("target_id", t.TargetID.String()), zap.Error(err))
		}
	}
	if len(stale) > 0 {
		s.logger.Info("targets: connections core held before it started are not connected", zap.Int("targets", len(stale)))
	}
}

// closeIdle closes the bind-persistent connections idle past their
// policy's time, but those a task they carried still runs on: its minion
// has more to say over it.
func (s *Service) closeIdle(ctx context.Context) {
	now := s.now()
	s.persist.mu.Lock()
	var idle []*Conn
	for _, cs := range s.persist.conns {
		for _, c := range cs {
			if limit := models.IdleClose(c.policy); limit > 0 && now.Sub(c.lastActive()) > limit {
				idle = append(idle, c)
			}
		}
	}
	s.persist.mu.Unlock()
	for _, c := range idle {
		if c.spoke().IsZero() || s.carriedRun(ctx, c) {
			continue // its open waits for its minion's first word; a task it carried runs
		}
		c.closeBecause(fmt.Sprintf("idle for %s", models.IdleClose(c.policy)))
	}
}
