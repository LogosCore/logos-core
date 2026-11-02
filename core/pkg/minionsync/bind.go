package minionsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// bind-call minions (docs: contracts/channel-core-connections/, bind-call
// minions): a page on a web server only answers, so core speaks first. It
// asks a channel instance, over the instance's control link, to call the
// page's target with a sealed message, and opens what came back.
//
//   - The first call is a rehello under the build id, sealed with the
//     build's keys: the page's hello opens a session with those keys (the
//     page skips staging), and the target is linked to it.
//   - After, calls go under the session id: when there is something to send
//     (a task, a cancel; Notify's news, coalesced over a second), on the
//     session's schedule when its hello said one, and on Check now. The
//     page's answer is taken as any message under the session (underSession),
//     and what core has to say back goes in another call at once, while
//     there is any.
//   - A call that reaches nothing leaves the session open and the target
//     unreachable, retried after 1 minute, 5, 30, then hourly, for a day.

// Linker reaches channel instances over their control links. Satisfied by
// *channellink.Hub.
type Linker interface {
	Call(ctx context.Context, module string, c channellink.Call) (protocol.LinkFrame, error)
}

const (
	// bindCoalesce is how long news for a bind session waits, so what is
	// queued together goes in one call.
	bindCoalesce = time.Second
	// bindMaxRounds bounds the calls one exchange makes back to back.
	bindMaxRounds = 16
	// bindTick is how often the scheduler looks for calls due.
	bindTick = 5 * time.Second
	// bindDefaultAllowance is how many tasks a call core starts brings when
	// the page never said (docs: one task a call).
	bindDefaultAllowance = 1
)

// Why a call is made.
type bindTrigger int

const (
	// bindNews: something changed for the session (a task queued, a
	// cancel): the call goes only when there is something new to send.
	bindNews bindTrigger = iota
	// bindScheduled: a retry, or the session's schedule.
	bindScheduled
	// bindCheck: an operator's Check now, or a target just added.
	bindCheck
)

// binds are the targets being called, and the news waiting.
type binds struct {
	mu      sync.Mutex
	locks   map[uuid.UUID]*sync.Mutex
	waiting map[uuid.UUID]bool
}

func newBinds() *binds {
	return &binds{locks: map[uuid.UUID]*sync.Mutex{}, waiting: map[uuid.UUID]bool{}}
}

// lock serializes a target's calls: one at a time.
func (b *binds) lock(target uuid.UUID) func() {
	b.mu.Lock()
	m, ok := b.locks[target]
	if !ok {
		m = &sync.Mutex{}
		b.locks[target] = m
	}
	b.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// TargetChecker says why a target's values no longer fit its channel's
// target fields (none when they fit). Satisfied by *miniontargets.Service.
type TargetChecker interface {
	Problems(ctx context.Context, t *models.MinionTarget) (map[string]string, error)
}

// WithTargetChecker has targets checked before each call: one whose values
// no longer fit is not called, and shows needs editing. Without it every
// target is called.
func (s *Service) WithTargetChecker(c TargetChecker, text func(map[string]string) string) *Service {
	s.targetChecker, s.problemsText = c, text
	return s
}

// WithTargets gives the service the targets of bind minions and the way to
// reach them. Without it, no target is called.
func (s *Service) WithTargets(targets repository.IMinionTargetRepository, link Linker) *Service {
	s.targets, s.link = targets, link
	return s
}

// CheckTarget calls a target now (Check now, or a target just added) and
// returns it as the call left it.
func (s *Service) CheckTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error) {
	if s.targets == nil {
		return nil, errors.New("targets are not served")
	}
	s.callTarget(ctx, targetID, bindCheck)
	return s.targets.FindByTargetID(ctx, targetID)
}

// RunTargets makes the calls due (retries, schedules) until ctx ends.
func (s *Service) RunTargets(ctx context.Context) {
	if s.targets == nil {
		return
	}
	t := time.NewTicker(bindTick)
	defer t.Stop()
	for {
		for {
			due, err := s.targets.ClaimDue(ctx, s.now())
			if err != nil {
				if !repository.IsNotFound(err) && ctx.Err() == nil {
					s.logger.Warn("targets: the calls due", zap.Error(err))
				}
				break
			}
			go s.callTarget(context.WithoutCancel(ctx), due.TargetID, bindScheduled)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// notifyBind calls the target of a bind session after bindCoalesce, once
// for what comes meanwhile; nothing for a session no target opened.
func (s *Service) notifyBind(session uuid.UUID) {
	if s.targets == nil {
		return
	}
	t, err := s.targets.FindBySessionID(context.Background(), session)
	if err != nil {
		return
	}
	s.binds.mu.Lock()
	if s.binds.waiting[t.TargetID] {
		s.binds.mu.Unlock()
		return
	}
	s.binds.waiting[t.TargetID] = true
	s.binds.mu.Unlock()
	time.AfterFunc(bindCoalesce, func() {
		s.binds.mu.Lock()
		delete(s.binds.waiting, t.TargetID)
		s.binds.mu.Unlock()
		s.callTarget(context.Background(), t.TargetID, bindNews)
	})
}

// callTarget makes a target's call: the first one, or one under its
// session.
func (s *Service) callTarget(ctx context.Context, targetID uuid.UUID, why bindTrigger) {
	defer s.binds.lock(targetID)()
	t, err := s.targets.FindByTargetID(ctx, targetID)
	if err != nil {
		if !repository.IsNotFound(err) {
			s.logger.Warn("targets: load a target", zap.String("target_id", targetID.String()), zap.Error(err))
		}
		return
	}
	if s.targetChecker != nil {
		problems, err := s.targetChecker.Problems(ctx, t)
		if err != nil {
			s.logger.Warn("targets: check a target's values", zap.String("target_id", targetID.String()), zap.Error(err))
			return
		}
		if len(problems) > 0 {
			// Not called with values the channel no longer reads; an edit
			// that fits calls it again.
			if t.State != models.TargetNeedsEditing {
				if err := s.saveCall(ctx, s.stateOnly(t, models.TargetNeedsEditing, s.problemsText(problems))); err != nil {
					s.logger.Error("targets: a call", zap.String("target_id", targetID.String()), zap.Error(err))
				}
			}
			return
		}
	}
	var session *models.MinionSession
	if t.SessionID != nil {
		session, err = s.sessions.FindBySessionID(ctx, *t.SessionID)
		switch {
		case repository.IsNotFound(err):
			session = nil
		case err != nil:
			s.logger.Warn("targets: load the session", zap.String("target_id", targetID.String()), zap.Error(err))
			return
		}
		if session != nil && session.Closed(s.now()) {
			// Its page is asked for a hello again, which opens another.
			session, t.SessionID = nil, nil
		}
	}
	if session == nil {
		if why == bindNews {
			return
		}
		err = s.firstCall(ctx, t)
	} else {
		err = s.sessionCalls(ctx, t, session, why)
	}
	if err != nil {
		s.logger.Error("targets: a call", zap.String("target_id", targetID.String()), zap.Error(err))
	}
}

// buildConn is the build connection a target is of; nil when the build no
// longer has it.
func buildConn(b *models.Build, t *models.MinionTarget) *protocol.BuildConnection {
	if t.Conn < 1 || t.Conn > len(b.Connections.Items) {
		return nil
	}
	return &b.Connections.Items[t.Conn-1]
}

// callValues are a call's values: the build connection's built-in ones and
// the target's, in one map.
func callValues(conn *protocol.BuildConnection, t *models.MinionTarget) map[string]any {
	v := maps.Clone(conn.Endpoint.Params)
	if v == nil {
		v = map[string]any{}
	}
	maps.Copy(v, t.Values)
	return v
}

// firstCall asks the target's page for a hello, under the build id with the
// build's keys: one that answers opens the session.
func (s *Service) firstCall(ctx context.Context, t *models.MinionTarget) error {
	b, err := s.builds.FindByBuildID(ctx, t.BuildID)
	if repository.IsNotFound(err) || (err == nil && (b.Crypto == nil || buildConn(b, t) == nil)) {
		return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "the build or its connection is gone"))
	}
	if err != nil {
		return fmt.Errorf("load the build: %w", err)
	}
	from := &sender{build: b, crypto: *b.Crypto}
	if err := from.readKeys(); err != nil {
		return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "core does not implement the build's crypto"))
	}
	rehello, err := protocol.MarshalCBOR(protocol.RehelloItem{T: protocol.ItemRehello})
	if err != nil {
		return err
	}
	msg, done, err := s.exchange(ctx, t, buildConn(b, t), from.keys, b.BuildID, []cbor.RawMessage{rehello})
	if err != nil || done {
		return err
	}
	var hello cbor.RawMessage
	for _, raw := range msg.Items {
		switch protocol.ItemType(raw) {
		case protocol.ItemHello:
			hello = raw
		case protocol.ItemErr:
			var e protocol.ErrItem
			_ = protocol.UnmarshalCBOR(raw, &e)
			return s.saveCall(ctx, s.answered(t, models.TargetRefused, fmt.Sprintf("%s: %s", protocol.ErrCodeName(e.Code), e.Msg)))
		}
	}
	if hello == nil {
		return s.saveCall(ctx, s.answered(t, models.TargetRefused, "the page answered without a hello"))
	}
	items, err := s.openSession(ctx, from, "", hello)
	if err != nil {
		return err
	}
	var opened protocol.HelloResponseItem
	if len(items) != 1 || protocol.ItemType(items[0]) != protocol.ItemHelloResponse || protocol.UnmarshalCBOR(items[0], &opened) != nil {
		// openSession's err item: the hello does not read.
		var e protocol.ErrItem
		if len(items) == 1 {
			_ = protocol.UnmarshalCBOR(items[0], &e)
		}
		return s.saveCall(ctx, s.answered(t, models.TargetRefused, "its hello: "+e.Msg))
	}
	sid, err := uuid.FromBytes(opened.SID)
	if err != nil {
		return err
	}
	session, err := s.sessions.FindBySessionID(ctx, sid)
	if err != nil {
		return fmt.Errorf("load the session its hello opened: %w", err)
	}
	t.SessionID = &sid
	s.allowanceOf(t, msg)
	s.logger.Info("targets: a page's hello opened a session", zap.String("target_id", t.TargetID.String()),
		zap.Int64("number", session.Number), zap.String("session_id", sid.String()))
	if err := s.saveCall(ctx, s.reached(t, session)); err != nil {
		return err
	}
	// What waits already goes at once.
	return s.sessionCalls(ctx, t, session, bindNews)
}

// sessionCalls calls the page under its session: what core has for it, and
// again at once with what core has to say back, while there is any.
func (s *Service) sessionCalls(ctx context.Context, t *models.MinionTarget, session *models.MinionSession, why bindTrigger) error {
	b, err := s.builds.FindByBuildID(ctx, t.BuildID)
	if repository.IsNotFound(err) || (err == nil && buildConn(b, t) == nil) {
		return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "the build or its connection is gone"))
	}
	if err != nil {
		return fmt.Errorf("load the build: %w", err)
	}
	from := &sender{session: session, crypto: session.Crypto}
	if err := from.readKeys(); err != nil {
		return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "core does not implement the session's crypto"))
	}
	items, fresh, err := s.bindItems(ctx, t, session)
	if err != nil {
		return err
	}
	if why == bindNews && !fresh {
		return nil
	}
	for round := 0; ; round++ {
		msg, done, err := s.exchange(ctx, t, buildConn(b, t), from.keys, session.SessionID, items)
		if err != nil || done {
			return err
		}
		if items, err = s.underSession(ctx, session, "", *msg, nil); err != nil {
			return err
		}
		s.allowanceOf(t, msg)
		if len(items) == 0 || round+1 >= bindMaxRounds {
			if round+1 >= bindMaxRounds && len(items) > 0 {
				s.logger.Info("targets: calls back to back stopped, the rest waits", zap.String("target_id", t.TargetID.String()))
			}
			break
		}
	}
	// The session as the answers left it: its schedule.
	if fresh, err := s.sessions.FindBySessionID(ctx, session.SessionID); err == nil {
		session = fresh
	}
	return s.saveCall(ctx, s.reached(t, session))
}

// bindItems are what a call core starts carries: the session's cancels, its
// tasks within the allowance (those sent and not acknowledged first), and
// its streams' acks. fresh says whether any of it is news: a cancel, or a
// task never sent.
func (s *Service) bindItems(ctx context.Context, t *models.MinionTarget, session *models.MinionSession) ([]cbor.RawMessage, bool, error) {
	if s.tasks == nil {
		return nil, false, nil
	}
	items, err := s.cancels(ctx, session)
	if err != nil {
		return nil, false, err
	}
	fresh := len(items) > 0
	limit := bindDefaultAllowance
	if t.Allowance != nil {
		limit = int(min(*t.Allowance, uint64(models.MaxMinionTasksPerReply)))
	}
	if limit > 0 {
		waiting, err := s.tasks.ForDelivery(ctx, session.SessionID, limit)
		if err != nil {
			return nil, false, fmt.Errorf("tasks to deliver: %w", err)
		}
		for _, w := range waiting {
			fresh = fresh || w.State == models.MinionTaskQueued
		}
		tasks, err := s.send(ctx, waiting)
		if err != nil {
			return nil, false, err
		}
		items = append(items, tasks...)
	}
	acks, err := s.streamAcks(ctx, session, nil)
	if err != nil {
		return nil, false, err
	}
	return append(items, acks...), fresh, nil
}

// allowanceOf takes the get_tasks of the page's answer as the target's
// allowance: how many tasks a call core starts may bring.
func (s *Service) allowanceOf(t *models.MinionTarget, msg *protocol.MinionMessage) {
	for _, raw := range msg.Items {
		if protocol.ItemType(raw) != protocol.ItemGetTasks {
			continue
		}
		if get, err := readGetTasks(raw); err == nil {
			if get.Max == nil {
				max := uint64(models.MaxMinionTasksPerReply)
				t.Allowance = &max
			} else {
				m := *get.Max
				t.Allowance = &m
			}
		}
	}
}

// exchange makes one call: items sealed under id with keys, to the target
// through its channel. It returns the page's answer, opened; or done, when
// the call reached nothing or not the page, which it recorded.
func (s *Service) exchange(ctx context.Context, t *models.MinionTarget, conn *protocol.BuildConnection,
	keys protocol.AES256CBCHMACSHA256Material, id uuid.UUID, items []cbor.RawMessage) (*protocol.MinionMessage, bool, error) {
	msgID := protocol.NewMessageID()
	plain, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV3, ID: msgID, Items: items})
	if err != nil {
		return nil, false, err
	}
	sealed, err := protocol.SealFrame(keys, id, protocol.FrameCoreToMinion, plain)
	if err != nil {
		return nil, false, err
	}
	if s.link == nil {
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetNoChannel, "core keeps no control links"))
	}
	t.Calls++
	a, err := s.link.Call(ctx, t.ModuleName, channellink.Call{Values: callValues(conn, t), ID: id, Data: sealed})
	switch {
	case errors.Is(err, channellink.ErrNoLink):
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetNoChannel, fmt.Sprintf("no %s channel has its control link open", t.ModuleName)))
	case errors.Is(err, channellink.ErrLinkDropped):
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetUnreachable, "the channel's control link dropped during the call"))
	case err != nil:
		return nil, false, err
	case a.Failure != nil:
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetUnreachable,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	case a.ID == nil:
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, notTheMinion(a.Status, a.Excerpt)))
	}
	// The page answers under the id it was called under, sealed with the
	// same keys, and answers this message: anything else is not it.
	if !bytes.Equal(a.ID, id[:]) {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, "an answer under another id"))
	}
	opened, err := protocol.OpenFrame(keys, id, protocol.FrameMinionToCore, a.Data)
	if err != nil {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, notTheMinion(a.Status, nil)+": an answer that does not open"))
	}
	var msg protocol.MinionMessage
	if err := protocol.UnmarshalCBOR(opened, &msg); err != nil || protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)) != nil ||
		!bytes.Equal(msg.Re, msgID) {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, "an answer that is not one to this call"))
	}
	return &msg, false, nil
}

// notTheMinion says what answered instead of the page.
func notTheMinion(status int, excerpt []byte) string {
	out := "something else answered"
	if status != 0 {
		out = fmt.Sprintf("HTTP %d", status)
	}
	if len(excerpt) > 0 {
		e := string(bytes.ToValidUTF8(excerpt, []byte("?")))
		if len(e) > 120 {
			e = e[:120] + "…"
		}
		out += ": " + e
	}
	return out
}

// reached is t after a call that reached the page: connected, next called
// on the session's schedule if it has one.
func (s *Service) reached(t *models.MinionTarget, session *models.MinionSession) *models.MinionTarget {
	now := s.now()
	t.State, t.StateDetail, t.StateAt = models.TargetConnected, "", now
	t.Failures, t.UnreachableSince = 0, nil
	t.LastCallAt, t.LastOKAt, t.NextCallAt = &now, &now, nil
	if session != nil && session.Sleep != nil && session.Sleep.Interval > 0 {
		next := now.Add(scheduleWait(session.Sleep))
		t.NextCallAt = &next
	}
	return t
}

// scheduleWait is the wait to the next call by a schedule: its interval,
// spread by its jitter.
func scheduleWait(sl *models.MinionSessionSleep) time.Duration {
	d := time.Duration(sl.Interval) * time.Millisecond
	if sl.Jitter > 0 {
		spread := float64(d) * float64(sl.Jitter) / 100
		d += time.Duration((rand.Float64()*2 - 1) * spread)
	}
	return max(d, time.Second)
}

// failed is t after a call that reached nothing: unreachable since the
// first failure in a row, retried on its schedule for a day (when it takes
// retries), then stopped.
func (s *Service) failed(t *models.MinionTarget, state, detail string) *models.MinionTarget {
	now := s.now()
	if t.UnreachableSince == nil {
		t.UnreachableSince = &now
	}
	t.Failures++
	t.State, t.StateDetail, t.StateAt, t.LastCallAt, t.NextCallAt = state, detail, now, &now, nil
	switch {
	case !t.Retries:
	case now.Sub(*t.UnreachableSince) >= models.TargetRetryFor:
		t.State = models.TargetStopped
	default:
		next := now.Add(models.TargetRetryAfter(t.Failures))
		t.NextCallAt = &next
	}
	return t
}

// answered is t after a call something answered that is not the page's
// session going on (not the page, an err): not retried, Check now calls
// again.
func (s *Service) answered(t *models.MinionTarget, state, detail string) *models.MinionTarget {
	now := s.now()
	t.State, t.StateDetail, t.StateAt, t.LastCallAt, t.NextCallAt = state, detail, now, &now, nil
	return t
}

// stateOnly is t in state with no call made.
func (s *Service) stateOnly(t *models.MinionTarget, state, detail string) *models.MinionTarget {
	t.State, t.StateDetail, t.StateAt, t.NextCallAt = state, detail, s.now(), nil
	return t
}

// saveCall stores what a call left of t, and tells the target's watchers
// (and the session's: its connection's state shows there).
func (s *Service) saveCall(ctx context.Context, t *models.MinionTarget) error {
	t.UpdatedAt = s.now()
	if err := s.targets.SetCallState(ctx, t); err != nil {
		return fmt.Errorf("store the target's call: %w", err)
	}
	s.bus.Publish(eventbus.NewMinionTargetChangedEvent(eventbus.MinionTargetEventPayload{
		TargetID: t.TargetID.String(), OperationID: t.OperationID.String()}))
	if t.SessionID != nil {
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(eventbus.MinionSessionEventPayload{
			SessionID: t.SessionID.String(), OperationID: t.OperationID.String()}))
	}
	return nil
}
