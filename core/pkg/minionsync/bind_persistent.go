package minionsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
// pushes over it, and its first session is linked to the target.
//
//   - Who opens it is the target's policy: on-demand (core, when the
//     session has something to send, and once when the target is added),
//     manual (an operator's Connect), always (core, and again whenever it
//     drops).
//   - It closes idle (models.IdleClose: 10 minutes on-demand, an hour
//     manual), on Disconnect, or when the minion, the channel or the
//     network drops it: the target is then not connected.
//   - An open that fails leaves the target unreachable, retried as a
//     call's would be (but manual's).

// openDeadline is how long core waits for an open's answer.
const openDeadline = 30 * time.Second

// alwaysReopen is how soon a connection of policy always is opened again
// after it closed.
const alwaysReopen = 5 * time.Second

// openKey names an open as a connection's socket names it.
type openKey struct {
	instance string
	request  uint64
}

// opening is an open waiting for its socket: its target and policy.
type opening struct {
	target uuid.UUID
	policy string
}

// persistent are the opens waiting for their sockets, and the targets'
// open connections.
type persistent struct {
	mu    sync.Mutex
	opens map[openKey]opening
	conns map[uuid.UUID][]*Conn
}

func newPersistent() *persistent {
	return &persistent{opens: map[openKey]opening{}, conns: map[uuid.UUID][]*Conn{}}
}

// OpenedFor is the target and policy of the open a connection socket
// names (instance and request), taken: a second socket naming it is
// refused. ok false for an open core is not waiting for.
func (s *Service) OpenedFor(instance string, request uint64) (target uuid.UUID, policy string, ok bool) {
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	o, ok := s.persist.opens[openKey{instance, request}]
	delete(s.persist.opens, openKey{instance, request})
	return o.target, o.policy, ok
}

// ForTarget makes c a bind-persistent connection of target (policy as it
// was opened) and keeps it with the target's: its controller calls it once
// the socket is up, before any frame.
func (s *Service) ForTarget(c *Conn, target uuid.UUID, policy string) {
	c.target, c.policy = target, policy
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	s.persist.conns[target] = append(s.persist.conns[target], c)
}

// targetConns are the target's open connections.
func (s *Service) targetConns(target uuid.UUID) []*Conn {
	s.persist.mu.Lock()
	defer s.persist.mu.Unlock()
	return append([]*Conn(nil), s.persist.conns[target]...)
}

// ConnectTarget opens a bind-persistent target's connection now (an
// operator's Connect, whatever its policy) and returns the target as the
// open left it.
func (s *Service) ConnectTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error) {
	if s.targets == nil {
		return nil, errors.New("targets are not served")
	}
	s.callTarget(ctx, targetID, bindConnect)
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

func (c *Conn) closeWhy() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.why
}

// openTarget opens a bind-persistent target's connection, when its policy
// takes why and none is open.
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
	if why == bindNews && policy == models.PolicyOnDemand && !s.workWaits(ctx, t) {
		// News with nothing to send (a task ended, core's own connect or
		// disconnect kept): on demand opens for work only.
		return nil
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
	if err := s.saveCall(ctx, s.stateOnly(t, models.TargetConnecting, "")); err != nil {
		return err
	}
	var key openKey
	sent := func(instance string, request uint64) {
		key = openKey{instance, request}
		s.persist.mu.Lock()
		s.persist.opens[key] = opening{target: t.TargetID, policy: policy}
		s.persist.mu.Unlock()
	}
	t.Calls++
	a, err := s.link.Open(ctx, t.ModuleName, callValues(buildConn(b, t), t), openDeadline, sent)
	// The socket comes before the answer, and takes its open: one left
	// came to nothing.
	s.persist.mu.Lock()
	delete(s.persist.opens, key)
	s.persist.mu.Unlock()
	switch {
	case errors.Is(err, channellink.ErrNoLink):
		return s.saveCall(ctx, s.openFailed(t, models.TargetNoChannel, fmt.Sprintf("no %s channel has its control link open", t.ModuleName)))
	case errors.Is(err, channellink.ErrLinkDropped):
		return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable, "the channel's control link dropped during the open"))
	case err != nil:
		return err
	case a.Failure != nil:
		return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	}
	if len(s.targetConns(t.TargetID)) == 0 {
		// The channel said it opened, and no socket came, or it closed
		// already (the minion hung up at once).
		return s.saveCall(ctx, s.openFailed(t, models.TargetUnreachable, "the connection closed as soon as it opened"))
	}
	now := s.now()
	t.State, t.StateDetail, t.StateAt = models.TargetConnected, "", now
	t.Failures, t.UnreachableSince = 0, nil
	t.LastCallAt, t.LastOKAt, t.NextCallAt = &now, &now, nil
	s.logger.Info("targets: a connection opened", zap.String("target_id", t.TargetID.String()), zap.String("policy", policy))
	return s.saveCall(ctx, t)
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
// not connected (once its last one went), and one of policy always is
// opened again soon.
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
	if len(left) > 0 || s.targets == nil {
		return
	}
	t, err := s.targets.FindByTargetID(ctx, c.target)
	if err != nil {
		return // removed with it
	}
	why := c.closeWhy()
	if why == "" {
		why = "closed by the minion, the channel or the network"
	}
	now := s.now()
	t.State, t.StateDetail, t.StateAt, t.NextCallAt = models.TargetNotConnected, why, now, nil
	switch t.PolicyOf() {
	case models.PolicyAlways:
		next := now.Add(alwaysReopen)
		t.NextCallAt = &next
	case models.PolicyOnDemand:
		// Dropped with work waiting (tasks not sent, or sent and not
		// acknowledged): opened again for it.
		if s.workWaits(ctx, t) {
			next := now.Add(alwaysReopen)
			t.NextCallAt = &next
		}
	}
	if err := s.saveCall(ctx, t); err != nil {
		s.logger.Warn("targets: a connection's close", zap.String("target_id", t.TargetID.String()), zap.Error(err))
	}
	s.logger.Info("targets: a connection closed", zap.String("target_id", t.TargetID.String()), zap.String("why", why))
}

// workWaits reports whether the target's session has tasks to send.
func (s *Service) workWaits(ctx context.Context, t *models.MinionTarget) bool {
	if t.SessionID == nil || s.tasks == nil {
		return false
	}
	waiting, err := s.tasks.ForDelivery(ctx, *t.SessionID, 1)
	return err == nil && len(waiting) > 0
}

// linkTarget links a target to the session its connection's first message
// came under: the minion's process, from its stage or its claim on.
func (s *Service) linkTarget(ctx context.Context, target uuid.UUID, session *models.MinionSession) {
	if s.targets == nil {
		return
	}
	t, err := s.targets.FindByTargetID(ctx, target)
	if err != nil || (t.SessionID != nil && *t.SessionID == session.SessionID) {
		return
	}
	sid := session.SessionID
	t.SessionID = &sid
	if err := s.saveCall(ctx, t); err != nil {
		s.logger.Warn("targets: link a session", zap.String("target_id", target.String()), zap.Error(err))
		return
	}
	s.logger.Info("targets: a connection's minion is a session", zap.String("target_id", target.String()),
		zap.Int64("number", session.Number), zap.String("session_id", sid.String()))
}

// resetPersistent, as core starts, says the bind-persistent targets left
// connected or connecting are not: their connections went with the process
// that held them (core keeps them in memory, as one instance). Those of
// policy always, and on demand with work waiting, are opened again.
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
		if p := t.PolicyOf(); p == models.PolicyAlways || (p == models.PolicyOnDemand && s.workWaits(ctx, t)) {
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
// policy's time.
func (s *Service) closeIdle() {
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
		c.closeBecause(fmt.Sprintf("idle for %s", models.IdleClose(c.policy)))
	}
}
