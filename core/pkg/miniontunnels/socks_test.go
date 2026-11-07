package miniontunnels

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// conn is a client's side of a handshake: what it sends, and what the
// server writes back.
type conn struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *conn) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c *conn) Write(p []byte) (int, error) { return c.out.Write(p) }

func client(b ...byte) *conn { return &conn{in: bytes.NewReader(b)} }

// SOCKS5 CONNECT reads to an IPv4 address, an IPv6 one and a name, with
// "no authentication" picked from the client's methods, and UDP ASSOCIATE;
// SOCKS4 to an address, 4a to a name. No byte past the request is read.
func TestReadRequest(t *testing.T) {
	cases := []struct {
		name  string
		in    []byte
		want  request
		reply []byte
	}{
		{"socks5 ipv4", []byte{5, 2, 2, 0, 5, 1, 0, 1, 10, 0, 0, 5, 1, 189, 'x'}, request{version: 5, host: "10.0.0.5", port: 445}, []byte{5, 0}},
		{"socks5 ipv6", append(append([]byte{5, 1, 0, 5, 1, 0, 4}, bytes.Repeat([]byte{0}, 15)...), 1, 0, 22, 'x'), request{version: 5, host: "::1", port: 22}, []byte{5, 0}},
		{"socks5 name", []byte{5, 1, 0, 5, 1, 0, 3, 8, 'i', 'n', 't', 'r', 'a', 'n', 'e', 't', 0x20, 0xfb, 'x'}, request{version: 5, host: "intranet", port: 8443}, []byte{5, 0}},
		{"socks4", []byte{4, 1, 0, 80, 192, 168, 1, 2, 'b', 'o', 'b', 0, 'x'}, request{version: 4, host: "192.168.1.2", port: 80}, nil},
		{"socks4a", []byte{4, 1, 1, 187, 0, 0, 0, 9, 0, 'd', 'c', '0', '1', 0, 'x'}, request{version: 4, host: "dc01", port: 443}, nil},
		{"socks5 udp associate", []byte{5, 1, 0, 5, 3, 0, 1, 0, 0, 0, 0, 0, 0, 'x'}, request{version: 5, host: "0.0.0.0", port: 0, udp: true}, []byte{5, 0}},
	}
	for _, c := range cases {
		cl := client(c.in...)
		got, err := readRequest(cl, nil)
		if err != nil || got != c.want {
			t.Errorf("%s: %+v %v, want %+v", c.name, got, err, c.want)
			continue
		}
		if !bytes.Equal(cl.out.Bytes(), c.reply) {
			t.Errorf("%s: wrote % x, want % x", c.name, cl.out.Bytes(), c.reply)
		}
		if rest, _ := io.ReadAll(cl.in); string(rest) != "x" {
			t.Errorf("%s: read past the request, left %q", c.name, rest)
		}
	}
}

// What core does not do is refused, and the client told: credentials only
// (no acceptable method), BIND (not supported), an address type it does not
// know, SOCKS4 BIND.
func TestReadRequestRefuses(t *testing.T) {
	cases := []struct {
		name  string
		in    []byte
		reply []byte
	}{
		{"credentials only", []byte{5, 1, 2}, []byte{5, 0xff}},
		{"bind", []byte{5, 1, 0, 5, 2, 0, 1, 10, 0, 0, 5, 0, 80}, []byte{5, 0, 5, socks5CommandNotSupported, 0, 1, 0, 0, 0, 0, 0, 0}},
		{"address type", []byte{5, 1, 0, 5, 1, 0, 9}, []byte{5, 0, 5, socks5AddressNotSupported, 0, 1, 0, 0, 0, 0, 0, 0}},
		{"socks4 bind", []byte{4, 2, 0, 80, 10, 0, 0, 1, 0}, []byte{0, socks4Rejected, 0, 0, 0, 0, 0, 0}},
	}
	for _, c := range cases {
		cl := client(c.in...)
		if _, err := readRequest(cl, nil); !errors.Is(err, errRefused) {
			t.Errorf("%s: %v, want refused", c.name, err)
		}
		if !bytes.Equal(cl.out.Bytes(), c.reply) {
			t.Errorf("%s: wrote % x, want % x", c.name, cl.out.Bytes(), c.reply)
		}
	}
	for name, in := range map[string][]byte{
		"version 6": {6, 1, 0}, "cut short": {5, 1, 0, 5, 1, 0, 1, 10}, "socks4 user id too long": append([]byte{4, 1, 0, 80, 10, 0, 0, 1}, bytes.Repeat([]byte{'u'}, 300)...),
	} {
		if _, err := readRequest(client(in...), nil); err == nil || errors.Is(err, errRefused) {
			t.Errorf("%s: %v, want an error", name, err)
		}
	}
}

// A reply carries the minion's bound address, IPv4 or IPv6 (0.0.0.0:0 when
// it does not read); a SOCKS4 client is granted or rejected.
func TestReply(t *testing.T) {
	cases := []struct {
		r     request
		code  int
		bound string
		want  []byte
	}{
		{request{version: 5}, socks5Succeeded, "10.0.0.7:51234", []byte{5, 0, 0, 1, 10, 0, 0, 7, 0xc8, 0x22}},
		{request{version: 5}, socks5Succeeded, "[fe80::1]:22", append(append([]byte{5, 0, 0, 4, 0xfe, 0x80}, append(bytes.Repeat([]byte{0}, 13), 1)...), 0, 22)},
		{request{version: 5}, socks5ConnectionRefused, "", []byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}},
		{request{version: 5}, socks5Succeeded, "not an address", []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}},
		{request{version: 4}, socks5Succeeded, "10.0.0.7:80", []byte{0, socks4Granted, 0, 80, 10, 0, 0, 7}},
		{request{version: 4}, socks5HostUnreachable, "", []byte{0, socks4Rejected, 0, 0, 0, 0, 0, 0}},
	}
	for _, c := range cases {
		var b bytes.Buffer
		if err := c.r.reply(&b, c.code, c.bound); err != nil || !bytes.Equal(b.Bytes(), c.want) {
			t.Errorf("%+v %d %q: % x, want % x", c.r, c.code, c.bound, b.Bytes(), c.want)
		}
	}
}

// Each tunnel result has its SOCKS5 reply, one core does not know a
// general failure.
func TestSocksCode(t *testing.T) {
	for result, want := range map[int]int{
		protocol.TunnelOpen: 0, protocol.TunnelRefused: 5, protocol.TunnelHostUnreachable: 4, protocol.TunnelNetUnreachable: 3,
		protocol.TunnelTimedOut: 6, protocol.TunnelNotAllowed: 2, protocol.TunnelFailed: 1, 42: 1,
	} {
		if got := socksCode(result); got != want {
			t.Errorf("result %d: %d, want %d", result, got, want)
		}
	}
}

// A SOCKS UDP datagram reads its destination (an IPv4 or IPv6 address, or a
// name) and payload; a fragment, or a header that does not read, is
// dropped. What the relay sends back says the source the same way.
func TestDatagrams(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		host string
		port int
		data string
	}{
		{"ipv4", []byte{0, 0, 0, 1, 10, 0, 0, 53, 0, 53, 'q'}, "10.0.0.53", 53, "q"},
		{"ipv6", append(append([]byte{0, 0, 0, 4}, append(bytes.Repeat([]byte{0}, 15), 1)...), 0, 123, 'n'), "::1", 123, "n"},
		{"name", []byte{0, 0, 0, 3, 4, 'd', 'c', '0', '1', 0, 161, 's', 'n'}, "dc01", 161, "sn"},
		{"empty payload", []byte{0, 0, 0, 1, 10, 0, 0, 53, 0, 53}, "10.0.0.53", 53, ""},
	}
	for _, c := range cases {
		host, port, data, ok := readDatagram(c.in)
		if !ok || host != c.host || port != c.port || string(data) != c.data {
			t.Errorf("%s: %q %d %q %v", c.name, host, port, data, ok)
		}
		if back, _, _, ok := readDatagram(datagram(c.host, c.port, []byte(c.data))); !ok || back != c.host {
			t.Errorf("%s: written back as %q", c.name, back)
		}
	}
	for name, in := range map[string][]byte{
		"a fragment":    {0, 0, 1, 1, 10, 0, 0, 53, 0, 53, 'q'},
		"rsv set":       {0, 1, 0, 1, 10, 0, 0, 53, 0, 53},
		"short":         {0, 0, 0, 1, 10, 0},
		"name too long": {0, 0, 0, 3, 9, 'd', 'c', 0, 53},
		"address type":  {0, 0, 0, 7, 1, 2, 3, 4, 0, 53},
	} {
		if _, _, _, ok := readDatagram(in); ok {
			t.Errorf("%s: taken", name)
		}
	}
}

// Each number of a datagram sequence is taken once: the latest and the 64
// before it are remembered, older ones dropped.
func TestReplayWindow(t *testing.T) {
	var w replayWindow
	for _, c := range []struct {
		seq  uint64
		want bool
	}{{5, true}, {5, false}, {7, true}, {6, true}, {6, false}, {100, true}, {40, true}, {36, false}, {99, true}, {40, false}} {
		if got := w.take(c.seq); got != c.want {
			t.Errorf("seq %d: %v, want %v", c.seq, got, c.want)
		}
	}
}
