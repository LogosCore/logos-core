package minionsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/netip"
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
//     (a task, a cancel, a stream that may go on: Nudge, coalesced), on the
//     session's schedule when its hello said one, and on Check now. A call
//     carries what the outbox has (outbox.go). The page's answer is taken
//     as any message under the session (underSession), and what core has
//     to say back goes in another call at once, while there is any and the
//     calls make progress: a file moves a chunk a call, for as many calls
//     as it takes. Two calls in a row that would carry the same items (a
//     page answering the same thing over and over) end the exchange.
//   - Every call says core's time (at): a page, which cannot remember
//     message ids, refuses a call too old (a replay) with err stale, and
//     the target says so.
//   - A call that reaches nothing leaves the session open and the target
//     unreachable, retried after 1 minute, 5, 30, then hourly, for a day.

// Linker reaches channel instances over their control links. Satisfied by
// *channellink.Hub.
type Linker interface {
	Call(ctx context.Context, module string, c channellink.Call) (protocol.LinkFrame, error)
	Open(ctx context.Context, module string, values map[string]any, deadline time.Duration,
		sent func(instance string, request uint64)) (protocol.LinkFrame, error)
}

const (
	// bindCoalesce is how long news for a bind session waits, so what is
	// queued together goes in one call.
	bindCoalesce = time.Second
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
	// bindWake: a stream of the session may go on (a downloader came or
	// made room, an uploader's bytes came): the call goes only when one of
	// its acks says go.
	bindWake
	// bindConnect: an operator's Connect (a bind-persistent target's).
	bindConnect
)

// bindWakeDelay coalesces a session's stream wakes: a downloader taking
// chunk after chunk is one call, as soon as it can be.
const bindWakeDelay = 100 * time.Millisecond

// binds are the targets being called, and the calls waiting.
type binds struct {
	mu      sync.Mutex
	locks   map[uuid.UUID]*sync.Mutex
	waiting map[waitKey]bool
}

func newBinds() *binds {
	return &binds{locks: map[uuid.UUID]*sync.Mutex{}, waiting: map[waitKey]bool{}}
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

// RunTargets makes the calls due (retries, schedules) until ctx ends. As
// core starts, it first says no session is connected: core held the
// connections, which went with the process before.
func (s *Service) RunTargets(ctx context.Context) {
	s.resetConnected(ctx)
	if s.targets == nil {
		return
	}
	s.resetPersistent(ctx)
	t := time.NewTicker(bindTick)
	defer t.Stop()
	for {
		s.closeIdle()
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

// callSoon calls a target after delay, once for what comes meanwhile (of
// one trigger).
func (s *Service) callSoon(target uuid.UUID, why bindTrigger, delay time.Duration) {
	key := waitKey{target, why}
	s.binds.mu.Lock()
	if s.binds.waiting[key] {
		s.binds.mu.Unlock()
		return
	}
	s.binds.waiting[key] = true
	s.binds.mu.Unlock()
	time.AfterFunc(delay, func() {
		s.binds.mu.Lock()
		delete(s.binds.waiting, key)
		s.binds.mu.Unlock()
		s.callTarget(context.Background(), target, why)
	})
}

// waitKey is a call waiting: its target and why.
type waitKey struct {
	target uuid.UUID
	why    bindTrigger
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
	if t.Kind == protocol.KindBindPersistent {
		// Its minion is reached by a connection kept open, not by calls.
		if err := s.openTarget(ctx, t, why); err != nil {
			s.logger.Error("targets: an open", zap.String("target_id", targetID.String()), zap.Error(err))
		}
		return
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
		if why == bindNews || why == bindWake {
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
	// A hello through the target is of its connection, when it says none
	// (a minion an agentless channel acts as knows no build).
	from := &sender{build: b, crypto: *b.Crypto, place: t.Conn}
	if err := from.readKeys(); err != nil {
		return s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "core does not implement the build's crypto"))
	}
	rehello, err := protocol.MarshalCBOR(protocol.RehelloItem{T: protocol.ItemRehello})
	if err != nil {
		return err
	}
	msg, peer, done, err := s.exchange(ctx, t, buildConn(b, t), from, b.BuildID, []cbor.RawMessage{rehello})
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
	items, err := s.openSession(ctx, from, peer, hello)
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
	// The page answered core's own call: core holds the session's id, so
	// nothing can lose it (as a hello_response can a minion's), and the
	// call is the session's first check-in. A page given nothing to do for
	// a day stays open.
	if err := s.sessions.CheckIn(ctx, sid, checkIn(s.now(), peer, nil, session.Sleep, session.ConnKind)); err != nil {
		return fmt.Errorf("check the page's session in: %w", err)
	}
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
	items, fresh, going, err := s.bindItems(ctx, t, session)
	if err != nil {
		return err
	}
	if (why == bindNews && !fresh) || (why == bindWake && !going) {
		return nil
	}
	// Calls go on while core has something to say back and it moves things
	// on; no count bounds them, as a file takes a call a chunk.
	var last []cbor.RawMessage
	for {
		msg, peer, done, err := s.exchange(ctx, t, buildConn(b, t), from, session.SessionID, items)
		if err != nil || done {
			return err
		}
		last = items
		nameUnknownTasks(msg, items)
		if items, err = s.underSession(ctx, session, peer, *msg, nil, pathCall); err != nil {
			return err
		}
		s.allowanceOf(t, msg)
		if len(items) == 0 {
			break
		}
		if sameItems(items, last) {
			s.logger.Info("targets: the page answers the same thing again, the exchange ends", zap.String("target_id", t.TargetID.String()))
			break
		}
	}
	// The session as the answers left it: its schedule.
	if fresh, err := s.sessions.FindBySessionID(ctx, session.SessionID); err == nil {
		session = fresh
	}
	return s.saveCall(ctx, s.reached(t, session))
}

// sameItems reports whether two calls would carry the same items: nothing
// moved on between them.
func sameItems(a, b []cbor.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// bindItems are what a call core starts carries: the outbox's, with the
// session's tasks within the target's allowance, the resumes of tasks under
// way and its streams' acks. fresh says whether any of it is news (a cancel,
// a task never sent, a resume); going, whether a stream's ack lets it go on.
func (s *Service) bindItems(ctx context.Context, t *models.MinionTarget, session *models.MinionSession) ([]cbor.RawMessage, bool, bool, error) {
	limit := bindDefaultAllowance
	if t.Allowance != nil {
		limit = int(min(*t.Allowance, uint64(models.MaxMinionTasksPerReply)))
	}
	o, err := s.outbox(ctx, session, delivery{tasks: limit, resumes: true, streams: true, room: roomFor(session.MessageLimit(), nil)})
	if err != nil {
		return nil, false, false, err
	}
	return o.items, o.fresh, o.going, nil
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

// exchange makes one call: items under id, sealed as from's suite has
// them, to the target through its channel. It returns the page's answer,
// opened; or done, when the call reached nothing or not the page, which it
// recorded.
func (s *Service) exchange(ctx context.Context, t *models.MinionTarget, conn *protocol.BuildConnection,
	from *sender, id uuid.UUID, items []cbor.RawMessage) (*protocol.MinionMessage, string, bool, error) {
	msgID := protocol.NewMessageID()
	plain, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV3, ID: msgID, Items: items, At: unixNow(s.now())})
	if err != nil {
		return nil, "", false, err
	}
	sealed, err := from.seal(id, plain)
	if err != nil {
		return nil, "", false, err
	}
	if s.link == nil {
		return nil, "", true, s.saveCall(ctx, s.failed(t, models.TargetNoChannel, "core keeps no control links"))
	}
	t.Calls++
	a, err := s.link.Call(ctx, t.ModuleName, channellink.Call{Values: callValues(conn, t), ID: id, Data: sealed})
	switch {
	case errors.Is(err, channellink.ErrNoLink):
		return nil, "", true, s.saveCall(ctx, s.failed(t, models.TargetNoChannel, fmt.Sprintf("no %s channel has its control link open", t.ModuleName)))
	case errors.Is(err, channellink.ErrLinkDropped):
		return nil, "", true, s.saveCall(ctx, s.failed(t, models.TargetUnreachable, "the channel's control link dropped during the call"))
	case err != nil:
		return nil, "", false, err
	case a.Failure != nil && refusal(a.Failure.Kind):
		// The target refused the channel (a login, a host key): calling it
		// again changes nothing until it is edited.
		return nil, "", true, s.saveCall(ctx, s.answered(t, models.TargetRefused,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	case a.Failure != nil:
		return nil, "", true, s.saveCall(ctx, s.failed(t, models.TargetUnreachable,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	case a.ID == nil:
		return nil, "", true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, notTheMinion(a.Status, a.Excerpt)))
	}
	// The page answers under the id it was called under, sealed with the
	// same keys, and answers this message: anything else is not it.
	if !bytes.Equal(a.ID, id[:]) {
		return nil, "", true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, "an answer under another id"))
	}
	opened, err := from.open(id, a.Data)
	if err != nil {
		return nil, "", true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, notTheMinion(a.Status, nil)+": an answer that does not open"))
	}
	var msg protocol.MinionMessage
	if err := protocol.UnmarshalCBOR(opened, &msg); err != nil || protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)) != nil ||
		!bytes.Equal(msg.Re, msgID) {
		return nil, "", true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, "an answer that is not one to this call"))
	}
	// A page whose clock is far from core's refuses the call as stale, as
	// it would a replay: nothing goes until the clocks agree.
	for _, raw := range msg.Items {
		var e protocol.ErrItem
		if protocol.ItemType(raw) == protocol.ItemErr && protocol.UnmarshalCBOR(raw, &e) == nil && e.Code == protocol.ErrItemStale {
			return nil, "", true, s.saveCall(ctx, s.answered(t, models.TargetRefused,
				"the page refused the call as stale: its clock is far from core's ("+e.Msg+")"))
		}
	}
	// The address the channel reached the page at: its external one.
	return &msg, peerAddress(a.Peer), false, nil
}

// refusal reports whether a link failure is the target refusing the
// channel (an agentless channel's: the login, the host key), which no retry
// changes until the target is edited.
func refusal(kind int) bool {
	return kind == protocol.LinkFailLogin || kind == protocol.LinkFailHostKey
}

// peerAddress is a channel's peer as sessions keep it: an IP address,
// unmapped; "" for none or one that does not read.
func peerAddress(p string) string {
	a, err := netip.ParseAddr(p)
	if err != nil {
		return ""
	}
	return a.Unmap().WithZone("").String()
}

// nameUnknownTasks names the task of each unknown_task in msg that says
// only which of sent, the items core called with, it is about (a page from
// before the err's task): a cancel's. Core then ends the task.
func nameUnknownTasks(msg *protocol.MinionMessage, sent []cbor.RawMessage) {
	for i, raw := range msg.Items {
		if protocol.ItemType(raw) != protocol.ItemErr {
			continue
		}
		var e protocol.ErrItem
		if protocol.UnmarshalCBOR(raw, &e) != nil || e.Code != protocol.ErrItemUnknownTask || e.Task != nil ||
			e.Item == nil || *e.Item < 0 || *e.Item >= len(sent) || protocol.ItemType(sent[*e.Item]) != protocol.ItemCancel {
			continue
		}
		var c protocol.CancelItem
		if protocol.UnmarshalCBOR(sent[*e.Item], &c) != nil {
			continue
		}
		e.Task = c.Task
		if named, err := protocol.MarshalCBOR(e); err == nil {
			msg.Items[i] = named
		}
	}
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
