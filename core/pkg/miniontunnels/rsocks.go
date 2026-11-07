package miniontunnels

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Reverse SOCKS (docs: contracts/minion-messages/tunnels.md, Reverse
// SOCKS): a reverse tunnel whose front is socks. Each connection to the
// minion's listener is a stream, as a reverse port forward's; core takes
// it at once, and speaks SOCKS5 on its end of it: the client says where to
// go, behind the user and password core made for the task (anyone else on
// the minion's network who finds the port gets nothing), and core connects
// there from its own network. The minion never sees SOCKS, nor where its
// streams go. Core's end of each stream is one end of a pipe in core; the
// SOCKS server and its connection hold the other.

// ReverseSOCKSUser is the user every reverse SOCKS proxy takes, its
// password the task's own.
const ReverseSOCKSUser = "logos"

// newPassword is a reverse SOCKS proxy's password: 24 hex digits.
func newPassword() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// socksAuth is the user and password a SOCKS5 client must give.
type socksAuth struct{ user, password string }

// ok: the client gave them.
func (a *socksAuth) ok(user, password []byte) bool {
	u := subtle.ConstantTimeCompare(user, []byte(a.user))
	p := subtle.ConstantTimeCompare(password, []byte(a.password))
	return u&p == 1
}

// takeSOCKS takes a reverse SOCKS proxy's stream: core opens it at once,
// and serves SOCKS on its end (room: what the minion takes past an ack).
func (s *Service) takeSOCKS(st *stream, room uint64) {
	defer s.done.Done()
	t := st.t
	s.seen(t.task, st.id)
	ours, theirs := newPipe()
	s.mu.Lock()
	if st.gone {
		s.mu.Unlock()
		_ = ours.Close()
		return
	}
	st.client, st.opened = ours, true
	st.upLimit, st.ackedEnd = room, uint64(st.window)
	open := protocol.TunnelReplyItem{T: protocol.ItemTunnelReply, Task: t.task[:], Stream: st.id, Result: protocol.TunnelOpen}
	st.answer = &open
	t.replies = append(t.replies, open)
	t.changed = true
	session, auth, wait := t.session, &socksAuth{user: t.user, password: t.password}, t.connectWait
	s.mu.Unlock()
	s.wake(session)
	s.done.Add(2)
	go s.writeClient(st)
	go func() {
		defer s.done.Done()
		s.readClient(st)
	}()
	s.serveReverseSOCKS(st, theirs, auth, wait)
}

// serveReverseSOCKS is the SOCKS5 server on core's end of a reverse SOCKS
// stream: the client's greeting, its user and password, its CONNECT, then
// its bytes, both ways, with the connection core made. It records the
// destination, and core's connect's result, on the stream.
func (s *Service) serveReverseSOCKS(st *stream, c *pipeEnd, auth *socksAuth, wait time.Duration) {
	defer func() { _ = c.Close() }()
	// A client that says nothing in time is closed.
	hold := time.AfterFunc(s.cfg.HandshakeWait, func() { _ = c.Close() })
	req, err := readRequest(c, auth)
	hold.Stop()
	if err != nil {
		return
	}
	if req.udp {
		_ = req.reply(c, socks5CommandNotSupported, "")
		return
	}
	s.mu.Lock()
	st.rec.Host, st.rec.Port = req.host, req.port
	st.rec.SearchText = models.TunnelStreamSearchText(&st.rec)
	st.t.changed = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	go func() {
		select {
		case <-st.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	var d net.Dialer
	dst, err := d.DialContext(ctx, "tcp", net.JoinHostPort(req.host, strconv.Itoa(req.port)))
	result := dialResult(err)
	s.mu.Lock()
	st.rec.Result = &result
	if err != nil {
		st.rec.Msg = clipText(err.Error(), protocol.MaxTunnelMsg)
	} else {
		st.rec.Bound = dst.LocalAddr().String()
	}
	st.t.changed = true
	s.mu.Unlock()
	// The reply says no address of core's.
	if err := req.reply(c, socksCode(result), ""); err != nil || dst == nil {
		if dst != nil {
			_ = dst.Close()
		}
		return
	}
	defer func() { _ = dst.Close() }()
	relay(c, dst)
}

// relay carries a's bytes to b and b's to a, each end's eof the other's
// half close, until both ended (or one failed).
func relay(a *pipeEnd, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(b, a); err == nil {
			if cw, ok := b.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
		} else {
			_ = b.Close()
		}
	}()
	if _, err := io.Copy(a, b); err == nil {
		_ = a.CloseWrite()
	} else {
		_ = a.Close()
	}
	wg.Wait()
}

// pipeEnd is one end of a connection in core's process: what one end
// writes the other reads, a CloseWrite ending the other's reads (an eof,
// as TCP's half close), a Close both ways.
type pipeEnd struct {
	r *io.PipeReader
	w *io.PipeWriter
}

// newPipe is a connection's two ends.
func newPipe() (a, b *pipeEnd) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	return &pipeEnd{r: ar, w: aw}, &pipeEnd{r: br, w: bw}
}

func (p *pipeEnd) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if errors.Is(err, io.ErrClosedPipe) {
		err = net.ErrClosed
	}
	return n, err
}

func (p *pipeEnd) Write(b []byte) (int, error) { return p.w.Write(b) }

// CloseWrite ends what this end sends: the other reads an eof.
func (p *pipeEnd) CloseWrite() error { return p.w.Close() }

// Close ends both ways: the other end's writes fail, and its reads end.
func (p *pipeEnd) Close() error {
	_ = p.w.Close()
	return p.r.CloseWithError(io.ErrClosedPipe)
}

func (p *pipeEnd) LocalAddr() net.Addr                { return pipeAddr{} }
func (p *pipeEnd) RemoteAddr() net.Addr               { return pipeAddr{} }
func (p *pipeEnd) SetDeadline(_ time.Time) error      { return nil }
func (p *pipeEnd) SetReadDeadline(_ time.Time) error  { return nil }
func (p *pipeEnd) SetWriteDeadline(_ time.Time) error { return nil }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "core" }
