package minionsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/netip"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/channellink"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// bind-call minions (docs: contracts/channel-core-connections/, bind-call
// minions): a minion that only answers (a page on a web server, a server
// an agentless channel acts on), so core speaks first. It asks a channel
// instance, over the instance's control link, to call the minion's target
// with a sealed message, and opens what came back.
//
//   - The first call is a rehello under the build id, sealed with the
//     build's keys: the minion's hello opens a session with those keys (it
//     skips staging), and the target is linked to it. What else the answer
//     holds is taken under that session, as any later answer's.
//   - After, calls go under the session id: when there is something to send
//     (a task, a cancel, a stream that may go on: Nudge, coalesced), on the
//     session's schedule when its hello said one, and on Check now. A call
//     carries what the outbox has (outbox.go). The minion's answer is taken
//     as any message under the session (underSession), and what core has
//     to say back goes in another call at once, while there is any and the
//     calls make progress: a file moves a chunk a call, for as many calls
//     as it takes. Two calls in a row that would carry the same items (a
//     minion answering the same thing over and over) end the exchange.
//   - Every call says core's time (at): a minion that cannot remember
//     message ids refuses a call too old (a replay) with err stale, and the
//     target says so. Every call says its deadline too: what the channel
//     says its kind's calls may take (max_call), within core's cap.
//   - A target's calls are core's own work: they go to their end whatever
//     asked for them, and a call sent is always waited for.
//   - A call that reaches nothing leaves the session open and the target
//     unreachable, retried after 1 minute, 5, 30, then hourly, for a day. A
//     target refused (a login, a host key, an err) is called again only on
//     Check now or an edit; one core itself could not call (a lookup, a
//     store) is tried again in a minute.

// Linker reaches channel instances over their control links. Satisfied by
// *channellink.Hub.
type Linker interface {
	Call(ctx context.Context, module string, c channellink.Call) (protocol.LinkFrame, error)
	Open(ctx context.Context, module string, values map[string]any, creds map[string]protocol.Credential, under [16]byte,
		keys *protocol.LinkKeys, acting *protocol.LinkActing, deadline time.Duration, sent func(instance string, request uint64)) (protocol.LinkFrame, error)
}

// Credentials resolves the credentials a call's values name, and learns
// from how a channel's logins with them fared: one that logged in works.
// Satisfied by *credentialrefs.Resolver.
type Credentials interface {
	ResolveFields(ctx context.Context, operationID uuid.UUID, fields []protocol.FieldSpec, values map[string]any) (map[string]protocol.Credential, error)
	LoginWorked(ctx context.Context, operationID, credentialID uuid.UUID) error
}

// WithCredentials has calls and opens carry the credentials their values
// name. Without it a target that names one is refused: its tests.
func (s *Service) WithCredentials(c Credentials) *Service {
	s.creds = c
	return s
}

// ChannelSchemas are the channels' connection schemas, by module name: nil
// for a channel that declared none (channelendpoints.Service).
type ChannelSchemas interface {
	Schema(ctx context.Context, moduleName string) (*protocol.ConnectionSchema, error)
}

// WithChannelSchemas has core read the channels' connection schemas: a
// call or an open to a kind a channel acts as the minion on (agentless)
// carries the keys of its id. Without them no call carries keys.
func (s *Service) WithChannelSchemas(c ChannelSchemas) *Service {
	s.channels = c
	return s
}

// callKind is what t's channel says of t's kind that its calls and opens
// go by.
type callKind struct {
	// agentless: the channel acts as the minion, so calls and opens carry
	// their id's keys.
	agentless bool
	// delegatedOnly: no instance of the channel carries the kind, only
	// delegates do.
	delegatedOnly bool
	// deadline is how long a call may take (callDeadline), open how long
	// an open may (openWithin).
	deadline, open time.Duration
	// fields are the kind's endpoint fields and its targets' fields: what
	// a call's values are by, the credentials they name among them.
	fields []protocol.FieldSpec
}

// kindOf is what t's channel's connection schema says of t's kind.
func (s *Service) kindOf(ctx context.Context, t *models.MinionTarget) (callKind, error) {
	k := callKind{deadline: callDeadline(0), open: openWithin(0)}
	if s.channels == nil {
		return k, nil
	}
	cs, err := s.channels.Schema(ctx, t.ModuleName)
	if err != nil {
		return k, fmt.Errorf("load the %s channel's schema: %w", t.ModuleName, err)
	}
	if cs != nil {
		k.agentless, k.deadline, k.open = cs.IsAgentless(t.Kind), callDeadline(cs.CallLimit(t.Kind)), openWithin(cs.OpenLimit(t.Kind))
		k.delegatedOnly = cs.IsDelegatedOnly(t.Kind)
		k.fields = append(slices.Clone(cs.Kinds[t.Kind].Fields), cs.TargetFields(t.Kind)...)
	}
	return k, nil
}

// credentialsOf are the credentials a call's or an open's values name, as
// the channel gets them. A credential gone, or not of a type its field takes
// now (credentialrefs.Unusable), refuses the target until it is edited:
// retrying changes nothing. Any other error is core's own.
func (s *Service) credentialsOf(ctx context.Context, t *models.MinionTarget, kind callKind, values map[string]any) (map[string]protocol.Credential, error) {
	if len(credentialrefs.IDs(kind.fields, values)) == 0 {
		return nil, nil
	}
	if s.creds == nil {
		return nil, &credentialrefs.Unusable{Why: "core resolves no credentials here"}
	}
	return s.creds.ResolveFields(ctx, t.OperationID, kind.fields, values)
}

// refusedCredentials is the state detail of a target whose credentials
// cannot go to its channel; "" for an error that is core's own.
func refusedCredentials(err error) string {
	var u *credentialrefs.Unusable
	if errors.As(err, &u) {
		return "credentials: " + u.Why
	}
	return ""
}

// noteLogins records how the credentials a channel tried fared at t (only
// those it was given), and marks one that logged in VALID: it works. What a
// channel says of a credential it was not given is no news.
func (s *Service) noteLogins(ctx context.Context, t *models.MinionTarget, given map[string]protocol.Credential, logins []protocol.LinkLogin) {
	if len(logins) == 0 {
		return
	}
	now := s.now()
	noted := map[string]models.TargetLogin{}
	for _, l := range logins {
		c, ok := given[l.Credential]
		id, err := uuid.Parse(l.Credential)
		if !ok || err != nil {
			continue
		}
		noted[l.Credential] = models.TargetLogin{Accepted: l.Accepted, At: now}
		s.logger.Info("a target's login", zap.String("target_id", t.TargetID.String()), zap.String("credential", c.Name),
			zap.Bool("accepted", l.Accepted))
		if l.Accepted && s.creds != nil {
			if err := s.creds.LoginWorked(ctx, t.OperationID, id); err != nil {
				s.logger.Warn("mark a credential that logged in valid", zap.String("credential_id", l.Credential), zap.Error(err))
			}
		}
	}
	if len(noted) == 0 {
		return
	}
	if err := s.targets.NoteLogins(ctx, t.TargetID, noted); err != nil {
		s.logger.Warn("note a target's logins", zap.String("target_id", t.TargetID.String()), zap.Error(err))
		return
	}
	if t.Logins == nil {
		t.Logins = map[string]models.TargetLogin{}
	}
	maps.Copy(t.Logins, noted)
}

const (
	// bindCoalesce is how long news for a bind session waits, so what is
	// queued together goes in one call.
	bindCoalesce = time.Second
	// bindTick is how often the scheduler looks for calls due.
	bindTick = 5 * time.Second
	// bindDefaultAllowance is how many tasks a call core starts brings when
	// the minion never said (docs: one task a call).
	bindDefaultAllowance = 1
	// bindCallDeadline is how long a call may take when its kind says
	// nothing (ConnectionKind.MaxCall): what core waits for its answer, the
	// link adding its slack. Every call says its deadline, and the channel
	// keeps to the shorter of it and its own request timeout, so no call
	// goes on past core's wait: one that did would run its tasks for an
	// answer core no longer takes, and the next call would run them again.
	bindCallDeadline = 2 * time.Minute
	// bindMaxCallDeadline caps what a kind may ask for.
	bindMaxCallDeadline = 10 * time.Minute
	// internalRetry is how soon a target is called again after a call core
	// itself could not make or finish (a lookup, a store).
	internalRetry = time.Minute
)

// callDeadline is a call's deadline: what its kind asks for (limit; 0 for
// nothing), within core's cap, else bindCallDeadline.
func callDeadline(limit time.Duration) time.Duration {
	if limit <= 0 {
		return bindCallDeadline
	}
	return min(limit, bindMaxCallDeadline)
}

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
// returns it as the call left it. The call is core's own: a caller that
// goes first (an operator's request closed) gets ctx's error, and the call
// goes on.
func (s *Service) CheckTarget(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error) {
	if s.targets == nil {
		return nil, errors.New("targets are not served")
	}
	if err := await(ctx, func(c context.Context) { s.callTarget(c, targetID, bindCheck) }); err != nil {
		return nil, err
	}
	return s.targets.FindByTargetID(ctx, targetID)
}

// await runs work as core's own, its context free of ctx's end, and waits
// for it while ctx lasts: ctx's error when the caller goes first, the work
// going on.
func await(ctx context.Context, work func(context.Context)) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(context.WithoutCancel(ctx))
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
		s.closeIdle(ctx)
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
// session, or its connection's open. It is core's own work: ctx's end does
// not stop it, so an answer that comes is always taken.
func (s *Service) callTarget(ctx context.Context, targetID uuid.UUID, why bindTrigger) {
	ctx = context.WithoutCancel(ctx)
	defer s.binds.lock(targetID)()
	t, err := s.targets.FindByTargetID(ctx, targetID)
	if err != nil {
		if !repository.IsNotFound(err) {
			s.logger.Warn("targets: load a target", zap.String("target_id", targetID.String()), zap.Error(err))
		}
		return
	}
	if t.State == models.TargetRefused && why != bindCheck && why != bindConnect {
		// Refused (a login, a host key, an err): calling again changes
		// nothing until an operator edits it or asks, and where it logs in
		// each call would be one more failed login on the target.
		return
	}
	err = s.callOf(ctx, t, why)
	switch {
	case repository.IsNotFound(err):
		// Removed during its call: what its open brought closes with it.
		s.closeTargetConns(targetID)
	case err != nil:
		s.failedInCore(ctx, targetID, err)
	}
}

// callOf makes t's call, as callTarget; an error is core's own (a lookup, a
// store), the target's outcome being recorded on it.
func (s *Service) callOf(ctx context.Context, t *models.MinionTarget, why bindTrigger) error {
	if s.targetChecker != nil {
		problems, err := s.targetChecker.Problems(ctx, t)
		if err != nil {
			return fmt.Errorf("check the target's values: %w", err)
		}
		if len(problems) > 0 {
			// Not called with values the channel no longer reads; an edit
			// that fits calls it again.
			if t.State != models.TargetNeedsEditing {
				return s.saveCall(ctx, s.stateOnly(t, models.TargetNeedsEditing, s.problemsText(problems)))
			}
			return nil
		}
	}
	if t.Kind == protocol.KindBindPersistent {
		// Its minion is reached by a connection kept open, not by calls.
		return s.openTarget(ctx, t, why)
	}
	var session *models.MinionSession
	if t.SessionID != nil {
		var err error
		session, err = s.sessions.FindBySessionID(ctx, *t.SessionID)
		switch {
		case repository.IsNotFound(err):
			session = nil
		case err != nil:
			return fmt.Errorf("load the session: %w", err)
		}
		if session != nil && session.Closed(s.now()) {
			// Its minion is asked for a hello again, which opens another.
			session, t.SessionID = nil, nil
		}
	}
	if session == nil {
		if why == bindNews || why == bindWake {
			return nil
		}
		return s.firstCall(ctx, t)
	}
	return s.sessionCalls(ctx, t, session, why)
}

// failedInCore records a call core itself could not make or finish (a
// lookup, a store failed): the target as it was, why in its state's detail,
// and another call in a minute, so its schedule (a retry, its minion's own)
// is not lost with the claim that cleared it. A manual target's connection
// is an operator's to open: it is told, not called.
func (s *Service) failedInCore(ctx context.Context, targetID uuid.UUID, cause error) {
	s.logger.Error("targets: a call core could not make", zap.String("target_id", targetID.String()), zap.Error(cause))
	t, err := s.targets.FindByTargetID(ctx, targetID)
	if err != nil {
		return
	}
	t.StateDetail = clip("core could not make the call: "+cause.Error(), 400)
	if t.Kind != protocol.KindBindPersistent || t.PolicyOf() != models.PolicyManual {
		next := s.now().Add(internalRetry)
		if t.NextCallAt == nil || t.NextCallAt.After(next) {
			t.NextCallAt = &next
		}
	}
	if err := s.saveCall(ctx, t); err != nil {
		s.logger.Error("targets: store a call core could not make", zap.String("target_id", targetID.String()), zap.Error(err))
	}
}

// clip is s cut to at most n bytes, on a character's edge.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
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

// firstCall asks the target's minion for a hello, under the build id with
// the build's keys: one that answers opens the session, and what else its
// answer holds is taken under it.
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
	answer, done, err := s.exchange(ctx, t, buildConn(b, t), from, b.BuildID, []cbor.RawMessage{rehello})
	if err != nil || done {
		return err
	}
	msg, peer, actx := answer.msg, answer.peer, answer.ctx(ctx, t)
	// A hello opens the session; without one, an err says why the minion
	// refused.
	var hello cbor.RawMessage
	var refused *protocol.ErrItem
	for _, raw := range msg.Items {
		switch protocol.ItemType(raw) {
		case protocol.ItemHello:
			if hello == nil {
				hello = raw
			}
		case protocol.ItemErr:
			var e protocol.ErrItem
			if refused == nil && protocol.UnmarshalCBOR(raw, &e) == nil {
				refused = &e
			}
		}
	}
	switch {
	case hello == nil && refused != nil:
		return s.saveCall(ctx, s.answered(t, models.TargetRefused, fmt.Sprintf("%s: %s", protocol.ErrCodeName(refused.Code), refused.Msg)))
	case hello == nil:
		return s.saveCall(ctx, s.answered(t, models.TargetRefused, "the minion answered without a hello"))
	}
	items, err := s.openSession(actx, from, peer, hello)
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
	// The minion answered core's own call: core holds the session's id, so
	// nothing can lose it (as a hello_response can a minion's), and the call
	// is the session's first check-in, so a minion given nothing to do for a
	// day stays open. The rest of the answer is the session's, as any later
	// answer is: its feedback, a schedule, what it asks for.
	rest := *msg
	rest.Items = slices.DeleteFunc(slices.Clone(msg.Items), func(raw cbor.RawMessage) bool {
		return protocol.ItemType(raw) == protocol.ItemHello
	})
	back, err := s.underSession(actx, session, peer, rest, nil, pathCall)
	if err != nil {
		return fmt.Errorf("take the first answer under its session: %w", err)
	}
	s.logger.Info("targets: a minion's hello opened a session", zap.String("target_id", t.TargetID.String()),
		zap.Int64("number", session.Number), zap.String("session_id", sid.String()))
	if err := s.saveCall(ctx, s.reached(t, session)); err != nil {
		return err
	}
	if len(back) > 0 {
		// What core says back to it goes at once, as after any answer.
		conn, from, done, err := s.sessionSender(ctx, t, session)
		if err != nil || done {
			return err
		}
		return s.callWhileWorkRemains(ctx, t, session, conn, from, back)
	}
	// What waits already goes at once.
	return s.sessionCalls(ctx, t, session, bindNews)
}

// sessionCalls calls the minion under its session: what core has for it,
// and again at once with what core has to say back, while there is any.
func (s *Service) sessionCalls(ctx context.Context, t *models.MinionTarget, session *models.MinionSession, why bindTrigger) error {
	conn, from, done, err := s.sessionSender(ctx, t, session)
	if err != nil || done {
		return err
	}
	items, fresh, going, err := s.bindItems(ctx, t, session)
	if err != nil {
		return err
	}
	if (why == bindNews && !fresh) || (why == bindWake && !going) {
		return nil
	}
	return s.callWhileWorkRemains(ctx, t, session, conn, from, items)
}

// sessionSender is what calls under session go through and with: t's build
// connection and the session's keys; done when there are none, which it
// recorded on t.
func (s *Service) sessionSender(ctx context.Context, t *models.MinionTarget, session *models.MinionSession) (*protocol.BuildConnection, *sender, bool, error) {
	b, err := s.builds.FindByBuildID(ctx, t.BuildID)
	if repository.IsNotFound(err) || (err == nil && buildConn(b, t) == nil) {
		return nil, nil, true, s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "the build or its connection is gone"))
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("load the build: %w", err)
	}
	from := &sender{session: session, crypto: session.Crypto}
	if err := from.readKeys(); err != nil {
		return nil, nil, true, s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, "core does not implement the session's crypto"))
	}
	return buildConn(b, t), from, false, nil
}

// callWhileWorkRemains calls the minion under its session with items, and
// again at once with what core has to say back, while there is any and the
// calls move things on; no count bounds them, as a file takes a call a
// chunk.
func (s *Service) callWhileWorkRemains(ctx context.Context, t *models.MinionTarget, session *models.MinionSession,
	conn *protocol.BuildConnection, from *sender, items []cbor.RawMessage) error {
	var last []cbor.RawMessage
	for {
		answer, done, err := s.exchange(ctx, t, conn, from, session.SessionID, items)
		if err != nil || done {
			return err
		}
		msg := answer.msg
		last = items
		nameUnknownTasks(msg, items)
		if items, err = s.underSession(answer.ctx(ctx, t), session, answer.peer, *msg, nil, pathCall); err != nil {
			return err
		}
		s.allowanceOf(t, msg)
		if len(items) == 0 {
			break
		}
		if sameItems(items, last) {
			s.logger.Info("targets: the minion answers the same thing again, the exchange ends", zap.String("target_id", t.TargetID.String()))
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
	o, err := s.outbox(ctx, session, delivery{tasks: limit, resumes: true, streams: true, room: roomFor(planLimit(session), nil)})
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

// callAnswer is a call's answer: the minion's message, opened, the address
// the channel reached the minion at (its external one), and the delegates
// it came through (the target's, when it names one).
type callAnswer struct {
	msg  *protocol.MinionMessage
	peer string
	via  []models.MinionSessionVia
}

// ctx is ctx for what a's message says, which came a's way.
func (a *callAnswer) ctx(ctx context.Context, t *models.MinionTarget) context.Context {
	if len(a.via) == 0 {
		return ctx
	}
	return withVia(ctx, a.via, t.OperationID)
}

// exchange makes one call: items under id, sealed as from's suite has
// them, to the target through its channel. It returns the minion's answer;
// or done, when the call reached nothing or not the minion, which it
// recorded.
func (s *Service) exchange(ctx context.Context, t *models.MinionTarget, conn *protocol.BuildConnection,
	from *sender, id uuid.UUID, items []cbor.RawMessage) (*callAnswer, bool, error) {
	msgID := protocol.NewMessageID()
	sealed, _, err := s.writeMessage(ctx, from, id, protocol.CoreMessage{V: protocol.MinionMessageV3, ID: msgID, Items: items,
		At: unixNow(s.now())})
	if err != nil {
		return nil, false, err
	}
	kind, err := s.kindOf(ctx, t)
	if err != nil {
		return nil, false, err
	}
	if why := delegateMismatch(t, kind); why != "" {
		return nil, true, s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, why))
	}
	link := s.linkerFor(t, conn)
	if link == nil {
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetNoChannel, "core keeps no control links"))
	}
	values := callValues(conn, t)
	creds, err := s.credentialsOf(ctx, t, kind, values)
	if why := refusedCredentials(err); why != "" {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetRefused, why))
	}
	if err != nil {
		return nil, false, err
	}
	t.Calls++
	call := channellink.Call{Values: values, Credentials: creds, ID: id, Data: sealed, Deadline: kind.deadline}
	// A channel that acts as the minion opens the message and seals the
	// answer itself: it gets the id's keys with every call.
	if kind.agentless {
		call.Keys = from.linkKeys()
		call.Acting = s.actingOf(ctx, from.session)
	}
	// A call sent is waited for, whatever asked for it: an answer not taken
	// would leave the tasks it ran unacknowledged, and the next call would
	// run them again.
	a, err := link.Call(context.WithoutCancel(ctx), t.ModuleName, call)
	if err == nil {
		s.noteLogins(ctx, t, creds, a.Logins)
	}
	if done, err := s.delegateFailed(ctx, t, err); done {
		return nil, true, err
	}
	switch {
	case errors.Is(err, channellink.ErrNoLink):
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetNoChannel, fmt.Sprintf("no %s channel has its control link open", t.ModuleName)))
	case errors.Is(err, channellink.ErrLinkDropped):
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetUnreachable, "the channel's control link dropped during the call"))
	case err != nil:
		return nil, false, err
	case a.Failure != nil && refusal(a.Failure.Kind):
		// The target refused the channel (a login, a host key): calling it
		// again changes nothing until it is edited.
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetRefused,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	case a.Failure != nil:
		return nil, true, s.saveCall(ctx, s.failed(t, models.TargetUnreachable,
			fmt.Sprintf("%s: %s", protocol.LinkFailName(a.Failure.Kind), a.Failure.Msg)))
	case a.ID == nil:
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, notTheMinion(a.Status, a.Excerpt)))
	}
	// The minion answers under the id it was called under, sealed with the
	// same keys, and answers this message: anything else is not it.
	if !bytes.Equal(a.ID, id[:]) {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, "an answer under another id"))
	}
	opened, err := s.readMessage(ctx, from, id, a.Data)
	if err != nil {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, notTheMinion(a.Status, nil)+": an answer that does not open"))
	}
	var msg protocol.MinionMessage
	if err := protocol.UnmarshalCBOR(opened, &msg); err != nil || protocol.ValidateMessageHead(msg.V, msg.ID, msg.Re, len(msg.Items)) != nil ||
		!bytes.Equal(msg.Re, msgID) {
		return nil, true, s.saveCall(ctx, s.answered(t, models.TargetNotTheMinion, "an answer that is not one to this call"))
	}
	// Packed items in their places (packing.go).
	unpackItems(from, msg.Items)
	// A minion whose clock is far from core's refuses the call as stale, as
	// it would a replay: nothing goes until the clocks agree.
	for _, raw := range msg.Items {
		var e protocol.ErrItem
		if protocol.ItemType(raw) == protocol.ItemErr && protocol.UnmarshalCBOR(raw, &e) == nil && e.Code == protocol.ErrItemStale {
			return nil, true, s.saveCall(ctx, s.answered(t, models.TargetRefused,
				"the minion refused the call as stale: its clock is far from core's ("+e.Msg+")"))
		}
	}
	answer := &callAnswer{msg: &msg, peer: peerAddress(a.Peer)}
	if dl, ok := link.(*delegateLink); ok {
		answer.via = dl.via
	}
	return answer, false, nil
}

// delegateMismatch says why t cannot be called as it is: through a
// delegate while its kind is agentless (its channel is the minion, and
// keys never go to a minion), or through none while only delegates carry
// its kind; "" when it can.
func delegateMismatch(t *models.MinionTarget, kind callKind) string {
	switch {
	case t.DelegateID != nil && kind.agentless:
		return "an agentless kind is not delegated: remove the target's delegate"
	case t.DelegateID == nil && kind.delegatedOnly:
		return fmt.Sprintf("only delegates carry %s %s connections: name the target's delegate", t.ModuleName, t.Kind)
	}
	return ""
}

// delegateFailed stores what a call or an open through t's delegate came
// to when the delegate could not take it: done when it did (or core could
// not store it, err).
func (s *Service) delegateFailed(ctx context.Context, t *models.MinionTarget, err error) (done bool, _ error) {
	switch {
	case errors.Is(err, ErrNoDelegateTask):
		// Called again once its minion has the task core queued, and in a
		// while whatever happens.
		now := s.now()
		next := now.Add(delegateWaitRetry)
		t.State, t.StateDetail, t.StateAt, t.NextCallAt = models.TargetConnecting, clip(err.Error(), 400), now, &next
		return true, s.saveCall(ctx, t)
	case errors.Is(err, ErrNotADelegate):
		return true, s.saveCall(ctx, s.stateOnly(t, models.TargetRefused, clip(err.Error(), 400)))
	}
	return false, nil
}

// delegateWaitRetry is how soon a target waiting for its delegate's task is
// called again by itself.
const delegateWaitRetry = 15 * time.Second

// refusal reports whether a link failure is the target refusing the
// channel (the login, the host key), which no retry changes until the
// target is edited.
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

// notTheMinion says what answered instead of the minion.
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

// reached is t after a call that reached the minion: connected, next called
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

// answered is t after a call something answered that is not the minion's
// session going on (not the minion, an err): not retried, Check now calls
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
