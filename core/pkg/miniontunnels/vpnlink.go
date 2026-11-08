package miniontunnels

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// The VPN link (docs: contracts/vpn-front, The link): a WebSocket of a
// client's. Text frames are JSON (protocol.VPNControl): core's welcome,
// sent by its controller, the routes when they change, end; the client's
// bye. Binary frames are tunnel items (protocol.VPNFrame): the client's
// opens, bytes, acks, closes and datagrams; core's replies, closes, acks,
// datagrams and bytes. The link is reliable while it lasts: nothing is sent
// again but after a reconnect, from the client's last ack.

// VPNLink is one link of a client's: its frames each way, and its close
// with a code (protocol.VPNCloseRefused, ...; 1000 normal) and why.
type VPNLink interface {
	// Read is the client's next frame: text (a control) or binary (items).
	Read() (text bool, b []byte, err error)
	Write(text bool, b []byte) error
	Close(code int, why string)
}

// vpnLinkState is a link being served: its writer's kick, and its end.
type vpnLinkState struct {
	l    VPNLink
	kick chan struct{}
	done chan struct{}
	once sync.Once
}

// stop ends the link's writer.
func (ls *vpnLinkState) stop() {
	ls.once.Do(func() { close(ls.done) })
}

// end says why core ends the client, and closes the link.
func (ls *vpnLinkState) end(why string) {
	ls.stop()
	if b, err := json.Marshal(protocol.VPNControl{End: why}); err == nil {
		_ = ls.l.Write(true, b)
	}
	ls.l.Close(1000, clipText(why, 120))
}

// close closes the link (core stops: the client connects again).
func (ls *vpnLinkState) close(code int, why string) {
	ls.stop()
	ls.l.Close(code, why)
}

// Serve carries the client over a link until it fails, after core sent its
// welcome: what the link before may have lost goes again (bytes from the
// client's last ack, acks). The client is kept protocol.VPNLinkGrace after
// the link fails, for another.
func (c *VPNClient) Serve(l VPNLink) {
	s := c.s
	ls := &vpnLinkState{l: l, kick: make(chan struct{}, 1), done: make(chan struct{})}
	s.mu.Lock()
	if c.ended {
		// Ended between its welcome and now: told why.
		why := c.endWhy
		s.mu.Unlock()
		ls.end(why)
		return
	}
	old := c.link
	c.link, c.droppedAt = ls, time.Time{}
	for _, g := range c.legs {
		g.outSent = g.outBase
		if g.open && !g.udp && !g.icmp {
			g.ackDue = true
		}
	}
	c.t.changed = true
	s.mu.Unlock()
	if old != nil {
		old.close(1000, "the client connected again")
	}
	signal(ls.kick)
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		c.write(ls)
	}()
	c.read(ls)
	ls.stop()
	s.mu.Lock()
	if c.link == ls {
		c.link, c.droppedAt, c.t.changed = nil, s.now(), true
		s.logger.Info("tunnels: a VPN client's link dropped", zap.String("task_id", c.t.task.String()), zap.String("client", c.name))
	}
	s.mu.Unlock()
}

// read takes the client's frames until the link fails, or it says bye.
func (c *VPNClient) read(ls *vpnLinkState) {
	s := c.s
	for {
		text, b, err := ls.l.Read()
		if err != nil {
			return
		}
		if text {
			var ctl protocol.VPNControl
			if json.Unmarshal(b, &ctl) == nil && ctl.Bye {
				s.mu.Lock()
				after := s.endClient(c, "")
				s.mu.Unlock()
				after()
				return
			}
			continue
		}
		items, err := protocol.ReadVPNFrame(b)
		if err != nil {
			s.logger.Debug("tunnels: a VPN client's frame does not read", zap.String("client", c.name), zap.Error(err))
			continue
		}
		s.mu.Lock()
		if c.ended || c.link != ls {
			s.mu.Unlock()
			return
		}
		for _, raw := range items {
			s.takeLeg(c, raw)
		}
		s.mu.Unlock()
	}
}

// takeLeg takes one of the client's items (s.mu held). One that does not
// read is dropped; one for a stream core does not have gets a close (a
// close none), one for another task's, a close saying so.
func (s *Service) takeLeg(c *VPNClient, raw cbor.RawMessage) {
	switch protocol.ItemType(raw) {
	case protocol.ItemTunnel:
		it, err := protocol.ReadTunnel(raw)
		if err != nil {
			if it.Answerable() && c.ours(it.Task) {
				c.answer(it.Stream, protocol.TunnelUnsupported, err.Error())
			}
			return
		}
		if !c.ours(it.Task) {
			c.closeLeg(it.Stream, protocol.TunnelCloseTaskEnded)
			return
		}
		s.takeLegOpen(c, it)
	case protocol.ItemTunnelData:
		var d protocol.TunnelDataItem
		if read(raw, &d) != nil {
			return
		}
		if g := c.leg(d.Task, d.Stream); g != nil {
			s.takeLegData(g, d)
		}
	case protocol.ItemTunnelAck:
		var a protocol.TunnelAckItem
		if read(raw, &a) != nil {
			return
		}
		if g := c.leg(a.Task, a.Stream); g != nil {
			s.takeLegAck(g, a)
		}
	case protocol.ItemTunnelClose:
		var cl protocol.TunnelCloseItem
		if read(raw, &cl) != nil || !c.ours(cl.Task) {
			return
		}
		if g := c.legs[cl.Stream]; g != nil {
			s.takeLegClose(g, cl)
		}
	case protocol.ItemTunnelDatagram:
		var d protocol.TunnelDatagramItem
		if read(raw, &d) != nil {
			return
		}
		if g := c.leg(d.Task, d.Stream); g != nil {
			s.takeLegDatagram(g, d)
		}
	}
}

// leg is the client's stream an item names: nil, with a close sent, when
// core has none.
func (c *VPNClient) leg(task []byte, id uint64) *vpnLeg {
	if !c.ours(task) {
		c.closeLeg(id, protocol.TunnelCloseTaskEnded)
		return nil
	}
	g := c.legs[id]
	if g == nil {
		c.closeLeg(id, protocol.TunnelCloseUnknown)
	}
	return g
}

// write sends the client what it has, a frame at a time, until the link
// ends.
func (c *VPNClient) write(ls *vpnLinkState) {
	s := c.s
	for {
		select {
		case <-ls.kick:
		case <-ls.done:
			return
		}
		for {
			s.mu.Lock()
			if c.link != ls {
				s.mu.Unlock()
				return
			}
			control, frame := c.nextFrame()
			s.mu.Unlock()
			if control == nil && frame == nil {
				break
			}
			if control != nil && ls.l.Write(true, control) != nil {
				ls.close(1011, "the link failed")
				return
			}
			if frame != nil && ls.l.Write(false, frame) != nil {
				ls.close(1011, "the link failed")
				return
			}
		}
	}
}

// nextFrame is what goes to the client next (s.mu held): the routes, when
// they changed, and a frame of items: replies, closes, acks, datagrams,
// then each stream's bytes in turn while the frame has room. Nil for
// nothing.
func (c *VPNClient) nextFrame() (control, frame []byte) {
	if c.routesDue {
		c.routesDue = false
		control, _ = json.Marshal(protocol.VPNControl{Routes: c.t.vpn.text})
	}
	f := &frameFill{room: vpnFrameData}
	for len(c.replies) > 0 && f.put(c.replies[0]) {
		c.replies = c.replies[1:]
	}
	for len(c.closes) > 0 && f.put(c.closes[0]) {
		c.closes = c.closes[1:]
	}
	legs := c.sortedLegs()
	for _, g := range legs {
		if !g.ackDue || !g.open {
			continue
		}
		room := uint64(max(vpnWindow-len(g.inBuf), 0))
		if g.inEOF {
			room = 0
		}
		if !f.put(protocol.TunnelAckItem{T: protocol.ItemTunnelAck, Task: c.t.task[:], Stream: g.id, Next: g.inNext, Room: &room}) {
			break
		}
		g.ackDue, g.ackedEnd = false, g.inNext+room
	}
	for len(c.dgrams) > 0 && f.put(c.dgrams[0]) {
		c.dgramBytes -= len(c.dgrams[0].Data)
		c.dgrams = c.dgrams[1:]
	}
	if n := len(legs); n > 0 {
		start := c.rotation % n
		c.rotation++
		for moved := true; moved; {
			moved = false
			for i := range n {
				g := legs[(start+i)%n]
				d := g.dataItem(min(f.dataRoom(), protocol.MaxTunnelData))
				if d == nil {
					continue
				}
				if !f.put(*d) {
					break
				}
				g.outSent = d.End()
				moved = true
			}
		}
	}
	if len(f.items) > 0 {
		var err error
		if frame, err = protocol.VPNFrame(f.items); err != nil {
			c.s.logger.Warn("tunnels: a VPN frame", zap.Error(err))
			frame = nil
		}
	}
	more := len(c.replies)+len(c.closes)+len(c.dgrams) > 0 || f.full
	for _, g := range legs {
		more = more || (g.ackDue && g.open) || g.dataItem(protocol.MaxTunnelData) != nil
	}
	if more && len(f.items) > 0 {
		// More than one frame holds: the writer comes back.
		c.kick()
	}
	return control, frame
}

// dataItem is the stream's next tunnel_data in room bytes: what was not
// sent yet, up to the client's room, then the minion's eof; nil for none.
func (g *vpnLeg) dataItem(room int) *protocol.TunnelDataItem {
	if !g.open || g.udp || g.icmp || g.outDone || room <= 0 {
		return nil
	}
	end := g.outBase + uint64(len(g.outBuf))
	sent := g.outSent
	n := 0
	if limit := min(end, g.outLimit); sent < limit {
		n = int(limit - sent)
	}
	eof := g.outEOF && sent <= end && sent+uint64(n) == end
	if n == 0 && !eof {
		return nil
	}
	if n > room {
		if room < minPiece {
			return nil
		}
		n, eof = room, false
	}
	d := protocol.TunnelDataItem{T: protocol.ItemTunnelData, Task: g.c.t.task[:], Stream: g.id, Off: sent, EOF: eof}
	if n > 0 {
		from := int(sent - g.outBase)
		d.Data = g.outBuf[from : from+n]
	}
	return &d
}

// frameFill is one frame's room for items.
type frameFill struct {
	items []cbor.RawMessage
	room  int
	full  bool
}

// put adds v's item when it fits.
func (f *frameFill) put(v any) bool {
	if len(f.items) >= protocol.MaxVPNFrameItems {
		f.full = true
		return false
	}
	item, err := protocol.MarshalCBOR(v)
	if err != nil {
		return false
	}
	if len(item)+itemOverhead > f.room {
		f.full = true
		return false
	}
	f.room -= len(item) + itemOverhead
	f.items = append(f.items, item)
	return true
}

// dataRoom is how many bytes a tunnel_data may carry in what is left.
func (f *frameFill) dataRoom() int {
	if len(f.items) >= protocol.MaxVPNFrameItems {
		return 0
	}
	return max(f.room-itemOverhead-dataHeader, 0)
}
