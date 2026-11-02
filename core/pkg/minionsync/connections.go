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
// connection serves. Everything that sends to a minion goes through Notify:
// callers say a session has news (a task queued, a cancel, a task ended),
// and Notify reads what to send from the database. Core runs as one
// instance (docs: architecture, One core instance), so the connections are
// this process's.

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

	// target is the bind-persistent target the connection was opened for
	// (uuid.Nil for a reverse one), policy its policy then; linked once
	// its first session is linked to the target; why why it is closing.
	target uuid.UUID
	policy string
	linked bool
	why    string
}

// NewConn is a connection whose frames go out through send, and which
// close closes (the socket).
func NewConn(id, kind, instance, peer string, send func(ctx context.Context, f protocol.ConnFrame) error, close func()) *Conn {
	return &Conn{ID: id, Kind: kind, Instance: instance, Peer: peer, send: send, close: close,
		allowance: map[uuid.UUID]*uint64{}, pending: map[string]time.Time{}, active: time.Now()}
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
	// scheduled are the sessions a Notify is due for.
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
// news for its session: Notify, coalesced, when the session has a
// connection open.
func (s *Service) WatchTasks() func() {
	return s.bus.Subscribe([]eventbus.Topic{eventbus.TopicMinionTaskChanged}, func(_ context.Context, e eventbus.Event) {
		p, ok := e.Payload.(eventbus.MinionTaskEventPayload)
		if !ok {
			return
		}
		if id, err := uuid.Parse(p.SessionID); err == nil {
			s.notifySoon(id)
			s.notifyBind(id)
		}
	})
}

// notifySoon notifies the session after notifyDelay, once for what comes
// meanwhile; nothing when it has no connection open.
func (s *Service) notifySoon(session uuid.UUID) {
	if !s.Connected(session) {
		return
	}
	s.conns.mu.Lock()
	if s.conns.scheduled[session] {
		s.conns.mu.Unlock()
		return
	}
	s.conns.scheduled[session] = true
	s.conns.mu.Unlock()
	time.AfterFunc(notifyDelay, func() {
		s.conns.mu.Lock()
		delete(s.conns.scheduled, session)
		s.conns.mu.Unlock()
		s.Notify(context.Background(), session)
	})
}

// Notify sends the session what is waiting for it, on the connection that
// carried its latest message, the one before when that send fails: cancels,
// and tasks within the allowance the minion said on that connection.
// Nothing goes when it has no connection open: what waits goes with its
// next exchange.
func (s *Service) Notify(ctx context.Context, session uuid.UUID) {
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
		items, err := s.pushItems(ctx, from.session, c)
		if err != nil {
			s.logger.Error("minion push: what to send", zap.String("session_id", session.String()), zap.Error(err))
			return
		}
		if len(items) == 0 {
			return
		}
		if err := s.push(ctx, c, from, items); err == nil {
			return
		} else {
			s.logger.Info("minion push failed, trying another connection", zap.String("session_id", session.String()),
				zap.String("conn", c.ID), zap.Error(err))
		}
	}
}

// WakeStreams pushes the session's stream acks to its minion now, over a
// persistent connection serving it: a stream it holds goes as soon as a
// downloader (or an uploader) comes, instead of with the minion's next
// message, which on a persistent connection may be long in coming. The
// minion holds such a stream, sending nothing, so the ack cannot cross its
// chunks. Nothing goes without a connection open. It returns at once.
//
// A session no persistent connection serves may be a bind-call page's: its
// target is called, if a stream of its may go on (bind.go).
func (s *Service) WakeStreams(session uuid.UUID) {
	if !s.Connected(session) {
		s.wakeBind(session)
		return
	}
	go s.wakeStreams(context.Background(), session)
}

// StreamRoom: a downloader took a chunk of one of the session's streams.
// Only a bind-call page is told, by a call: a minion that polls, or holds a
// persistent connection, asks on its own.
func (s *Service) StreamRoom(session uuid.UUID) {
	if !s.Connected(session) {
		s.wakeBind(session)
	}
}

func (s *Service) wakeStreams(ctx context.Context, session uuid.UUID) {
	conns := s.conns.of(session)
	if len(conns) == 0 {
		return
	}
	defer s.conns.lock(session)()
	from, err := s.sender(ctx, session)
	if err != nil || from.session == nil || from.session.Closed(s.now()) {
		return
	}
	items, err := s.streamAcks(ctx, from.session, nil)
	if err != nil {
		s.logger.Error("minion push: streams' acks", zap.String("session_id", session.String()), zap.Error(err))
		return
	}
	if len(items) == 0 {
		return
	}
	for _, c := range conns {
		if err := s.push(ctx, c, from, items); err == nil {
			return
		} else {
			s.logger.Info("minion push failed, trying another connection", zap.String("session_id", session.String()),
				zap.String("conn", c.ID), zap.Error(err))
		}
	}
}

// pushItems is what goes to the session on c: its cancels, and its tasks
// within c's allowance.
func (s *Service) pushItems(ctx context.Context, session *models.MinionSession, c *Conn) ([]cbor.RawMessage, error) {
	items, err := s.cancels(ctx, session)
	if err != nil {
		return nil, err
	}
	if max, ok := c.allowanceOf(session.SessionID); ok {
		limit, err := s.room(ctx, session, max)
		if err != nil {
			return nil, err
		}
		tasks, err := s.deliver(ctx, session, limit)
		if err != nil {
			return nil, err
		}
		items = append(items, tasks...)
	}
	return items, nil
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
	c.touch(s.now())
	id := protocol.NewMessageID()
	plain, err := protocol.MarshalCBOR(protocol.CoreMessage{V: protocol.MinionMessageV3, ID: id, Items: items})
	if err != nil {
		return err
	}
	sid := from.session.SessionID
	sealed, err := protocol.SealFrame(from.keys, sid, protocol.FrameCoreToMinion, plain)
	if err != nil {
		return err
	}
	c.addPending(id, s.now())
	if err := c.send(ctx, protocol.ConnFrame{ID: sid[:], Data: sealed}); err != nil {
		c.takePending(id)
		return err
	}
	return nil
}

// seenOn records that via carried a message of session: a session that had
// no connection open is now connected, and a connection a full list let
// go is closed.
func (s *Service) seenOn(ctx context.Context, via *Conn, session *models.MinionSession) {
	if via.target != uuid.Nil && !via.linked {
		via.linked = true
		s.linkTarget(ctx, via.target, session)
	}
	first, evicted := s.conns.seen(session.SessionID, via, s.now())
	if evicted != nil && evicted.close != nil {
		s.logger.Info("minion connection: a session's oldest let go", zap.String("session_id", session.SessionID.String()),
			zap.String("conn", evicted.ID))
		evicted.close()
	}
	if first {
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(session)))
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
	if c.target != uuid.Nil {
		s.persistentClosed(ctx, c)
	}
	served, nowNone := s.conns.remove(c)
	for _, session := range served {
		s.touch(ctx, session, c.Peer)
	}
	for _, session := range nowNone {
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
	if err := s.sessions.CheckIn(ctx, session, checkIn(s.now(), peer, nil, from.session.Sleep)); err != nil {
		s.logger.Warn("minion connection: check the session in", zap.String("session_id", session.String()), zap.Error(err))
	}
}

func (s *Service) publishSession(ctx context.Context, session uuid.UUID) {
	if from, err := s.sender(ctx, session); err == nil && from.session != nil {
		s.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(sessionPayload(from.session)))
	}
}
