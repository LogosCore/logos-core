package miniontunnels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
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

// tunnel is a tunnel task's: its port, what core puts in front of it (its
// purpose: a SOCKS proxy, an HTTP proxy, or a forward to fwdHost:fwdPort),
// and its streams. A reverse tunnel's (reverse) has no port and no
// listener of core's: the minion listens, at bind:listen as asked and
// bound as it says, and opens its streams, each of which core connects to
// fwdHost:fwdPort (its front forward), or serves SOCKS on (socks).
type tunnel struct {
	task, session, operation uuid.UUID
	port                     int
	purpose                  string
	fwdHost                  string
	fwdPort                  int
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
	// closes go to the minion with the session's next message, and so do
	// replies, core's answers to a reverse port forward's streams.
	closes  []protocol.TunnelCloseItem
	replies []protocol.TunnelReplyItem
	reverse bool
	bind    string
	listen  int
	bound   string
	// reverseUDP: a reverse port forward whose minion listens for
	// datagrams: its streams are UDP ones, each a source's.
	reverseUDP bool
	// stopIdle stops the task once no stream has been open that long (0:
	// never); idleSince is since when none has been (zero while one is,
	// or before the clock runs), and the clock runs once core has seen the
	// minion has the task (idleArmed): from then, or a restart, on.
	stopIdle  time.Duration
	idleSince time.Time
	idleArmed bool
	stopping  bool
	// relay is its UDP relay (udp.go); nil when the port's UDP is taken, or
	// it takes none (udpRelay: its SOCKS proxy takes UDP ASSOCIATE, as its
	// command opens udp).
	relay    net.PacketConn
	udpRelay bool
	// front is a reverse tunnel's: what core does with its streams
	// (protocol.FrontForward).
	front string
	// fwdKind and fwdPath: a forward's to a unix socket or a pipe, instead
	// of fwdHost and fwdPort. source: the address of the minion's host its
	// TCP streams go from; tls: a forward's speak TLS to its destination.
	fwdKind int
	fwdPath string
	source  string
	tls     bool
	// accept: its SOCKS proxy takes BIND (its command opens accept). A
	// terminal's (purpose terminal): the program each connection runs
	// (none: the host's shell), its args, and the terminal's size on the
	// raw port.
	accept     bool
	program    string
	args       []string
	rows, cols int
	// last is the last stream's number; blockEnd the last core took for it
	// (a port's streams are numbered from blocks: nextNumber), numMu
	// numbering one at a time. opened is how many streams it has opened
	// (keptOpened, as last kept). up and down the bytes its tools sent
	// through it and got back (since a restart: what its port or listener
	// kept, and on); keptUp and keptDown what was last kept (keepBytes).
	last               uint64
	blockEnd           uint64
	numMu              sync.Mutex
	opened, keptOpened uint64
	up, down           int64
	keptUp, keptDown   int64
	// cap is its bandwidth cap, bytes a second each way (0: none); out and
	// in what it may still send the minion, and let it send (cap.go);
	// capWake: a wake is set for when the cap lets it go on.
	cap     int64
	out, in bucket
	capWake bool
	// extra is what its streams' windows grew past downWindow, together:
	// at most maxTunnelExtra (rtt.go).
	extra int
	// ended are its latest ended streams, to list (kept.go).
	ended endedStreams
	// changed: its task's view changed since the last event said so;
	// counted: its streams' numbers did (the Ports page's).
	changed bool
	counted bool
	// vpn is a VPN tunnel's (vpn.go): its routes and clients; nil for any
	// other.
	vpn *vpnTunnel
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
	purpose := rec.Purpose
	if purpose == "" {
		purpose = models.OperationalPurposeSOCKS
	}
	return &tunnel{task: rec.TaskID, session: rec.SessionID, operation: rec.OperationID, port: rec.Port, ln: ln,
		purpose: purpose, fwdHost: rec.Host, fwdPort: rec.HostPort, fwdKind: rec.Kind, fwdPath: rec.Path, source: rec.Source, tls: rec.TLS,
		stopIdle: rec.StopIdle, udpRelay: rec.UDP, cap: rec.Cap, accept: rec.Accept, program: rec.Program, args: rec.Args, rows: rec.Rows, cols: rec.Cols,
		maxStreams: maxStreams, connectWait: timeout, acked: acked, streams: map[uint64]*stream{}, handshaking: map[net.Conn]bool{},
		last: rec.Streams, blockEnd: rec.Streams, opened: rec.Opened, keptOpened: rec.Opened, up: rec.Up, down: rec.Down,
		keptUp: rec.Up, keptDown: rec.Down}
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

// stream is one of a tunnel's streams, carried by the minion: a client's
// connection to the port (a SOCKS proxy's, an HTTP proxy's, a forward's)
// or a UDP association, or a connection the minion took at a reverse
// tunnel's listener (a source's datagrams, for a UDP one).
type stream struct {
	t      *tunnel
	id     uint64
	client net.Conn
	rec    models.MinionTunnelStream
	// early: its client's bytes go with its open, before the minion's
	// reply (a forward's, a plain HTTP request's): a client that speaks
	// first waits no round trip.
	early bool
	// answer is core's reply to a stream the minion opened, said again
	// should the minion ask again.
	answer *protocol.TunnelReplyItem
	// window is the room core gives the minion past its next: downWindow,
	// grown while the minion fills it and the client keeps up (rtt.go).
	// capHeld: an ack gave none for the tunnel's cap; another goes once
	// the cap allows.
	window  int
	capHeld bool
	// rttEnd and rttAt: a round trip being measured, the offset whose ack
	// ends it and when its bytes went (zero: none).
	rttEnd uint64
	rttAt  time.Time
	// udp: a UDP association's (udp.go); nil for a TCP stream.
	udp *assoc
	// kind is what it opens past a TCP connection or a UDP association:
	// a unix socket, a pipe, a process, an accept (protocol.TunnelKind...);
	// a process's args and terminal size go with its open.
	kind       int
	args       []string
	rows, cols int
	// Its tunnel_ctl items (ctl.go): toward the minion, ctlOut from
	// ctlBase (the next the minion's ack has not covered), sent up to
	// ctlSent, last at ctlSentAt; from it, ctlIn, the next core takes.
	// came gets an accept's connection, once the minion says who.
	ctlOut    []protocol.TunnelCtlItem
	ctlBase   uint64
	ctlSent   uint64
	ctlSentAt time.Time
	ctlIn     uint64
	came      chan protocol.TunnelCtlItem

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
	// A UDP reverse port forward's stream: the source its datagrams come
	// from (where core's go back to), and when a datagram last went either
	// way.
	srcHost    string
	srcPort    int
	lastActive time.Time
	// leg is a VPN client's stream this one carries (vpn.go); nil for
	// any other.
	leg *vpnLeg
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
	switch t.purpose {
	case models.OperationalPurposeForward:
		s.serveForward(t, c)
		return
	case models.OperationalPurposeHTTP:
		s.serveHTTP(t, c)
		return
	case models.OperationalPurposeTerminal:
		s.serveTerminal(t, c)
		return
	}
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
	if req.bind {
		s.serveBind(t, c, req)
		return
	}
	if req.udp {
		s.mu.Lock()
		relayed := t.relay != nil
		s.mu.Unlock()
		if !relayed {
			_ = req.reply(c, socks5CommandNotSupported, "")
			_ = c.Close()
			return
		}
	} else if open := (protocol.TunnelItem{T: protocol.ItemTunnel, Task: t.task[:], Stream: 1, Host: req.host, Port: uint64(req.port)}); open.Validate() != nil {
		_ = req.reply(c, socks5HostUnreachable, "")
		_ = c.Close()
		return
	}
	st, code := s.newStream(t, c, req, false)
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
		bound := r.Bound
		if req.udp {
			// The tool's datagrams go to the relay, not the minion's socket.
			bound = s.relayAddress(t, c)
		}
		if err := req.reply(c, socks5Succeeded, bound); err != nil {
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
	if req.udp {
		s.holdAssociation(st)
		return
	}
	s.done.Add(1)
	go s.writeClient(st)
	s.readClient(st)
}

// serveForward is one client of a port forward: no handshake, a stream to
// the forward's destination at once, its bytes going with the open (the
// client may speak first: RDP, HTTP, SMB); what comes back flows once the
// minion has connected, and a connect that fails resets the client's
// connection, as the destination refusing it would.
func (s *Service) serveForward(t *tunnel, c net.Conn) {
	s.mu.Lock()
	delete(t.handshaking, c)
	closing := t.closing
	s.mu.Unlock()
	reset := func() {
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		_ = c.Close()
	}
	if closing {
		reset()
		return
	}
	req := request{host: t.fwdHost, port: t.fwdPort}
	if t.fwdPath != "" {
		req = request{kind: t.fwdKind, path: t.fwdPath}
	}
	st, _ := s.newStream(t, c, req, true)
	if st == nil {
		reset()
		return
	}
	// Its bytes go with the open: read on while the minion connects.
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		s.readClient(st)
	}()
	timer := time.NewTimer(t.connectWait + s.cfg.ConnectSlack)
	defer timer.Stop()
	select {
	case r := <-st.replied:
		if r.Result != protocol.TunnelOpen {
			s.mu.Lock()
			s.drop(st, models.TunnelEndNotOpened, true)
			s.mu.Unlock()
			return
		}
	case <-timer.C:
		s.mu.Lock()
		s.abort(st, models.TunnelEndNoReply)
		s.mu.Unlock()
		return
	case <-st.done:
		return
	}
	s.done.Add(1)
	go s.writeClient(st)
}

// StreamBlock is how many stream numbers core takes for a port's task at a
// time: one write for that many streams. A restart goes on past the last
// block taken, so a number is still never used twice.
const StreamBlock = 1000

// nextNumber is the tunnel's next stream number, from its block, taking
// another block when it ran out.
func (s *Service) nextNumber(t *tunnel) (uint64, error) {
	t.numMu.Lock()
	defer t.numMu.Unlock()
	s.mu.Lock()
	if t.last < t.blockEnd {
		t.last++
		id := t.last
		s.mu.Unlock()
		return id, nil
	}
	s.mu.Unlock()
	var end uint64
	var err error
	if t.vpn != nil {
		end, err = s.vpnTunnels.ReserveStreams(context.Background(), t.task, StreamBlock)
	} else {
		end, err = s.ports.ReserveStreams(context.Background(), t.task, StreamBlock)
	}
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.blockEnd = end
	t.last = max(t.last, end-min(end, StreamBlock)) + 1
	return t.last, nil
}

// newStream gives the client's connection a stream of the tunnel, to open:
// nil and the SOCKS code when it may not have one. An early one's bytes go
// with its open, within the minion's default room.
func (s *Service) newStream(t *tunnel, c net.Conn, req request, early bool) (*stream, int) {
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
	id, err := s.nextNumber(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	t.numbering--
	if err != nil || t.closing {
		if err != nil {
			s.logger.Warn("tunnels: number a stream", zap.String("task_id", t.task.String()), zap.Error(err))
		}
		return nil, socks5GeneralFailure
	}
	client := req.client
	if client == "" {
		client = c.RemoteAddr().String()
	}
	st := &stream{t: t, id: id, client: c, replied: make(chan protocol.TunnelReplyItem, 1), done: make(chan struct{}),
		upSignal: make(chan struct{}, 1), downSignal: make(chan struct{}, 1), window: downWindow, early: early,
		kind: req.kind, args: req.args, rows: req.rows, cols: req.cols, came: make(chan protocol.TunnelCtlItem, 1),
		rec: models.MinionTunnelStream{TaskID: t.task, Stream: id, Client: client, Host: req.host, Port: req.port,
			Path: req.path, Kind: req.kind, Program: req.program, OpenedAt: s.now()}}
	if req.udp {
		var ip net.IP
		if ra, ok := c.RemoteAddr().(*net.TCPAddr); ok {
			ip = ra.IP
		}
		st.udp = &assoc{clientIP: ip}
		st.rec.UDP, st.rec.Host, st.rec.Port = true, "", 0
	}
	if early {
		st.upLimit = protocol.DefaultTunnelRoom
	}
	st.rec.SearchText = models.TunnelStreamSearchText(&st.rec)
	t.streams[id] = st
	t.opened++
	t.streamsChanged(s.now())
	t.last, t.changed, t.counted = max(t.last, id), true, true
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
			st.t.up += int64(k)
			st.t.changed = true
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
		st.t.down += int64(len(piece))
		st.t.changed = true
		// Room given back: said once it is worth an ack, or more of it
		// once the window holds the stream back.
		wake := st.roomGrew()
		if len(st.down) == 0 && st.grow() {
			wake = true
		}
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
	room := uint64(max(st.window-len(st.down), 0))
	end := st.downNext + room
	if end >= st.ackedEnd+uint64(st.window/4) {
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
	if st.t.reverse || !st.openSent.IsZero() {
		st.t.closes = append(st.t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: st.t.task[:], Stream: st.id,
			Reason: protocol.TunnelCloseReset})
		s.wake(st.t.session)
	}
	s.drop(st, end, true)
}

// doneIfOver drops the stream once both directions ended and each side
// has the other's eof, telling the minion.
func (s *Service) doneIfOver(st *stream) {
	if st.gone || !st.upDone || !st.downShut || len(st.ctlOut) > 0 {
		// A stream ends done once its ctls are acked too.
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
	st.shrink()
	st.t.streamsChanged(s.now())
	if st.client != nil {
		if reset {
			if tc, ok := st.client.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
		}
		_ = st.client.Close()
	}
	now := s.now()
	st.rec.EndedAt, st.rec.End = &now, end
	st.t.changed, st.t.counted = true, true
	rec := st.rec
	rec.Up, rec.Down = st.upBytes, st.downBytes
	st.t.ended.add(rec)
}

// Fill adds to a message for the session's minion, through add, what its
// tunnels have to send, in room bytes and most items: replies, closes,
// opens, acks, then data, a stream's item in turn while room lasts, an
// early stream's with its open, and no tunnel past its cap. It says how
// many it added, and how soon a minion that polls should come back; polls
// false: no tunnel of the session's runs. When something is to go again
// unasked (an open, bytes the minion did not ack), it has the session
// woken then.
func (s *Service) Fill(ctx context.Context, session uuid.UUID, room, most int, add func(cbor.RawMessage) bool) (added int, poll time.Duration, polls bool) {
	s.checkAcks(ctx, session)
	s.mu.Lock()
	defer s.mu.Unlock()
	tunnels := s.ofSession(session)
	if len(tunnels) == 0 {
		return 0, 0, false
	}
	now := s.now()
	resend := s.resendAfter(session)
	f := &filler{room: room, most: most, add: add}
	work := false
	for _, t := range tunnels {
		// A reply before the stream's bytes, and before a close of it.
		for len(t.replies) > 0 && f.put(t.replies[0]) {
			t.replies = t.replies[1:]
			work = true
		}
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
			// The minion's own streams open from its side: core answers
			// once its connect ends.
			if !t.reverse && t.acked && st.openDue(now, resend) && f.put(st.openItem()) {
				st.openSent, work = now, true
			}
			// An early stream's bytes go once its open went.
			if st.early && !st.openSent.IsZero() && st.rec.Result == nil {
				open = append(open, st)
			}
		}
	}
	for _, st := range open {
		if st.opened && st.ackDue {
			if a := st.ackItem(now); f.put(a) {
				st.acked(*a.Room)
			}
			if st.capHeld {
				// No room for the cap: another ack once it allows.
				s.capLater(st.t, now)
			}
		}
		// Its ctls before its bytes: a size, a signal.
		if st.putCtls(f, now, resend) {
			work = true
		}
	}
	if n := len(open); n > 0 {
		start := s.rotation[session] % n
		s.rotation[session]++
		for moved := true; moved; {
			moved = false
			for i := range n {
				st := open[(start+i)%n]
				t := st.t
				if st.udp != nil {
					if len(st.udp.out) == 0 {
						continue
					}
					if size := len(st.udp.out[0].Data); t.outAllows(now) < size {
						// Over the tunnel's cap: dropped, as UDP is.
						st.sentDatagram()
						continue
					}
					if f.put(st.udp.out[0]) {
						t.sentOut(len(st.udp.out[0].Data))
						st.sentDatagram()
						moved, work = true, true
						continue
					}
					if f.n == 0 && f.most > 0 {
						// It fits no message: dropped, as UDP is.
						st.sentDatagram()
						continue
					}
					break
				}
				allows := t.outAllows(now)
				if allows < minPiece {
					// Over the tunnel's cap: its bytes wait.
					s.capLater(t, now)
				}
				d := st.dataItem(now, min(f.dataRoom(), allows), resend)
				if d == nil {
					continue
				}
				if !f.put(*d) {
					break
				}
				st.measure(d.End(), d.Off < st.upSent, now)
				st.sent(*d, now)
				t.sentOut(len(d.Data))
				moved, work = true, true
			}
		}
	}
	pending := false
	var due time.Time
	for _, t := range tunnels {
		pending = pending || len(t.closes) > 0 || len(t.replies) > 0
		allows := t.outAllows(now)
		for _, st := range t.streams {
			early := st.early && !st.opened && !st.openSent.IsZero() && st.rec.Result == nil
			pending = pending || (!t.reverse && t.acked && !st.opened && st.openDue(now, resend)) ||
				(st.opened && st.udp != nil && len(st.udp.out) > 0) ||
				(st.opened && st.udp == nil && st.ackDue) || st.ctlDue(now, resend) ||
				((st.opened || early) && st.udp == nil && allows >= minPiece && st.dataItem(now, protocol.MaxTunnelData, resend) != nil)
			if at := st.dueAt(resend); !at.IsZero() && (due.IsZero() || at.Before(due)) {
				due = at
			}
		}
	}
	if pending && f.n > 0 {
		// More than one message holds: the next goes as soon as it may.
		s.wake(session)
	}
	if !due.IsZero() {
		s.wakeAt(session, due)
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
	if st.kind == protocol.TunnelKindICMP {
		// A VPN client's pings: each datagram names its host.
		return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, Kind: st.kind}
	}
	if st.udp != nil {
		return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, UDP: true}
	}
	switch st.kind {
	case protocol.TunnelKindUnix, protocol.TunnelKindPipe:
		return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, Kind: st.kind, Path: st.rec.Path}
	case protocol.TunnelKindProcess:
		return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, Kind: st.kind, Path: st.rec.Program,
			Args: st.args, Rows: uint64(st.rows), Cols: uint64(st.cols)}
	case protocol.TunnelKindAccept:
		// SOCKS BIND: on every address of the minion's host, any free port.
		return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, Kind: st.kind, Host: "0.0.0.0"}
	}
	// A TCP stream: from the task's source address, TLS on top for a
	// forward that says it.
	return protocol.TunnelItem{T: protocol.ItemTunnel, Task: st.t.task[:], Stream: st.id, Host: st.rec.Host, Port: uint64(st.rec.Port),
		Source: st.t.source, TLS: st.t.tls}
}

// ackItem says how far core has the minion's bytes, and the room past: no
// more than the tunnel's cap allows now, none while it is over it (another
// ack gives more once the cap allows).
func (st *stream) ackItem(now time.Time) protocol.TunnelAckItem {
	room := uint64(max(st.window-len(st.down), 0))
	if allows := st.t.inAllowsNow(now); allows < room {
		room, st.capHeld = allows, true
	}
	return protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: st.t.task[:], Stream: st.id, Next: st.downNext, Room: &room, Ctl: st.ctlIn}
}

// acked notes the ack went: the window's end as it said.
func (st *stream) acked(room uint64) {
	st.ackDue = false
	st.ackedEnd = st.downNext + room
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

// Take takes one of the minion's tunnel items for the session (21 to 28):
// what goes back with the reply at once (a close for a stream core does not
// have); bad when the item does not read.
func (s *Service) Take(ctx context.Context, session uuid.UUID, raw cbor.RawMessage) (answers []cbor.RawMessage, bad error) {
	switch protocol.ItemType(raw) {
	case protocol.ItemTunnel:
		// Read strictly: an open core does not take as it is gets
		// unsupported, never a connect it misread.
		it, err := protocol.ReadTunnel(raw)
		if err != nil {
			if !it.Answerable() {
				return nil, err
			}
			return s.refuseOpen(session, it, err)
		}
		return s.takeOpen(session, it)
	case protocol.ItemTunnelListen:
		var l protocol.TunnelListenItem
		if err := read(raw, &l); err != nil {
			return nil, err
		}
		if s.listeners != nil {
			s.takeListen(ctx, session, l)
		}
		return nil, nil
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
		return s.takeDatagram(session, d)
	case protocol.ItemTunnelCtl:
		var c protocol.TunnelCtlItem
		if err := read(raw, &c); err != nil {
			return nil, err
		}
		return s.takeCtl(session, c)
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
	// A reverse port forward's streams are the minion's: core answers
	// them, and takes no reply.
	if st.t.reverse || st.opened || st.rec.Result != nil {
		return nil, nil
	}
	result := r.Result
	st.rec.Result, st.rec.Bound, st.rec.Msg = &result, r.Bound, r.Msg
	if st.kind == protocol.TunnelKindProcess && r.Result == protocol.TunnelOpen {
		terminal := r.Terminal
		st.rec.Terminal = &terminal
	}
	if r.Result == protocol.TunnelOpen {
		st.opened = true
		// An early stream's bytes went within the default room already.
		st.upLimit = max(st.upLimit, protocol.TunnelRoom(r.Room))
		st.ackedEnd = uint64(st.window)
	}
	st.t.changed = true
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
		if d.Off+uint64(len(d.Data)) <= st.downNext+uint64(max(st.window-len(st.down), 0)) && st.aheadBytes+len(d.Data) <= st.window {
			d.Data = slices.Clone(d.Data)
			st.ahead = append(st.ahead, d)
			st.aheadBytes += len(d.Data)
		}
		return nil, nil
	}
	before := st.downNext
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
	st.t.tookIn(int(st.downNext-before), s.now())
	signal(st.downSignal)
	return nil, nil
}

// take takes d's bytes from core's next (d starts at or before it, and
// ends past it), within the room core gave.
func (st *stream) take(d protocol.TunnelDataItem) {
	data := d.Data[min(st.downNext-d.Off, uint64(len(d.Data))):]
	eof := d.EOF
	if free := max(st.window-len(st.down), 0); len(data) > free {
		// Past the room core gave: not taken.
		data, eof = data[:free], false
	}
	st.down = append(st.down, data...)
	st.downNext += uint64(len(data))
	if eof {
		st.downEOF = true
		st.downNext++
		st.ahead, st.aheadBytes = nil, 0
		if st.kind == protocol.TunnelKindProcess && st.rec.Exit != nil {
			// The terminal says how its process ended, as its last line.
			st.down = append(st.down, exitNote(*st.rec.Exit)...)
		}
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
	if st.ctlAcked(a.Ctl) && st.upDone {
		// Its last ctl acked: it may be over.
		s.doneIfOver(st)
		return nil, nil
	}
	end := st.upBase + uint64(len(st.up))
	if !(st.opened || st.early) || st.upDone || a.Next < st.upBase || a.Next > end+1 || (a.Next == end+1 && !st.upEOF) {
		return nil, nil
	}
	s.measured(st, a.Next)
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
