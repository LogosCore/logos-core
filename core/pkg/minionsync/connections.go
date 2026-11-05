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
	// pending are the ids of the messages core sent on it, waiting for
	// their answers.
	pending map[string]time.Time
	// active is when a message last went over it, either way: a
	// bind-persistent connection closes after its policy's idle time.
	active time.Time

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
	return &Conn{ID: id, Kind: kind, Instance: instance, Peer: peer, send: send, close: close,
		allowance: map[uuid.UUID]*uint64{}, pending: map[string]time.Time{}, active: time.Now(),
		heard: make(chan struct{}), gone: make(chan struct{}), carried: map[uuid.UUID]bool{}}
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

func (c *Conn) addPending(id []byte, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, at := range c.pending {
		if now.Sub(at) > pendingTTL {
			delete(c.pending, k)
		}
	}
	c.pending[string(id)] = now
}

// takePending reports whether id names a message core sent on c and is
// waiting the answer to, which it no longer waits for.
func (c *Conn) takePending(id []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.pending[string(id)]
	delete(c.pending, string(id))
	return ok
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

// remove forgets c, and returns the sessions it served that have no other
// connection open.
func (cs *connections) remove(c *Conn) (served, nowNone []uuid.UUID) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for session, uses := range cs.bySession {
		kept := uses[:0]
		had := false
		for _, u := range uses {
			if u.conn == c {
				had = true
				continue
			}
			kept = append(kept, u)
		}
		if !had {
			continue
		}
		served = append(served, session)
		if len(kept) == 0 {
			delete(cs.bySession, session)
			nowNone = append(nowNone, session)
		} else {
			cs.bySession[session] = kept
		}
	}
	return served, nowNone
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

// Connected reports whether a persistent connection serving the session is
// open.
func (s *Service) Connected(session uuid.UUID) bool {
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
	s.conns.mu.Lock()
	withStreams, due := s.conns.scheduled[session]
	s.conns.scheduled[session] = withStreams || streams
	s.conns.mu.Unlock()
	if due {
		return
	}
	time.AfterFunc(notifyDelay, func() {
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
// go again within it).
func (s *Service) room(ctx context.Context, session *models.MinionSession, allowance *uint64) (int, error) {
	if allowance == nil {
		return models.MaxMinionTasksPerReply, nil
	}
	running, err := s.tasks.CountRunning(ctx, session.SessionID)
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
	plain, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV3, ID: msgID, Items: items, At: unixNow(s.now())})
	if err != nil {
		return err
	}
	sealed, err := from.seal(id, plain)
	if err != nil {
		return err
	}
	c.addPending(msgID, s.now())
	if err := c.send(ctx, protocol.ConnFrame{ID: id[:], Data: sealed}); err != nil {
		c.takePending(msgID)
		return err
	}
	c.carry(items)
	return nil
}

// seenOn records that via carried a message of session: a session that had
// no connection open is now connected, and a connection a full list let
// go is closed.
func (s *Service) seenOn(ctx context.Context, via *Conn, session *models.MinionSession) {
	if via.target != uuid.Nil && via.follow(session.SessionID) {
		// A bind-persistent target's minion is the session that last spoke
		// over its connection: the one it opened with, or one it staged
		// over it since (its session closed, or it restarted).
		s.linkTarget(ctx, via, session)
	}
	first, evicted := s.conns.seen(session.SessionID, via, s.now())
	if evicted != nil {
		s.logger.Info("minion connection: a session's oldest let go", zap.String("session_id", session.SessionID.String()),
			zap.String("conn", evicted.ID))
		evicted.closeBecause("a newer connection of its session took its place")
	}
	if first {
		s.setConnected(ctx, session.SessionID, true)
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
	}
	if via.target != uuid.Nil && via.asksFor(session.SessionID) {
		// Back over a new connection: its minion may not have the tasks
		// core thinks it runs (a channel acting as the minion ran them
		// inside the one that went).
		s.askTasks(ctx, session.SessionID, "back over a new connection")
	}
}

// setConnected records whether a persistent connection serving the session
// is open, for the Sessions list's filters (models.MinionSession.Connected).
func (s *Service) setConnected(ctx context.Context, session uuid.UUID, connected bool) {
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

// KeepAlive tells the sessions c serves that their minion is there: a
// check-in each, so their last seen stays now and they are not late. A
// connection's socket handler calls it while the socket is open.
func (s *Service) KeepAlive(ctx context.Context, c *Conn) {
	for _, session := range s.conns.sessionsOf(c) {
		s.touch(ctx, session, c.Peer)
	}
}

// Closed forgets c once its socket closed: the sessions it served are seen
// a last time now, and those it leaves with no connection are told of.
func (s *Service) Closed(ctx context.Context, c *Conn) {
	// First: an open waiting for its minion's first word over c stops
	// waiting.
	c.end()
	if c.target != uuid.Nil {
		s.persistentClosed(ctx, c)
	}
	served, nowNone := s.conns.remove(c)
	for _, session := range served {
		s.touch(ctx, session, c.Peer)
	}
	for _, session := range nowNone {
		s.setConnected(ctx, session, false)
		s.publishSession(ctx, session)
	}
}

// touch checks the session in now, as a message would: its last seen, and
// when it goes late and lost by its schedule.
func (s *Service) touch(ctx context.Context, session uuid.UUID, peer string) {
	from, err := s.sender(ctx, session)
	if err != nil || from.session == nil || from.session.Closed(s.now()) {
		return
	}
	if err := s.sessions.CheckIn(ctx, session, checkIn(s.now(), peer, nil, from.session.Sleep, from.session.ConnKind)); err != nil {
		s.logger.Warn("minion connection: check the session in", zap.String("session_id", session.String()), zap.Error(err))
	}
}

func (s *Service) publishSession(ctx context.Context, session uuid.UUID) {
	if from, err := s.sender(ctx, session); err == nil && from.session != nil {
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(from.session)))
	}
}
