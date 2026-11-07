package miniontunnels

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Core's SOCKS front (docs: contracts/minion-messages/tunnels.md, SOCKS):
// SOCKS5 (RFC 1928) with "no authentication" only, CONNECT and UDP
// ASSOCIATE, and SOCKS4 / 4a CONNECT. A name goes to the minion as it came;
// BIND is refused as not supported. A reverse SOCKS proxy (rsocks.go) takes
// SOCKS5 alone, with a user and password (RFC 1929).

// The SOCKS5 replies core gives.
const (
	socks5Succeeded           = 0
	socks5GeneralFailure      = 1
	socks5NotAllowed          = 2
	socks5NetworkUnreachable  = 3
	socks5HostUnreachable     = 4
	socks5ConnectionRefused   = 5
	socks5TTLExpired          = 6
	socks5CommandNotSupported = 7
	socks5AddressNotSupported = 8
)

// The SOCKS4 replies.
const (
	socks4Granted  = 90
	socks4Rejected = 91
)

// request is what a SOCKS client asked for: a CONNECT to host:port, or
// (udp) a UDP association, whose host and port are where the client says
// it sends its datagrams from (zeros: it does not know).
type request struct {
	version int
	host    string
	port    int
	udp     bool
	// bind: SOCKS5 BIND, the minion listening for one connection (an
	// accept), host and port the peer the client expects.
	bind bool
	// kind: what the stream opens past TCP (protocol.TunnelKind...); path a
	// forward's unix socket or pipe, instead of host and port; program,
	// args, rows and cols a terminal's process. client names who asked,
	// when not the connection's address (the web app's terminal).
	kind       int
	path       string
	program    string
	args       []string
	rows, cols int
	client     string
}

// errRefused: the client asked for something core does not do, and was
// told so.
var errRefused = errors.New("socks: refused")

// readRequest reads a SOCKS greeting and its CONNECT from c, reading no
// byte past them (what follows is the stream's). What core does not do it
// answers itself, and returns errRefused. With auth, the client must give
// its user and password: SOCKS5 alone, which has them.
func readRequest(c io.ReadWriter, auth *socksAuth) (request, error) {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:1]); err != nil {
		return request{}, err
	}
	switch {
	case head[0] == 5:
		return readSOCKS5(c, auth)
	case head[0] == 4 && auth == nil:
		return readSOCKS4(c)
	case head[0] == 4:
		// SOCKS4 has no password: rejected, once its request is read (a
		// close with bytes unread would reset the rejection away).
		if _, err := readSOCKS4(c); err != nil {
			return request{}, err
		}
		_, _ = c.Write([]byte{0, socks4Rejected, 0, 0, 0, 0, 0, 0})
		return request{}, errRefused
	}
	return request{}, fmt.Errorf("socks: version %d", head[0])
}

// readAuth reads a SOCKS5 user and password (RFC 1929) and answers whether
// they are auth's.
func readAuth(c io.ReadWriter, auth *socksAuth) error {
	var head [2]byte // VER ULEN
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return err
	}
	if head[0] != 1 {
		return fmt.Errorf("socks5: auth version %d", head[0])
	}
	user := make([]byte, head[1])
	if _, err := io.ReadFull(c, user); err != nil {
		return err
	}
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return err
	}
	password := make([]byte, n[0])
	if _, err := io.ReadFull(c, password); err != nil {
		return err
	}
	if !auth.ok(user, password) {
		_, _ = c.Write([]byte{1, 1})
		return errRefused
	}
	_, err := c.Write([]byte{1, 0})
	return err
}

func readSOCKS5(c io.ReadWriter, auth *socksAuth) (request, error) {
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return request{}, err
	}
	methods := make([]byte, n[0])
	if _, err := io.ReadFull(c, methods); err != nil {
		return request{}, err
	}
	// No authentication (0), or a user and password (2) where core asks
	// for them.
	want := byte(0)
	if auth != nil {
		want = 2
	}
	offered := false
	for _, m := range methods {
		offered = offered || m == want
	}
	if !offered {
		// No acceptable method.
		_, _ = c.Write([]byte{5, 0xff})
		return request{}, errRefused
	}
	if _, err := c.Write([]byte{5, want}); err != nil {
		return request{}, err
	}
	if auth != nil {
		if err := readAuth(c, auth); err != nil {
			return request{}, err
		}
	}
	var req [4]byte // VER CMD RSV ATYP
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return request{}, err
	}
	if req[0] != 5 {
		return request{}, fmt.Errorf("socks5: request version %d", req[0])
	}
	r := request{version: 5}
	var err error
	switch req[3] {
	case 1:
		r.host, err = readIP(c, net.IPv4len)
	case 4:
		r.host, err = readIP(c, net.IPv6len)
	case 3:
		if _, err = io.ReadFull(c, n[:]); err == nil {
			name := make([]byte, n[0])
			_, err = io.ReadFull(c, name)
			r.host = string(name)
		}
	default:
		_ = r.reply(c, socks5AddressNotSupported, "")
		return request{}, errRefused
	}
	if err != nil {
		return request{}, err
	}
	var port [2]byte
	if _, err := io.ReadFull(c, port[:]); err != nil {
		return request{}, err
	}
	r.port = int(binary.BigEndian.Uint16(port[:]))
	switch req[1] {
	case 1:
	case 2:
		r.bind, r.kind = true, protocol.TunnelKindAccept
	case 3:
		r.udp = true
	default:
		_ = r.reply(c, socks5CommandNotSupported, "")
		return request{}, errRefused
	}
	return r, nil
}

func readIP(c io.Reader, size int) (string, error) {
	ip := make(net.IP, size)
	if _, err := io.ReadFull(c, ip); err != nil {
		return "", err
	}
	return ip.String(), nil
}

func readSOCKS4(c io.ReadWriter) (request, error) {
	var req [7]byte // CD DSTPORT DSTIP
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return request{}, err
	}
	r := request{version: 4, port: int(binary.BigEndian.Uint16(req[1:3]))}
	// SOCKS4 has no UDP.
	if _, err := readZeroEnded(c); err != nil { // USERID, which core ignores
		return request{}, err
	}
	ip := net.IP(req[3:7])
	if ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] != 0 {
		// 4a: 0.0.0.x, the name after the user id.
		name, err := readZeroEnded(c)
		if err != nil {
			return request{}, err
		}
		r.host = name
	} else {
		r.host = ip.String()
	}
	if req[0] != 1 {
		_ = r.reply(c, socks5CommandNotSupported, "")
		return request{}, errRefused
	}
	return r, nil
}

// readZeroEnded reads a SOCKS4 string: up to 255 bytes and a zero.
func readZeroEnded(c io.Reader) (string, error) {
	var b [1]byte
	var out []byte
	for {
		if _, err := io.ReadFull(c, b[:]); err != nil {
			return "", err
		}
		if b[0] == 0 {
			return string(out), nil
		}
		if len(out) == 255 {
			return "", errors.New("socks4: a string longer than 255 bytes")
		}
		out = append(out, b[0])
	}
}

// reply answers the request with a SOCKS5 code (a SOCKS4 client gets
// granted or rejected) and the address the minion's socket has ("ip:port",
// empty for none).
func (r request) reply(w io.Writer, code int, bound string) error {
	ip, port := boundAddr(bound)
	if r.version == 4 {
		cd := byte(socks4Granted)
		if code != socks5Succeeded {
			cd = socks4Rejected
		}
		out := []byte{0, cd, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(out[2:4], uint16(port))
		if v4 := ip.To4(); v4 != nil {
			copy(out[4:8], v4)
		}
		_, err := w.Write(out)
		return err
	}
	out := []byte{5, byte(code), 0}
	if v4 := ip.To4(); v4 != nil {
		out = append(append(out, 1), v4...)
	} else {
		out = append(append(out, 4), ip.To16()...)
	}
	out = binary.BigEndian.AppendUint16(out, uint16(port))
	_, err := w.Write(out)
	return err
}

// boundAddr reads a tunnel_reply's bound: 0.0.0.0:0 when it does not read.
func boundAddr(bound string) (net.IP, int) {
	host, p, err := net.SplitHostPort(bound)
	if err != nil {
		return net.IPv4zero, 0
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(p)
	if ip == nil || err != nil || port < 0 || port > 65535 {
		return net.IPv4zero, 0
	}
	return ip, port
}

// readDatagram reads a datagram a SOCKS client sends the relay: RSV (2),
// FRAG, ATYP, the destination, then the payload. ok false for one to drop:
// a fragment (as most SOCKS servers, core takes none), or a header that
// does not read.
func readDatagram(b []byte) (host string, port int, data []byte, ok bool) {
	if len(b) < 4 || b[0] != 0 || b[1] != 0 || b[2] != 0 {
		return "", 0, nil, false
	}
	rest := b[4:]
	switch b[3] {
	case 1:
		if len(rest) < net.IPv4len+2 {
			return "", 0, nil, false
		}
		host, rest = net.IP(rest[:net.IPv4len]).String(), rest[net.IPv4len:]
	case 4:
		if len(rest) < net.IPv6len+2 {
			return "", 0, nil, false
		}
		host, rest = net.IP(rest[:net.IPv6len]).String(), rest[net.IPv6len:]
	case 3:
		if len(rest) < 1 || len(rest) < 1+int(rest[0])+2 {
			return "", 0, nil, false
		}
		host, rest = string(rest[1:1+int(rest[0])]), rest[1+int(rest[0]):]
	default:
		return "", 0, nil, false
	}
	return host, int(binary.BigEndian.Uint16(rest[:2])), rest[2:], true
}

// datagram is what the relay sends a SOCKS client: the header saying the
// datagram's source (an address, or a name), then its payload.
func datagram(host string, port int, data []byte) []byte {
	out := []byte{0, 0, 0}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		name := host[:min(len(host), 255)]
		out = append(append(out, 3, byte(len(name))), name...)
	case ip.To4() != nil:
		out = append(append(out, 1), ip.To4()...)
	default:
		out = append(append(out, 4), ip.To16()...)
	}
	out = binary.BigEndian.AppendUint16(out, uint16(port))
	return append(out, data...)
}

// socksCode is the SOCKS5 reply for a tunnel_reply's result: one core does
// not know is a general failure.
func socksCode(result int) int {
	switch result {
	case protocol.TunnelOpen:
		return socks5Succeeded
	case protocol.TunnelRefused:
		return socks5ConnectionRefused
	case protocol.TunnelHostUnreachable:
		return socks5HostUnreachable
	case protocol.TunnelNetUnreachable:
		return socks5NetworkUnreachable
	case protocol.TunnelTimedOut:
		return socks5TTLExpired
	case protocol.TunnelNotAllowed, protocol.TunnelDenied:
		return socks5NotAllowed
	case protocol.TunnelUnsupported:
		return socks5CommandNotSupported
	}
	return socks5GeneralFailure
}
