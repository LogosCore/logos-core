package minionsync

import (
	"context"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Persistent connections (docs: contracts/channel-core-connections/): a
// channel opens a connection socket for each minion connection that stays
// open, and core sends on it whenever it has something for a session the
// connection serves. Callers say a session has news (Nudge: a task queued,
// a cancel, a task ended, a stream that may go on), and the push reads what
// to send from the outbox (outbox.go). Core runs as one instance (docs:
// architecture, One core instance), so the connections are this process's.

// maxSessionConns is how many open connections a session keeps; one more
// closes its oldest.
const maxSessionConns = 8

// notifyDelay coalesces a session's news: what comes within it goes in one
// message.
const notifyDelay = 100 * time.Millisecond

// pendingTTL is how long core waits for the answer to a message it sent.
const pendingTTL = 10 * time.Minute

// unusedFor is how long a reverse-persistent socket stays open while no
// open session uses it: nothing spoke over it yet (its minion speaks first,
// at once), or its sessions closed and its minion did not start over on
// it.
var unusedFor = 2 * time.Minute

// channelKey keys the channel module a minion message came through in a
// context (ViaChannel).
type channelKey struct{}

// ViaChannel is ctx for the minion messages a channel of module carries (a
// sync call's, a connection socket's): a hello that names no connection is
// taken to come through the build's connection of that channel, when only
// one fits.
func ViaChannel(ctx context.Context, module string) context.Context {
	return context.WithValue(ctx, channelKey{}, module)
}

// ChannelOf is the channel module ctx says carried the message
// (ViaChannel); "" for none.
func ChannelOf(ctx context.Context) string {
	m, _ := ctx.Value(channelKey{}).(string)
	return m
}

// Conn is a persistent minion connection: a connection socket a channel
// opened. Its socket's handler reads frames into Exchange and gives Conn
// the way to write them back.
type Conn struct {
	// ID names the connection in logs.
	ID string
	// Kind is the connection's kind (protocol.KindReversePersistent).
	Kind string
	// Instance is the channel instance that holds it.
	Instance string
	// Peer is the minion's address as the channel saw it, "" for none.
	Peer  string
	send  func(ctx context.Context, f protocol.ConnFrame) error
	close func()

	mu sync.Mutex
	// allowance is each session's standing get_tasks: present once the
	// minion said one on this connection, a nil max for no limit.
	allowance map[uuid.UUID]*uint64
	// active is when a message last went over it, either way: a
	// bind-persistent connection closes after its policy's idle time.
	active time.Time
	// owner is the session whose connection it is: the first that spoke
	// over it, or the next once that one's session closed or another
	// process took it. A connection carries one minion's messages
	// (Exchange takes no other's): a minion that relays others delegates
	// (delegates.go). ownerBuild is its build: a hello of it over the
	// connection (its minion staging again) moves the connection to the
	// session it opens.
	owner      uuid.UUID
	ownerBuild uuid.UUID
	// unusedSince is since when no open session uses it (from its open);
	// zero while one does.
	unusedSince time.Time

	// A bind-persistent connection (bind_persistent.go): target is the
	// target it was opened for (uuid.Nil for a reverse one), and the rest
	// what the open that brought it knew of the target: its policy, its
	// build and operation (the minion over it is of them), the session it
	// is linked to (kept as the minion's sessions change), and its build
	// connection (place: a hello over it that says none is of it).
	target           uuid.UUID
	policy           string
	build, operation uuid.UUID
	session          uuid.UUID
	place            int
	// failures and downSince are the target's failures in a row, and since
	// when, as its open found them: a connection that drops soon after its
	// minion spoke counts one more.
	failures  int
	downSince *time.Time
	// heard is closed at the first message of the target's minion over it
	// (spokeAt), gone once its socket closed.
	heard   chan struct{}
	spokeAt time.Time
	gone    chan struct{}
	ended   bool
	// why is why core closes it; refused says why is that a minion not the
	// target's spoke over it.
	why     string
	refused bool
	// carried are the tasks it carried, by id: sent over it, or named by a
	// result or an ack that came over it. It is not closed idle while one
	// of them runs.
	carried map[uuid.UUID]bool
	// asked is the session core asked over it which tasks its minion still
	// has (reacks.go).
	asked uuid.UUID
}

// placeOf is the build connection of c's target, once known; 0 otherwise.
func (c *Conn) placeOf() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.place
}

// NewConn is a connection whose frames go out through send, and which
// close closes (the socket).
func NewConn(id, kind, instance, peer string, send func(ctx context.Context, f protocol.ConnFrame) error, close func()) *Conn {
	now := time.Now()
	return &Conn{ID: id, Kind: kind, Instance: instance, Peer: peer, send: send, close: close,
		allowance: map[uuid.UUID]*uint64{}, active: now, unusedSince: now,
		heard: make(chan struct{}), gone: make(chan struct{}), carried: map[uuid.UUID]bool{}}
}

// claim makes session, of build, c's own when c has none: the minion whose
// connection it is speaks first. It reports whether session is.
func (c *Conn) claim(session, build uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner == uuid.Nil {
		c.owner, c.ownerBuild = session, build
	}
	return c.owner == session
}

// carries reports whether c carries from's messages: those of its own
// session, or under its build's ids (its minion staging again); any when
// it has none.
func (c *Conn) carries(from *sender) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.owner == uuid.Nil:
		return true
	case from.session != nil:
		return from.session.SessionID == c.owner
	}
	return from.build != nil && from.build.BuildID == c.ownerBuild
}

// disown says c is no longer session's own (its session closed, another
// process took it, or c no longer serves it): the next session to speak
// over it is.
func (c *Conn) disown(session uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner == session {
		c.owner = uuid.Nil
	}
}

// ownerOf is the session whose own connection c is; uuid.Nil for none.
func (c *Conn) ownerOf() uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.owner
}

// touch records an exchange on c now: its idle time starts over.
func (c *Conn) touch(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = now
}

func (c *Conn) lastActive() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

func (c *Conn) setAllowance(session uuid.UUID, max *uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowance[session] = max
}

// allowanceOf is the session's allowance on c; ok false when the minion
// said none.
func (c *Conn) allowanceOf(session uuid.UUID) (max *uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	max, ok = c.allowance[session]
	return max, ok
}

// answers are the messages core pushed to sessions and waits for the
// minions' answers to, by session: an answer is taken over any socket of
// its session (the minion may have connected again), once, within
// pendingTTL.
type answers struct {
	mu        sync.Mutex
	bySession map[uuid.UUID]map[string]time.Time
	swept     time.Time
}

func newAnswers() *answers { return &answers{bySession: map[uuid.UUID]map[string]time.Time{}} }

// add records that core sent message id to session at now.
func (a *answers) add(session uuid.UUID, id []byte, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Sub(a.swept) > pendingTTL {
		// Now and then, those no answer came for.
		for sid, m := range a.bySession {
			for k, at := range m {
				if now.Sub(at) > pendingTTL {
					delete(m, k)
				}
			}
			if len(m) == 0 {
				delete(a.bySession, sid)
			}
		}
		a.swept = now
	}
	m := a.bySession[session]
	if m == nil {
		m = map[string]time.Time{}
		a.bySession[session] = m
	}
	m[string(id)] = now
}

// take reports whether id names a message core sent to session and still
// waits the answer to, which it no longer waits for.
func (a *answers) take(session uuid.UUID, id []byte, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.bySession[session]
	at, ok := m[string(id)]
	delete(m, string(id))
	if len(m) == 0 {
		delete(a.bySession, session)
	}
	return ok && now.Sub(at) <= pendingTTL
}

// connUse is a connection serving a session, with when it last carried one
// of its messages.
type connUse struct {
	conn *Conn
	last time.Time
}

// connections are the open connections, by the sessions they serve.
type connections struct {
	mu        sync.Mutex
	bySession map[uuid.UUID][]connUse
	// scheduled are the sessions a push is due for, and whether it carries
	// the streams' acks.
	scheduled map[uuid.UUID]bool
	// sending serializes a session's pushes.
	sending map[uuid.UUID]*sync.Mutex
	// flags serialize the writes of sessions' connected flags, by their
	// id's first byte.
	flags [256]sync.Mutex
}

func newConnections() *connections {
	return &connections{bySession: map[uuid.UUID][]connUse{}, scheduled: map[uuid.UUID]bool{},
		sending: map[uuid.UUID]*sync.Mutex{}}
}

// seen records that c carried a message of session at at: it serves the
// session, first among its connections. first reports whether the session
// had none open before; evicted is the connection a full list let go.
func (cs *connections) seen(session uuid.UUID, c *Conn, at time.Time) (first bool, evicted *Conn) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	uses := cs.bySession[session]
	first = len(uses) == 0
	kept := []connUse{{conn: c, last: at}}
	for _, u := range uses {
		if u.conn != c {
			kept = append(kept, u)
		}
	}
	if len(kept) > maxSessionConns {
		evicted = kept[len(kept)-1].conn
		kept = kept[:maxSessionConns]
	}
	cs.bySession[session] = kept
	return first, evicted
}

// remove forgets c, for every session it served.
func (cs *connections) remove(c *Conn) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for session := range cs.bySession {
		cs.dropLocked(session, c)
	}
}

// drop forgets that c serves session.
func (cs *connections) drop(session uuid.UUID, c *Conn) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.dropLocked(session, c)
}

func (cs *connections) dropLocked(session uuid.UUID, c *Conn) {
	uses := cs.bySession[session]
	kept := make([]connUse, 0, len(uses))
	for _, u := range uses {
		if u.conn != c {
			kept = append(kept, u)
		}
	}
	switch {
	case len(kept) == len(uses):
	case len(kept) == 0:
		delete(cs.bySession, session)
	default:
		cs.bySession[session] = kept
	}
}

// of are the session's open connections, the one that carried its latest
// message first.
func (cs *connections) of(session uuid.UUID) []*Conn {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]*Conn, 0, len(cs.bySession[session]))
	for _, u := range cs.bySession[session] {
		out = append(out, u.conn)
	}
	return out
}

// sessionsOf are the sessions c serves.
func (cs *connections) sessionsOf(c *Conn) []uuid.UUID {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var out []uuid.UUID
	for session, uses := range cs.bySession {
		for _, u := range uses {
			if u.conn == c {
				out = append(out, session)
				break
			}
		}
	}
	return out
}

func (cs *connections) lock(session uuid.UUID) func() {
	cs.mu.Lock()
	m, ok := cs.sending[session]
	if !ok {
		m = &sync.Mutex{}
		cs.sending[session] = m
	}
	cs.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Connected reports whether a persistent connection of the session's own
// is open: its minion is there.
func (s *Service) Connected(session uuid.UUID) bool {
	for _, c := range s.conns.of(session) {
		if c.ownerOf() == session {
			return true
		}
	}
	return false
}

// reachable reports whether a persistent connection open reaches the
// session.
func (s *Service) reachable(session uuid.UUID) bool {
	return len(s.conns.of(session)) > 0
}

// WatchTasks makes every change to a task (queued, cancelled, a part taken)
// news for its session (Nudge).
func (s *Service) WatchTasks() func() {
	return s.bus.Subscribe([]eventbus.Topic{eventbus.TopicMinionTaskChanged}, func(_ context.Context, e eventbus.Event) {
		p, ok := e.Payload.(eventbus.MinionTaskEventPayload)
		if !ok {
			return
		}
		if id, err := uuid.Parse(p.SessionID); err == nil {
			s.Nudge(id, false)
		}
	})
}

// pushSoon pushes to the session after notifyDelay, once for what comes
// meanwhile, with the streams' acks when any nudge asked for them.
func (s *Service) pushSoon(session uuid.UUID, streams bool) {
	s.pushAfter(session, streams, notifyDelay)
}

// pushAfter is pushSoon after delay; a push due already goes when it was
// to.
func (s *Service) pushAfter(session uuid.UUID, streams bool, delay time.Duration) {
	s.conns.mu.Lock()
	withStreams, due := s.conns.scheduled[session]
	s.conns.scheduled[session] = withStreams || streams
	s.conns.mu.Unlock()
	if due {
		return
	}
	time.AfterFunc(delay, func() {
		s.conns.mu.Lock()
		streams := s.conns.scheduled[session]
		delete(s.conns.scheduled, session)
		s.conns.mu.Unlock()
		s.pushNow(context.Background(), session, streams)
	})
}

// Notify pushes what waits for the session now: cancels, and tasks within
// the allowance the minion said on the connection.
func (s *Service) Notify(ctx context.Context, session uuid.UUID) { s.pushNow(ctx, session, false) }

// pushNow sends the session what the outbox has for it, on the connection
// that carried its latest message, the one before when that send fails:
// cancels, tasks within the allowance the minion said on that connection,
// and with streams the streams' acks (a stream it holds may go on: the
// minion sends nothing of it meanwhile, so the ack cannot cross its
// chunks). Nothing goes when it has no connection open: what waits goes
// with its next exchange.
func (s *Service) pushNow(ctx context.Context, session uuid.UUID, streams bool) {
	conns := s.conns.of(session)
	if len(conns) == 0 || s.tasks == nil {
		return
	}
	defer s.conns.lock(session)()
	from, err := s.sender(ctx, session)
	if err != nil || from.session == nil || from.session.Closed(s.now()) {
		return
	}
	for _, c := range conns {
		d := delivery{streams: streams, room: roomFor(from.session.MessageLimit(), nil)}
		if max, ok := c.allowanceOf(session); ok {
			if d.tasks, err = s.room(ctx, from.session, max); err != nil {
				s.logger.Error("minion push: the allowance's room", zap.String("session_id", session.String()), zap.Error(err))
				return
			}
		}
		o, err := s.outbox(ctx, from.session, d)
		if err != nil {
			s.logger.Error("minion push: what to send", zap.String("session_id", session.String()), zap.Error(err))
			return
		}
		if len(o.items) == 0 {
			return
		}
		if err := s.push(ctx, c, from, o.items); err == nil {
			return
		} else {
			s.logger.Info("minion push failed, trying another connection", zap.String("session_id", session.String()),
				zap.String("conn", c.ID), zap.Error(err))
		}
	}
}

// room is how many tasks an allowance of max leaves room for: max less the
// tasks the minion has and has not ended (those sent and not acknowledged
// go again within it), but tunnels, which hold no place.
func (s *Service) room(ctx context.Context, session *models.MinionSession, allowance *uint64) (int, error) {
	if allowance == nil {
		return models.MaxMinionTasksPerReply, nil
	}
	running, err := s.tasks.CountSlots(ctx, session.SessionID)
	if err != nil {
		return 0, err
	}
	room := int(min(*allowance, uint64(models.MaxMinionTasksPerReply))) - running
	return max(room, 0), nil
}

// push sends items on c as a message core starts, under the session's id,
// and waits for its answer.
func (s *Service) push(ctx context.Context, c *Conn, from *sender, items []cbor.RawMessage) error {
	return s.pushUnder(ctx, c, from, from.session.SessionID, items)
}

// pushUnder is push under id: the session's, or a build's (core's first
// word to a minion that has none yet).
func (s *Service) pushUnder(ctx context.Context, c *Conn, from *sender, id uuid.UUID, items []cbor.RawMessage) error {
	c.touch(s.now())
	msgID := protocol.NewMessageID()
	plain, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV3, ID: msgID, Items: packMessage(from.session, items),
		At: unixNow(s.now())})
	if err != nil {
		return err
	}
	sealed, err := from.seal(id, plain)
	if err != nil {
		return err
	}
	s.answers.add(id, msgID, s.now())
	if err := c.send(ctx, protocol.ConnFrame{ID: id[:], Data: sealed}); err != nil {
		s.answers.take(id, msgID, s.now())
		return err
	}
	c.carry(items)
	return nil
}

// seenOn records that via carried a message of session: via reaches the
// session from now on, first among its connections, and is its own when
// via has none (the minion whose connection it is speaks first). A
// session's oldest connection a full list let go no longer reaches it, and
// closes when it reaches no one.
func (s *Service) seenOn(ctx context.Context, via *Conn, session *models.MinionSession) {
	sid := session.SessionID
	if via.target != uuid.Nil && via.follow(sid) {
		// A bind-persistent target's minion is the session that last spoke
		// over its connection: the one it opened with, or one it staged
		// over it since (its session closed, or it restarted).
		s.linkTarget(ctx, via, session)
	}
	was := s.Connected(sid)
	via.claim(sid, session.BuildID)
	_, evicted := s.conns.seen(sid, via, s.now())
	if evicted != nil {
		evicted.disown(sid)
		s.logger.Info("minion connection: a session's oldest let go", zap.String("session_id", sid.String()),
			zap.String("conn", evicted.ID))
		if len(s.conns.sessionsOf(evicted)) == 0 {
			evicted.closeBecause("a newer connection of its session took its place")
		}
	}
	if now := s.Connected(sid); now != was {
		s.setConnected(ctx, sid)
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
	}
	if via.target != uuid.Nil && via.asksFor(session.SessionID) {
		// Back over a new connection: its minion may not have the tasks
		// core thinks it runs (a channel acting as the minion ran them
		// inside the one that went).
		s.askTasks(ctx, session.SessionID, "back over a new connection")
	}
}

// setConnected records whether a persistent connection of the session's
// own is open, for the Sessions list's filters
// (models.MinionSession.Connected): what core holds as it writes, one write
// of the session's at a time, so of two changes racing (a socket closing as
// the next opens) the last write says the latest.
func (s *Service) setConnected(ctx context.Context, session uuid.UUID) {
	m := &s.conns.flags[session[0]]
	m.Lock()
	defer m.Unlock()
	connected := s.Connected(session)
	if err := s.sessions.SetConnected(ctx, session, connected); err != nil {
		s.logger.Warn("minion connection: record the session connected", zap.String("session_id", session.String()),
			zap.Bool("connected", connected), zap.Error(err))
	}
}

// resetConnected, as core starts, says no session is connected: the
// connections went with the process that held them.
func (s *Service) resetConnected(ctx context.Context) {
	n, err := s.sessions.ClearConnected(ctx)
	if err != nil {
		s.logger.Warn("minion connections: clear the sessions connected before core started", zap.Error(err))
		return
	}
	if n > 0 {
		s.logger.Info("minion connections: sessions connected before core started are not", zap.Int64("sessions", n))
	}
}

// KeepAlive is the open socket of c saying its minion is there, every
// connKeepAlive (its handler calls it while the socket is open): a check-in
// for the session whose own connection it is, so its last seen stays now
// and it is not late. A session closed meanwhile is told so (session_closed, as the
// reply to its next message would say) and no longer served; a
// reverse-persistent socket no open session uses for unusedFor closes.
func (s *Service) KeepAlive(ctx context.Context, c *Conn) {
	now := s.now()
	owner := c.ownerOf()
	used := false
	for _, sid := range s.conns.sessionsOf(c) {
		from, err := s.sender(ctx, sid)
		switch {
		case err != nil || from.session == nil:
			used = true // not known now: kept
			continue
		case from.session.Closed(now):
			s.tellClosed(ctx, c, from)
			continue
		}
		used = true
		if sid == owner {
			s.checkInBy(ctx, from, c.Peer)
		}
	}
	s.closeUnused(c, used, now)
}

// tellClosed tells the minion over c that its session closed (an
// operation deleted with it), as the reply to its next message would: a
// minion that sends nothing while connected learns it too, and starts
// over. c no longer serves the session.
func (s *Service) tellClosed(ctx context.Context, c *Conn, from *sender) {
	sid := from.session.SessionID
	s.conns.drop(sid, c)
	c.disown(sid)
	s.setConnected(ctx, sid)
	items, err := errMessage(protocol.ErrItemSessionClosed, "the session is closed: start over")
	if err == nil {
		err = s.push(ctx, c, from, items)
	}
	s.logger.Info("minion connection: its session closed, its minion told", zap.String("session_id", sid.String()),
		zap.String("conn", c.ID), zap.Error(err))
}

// closeUnused closes c, a reverse-persistent socket, once no open session
// has used it for unusedFor: none spoke over it, or its sessions closed and
// its minion did not start over on it. A target's socket goes as its open
// and policy say.
func (s *Service) closeUnused(c *Conn, used bool, now time.Time) {
	if c.target != uuid.Nil {
		return
	}
	c.mu.Lock()
	if used {
		c.unusedSince = time.Time{}
		c.mu.Unlock()
		return
	}
	if c.unusedSince.IsZero() {
		c.unusedSince = now
	}
	unused := now.Sub(c.unusedSince)
	c.mu.Unlock()
	if unused >= unusedFor {
		s.logger.Info("minion connection: no open session uses it, closing", zap.String("conn", c.ID),
			zap.Duration("for", unused.Round(time.Second)))
		c.closeBecause("no open session uses it")
	}
}

// Closed forgets c once its socket closed. The session whose connection it
// was is seen a last time now (late and lost run from here, by its
// schedule), and told of when it has none left.
func (s *Service) Closed(ctx context.Context, c *Conn) {
	// First: an open waiting for its minion's first word over c stops
	// waiting.
	c.end()
	if c.target != uuid.Nil {
		s.persistentClosed(ctx, c)
	}
	owner := c.ownerOf()
	was := owner != uuid.Nil && s.Connected(owner)
	s.conns.remove(c)
	if owner == uuid.Nil {
		return
	}
	s.touch(ctx, owner, c.Peer)
	if was && !s.Connected(owner) {
		s.setConnected(ctx, owner)
		s.publishSession(ctx, owner)
	}
}

// touch checks the session in now, as a message would: its last seen, and
// when it goes late and lost by its schedule.
func (s *Service) touch(ctx context.Context, session uuid.UUID, peer string) {
	from, err := s.sender(ctx, session)
	if err != nil || from.session == nil {
		return
	}
	s.checkInBy(ctx, from, peer)
}

// checkInBy checks from's open session in now, from peer.
func (s *Service) checkInBy(ctx context.Context, from *sender, peer string) {
	session := from.session
	if session.Closed(s.now()) {
		return
	}
	if err := s.sessions.CheckIn(ctx, session.SessionID, checkIn(s.now(), peer, nil, session.Sleep, session.ConnKind)); err != nil {
		s.logger.Warn("minion connection: check the session in", zap.String("session_id", session.SessionID.String()), zap.Error(err))
	}
}

func (s *Service) publishSession(ctx context.Context, session uuid.UUID) {
	if from, err := s.sender(ctx, session); err == nil && from.session != nil {
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(from.session)))
	}
}
