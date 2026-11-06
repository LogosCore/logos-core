package miniontunnels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// A tunnel's streams (docs: contracts/minion-messages/tunnels.md, Streams):
// each direction a byte stream from 0, the eof one place past its last
// byte. Core keeps what it sent until the minion's ack covers it and sends
// nothing past the ack's room; it takes the minion's bytes at its next and
// gives room as its client takes them. Everything below runs under s.mu
// but the reads and writes of the clients' sockets.

const (
	// itemOverhead is what an item adds to a message's items, as minionsync
	// counts it.
	itemOverhead = 8
	// dataHeader is what a tunnel_data takes besides its bytes, at most.
	dataHeader = 64
	// minPiece is the least a tunnel_data carries when the stream has more:
	// a message nearly full waits for the next rather than carry crumbs.
	minPiece = 1 << 10
	// downWindow is the room core gives each stream's minion side: what it
	// holds of the stream for a slow client. It is the default room, so
	// core's tunnel items leave it out.
	downWindow = protocol.DefaultTunnelRoom
	// maxUpHeld is the most core reads ahead of the minion's ack, whatever
	// room the minion gives.
	maxUpHeld = 1 << 20
	// readPiece is how much one read of a client takes at most.
	readPiece = 32 << 10
)

// tunnel is a tunnel task's: its port and its streams.
type tunnel struct {
	task, session, operation uuid.UUID
	port                     int
	ln                       net.Listener
	maxStreams               int
	connectWait              time.Duration
	// acked: the minion has the task, so streams may open; until then
	// SOCKS clients wait.
	acked   bool
	closing bool
	streams map[uint64]*stream
	// numbering: streams being given a number, counted against
	// maxStreams.
	numbering int
	// handshaking are the clients still saying what they want.
	handshaking map[net.Conn]bool
	// closes go to the minion with the session's next message.
	closes []protocol.TunnelCloseItem
}

func newTunnel(rec *models.OperationalPort, ln net.Listener, acked bool) *tunnel {
	maxStreams := rec.MaxStreams
	if maxStreams <= 0 {
		maxStreams = DefaultMaxStreams
	}
	timeout := rec.Timeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	return &tunnel{task: rec.TaskID, session: rec.SessionID, operation: rec.OperationID, port: rec.Port, ln: ln,
		maxStreams: maxStreams, connectWait: timeout, acked: acked, streams: map[uint64]*stream{}, handshaking: map[net.Conn]bool{}}
}

// The command's defaults, as the docs give them, for a task whose args
// leave them out.
const (
	DefaultMaxStreams     = 256
	DefaultConnectTimeout = 10 * time.Second
)

// everyClient is the connections still in their SOCKS handshake.
func (t *tunnel) everyClient() []net.Conn {
	out := make([]net.Conn, 0, len(t.handshaking))
	for c := range t.handshaking {
		out = append(out, c)
	}
	return out
}

// sorted is the tunnel's streams by number.
func (t *tunnel) sorted() []*stream {
	out := make([]*stream, 0, len(t.streams))
	for _, st := range t.streams {
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b *stream) int { return compareUint(a.id, b.id) })
	return out
}

func compareUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// stream is one SOCKS client's connection, carried by the minion.
type stream struct {
	t      *tunnel
	id     uint64
	client net.Conn
	rec    models.MinionTunnelStream

	// opened: the minion's reply said open. openSent: when its tunnel
	// item last went (zero: not yet).
	opened   bool
	openSent time.Time
	replied  chan protocol.TunnelReplyItem
	// done closes when the stream is dropped.
	done chan struct{}
	gone bool

	// Toward the minion: the client's bytes from upBase (the minion's
	// next) not yet acked; sent up to upSent (an eof sent counts one past
	// the bytes); nothing past upLimit, the furthest an ack's room
	// reached.
	up       []byte
	upBase   uint64
	upSent   uint64
	upSentAt time.Time
	upLimit  uint64
	// upEOF: the client sends no more, the eof is at upBase+len(up);
	// upDone: the minion has every byte and the eof.
	upEOF    bool
	upDone   bool
	upSignal chan struct{}

	// Toward the client: the minion's bytes not yet written; downNext is
	// where core takes the stream (the eof counted); downEOF: the eof
	// came; downShut: the client has it.
	down     []byte
	downNext uint64
	// ahead are the minion's items that came before those ahead of them
	// (two messages crossing), kept within the room until the bytes
	// before them come.
	ahead      []protocol.TunnelDataItem
	aheadBytes int
	downEOF    bool
	downShut   bool
	ackDue     bool
	// ackedEnd is the furthest the acks core sent let the minion send to.
	ackedEnd   uint64
	downSignal chan struct{}
	// finishing: the minion said done; the stream drops once the client
	// has every byte.
	finishing bool

	upBytes, downBytes int64
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// accept takes the port's clients, each served on its own.
func (s *Service) accept(t *tunnel) {
	defer s.done.Done()
	for {
		c, err := t.ln.Accept()
		s.mu.Lock()
		closing := t.closing
		if err == nil && !closing {
			t.handshaking[c] = true
		}
		s.mu.Unlock()
		if closing {
			if err == nil {
				_ = c.Close()
			}
			return
		}
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Out of file descriptors, say: a moment, then on.
			s.logger.Warn("tunnels: accept", zap.Int("port", t.port), zap.Error(err))
			time.Sleep(50 * time.Millisecond)
			continue
		}
		s.done.Add(1)
		go s.serve(t, c)
	}
}

// serve is one client: its SOCKS request, the stream the minion opens for
// it, then its bytes.
func (s *Service) serve(t *tunnel, c net.Conn) {
	defer s.done.Done()
	_ = c.SetDeadline(time.Now().Add(s.cfg.HandshakeWait))
	req, err := readRequest(c)
	_ = c.SetDeadline(time.Time{})
	s.mu.Lock()
	delete(t.handshaking, c)
	closing := t.closing
	s.mu.Unlock()
	if err != nil || closing {
		_ = c.Close()
		return
	}
	open := protocol.TunnelItem{T: protocol.ItemTunnel, Task: t.task[:], Stream: 1, Host: req.host, Port: uint64(req.port)}
	if open.Validate() != nil {
		_ = req.reply(c, socks5HostUnreachable, "")
		_ = c.Close()
		return
	}
	st, code := s.newStream(t, c, req)
	if st == nil {
		_ = req.reply(c, code, "")
		_ = c.Close()
		return
	}
	timer := time.NewTimer(t.connectWait + s.cfg.ConnectSlack)
	defer timer.Stop()
	select {
	case r := <-st.replied:
		if r.Result != protocol.TunnelOpen {
			_ = req.reply(c, socksCode(r.Result), "")
			s.mu.Lock()
			s.drop(st, models.TunnelEndNotOpened, false)
			s.mu.Unlock()
			return
		}
		if err := req.reply(c, socks5Succeeded, r.Bound); err != nil {
			s.mu.Lock()
			s.abort(st, models.TunnelEndClientReset)
			s.mu.Unlock()
			return
		}
	case <-timer.C:
		_ = req.reply(c, socks5TTLExpired, "")
		s.mu.Lock()
		s.abort(st, models.TunnelEndNoReply)
		s.mu.Unlock()
		return
	case <-st.done:
		_ = req.reply(c, socks5GeneralFailure, "")
		return
	}
	s.done.Add(1)
	go s.writeClient(st)
	s.readClient(st)
}

// newStream gives the client's connection a stream of the tunnel, to open:
// nil and the SOCKS code when it may not have one.
func (s *Service) newStream(t *tunnel, c net.Conn, req request) (*stream, int) {
	s.mu.Lock()
	switch {
	case t.closing:
		s.mu.Unlock()
		return nil, socks5GeneralFailure
	case len(t.streams)+t.numbering >= t.maxStreams:
		s.mu.Unlock()
		return nil, socks5NotAllowed
	}
	t.numbering++
	s.mu.Unlock()
	id, err := s.ports.NextStream(context.Background(), t.task)
	s.mu.Lock()
	defer s.mu.Unlock()
	t.numbering--
	if err != nil || t.closing {
		if err != nil {
			s.logger.Warn("tunnels: number a stream", zap.String("task_id", t.task.String()), zap.Error(err))
		}
		return nil, socks5GeneralFailure
	}
	st := &stream{t: t, id: id, client: c, replied: make(chan protocol.TunnelReplyItem, 1), done: make(chan struct{}),
		upSignal: make(chan struct{}, 1), downSignal: make(chan struct{}, 1),
		rec: models.MinionTunnelStream{TaskID: t.task, Stream: id, OperationID: t.operation, SessionID: t.session,
			Client: c.RemoteAddr().String(), Host: req.host, Port: req.port, OpenedAt: s.now()}}
	t.streams[id] = st
	s.record(st, true)
	s.wake(t.session)
	return st, 0
}

// readClient carries the client's bytes toward the minion while the
// minion's room lasts.
func (s *Service) readClient(st *stream) {
	buf := make([]byte, readPiece)
	for {
		s.mu.Lock()
		for !st.gone && st.upFree() == 0 {
			s.mu.Unlock()
			select {
			case <-st.upSignal:
			case <-st.done:
			}
			s.mu.Lock()
		}
		if st.gone {
			s.mu.Unlock()
			return
		}
		n := min(len(buf), st.upFree())
		s.mu.Unlock()
		k, err := st.client.Read(buf[:n])
		s.mu.Lock()
		if st.gone {
			s.mu.Unlock()
			return
		}
		if k > 0 {
			st.up = append(st.up, buf[:k]...)
			st.upBytes += int64(k)
		}
		switch {
		case errors.Is(err, io.EOF):
			st.upEOF = true
		case err != nil:
			s.abort(st, models.TunnelEndClientReset)
		}
		session := st.t.session
		s.mu.Unlock()
		s.wake(session)
		if err != nil {
			return
		}
	}
}

// upFree is how much more of the client core may read now.
func (st *stream) upFree() int {
	if st.upEOF {
		return 0
	}
	limit := min(st.upLimit-st.upBase, maxUpHeld)
	return max(int(limit)-len(st.up), 0)
}

// writeClient writes the minion's bytes to the client, giving the room
// back as it goes, and ends the client's end with the minion's eof.
func (s *Service) writeClient(st *stream) {
	defer s.done.Done()
	for {
		s.mu.Lock()
		for !st.gone && len(st.down) == 0 && !(st.downEOF && !st.downShut) && !st.finishing {
			s.mu.Unlock()
			select {
			case <-st.downSignal:
			case <-st.done:
			}
			s.mu.Lock()
		}
		if st.gone {
			s.mu.Unlock()
			return
		}
		if len(st.down) == 0 {
			if st.downEOF && !st.downShut {
				st.downShut = true
				if tc, ok := st.client.(interface{ CloseWrite() error }); ok {
					_ = tc.CloseWrite()
				}
			}
			if st.finishing {
				s.drop(st, models.TunnelEndDone, false)
			} else {
				s.doneIfOver(st)
			}
			s.mu.Unlock()
			return
		}
		piece := st.down[:min(len(st.down), protocol.MaxTunnelData)]
		s.mu.Unlock()
		_, err := st.client.Write(piece)
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
		st.down = st.down[len(piece):]
		if len(st.down) == 0 {
			st.down = nil
		}
		st.downBytes += int64(len(piece))
		// Room given back: said once it is worth an ack.
		wake := st.roomGrew()
		session := st.t.session
		s.mu.Unlock()
		if wake {
			s.wake(session)
		}
	}
}

// roomGrew marks an ack due when the client took enough to say so: a
// quarter of the window, or any room after none.
func (st *stream) roomGrew() bool {
	if st.ackDue || st.downEOF {
		return false
	}
	room := uint64(downWindow - len(st.down))
	end := st.downNext + room
	if end >= st.ackedEnd+downWindow/4 {
		st.ackDue = true
		return true
	}
	return false
}

// abort ends the stream as a reset: the minion is told, and the client's
// connection is reset.
func (s *Service) abort(st *stream, end string) {
	if st.gone {
		return
	}
	if !st.openSent.IsZero() {
		st.t.closes = append(st.t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: st.t.task[:], Stream: st.id,
			Reason: protocol.TunnelCloseReset})
		s.wake(st.t.session)
	}
	s.drop(st, end, true)
}

// doneIfOver drops the stream once both directions ended and each side
// has the other's eof, telling the minion.
func (s *Service) doneIfOver(st *stream) {
	if st.gone || !st.upDone || !st.downShut {
		return
	}
	st.t.closes = append(st.t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: st.t.task[:], Stream: st.id,
		Reason: protocol.TunnelCloseDone})
	s.wake(st.t.session)
	s.drop(st, models.TunnelEndDone, false)
}

// drop forgets the stream and closes its client's connection (reset:
// aborted), recording how it ended.
func (s *Service) drop(st *stream, end string, reset bool) {
	if st.gone {
		return
	}
	st.gone = true
	close(st.done)
	delete(st.t.streams, st.id)
	if reset {
		if tc, ok := st.client.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
	}
	_ = st.client.Close()
	now := s.now()
	st.rec.EndedAt, st.rec.End = &now, end
	s.record(st, false)
}

// Fill adds to a message for the session's minion, through add, what its
// tunnels have to send, in room bytes and most items: closes, opens, acks,
// then data, a stream's item in turn while room lasts. It says how many it
// added, and how soon a minion that polls should come back; polls false:
// no tunnel of the session's runs.
func (s *Service) Fill(ctx context.Context, session uuid.UUID, room, most int, add func(cbor.RawMessage) bool) (added int, poll time.Duration, polls bool) {
	s.checkAcks(ctx, session)
	s.mu.Lock()
	defer s.mu.Unlock()
	tunnels := s.ofSession(session)
	if len(tunnels) == 0 {
		return 0, 0, false
	}
	now := s.now()
	f := &filler{room: room, most: most, add: add}
	work := false
	for _, t := range tunnels {
		for len(t.closes) > 0 && f.put(t.closes[0]) {
			t.closes = t.closes[1:]
			work = true
		}
	}
	var open []*stream
	for _, t := range tunnels {
		for _, st := range t.sorted() {
			if st.opened {
				open = append(open, st)
				continue
			}
			if t.acked && st.openDue(now, s.cfg.ResendAfter) && f.put(st.openItem()) {
				st.openSent, work = now, true
			}
		}
	}
	for _, st := range open {
		if st.ackDue && f.put(st.ackItem()) {
			st.acked()
		}
	}
	if n := len(open); n > 0 {
		start := s.rotation[session] % n
		s.rotation[session]++
		for moved := true; moved; {
			moved = false
			for i := range n {
				st := open[(start+i)%n]
				d := st.dataItem(now, f.dataRoom(), s.cfg.ResendAfter)
				if d == nil {
					continue
				}
				if !f.put(*d) {
					break
				}
				st.sent(*d, now)
				moved, work = true, true
			}
		}
	}
	pending := false
	for _, t := range tunnels {
		pending = pending || len(t.closes) > 0
		for _, st := range t.streams {
			pending = pending || (t.acked && !st.opened && st.openDue(now, s.cfg.ResendAfter)) ||
				(st.opened && (st.ackDue || st.dataItem(now, protocol.MaxTunnelData, s.cfg.ResendAfter) != nil))
		}
	}
	if pending && f.n > 0 {
		// More than one message holds: the next goes as soon as it may.
		s.wake(session)
	}
	switch {
	case pending || work:
		poll = 0
	case len(open) > 0:
		poll = s.cfg.IdlePoll
	default:
		poll = s.cfg.TaskPoll
	}
	return f.n, poll, true
}

// checkAcks reads the tasks of the session's tunnels the minion may not
// have yet, when streams wait for it: an ack this very message took opens
// them in its reply.
func (s *Service) checkAcks(ctx context.Context, session uuid.UUID) {
	s.mu.Lock()
	var waiting []uuid.UUID
	for _, t := range s.tunnels {
		if t.session == session && !t.acked && len(t.streams) > 0 {
			waiting = append(waiting, t.task)
		}
	}
	s.mu.Unlock()
	for _, id := range waiting {
		if task, err := s.tasks.FindByTaskID(ctx, id); err == nil && task.AckedAt != nil {
			s.acked(id)
		}
	}
}

// ofSession is the session's tunnels, by task.
func (s *Service) ofSession(session uuid.UUID) []*tunnel {
	var out []*tunnel
	for _, t := range s.tunnels {
		if t.session == session {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b *tunnel) int { return compareBytes(a.task[:], b.task[:]) })
	return out
}

func compareBytes(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return len(a) - len(b)
}

// filler is one message's room for tunnel items.
type filler struct {
	room, most, n int
	add           func(cbor.RawMessage) bool
}

// put adds v's item when it fits.
func (f *filler) put(v any) bool {
	if f.n >= f.most {
		return false
	}
	item, err := protocol.MarshalCBOR(v)
	if err != nil || len(item)+itemOverhead > f.room || !f.add(item) {
		return false
	}
	f.room -= len(item) + itemOverhead
	f.n++
	return true
}

// dataRoom is how many bytes a tunnel_data may carry in what is left.
func (f *filler) dataRoom() int {
	if f.n >= f.most {
		return 0
	}
	return max(f.room-itemOverhead-dataHeader, 0)
}

// openDue: its tunnel item has not gone, or went long enough ago with no
// reply.
func (st *stream) openDue(now time.Time, resend time.Duration) bool {
	return st.rec.Result == nil && (st.openSent.IsZero() || now.Sub(st.openSent) >= resend)
}

func (st *stream) openItem() protocol.TunnelItem {
	return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, Host: st.rec.Host, Port: uint64(st.rec.Port)}
}

// ackItem says how far core has the minion's bytes, and the room past.
func (st *stream) ackItem() protocol.TunnelAckItem {
	room := uint64(downWindow - len(st.down))
	return protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: st.t.task[:], Stream: st.id, Next: st.downNext, Room: &room}
}

// acked notes the ack went: the window's end as it said.
func (st *stream) acked() {
	st.ackDue = false
	st.ackedEnd = st.downNext + uint64(downWindow-len(st.down))
}

// dataItem is the stream's next tunnel_data in room bytes: what was not
// sent (from the ack again, once a send went unacked too long), up to the
// minion's room, then its eof once every byte went (the eof needs no
// room); nil for nothing, or too little room. A window shut that long with
// bytes waiting gets a probe, one byte past it, as TCP probes a zero
// window: the ack that gave room may have been lost, and the minion's
// answer to the probe says its room again.
func (st *stream) dataItem(now time.Time, room int, resend time.Duration) *protocol.TunnelDataItem {
	if st.upDone || room <= 0 {
		return nil
	}
	end := st.upBase + uint64(len(st.up))
	sent := st.upSent
	quiet := now.Sub(st.upSentAt) >= resend
	if sent > st.upBase && quiet {
		sent = st.upBase
	}
	n := 0
	if limit := min(end, st.upLimit); sent < limit {
		n = int(limit - sent)
	}
	// sent past end: the eof went already.
	eof := st.upEOF && sent <= end && sent+uint64(n) == end
	if n == 0 && !eof {
		if sent >= end || !quiet {
			return nil
		}
		n = 1
	}
	if most := min(room, protocol.MaxTunnelData); n > most {
		if most < minPiece {
			return nil
		}
		n, eof = most, false
	}
	from := int(sent - st.upBase)
	d := protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: st.t.task[:], Stream: st.id, Off: sent, EOF: eof}
	if n > 0 {
		d.Data = st.up[from : from+n]
	}
	return &d
}

// sent notes d went.
func (st *stream) sent(d protocol.TunnelDataItem, now time.Time) {
	st.upSent, st.upSentAt = d.End(), now
}

// Take takes one of the minion's tunnel items for the session (22 to 26):
// what goes back with the reply at once (a close for a stream core does not
// have); bad when the item does not read.
func (s *Service) Take(ctx context.Context, session uuid.UUID, raw cbor.RawMessage) (answers []cbor.RawMessage, bad error) {
	switch protocol.ItemType(raw) {
	case protocol.ItemTunnelReply:
		var r protocol.TunnelReplyItem
		if err := read(raw, &r); err != nil {
			return nil, err
		}
		return s.takeReply(session, r)
	case protocol.ItemTunnelData:
		var d protocol.TunnelDataItem
		if err := read(raw, &d); err != nil {
			return nil, err
		}
		return s.takeData(session, d)
	case protocol.ItemTunnelAck:
		var a protocol.TunnelAckItem
		if err := read(raw, &a); err != nil {
			return nil, err
		}
		return s.takeAck(session, a)
	case protocol.ItemTunnelClose:
		var c protocol.TunnelCloseItem
		if err := read(raw, &c); err != nil {
			return nil, err
		}
		s.takeClose(session, c)
		return nil, nil
	case protocol.ItemTunnelDatagram:
		var d protocol.TunnelDatagramItem
		if err := read(raw, &d); err != nil {
			return nil, err
		}
		// No association is open until UDP lands.
		return s.closeFor(session, d.Task, d.Stream)
	}
	return nil, fmt.Errorf("item %d is not a tunnel item the minion sends", protocol.ItemType(raw))
}

func read(raw cbor.RawMessage, v interface{ Validate() error }) error {
	if err := protocol.UnmarshalCBOR(raw, v); err != nil {
		return err
	}
	return v.Validate()
}

// find is the session's stream; the reason to close it with when core has
// none.
func (s *Service) find(session uuid.UUID, task []byte, id uint64) (*stream, int) {
	tid, err := uuid.FromBytes(task)
	if err != nil {
		return nil, protocol.TunnelCloseTaskEnded
	}
	t := s.tunnels[tid]
	if t == nil || t.session != session || t.closing {
		return nil, protocol.TunnelCloseTaskEnded
	}
	st := t.streams[id]
	if st == nil {
		return nil, protocol.TunnelCloseUnknown
	}
	return st, 0
}

// closeFor answers an item about a stream core does not have.
func (s *Service) closeFor(session uuid.UUID, task []byte, id uint64) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	st, reason := s.find(session, task, id)
	s.mu.Unlock()
	if st != nil {
		reason = protocol.TunnelCloseUnknown
	}
	return closeItem(task, id, reason)
}

func closeItem(task []byte, id uint64, reason int) ([]cbor.RawMessage, error) {
	item, err := protocol.MarshalCBOR(protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: task, Stream: id, Reason: reason})
	if err != nil {
		return nil, err
	}
	return []cbor.RawMessage{item}, nil
}

// takeReply: the minion opened a stream, or could not; a reply again for
// one open already is nothing.
func (s *Service) takeReply(session uuid.UUID, r protocol.TunnelReplyItem) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, reason := s.find(session, r.Task, r.Stream)
	if st == nil {
		if r.Result != protocol.TunnelOpen {
			return nil, nil
		}
		// Opened for a client gone (it waited too long): closed again.
		return closeItem(r.Task, r.Stream, reason)
	}
	if st.opened || st.rec.Result != nil {
		return nil, nil
	}
	result := r.Result
	st.rec.Result, st.rec.Bound, st.rec.Msg = &result, r.Bound, r.Msg
	if r.Result == protocol.TunnelOpen {
		st.opened = true
		st.upLimit = protocol.TunnelRoom(r.Room)
		st.ackedEnd = downWindow
	}
	s.record(st, false)
	st.replied <- r
	return nil, nil
}

// takeData takes the minion's bytes at core's next, within the room core
// gave. One ahead of next (an earlier one late, or lost) is kept within
// the room, and taken once the bytes before it come; what core has already
// is acked again and dropped.
func (s *Service) takeData(session uuid.UUID, d protocol.TunnelDataItem) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, reason := s.find(session, d.Task, d.Stream)
	if st == nil {
		return closeItem(d.Task, d.Stream, reason)
	}
	if !st.opened {
		return nil, nil
	}
	st.ackDue = true
	switch {
	case st.downEOF || d.End() <= st.downNext:
		return nil, nil
	case d.Off > st.downNext:
		// Its bytes within the room (its eof needs none).
		if d.Off+uint64(len(d.Data)) <= st.downNext+uint64(downWindow-len(st.down)) && st.aheadBytes+len(d.Data) <= downWindow {
			d.Data = slices.Clone(d.Data)
			st.ahead = append(st.ahead, d)
			st.aheadBytes += len(d.Data)
		}
		return nil, nil
	}
	st.take(d)
	// What came ahead, now at next.
	for moved := true; moved && !st.downEOF; {
		moved = false
		kept := st.ahead[:0]
		for _, a := range st.ahead {
			switch {
			case a.End() <= st.downNext:
				st.aheadBytes -= len(a.Data)
			case a.Off <= st.downNext:
				st.aheadBytes -= len(a.Data)
				st.take(a)
				moved = true
			default:
				kept = append(kept, a)
			}
		}
		st.ahead = kept
	}
	signal(st.downSignal)
	return nil, nil
}

// take takes d's bytes from core's next (d starts at or before it, and
// ends past it), within the room core gave.
func (st *stream) take(d protocol.TunnelDataItem) {
	data := d.Data[min(st.downNext-d.Off, uint64(len(d.Data))):]
	eof := d.EOF
	if free := downWindow - len(st.down); len(data) > free {
		// Past the room core gave: not taken.
		data, eof = data[:free], false
	}
	st.down = append(st.down, data...)
	st.downNext += uint64(len(data))
	if eof {
		st.downEOF = true
		st.downNext++
		st.ahead, st.aheadBytes = nil, 0
	}
}

// takeAck drops what the minion has of the client's bytes and takes the
// room it gives (unchanged when it says none); one below the last is
// nothing, one past what core has is not an ack of core's.
func (s *Service) takeAck(session uuid.UUID, a protocol.TunnelAckItem) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, reason := s.find(session, a.Task, a.Stream)
	if st == nil {
		return closeItem(a.Task, a.Stream, reason)
	}
	end := st.upBase + uint64(len(st.up))
	if !st.opened || st.upDone || a.Next < st.upBase || a.Next > end+1 || (a.Next == end+1 && !st.upEOF) {
		return nil, nil
	}
	room := st.upLimit - min(st.upLimit, st.upBase)
	if a.Room != nil {
		room = *a.Room
	}
	if a.Next > st.upBase {
		// Progress: the resend waits again from now.
		st.upSentAt = s.now()
	}
	if a.Next == end+1 {
		st.up, st.upBase, st.upDone = nil, end, true
	} else {
		st.up = st.up[a.Next-st.upBase:]
		st.upBase = a.Next
	}
	st.upSent = max(st.upSent, a.Next)
	st.upLimit = max(st.upLimit, a.Next+room)
	signal(st.upSignal)
	if st.upDone {
		s.doneIfOver(st)
	}
	return nil, nil
}

// takeClose: the minion ended the stream. Done, it ends once the client has
// every byte; any other reason (one core does not know reads as a reset)
// resets the client's connection at once.
func (s *Service) takeClose(session uuid.UUID, c protocol.TunnelCloseItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _ := s.find(session, c.Task, c.Stream)
	if st == nil {
		return
	}
	switch c.Reason {
	case protocol.TunnelCloseDone:
		if len(st.down) == 0 {
			s.drop(st, models.TunnelEndDone, false)
			return
		}
		// The writer drops it once the client has the rest.
		st.finishing = true
		signal(st.downSignal)
	case protocol.TunnelCloseUnknown:
		s.drop(st, models.TunnelEndLost, true)
	case protocol.TunnelCloseTaskEnded:
		s.drop(st, models.TunnelEndTaskEnded, true)
	default:
		s.drop(st, models.TunnelEndReset, true)
	}
}
