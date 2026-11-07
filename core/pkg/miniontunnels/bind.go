package miniontunnels

import (
	"net"
	"strconv"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// serveBind is a SOCKS5 BIND (docs: contracts/minion-messages/tunnels.md,
// SOCKS): the minion listens for one connection on its host (an accept),
// the client is told where (the first reply, the minion's), then who came
// (the second), and the stream is that connection's. Through a command
// that opens accept; others are told the command is not supported.
func (s *Service) serveBind(t *tunnel, c net.Conn, req request) {
	if !t.accept {
		_ = req.reply(c, socks5CommandNotSupported, "")
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
	// Who comes, within the minion's wait and its way back.
	timer.Reset(protocol.TunnelAcceptWait + s.cfg.ConnectSlack)
	select {
	case came := <-st.came:
		if err := req.reply(c, socks5Succeeded, net.JoinHostPort(came.Host, strconv.FormatUint(came.Port, 10))); err != nil {
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
		// The minion closed it: no one came.
		_ = req.reply(c, socks5GeneralFailure, "")
		return
	}
	s.done.Add(1)
	go s.writeClient(st)
	s.readClient(st)
}
