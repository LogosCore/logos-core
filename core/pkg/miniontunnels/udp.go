package miniontunnels

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strconv"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// UDP associations (docs: contracts/minion-messages/tunnels.md, UDP): a
// SOCKS5 UDP ASSOCIATE is a stream the minion opens as a UDP socket (its
// tunnel item says udp, and no destination). The tool sends its datagrams
// to the tunnel's relay, a UDP socket on the same port number, each with
// the SOCKS UDP header naming where it goes; core sends them to the minion
// as tunnel_datagram items, and the minion's (each saying where it came
// from) back to the tool with the header. As UDP is: no acks, no resending,
// a datagram that finds the queue full dropped. The association lasts as
// long as the tool's SOCKS connection.
//
// A datagram is the association's whose tool sent it: the first that comes
// from the tool's address (its SOCKS connection's) binds the association to
// its source, port and all. What the request says the tool sends from is
// not checked: NAT, and Docker's proxy for a published port, change it.

// udpQueue is the most of an association's datagrams core holds for the
// minion; past it they are dropped.
const udpQueue = 256 << 10

// assoc is a UDP association's own: whose it is, what waits for the
// minion.
type assoc struct {
	// clientIP is the tool's (its SOCKS connection's); from, where its
	// datagrams come from, once one came.
	clientIP net.IP
	from     *net.UDPAddr
	// out are the tool's datagrams for the minion, outBytes their payloads;
	// seq numbers the next one.
	out      []protocol.TunnelDatagramItem
	outBytes int
	seq      uint64
	// in: the minion's datagrams taken, by their numbers.
	in replayWindow
}

// replayWindow takes each of a sequence's numbers once, as a message sent
// again repeats them: the latest, and the 64 before it.
type replayWindow struct {
	started bool
	last    uint64
	seen    uint64
}

// take says whether seq is new, and notes it.
func (w *replayWindow) take(seq uint64) bool {
	switch {
	case !w.started:
		w.started, w.last, w.seen = true, seq, 1
		return true
	case seq > w.last:
		if shift := seq - w.last; shift < 64 {
			w.seen <<= shift
		} else {
			w.seen = 0
		}
		w.last, w.seen = seq, w.seen|1
		return true
	case w.last-seq >= 64:
		return false
	}
	bit := uint64(1) << (w.last - seq)
	if w.seen&bit != 0 {
		return false
	}
	w.seen |= bit
	return true
}

// openRelay opens the tunnel's UDP relay on its port number; a port whose
// UDP is taken leaves the tunnel without one (its UDP ASSOCIATEs are
// refused).
func (s *Service) openRelay(t *tunnel) {
	pc, err := net.ListenPacket("udp", net.JoinHostPort(s.cfg.Bind, strconv.Itoa(t.port)))
	if err != nil {
		s.logger.Warn("tunnels: the UDP relay does not open: no UDP through this port", zap.Int("port", t.port), zap.Error(err))
		return
	}
	s.mu.Lock()
	t.relay = pc
	s.mu.Unlock()
	s.done.Add(1)
	go s.relay(t, pc)
}

// relay reads what the tools send the tunnel's relay: each datagram is
// queued for its association's minion.
func (s *Service) relay(t *tunnel, pc net.PacketConn) {
	defer s.done.Done()
	buf := make([]byte, 64<<10+512)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		host, port, data, ok := readDatagram(buf[:n])
		ua, isUDP := from.(*net.UDPAddr)
		if !ok || !isUDP {
			continue
		}
		s.mu.Lock()
		st := t.assocOf(ua)
		queued := st != nil && st.queueDatagram(host, port, data)
		s.mu.Unlock()
		if queued {
			s.wake(t.session)
		}
	}
}

// assocOf is the association a datagram from addr is for: the one bound to
// addr, else the oldest of its tool's address not bound yet, which it binds.
func (t *tunnel) assocOf(addr *net.UDPAddr) *stream {
	var free *stream
	for _, st := range t.streams {
		a := st.udp
		if a == nil || !st.opened || st.gone {
			continue
		}
		if a.from != nil {
			if a.from.IP.Equal(addr.IP) && a.from.Port == addr.Port {
				return st
			}
			continue
		}
		if a.clientIP.Equal(addr.IP) && (free == nil || st.id < free.id) {
			free = st
		}
	}
	if free != nil {
		free.udp.from = addr
	}
	return free
}

// queueDatagram queues a tool's datagram for the minion: false when it is
// dropped (its queue full, a destination that does not read).
func (st *stream) queueDatagram(host string, port int, data []byte) bool {
	a := st.udp
	d := protocol.TunnelDatagramItem{T: protocol.ItemTunnelDatagram, Task: st.t.task[:], Stream: st.id, Seq: a.seq, Host: host,
		Port: uint64(port), Data: bytes.Clone(data)}
	if d.Validate() != nil || a.outBytes+len(data) > udpQueue {
		return false
	}
	a.seq++
	a.out = append(a.out, d)
	a.outBytes += len(data)
	st.upBytes += int64(len(data))
	st.t.up += int64(len(data))
	st.t.changed = true
	return true
}

// sentDatagram takes the first datagram off the association's queue: sent,
// or dropped.
func (st *stream) sentDatagram() {
	a := st.udp
	a.outBytes -= len(a.out[0].Data)
	a.out[0] = protocol.TunnelDatagramItem{}
	a.out = a.out[1:]
}

// takeDatagram sends the tool a datagram the minion's socket got, with the
// header saying where it came from; one taken already, or before the tool
// sent one (core does not know where it is), goes nowhere.
func (s *Service) takeDatagram(session uuid.UUID, d protocol.TunnelDatagramItem) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	st, reason := s.find(session, d.Task, d.Stream)
	if st == nil {
		s.mu.Unlock()
		return closeItem(d.Task, d.Stream, reason)
	}
	if st.t.reverse {
		s.takeReverseDatagram(st, d)
		return nil, nil
	}
	if st.leg != nil {
		// A VPN client's: it goes to the client, within the cap.
		if a := st.udp; a != nil && st.opened && a.in.take(d.Seq) && st.t.inAllows(len(d.Data), s.now()) {
			s.vpnDatagram(st, d)
		}
		s.mu.Unlock()
		return nil, nil
	}
	a := st.udp
	if a == nil || !st.opened || !a.in.take(d.Seq) || a.from == nil || st.t.relay == nil || !st.t.inAllows(len(d.Data), s.now()) {
		s.mu.Unlock()
		return nil, nil
	}
	pkt, to, pc := datagram(d.Host, int(d.Port), d.Data), a.from, st.t.relay
	st.downBytes += int64(len(d.Data))
	st.t.down += int64(len(d.Data))
	st.t.changed = true
	s.mu.Unlock()
	_, _ = pc.WriteTo(pkt, to)
	return nil, nil
}

// relayAddress is where the tool sends its datagrams, as the reply to its
// UDP ASSOCIATE says: core's address when it is an IP, else the one the
// tool reached core at, and the tunnel's port. (Tools such as PySocks send
// to the proxy's host they know, and the reply's port.)
func (s *Service) relayAddress(t *tunnel, c net.Conn) string {
	ip := net.ParseIP(s.cfg.Address)
	if ip == nil {
		if la, ok := c.LocalAddr().(*net.TCPAddr); ok {
			ip = la.IP
		}
	}
	if ip == nil {
		ip = net.IPv4zero
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(t.port))
}

// holdAssociation keeps a UDP association while the tool's SOCKS
// connection lasts; when it closes, the minion is told and its socket goes.
func (s *Service) holdAssociation(st *stream) {
	_, _ = io.Copy(io.Discard, st.client)
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.gone {
		return
	}
	st.t.closes = append(st.t.closes, protocol.TunnelCloseItem{T: protocol.ItemTunnelClose, Task: st.t.task[:], Stream: st.id,
		Reason: protocol.TunnelCloseDone})
	s.wake(st.t.session)
	s.drop(st, models.TunnelEndDone, false)
}
