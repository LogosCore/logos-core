package miniontunnels

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Reverse port forwards (docs: contracts/minion-messages/tunnels.md,
// Reverse port forward): the minion listens on its host, says where with a
// tunnel_listen, and opens a stream for each connection that comes there (a
// tunnel item from the minion, numbered by it). Core connects each to the
// task's destination from its own network and answers with a tunnel_reply
// that says nothing of its side: its result alone. From then on the stream
// is any other: core's end of it is the connection it made. No operational
// port: the listener is kept as a models.MinionListener. A UDP one's minion
// listens for datagrams: a stream for each address they come from, which
// core opens a socket of its own for (so what comes back is that source's),
// its datagrams going as tunnel_datagram items both ways, and the stream
// ending once it idled (the minion closes it; core, later, as a backstop).

// ErrNoListeners: this core keeps no reverse port forwards.
var ErrNoListeners = errors.New("core opens no reverse port forwards")

// WithListeners keeps reverse port forwards' listeners; without it core
// opens none.
func (s *Service) WithListeners(l repository.IMinionListenerRepository) *Service {
	s.listeners = l
	return s
}

// OpenReverse keeps a reverse tunnel's listener for a task about to be
// queued: the minion listens at o.Bind:o.Listen once it has the task, and
// core connects its streams to o.Host:o.HostPort (o.Front forward), or
// serves SOCKS5 on them behind a password it makes now (socks).
func (s *Service) OpenReverse(ctx context.Context, o Opening) error {
	if s.listeners == nil {
		return ErrNoListeners
	}
	front := o.Front
	if front == "" {
		front = protocol.FrontForward
	}
	rec := &models.MinionListener{TaskID: o.Task, OperationID: o.Operation, SessionID: o.Session, OpenedBy: o.By, OpenedAt: s.now(),
		SessionNumber: o.SessionNumber, OpenedByName: o.ByName, Bind: o.Bind, Listen: o.Listen, UDP: o.UDP, Front: front,
		MaxStreams: o.MaxStreams, Timeout: o.Timeout, StopIdle: o.StopIdle, Cap: o.Cap}
	if front == protocol.FrontSOCKS {
		rec.User, rec.Password = ReverseSOCKSUser, newPassword()
	} else {
		rec.Host, rec.HostPort = o.Host, o.HostPort
	}
	if err := s.listeners.Insert(ctx, rec); err != nil {
		return fmt.Errorf("keep a reverse port forward: %w", err)
	}
	s.takeUp(rec)
	s.listenerChanged(o.Task, o.Operation, true, false)
	return nil
}

// takeUp holds a reverse port forward's tunnel: its streams come from the
// minion, numbered past rec.Streams.
func (s *Service) takeUp(rec *models.MinionListener) *tunnel {
	t := newTunnel(&models.OperationalPort{TaskID: rec.TaskID, SessionID: rec.SessionID, OperationID: rec.OperationID,
		Purpose: models.OperationalPurposeReverse, Host: rec.Host, HostPort: rec.HostPort, Streams: rec.Streams, Opened: rec.Opened,
		Up: rec.Up, Down: rec.Down, MaxStreams: rec.MaxStreams, Timeout: rec.Timeout, StopIdle: rec.StopIdle, Cap: rec.Cap}, nil, true)
	t.reverse, t.bind, t.listen, t.bound, t.reverseUDP = true, rec.Bind, rec.Listen, rec.Bound, rec.UDP
	t.front, t.user, t.password = rec.Front, rec.User, rec.Password
	s.mu.Lock()
	s.tunnels[rec.TaskID] = t
	s.mu.Unlock()
	s.logger.Info("tunnels: reverse port forward", zap.String("task_id", rec.TaskID.String()),
		zap.String("session_id", rec.SessionID.String()))
	return t
}

// reopenReverse takes up a restart's reverse port forward whose task still
// runs, its numbers as its listener kept them; one whose task ended is
// forgotten, what it came to kept with its task. Its streams went with the
// restart: the minion's next word of each is answered with a close.
func (s *Service) reopenReverse(ctx context.Context, rec *models.MinionListener) {
	task, err := s.tasks.FindByTaskID(ctx, rec.TaskID)
	switch {
	case repository.IsNotFound(err) || (err == nil && (task.Finished() || task.CancelRequestedAt != nil)):
		if err == nil {
			s.keepTotals(ctx, rec.TaskID, models.MinionTaskTunnel{Streams: rec.Opened, Up: rec.Up, Down: rec.Down})
		}
		if err := s.listeners.Release(ctx, rec.TaskID); err != nil {
			s.logger.Warn("tunnels: forget an ended task's reverse port forward", zap.Error(err))
		}
		return
	case err != nil:
		s.logger.Error("tunnels: read a reverse port forward's task", zap.String("task_id", rec.TaskID.String()), zap.Error(err))
		return
	}
	s.takeUp(rec)
	s.listenerChanged(rec.TaskID, rec.OperationID, false, false)
}

// listenerChanged says a reverse port forward's listener changed (the
// Ports page's): it opened, the minion said where it listens, its streams'
// numbers changed, or it closed.
func (s *Service) listenerChanged(task, operation uuid.UUID, opened, closed bool) {
	if s.bus != nil {
		s.bus.Publish(eventbus.NewMinionListenerChangedEvent(eventbus.MinionListenerEventPayload{TaskID: task.String(),
			OperationID: operation.String(), Opened: opened, Closed: closed}))
	}
}

// takeListen: the minion listens for a reverse port forward, at bound.
func (s *Service) takeListen(ctx context.Context, session uuid.UUID, l protocol.TunnelListenItem) {
	id, err := uuid.FromBytes(l.Task)
	if err != nil {
		return
	}
	s.mu.Lock()
	t := s.tunnels[id]
	if t == nil || t.session != session || !t.reverse || t.closing || t.bound == l.Bound {
		s.mu.Unlock()
		return
	}
	t.bound, t.changed = l.Bound, true
	operation := t.operation
	s.mu.Unlock()
	if err := s.listeners.SetBound(ctx, id, l.Bound); err != nil {
		s.logger.Warn("tunnels: keep where a minion listens", zap.String("task_id", id.String()), zap.Error(err))
	}
	s.logger.Info("tunnels: the minion listens", zap.String("task_id", id.String()), zap.String("bound", l.Bound))
	s.listenerChanged(id, operation, false, false)
}

// takeOpen: the minion opened a stream of a reverse port forward, for a
// connection that came to its listener. Core connects it to the task's
// destination, and answers once the connect ends. A tunnel again for a
// stream core has gets its reply again (once the connect ended); one for a
// stream that went (its number at or below the last) a close, and never a
// connect again. A tunnel for any other task is answered with a close:
// core opens the streams of every other tunnel.
func (s *Service) takeOpen(session uuid.UUID, it protocol.TunnelItem) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := uuid.FromBytes(it.Task)
	if err != nil {
		return closeItem(it.Task, it.Stream, protocol.TunnelCloseTaskEnded)
	}
	t := s.tunnels[tid]
	if t == nil || t.session != session || t.closing || !t.reverse {
		return closeItem(it.Task, it.Stream, protocol.TunnelCloseTaskEnded)
	}
	if st := t.streams[it.Stream]; st != nil {
		if st.answer != nil {
			t.replies = append(t.replies, *st.answer)
			s.wake(t.session)
		}
		return nil, nil
	}
	if it.Stream <= t.last {
		return closeItem(it.Task, it.Stream, protocol.TunnelCloseUnknown)
	}
	t.last, t.counted = it.Stream, true
	refuse := 0
	switch {
	case it.UDP != t.reverseUDP, it.UDP && it.Host == "", it.UDP && t.front == protocol.FrontSOCKS:
		// A TCP stream of a UDP forward, or the other way; a UDP one that
		// names no source to send back to; any but TCP for SOCKS.
		refuse = protocol.TunnelFailed
	case len(t.streams) >= t.maxStreams:
		refuse = protocol.TunnelNotAllowed
	}
	if refuse != 0 {
		// A stream it had, if not opened.
		t.opened++
		t.replies = append(t.replies, protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: t.task[:], Stream: it.Stream, Result: refuse})
		s.wake(t.session)
		s.done.Add(1)
		go func() {
			defer s.done.Done()
			s.seen(t.task, it.Stream)
		}()
		return nil, nil
	}
	st := &stream{t: t, id: it.Stream, done: make(chan struct{}), upSignal: make(chan struct{}, 1), downSignal: make(chan struct{}, 1),
		window: downWindow,
		rec: models.MinionTunnelStream{TaskID: t.task, Stream: it.Stream, Client: net.JoinHostPort(it.Host, strconv.FormatUint(it.Port, 10)),
			Host: t.fwdHost, Port: t.fwdPort, OpenedAt: s.now()}}
	if it.UDP {
		st.udp, st.rec.UDP, st.srcHost, st.srcPort = &assoc{}, true, it.Host, int(it.Port)
	}
	st.rec.SearchText = models.TunnelStreamSearchText(&st.rec)
	t.streams[it.Stream] = st
	t.opened++
	t.streamsChanged(s.now())
	t.changed = true
	s.done.Add(1)
	switch {
	case it.UDP:
		go s.connectUDP(st)
	case t.front == protocol.FrontSOCKS:
		go s.takeSOCKS(st, protocol.TunnelRoom(it.Room))
	default:
		go s.connect(st, protocol.TunnelRoom(it.Room))
	}
	return nil, nil
}

// refuseOpen answers an open of the minion's that core does not take as
// it is (why: a key it does not know, an item that does not read): a
// reverse tunnel's stream gets unsupported, and its number is used up; any
// other task's, a close, as takeOpen gives one.
func (s *Service) refuseOpen(session uuid.UUID, it protocol.TunnelItem, why error) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := uuid.FromBytes(it.Task)
	if err != nil {
		return closeItem(it.Task, it.Stream, protocol.TunnelCloseTaskEnded)
	}
	t := s.tunnels[tid]
	if t == nil || t.session != session || t.closing || !t.reverse {
		return closeItem(it.Task, it.Stream, protocol.TunnelCloseTaskEnded)
	}
	if t.streams[it.Stream] != nil || it.Stream <= t.last {
		return nil, nil
	}
	t.last, t.counted = it.Stream, true
	t.opened++
	s.logger.Info("tunnels: an open core does not take", zap.String("task_id", tid.String()), zap.Uint64("stream", it.Stream), zap.Error(why))
	t.replies = append(t.replies, protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: t.task[:], Stream: it.Stream,
		Result: protocol.TunnelUnsupported})
	s.wake(t.session)
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		s.seen(t.task, it.Stream)
	}()
	return nil, nil
}

// connectUDP opens a UDP reverse port forward's stream: a socket of its own
// to the destination, so what comes back is the stream's source's. It
// answers the minion, then sends each datagram that comes back to the
// minion, for the source; the stream going (idle, or its task ending)
// closes the socket.
func (s *Service) connectUDP(st *stream) {
	defer s.done.Done()
	t := st.t
	s.seen(t.task, st.id)
	ctx, cancel := context.WithTimeout(context.Background(), t.connectWait)
	go func() {
		select {
		case <-st.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", net.JoinHostPort(t.fwdHost, strconv.Itoa(t.fwdPort)))
	cancel()
	s.mu.Lock()
	if st.gone {
		s.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		return
	}
	result := dialResult(err)
	st.rec.Result = &result
	session := t.session
	if err != nil {
		st.rec.Msg = clipText(err.Error(), protocol.MaxTunnelMsg)
		t.replies = append(t.replies, st.replyItem())
		s.drop(st, models.TunnelEndNotOpened, false)
		s.mu.Unlock()
		s.wake(session)
		return
	}
	st.client, st.opened, st.lastActive = c, true, s.now()
	st.rec.Bound = c.LocalAddr().String()
	answer := st.replyItem()
	st.answer = &answer
	t.replies = append(t.replies, answer)
	t.changed = true
	s.mu.Unlock()
	s.wake(session)
	buf := make([]byte, protocol.MaxTunnelDatagram)
	for {
		n, err := c.Read(buf)
		if errors.Is(err, syscall.ECONNREFUSED) {
			// Nothing listens there, an ICMP said: as UDP is, the stream
			// goes on.
			continue
		}
		s.mu.Lock()
		if st.gone {
			s.mu.Unlock()
			return
		}
		if err != nil {
			s.abort(st, models.TunnelEndClientReset)
			s.mu.Unlock()
			return
		}
		if st.queueDatagram(st.srcHost, st.srcPort, buf[:n]) {
			st.lastActive = s.now()
		}
		s.mu.Unlock()
		s.wake(session)
	}
}

// takeReverseDatagram sends one of the minion's datagrams on to the
// destination, from the stream's socket (s.mu held; it is let go). One
// taken already, or for a stream not open yet, goes nowhere: the minion
// sends none before core's reply.
func (s *Service) takeReverseDatagram(st *stream, d protocol.TunnelDatagramItem) {
	a, c := st.udp, st.client
	if a == nil || !st.opened || c == nil || !a.in.take(d.Seq) || !st.t.inAllows(len(d.Data), s.now()) {
		// Taken already, not open yet, or over the tunnel's cap: dropped,
		// as UDP is.
		s.mu.Unlock()
		return
	}
	st.downBytes += int64(len(d.Data))
	st.t.down += int64(len(d.Data))
	st.t.changed = true
	st.lastActive = s.now()
	s.mu.Unlock()
	_, _ = c.Write(d.Data)
}

// endIdleUDP ends the UDP reverse port forwards' streams that idled past
// cfg.UDPIdle: the minion's own close (protocol.TunnelUDPIdle) went
// missing, or it went away. The minion is told.
func (s *Service) endIdleUDP() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, t := range s.tunnels {
		if !t.reverseUDP {
			continue
		}
		for _, st := range t.sorted() {
			if st.udp == nil || !st.opened || now.Sub(st.lastActive) < s.cfg.UDPIdle {
				continue
			}
			t.closes = append(t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: t.task[:], Stream: st.id,
				Reason: protocol.TunnelCloseDone})
			s.drop(st, models.TunnelEndDone, false)
			s.wake(t.session)
		}
	}
}

// seen keeps the highest stream number the task's minion has opened, so a
// tunnel for one that went is never connected again, a restart's neither.
func (s *Service) seen(task uuid.UUID, n uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.listeners.SeenStream(ctx, task, n); err != nil && !repository.IsNotFound(err) {
		s.logger.Warn("tunnels: keep a reverse port forward's last stream", zap.String("task_id", task.String()), zap.Error(err))
	}
}

// connect connects a reverse port forward's stream to its destination
// within the task's timeout, answers the minion, and carries the stream's
// bytes once it is open (room: what the minion takes past an ack). The
// stream going (its task ending) stops a connect under way.
func (s *Service) connect(st *stream, room uint64) {
	defer s.done.Done()
	t := st.t
	s.seen(t.task, st.id)
	ctx, cancel := context.WithTimeout(context.Background(), t.connectWait)
	defer cancel()
	go func() {
		select {
		case <-st.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(t.fwdHost, strconv.Itoa(t.fwdPort)))
	s.mu.Lock()
	if st.gone {
		s.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		return
	}
	result := dialResult(err)
	st.rec.Result = &result
	session := t.session
	if err != nil {
		// Core's record says why; the minion hears the result alone.
		st.rec.Msg = clipText(err.Error(), protocol.MaxTunnelMsg)
		t.replies = append(t.replies, st.replyItem())
		s.drop(st, models.TunnelEndNotOpened, false)
		s.mu.Unlock()
		s.wake(session)
		return
	}
	st.client, st.opened = c, true
	st.rec.Bound = c.LocalAddr().String()
	st.upLimit, st.ackedEnd = room, uint64(st.window)
	answer := st.replyItem()
	st.answer = &answer
	t.replies = append(t.replies, answer)
	t.changed = true
	s.mu.Unlock()
	s.wake(session)
	s.done.Add(1)
	go s.writeClient(st)
	s.readClient(st)
}

// replyItem is core's answer to one of the minion's streams: its result
// alone, core saying nothing of its side (no bound, no msg). Core's room is
// the default, so it says none.
func (st *stream) replyItem() protocol.TunnelReplyItem {
	r := protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: st.t.task[:], Stream: st.id, Result: protocol.TunnelFailed}
	if st.rec.Result != nil {
		r.Result = *st.rec.Result
	}
	return r
}

// dialResult is a connect's outcome as a tunnel result.
func dialResult(err error) int {
	var dns *net.DNSError
	var nerr net.Error
	switch {
	case err == nil:
		return protocol.TunnelOpen
	case errors.Is(err, syscall.ECONNREFUSED):
		return protocol.TunnelRefused
	case errors.As(err, &dns), errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.EHOSTDOWN):
		return protocol.TunnelHostUnreachable
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.ENETDOWN):
		return protocol.TunnelNetUnreachable
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, syscall.ETIMEDOUT), errors.As(err, &nerr) && nerr.Timeout():
		return protocol.TunnelTimedOut
	}
	return protocol.TunnelFailed
}

// clipText is s cut to n bytes at most, on a rune's edge.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Listener is the task's reverse port forward's listener; IsNotFound for
// none.
func (s *Service) Listener(ctx context.Context, task uuid.UUID) (*models.MinionListener, error) {
	if s.listeners == nil {
		return nil, ErrNoListeners
	}
	return s.listeners.FindByTaskID(ctx, task)
}

// Listeners is a page of the operation's reverse port forwards' listeners,
// the latest opened first; CountListeners counts them.
func (s *Service) Listeners(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionListener, error) {
	if s.listeners == nil {
		return []models.MinionListener{}, nil
	}
	return s.listeners.FindByOperationIDWithCursor(ctx, operationID, search, cursor, limit, forward)
}

func (s *Service) CountListeners(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	if s.listeners == nil {
		return 0, nil
	}
	return s.listeners.CountByOperationID(ctx, operationID, search)
}
