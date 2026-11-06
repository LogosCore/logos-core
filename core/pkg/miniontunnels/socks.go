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
// SOCKS5 (RFC 1928) with "no authentication" only, and SOCKS4 / 4a, each
// CONNECT. A name goes to the minion as it came; BIND and, until UDP
// lands, UDP ASSOCIATE are refused as not supported.

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

// request is what a SOCKS client asked for: a CONNECT to host:port.
type request struct {
	version int
	host    string
	port    int
}

// errRefused: the client asked for something core does not do, and was
// told so.
var errRefused = errors.New("socks: refused")

// readRequest reads a SOCKS greeting and its CONNECT from c, reading no
// byte past them (what follows is the stream's). What core does not do it
// answers itself, and returns errRefused.
func readRequest(c io.ReadWriter) (request, error) {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:1]); err != nil {
		return request{}, err
	}
	switch head[0] {
	case 5:
		return readSOCKS5(c)
	case 4:
		return readSOCKS4(c)
	}
	return request{}, fmt.Errorf("socks: version %d", head[0])
}

func readSOCKS5(c io.ReadWriter) (request, error) {
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return request{}, err
	}
	methods := make([]byte, n[0])
	if _, err := io.ReadFull(c, methods); err != nil {
		return request{}, err
	}
	noAuth := false
	for _, m := range methods {
		noAuth = noAuth || m == 0
	}
	if !noAuth {
		// No acceptable method: core takes no credentials.
		_, _ = c.Write([]byte{5, 0xff})
		return request{}, errRefused
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return request{}, err
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
	if req[1] != 1 {
		// BIND, and UDP ASSOCIATE until it lands.
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
	case protocol.TunnelNotAllowed:
		return socks5NotAllowed
	}
	return socks5GeneralFailure
}
