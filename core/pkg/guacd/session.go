package guacd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Status codes of the Guacamole protocol's, as its error instruction and
// the web app's tunnel say them (Guacamole.Status.Code).
const (
	StatusServerError         = 0x0200
	StatusUpstreamTimeout     = 0x0202
	StatusUpstreamError       = 0x0203
	StatusResourceNotFound    = 0x0204
	StatusUpstreamNotFound    = 0x0207
	StatusUpstreamUnavailable = 0x0208
	StatusClientBadRequest    = 0x0300
	StatusClientForbidden     = 0x0303
)

// Error is a session that did not open: guacd's error instruction, or why
// core could not ask. Status is a Guacamole status code.
type Error struct {
	Message string
	Status  int
}

func (e *Error) Error() string { return fmt.Sprintf("%s (%#04x)", e.Message, e.Status) }

// Config is what a desktop's session opens with.
type Config struct {
	// Protocol is the session's: rdp, vnc, ssh or telnet.
	Protocol string
	// Params are its connection's, by the names guacd asks for (hostname,
	// port, username, password, ...); one guacd asks for that is not here
	// goes empty.
	Params map[string]string
	// Width, Height and DPI are the web app's display.
	Width, Height, DPI int
	// Images are the image types the web app takes (image/png, ...).
	Images []string
	// Timezone is the web app's (an IANA name); empty: none said.
	Timezone string
	// Name is who opens it, as guacd logs it.
	Name string
}

// HandshakeWait bounds the session's opening, guacd's ready included.
const HandshakeWait = 30 * time.Second

// Session is a desktop's session in guacd, open: what guacd sends next,
// read with Read, and what goes to it, with Write.
type Session struct {
	conn net.Conn
	r    *bufio.Reader
	// ID is the connection's id, as guacd said it ready.
	ID string
	// Version is the protocol's version guacd speaks (VERSION_1_5_0).
	Version string
}

// Open dials guacd at addr and opens a session of c: an *Error when guacd
// refuses it, or cannot be reached.
func Open(ctx context.Context, addr string, c Config) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, HandshakeWait)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &Error{Message: "core cannot reach guacd: " + err.Error(), Status: StatusUpstreamNotFound}
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	s := &Session{conn: conn, r: bufio.NewReaderSize(conn, 32<<10)}
	if err := s.handshake(c); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return s, nil
}

func (s *Session) handshake(c Config) error {
	if err := s.send(Instruction{Opcode: "select", Args: []string{c.Protocol}}); err != nil {
		return err
	}
	args, err := s.expect("args")
	if err != nil {
		return err
	}
	// Since 1.1.0 guacd says its version first: the connect says the same.
	names := args.Args
	if len(names) > 0 && strings.HasPrefix(names[0], "VERSION_") {
		s.Version, names = names[0], names[1:]
	}
	images := c.Images
	if len(images) == 0 {
		images = []string{"image/png", "image/jpeg"}
	}
	out := []Instruction{
		{Opcode: "size", Args: []string{strconv.Itoa(c.Width), strconv.Itoa(c.Height), strconv.Itoa(c.DPI)}},
		{Opcode: "audio"},
		{Opcode: "video"},
		{Opcode: "image", Args: images},
	}
	if s.Version != "" && c.Timezone != "" {
		out = append(out, Instruction{Opcode: "timezone", Args: []string{c.Timezone}})
	}
	if versionAtLeast(s.Version, 1, 3) && c.Name != "" {
		out = append(out, Instruction{Opcode: "name", Args: []string{c.Name}})
	}
	values := make([]string, 0, len(names)+1)
	if s.Version != "" {
		values = append(values, s.Version)
	}
	for _, n := range names {
		values = append(values, c.Params[n])
	}
	out = append(out, Instruction{Opcode: "connect", Args: values})
	for _, ins := range out {
		if err := s.send(ins); err != nil {
			return err
		}
	}
	ready, err := s.expect("ready")
	if err != nil {
		return err
	}
	if len(ready.Args) > 0 {
		s.ID = ready.Args[0]
	}
	return nil
}

// versionAtLeast reports whether a protocol version (VERSION_1_5_0) is at
// least major.minor.
func versionAtLeast(v string, major, minor int) bool {
	parts := strings.Split(strings.TrimPrefix(v, "VERSION_"), "_")
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	return err1 == nil && err2 == nil && (ma > major || ma == major && mi >= minor)
}

func (s *Session) send(ins Instruction) error {
	if _, err := s.conn.Write([]byte(ins.Encode())); err != nil {
		return &Error{Message: "guacd: " + err.Error(), Status: StatusUpstreamError}
	}
	return nil
}

// expect reads guacd's next instruction, which is to be opcode; its error
// instruction is an *Error.
func (s *Session) expect(opcode string) (Instruction, error) {
	ins, err := Read(s.r)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return ins, &Error{Message: "guacd did not answer in time", Status: StatusUpstreamTimeout}
		}
		return ins, &Error{Message: "guacd: " + err.Error(), Status: StatusUpstreamError}
	}
	switch ins.Opcode {
	case opcode:
		return ins, nil
	case "error":
		e := &Error{Message: "guacd refused the session", Status: StatusUpstreamError}
		if len(ins.Args) > 0 && ins.Args[0] != "" {
			e.Message = ins.Args[0]
		}
		if len(ins.Args) > 1 {
			if n, err := strconv.Atoi(ins.Args[1]); err == nil {
				e.Status = n
			}
		}
		return ins, e
	}
	return ins, &Error{Message: fmt.Sprintf("guacd said %s, not %s", ins.Opcode, opcode), Status: StatusUpstreamError}
}

// Read reads what guacd sends next, as it comes: its instructions' bytes,
// which a message may cut anywhere (CutUTF8 keeps characters whole).
func (s *Session) Read(p []byte) (int, error) { return s.r.Read(p) }

// Write sends guacd the web app's instructions, whole.
func (s *Session) Write(p []byte) (int, error) { return s.conn.Write(p) }

// Close ends the session: guacd closes its connection to the server.
func (s *Session) Close() error {
	_, _ = s.conn.Write([]byte(Instruction{Opcode: "disconnect"}.Encode()))
	return s.conn.Close()
}
