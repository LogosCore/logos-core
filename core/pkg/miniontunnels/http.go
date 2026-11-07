package miniontunnels

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Core's HTTP front (docs: contracts/minion-messages/tunnels.md, HTTP
// proxy): an HTTP proxy on the task's port, for the tools that speak no
// SOCKS. CONNECT host:port (HTTPS, and whatever else asks for a tunnel) is
// a stream to host:port, answered 200 once the minion opened it; a plain
// request (GET http://host/path) is a stream to its host, the request sent
// on with its target a path, its proxy headers dropped and Connection:
// close (a connection carries one request: a client opens another for
// the next), its bytes going with the stream's open. No credentials, as
// SOCKS: a port is as private as the network in front of it.

// serveHTTP is one client of an HTTP proxy.
func (s *Service) serveHTTP(t *tunnel, c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(s.cfg.HandshakeWait))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	_ = c.SetDeadline(time.Time{})
	s.mu.Lock()
	delete(t.handshaking, c)
	closing := t.closing
	s.mu.Unlock()
	if closing {
		_ = c.Close()
		return
	}
	if err != nil {
		if !errors.Is(err, io.EOF) {
			writeHTTPError(c, http.StatusBadRequest, "not an HTTP request a proxy takes")
		}
		_ = c.Close()
		return
	}
	host, port, head, err := httpTarget(req)
	if err == nil {
		if open := (protocol.TunnelItem{T: protocol.ItemTunnel, Task: t.task[:], Stream: 1, Host: host, Port: uint64(port)}); open.Validate() != nil {
			err = fmt.Errorf("a destination core cannot open: %s", net.JoinHostPort(host, strconv.Itoa(port)))
		}
	}
	if err != nil {
		writeHTTPError(c, http.StatusBadRequest, err.Error())
		_ = c.Close()
		return
	}
	plain := req.Method != http.MethodConnect
	st, code := s.newStream(t, c, request{host: host, port: port}, plain)
	if st == nil {
		status := http.StatusBadGateway
		if code == socks5NotAllowed {
			status = http.StatusServiceUnavailable
		}
		writeHTTPError(c, status, "the tunnel takes no more connections")
		_ = c.Close()
		return
	}
	// What came past the head (a request's body, a TLS hello sent early)
	// goes after it, a plain request's own rewritten head first.
	extra, _ := br.Peek(br.Buffered())
	s.mu.Lock()
	first := append(head, extra...)
	st.up = append(st.up, first...)
	st.upBytes += int64(len(first))
	t.up += int64(len(first))
	s.mu.Unlock()
	if plain {
		// Its bytes go with the open: read on while the minion connects.
		s.done.Add(1)
		go func() {
			defer s.done.Done()
			s.readClient(st)
		}()
	}
	timer := time.NewTimer(t.connectWait + s.cfg.ConnectSlack)
	defer timer.Stop()
	select {
	case r := <-st.replied:
		if r.Result != protocol.TunnelOpen {
			status, why := httpStatus(r.Result)
			writeHTTPError(c, status, why)
			s.mu.Lock()
			s.drop(st, models.TunnelEndNotOpened, false)
			s.mu.Unlock()
			return
		}
		if !plain {
			if _, err := io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
				s.mu.Lock()
				s.abort(st, models.TunnelEndClientReset)
				s.mu.Unlock()
				return
			}
		}
	case <-timer.C:
		writeHTTPError(c, http.StatusGatewayTimeout, "the minion did not answer in time")
		s.mu.Lock()
		s.abort(st, models.TunnelEndNoReply)
		s.mu.Unlock()
		return
	case <-st.done:
		return
	}
	s.done.Add(1)
	go s.writeClient(st)
	if !plain {
		s.readClient(st)
	}
}

// hopHeaders are a proxy's own headers, which a request sent on drops.
var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authorization", "Proxy-Authenticate", "Te", "Trailer",
	"Upgrade"}

// httpTarget is where a proxy's request goes, and for a plain request what
// to send there: its head, its target a path, Connection: close.
func httpTarget(req *http.Request) (host string, port int, head []byte, err error) {
	if req.Method == http.MethodConnect {
		h, p, err := net.SplitHostPort(req.Host)
		if err != nil {
			return "", 0, nil, errors.New("CONNECT takes host:port")
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", 0, nil, errors.New("CONNECT takes a port from 1 to 65535")
		}
		return h, n, nil, nil
	}
	u := req.URL
	if u == nil || u.Scheme != "http" || u.Host == "" {
		return "", 0, nil, errors.New("a proxy takes CONNECT, or a request for an http:// URL")
	}
	host, port = u.Hostname(), 80
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil || port < 1 || port > 65535 {
			return "", 0, nil, errors.New("a port from 1 to 65535")
		}
	}
	h := req.Header.Clone()
	// What Connection names is the proxy's too.
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, k := range hopHeaders {
		h.Del(k)
	}
	// The body goes on as it came, so does how it is framed.
	if len(req.TransferEncoding) > 0 {
		h.Set("Transfer-Encoding", strings.Join(req.TransferEncoding, ", "))
	} else if req.ContentLength > 0 && h.Get("Content-Length") == "" {
		h.Set("Content-Length", strconv.FormatInt(req.ContentLength, 10))
	}
	h.Set("Connection", "close")
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\nHost: %s\r\n", req.Method, u.RequestURI(), req.Host)
	_ = h.Write(&b)
	b.WriteString("\r\n")
	return host, port, b.Bytes(), nil
}

// httpStatus is the proxy's answer for a tunnel_reply's result that did
// not open.
func httpStatus(result int) (int, string) {
	switch result {
	case protocol.TunnelRefused:
		return http.StatusBadGateway, "refused: nothing listens there"
	case protocol.TunnelHostUnreachable:
		return http.StatusBadGateway, "host unreachable, or its name does not resolve"
	case protocol.TunnelNetUnreachable:
		return http.StatusBadGateway, "network unreachable"
	case protocol.TunnelTimedOut:
		return http.StatusGatewayTimeout, "timed out connecting"
	case protocol.TunnelNotAllowed:
		return http.StatusServiceUnavailable, "not allowed: as many streams open as the task takes"
	case protocol.TunnelUnsupported:
		return http.StatusNotImplemented, "the minion does not take this open"
	case protocol.TunnelDenied:
		return http.StatusForbidden, "denied: the minion's host does not let it connect there"
	}
	return http.StatusBadGateway, "the minion could not connect"
}

// writeHTTPError answers a client with status and why, closing.
func writeHTTPError(w io.Writer, status int, why string) {
	body := why + "\n"
	_, _ = fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status, http.StatusText(status), len(body), body)
}
