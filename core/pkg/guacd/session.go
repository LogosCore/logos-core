package guacd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Status codes of the Guacamole protocol's, as its error instruction and
// the web app's tunnel say them (Guacamole.Status.Code).
const (
	StatusUnsupported         = 0x0100
	StatusServerError         = 0x0200
	StatusUpstreamTimeout     = 0x0202
	StatusUpstreamError       = 0x0203
	StatusResourceNotFound    = 0x0204
	StatusUpstreamNotFound    = 0x0207
	StatusUpstreamUnavailable = 0x0208
	StatusClientBadRequest    = 0x0300
	StatusClientForbidden     = 0x0303
	StatusClientTimeout       = 0x0308
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
	// goes empty, and one with a value that guacd does not ask for refuses
	// the session (StatusUnsupported): a guacd older than the params, or
	// not built as core's (deploy/guacd), would leave it out unsaid.
	Params map[string]string
	// Width, Height and DPI are the display's: the web app's, or a fixed
	// screen's.
	Width, Height, DPI int
	// Images are the image types the web app takes (image/png, ...).
	Images []string
	// Name is who opens it, as guacd logs it.
	Name string
	// Dialed, when set, is told guacd's address as core reached it, before
	// the session asks guacd for anything: guacd connects to the desktop's
	// server from there (where nothing translates addresses between them).
	Dialed func(remote net.Addr)
}

// HandshakeWait bounds the session's opening, guacd's ready included
// (guacd answers at once; one that does not is not reached, or stuck). It
// is shorter than the web app's Guacamole tunnel waits for a word (15 s:
// its receive timeout), which core's relay does not answer while the
// session opens: the web app gets core's error, not its own timeout.
const HandshakeWait = 10 * time.Second

// WriteWait bounds a write to guacd once the session is open: guacd that
// takes nothing for that long is stuck, and the write fails (the session
// ends). ReadWait bounds guacd's silence likewise: guacd sends a nop where
// it wrote nothing for 5 s, looking every 5 s (its keep-alive, on in each
// connection's process, a thread of its own), so it speaks at least every
// 10 s, and one silent for 15 s is stuck or unreachable: the read fails (the
// session ends), as Guacamole's own web application has it. CloseWait
// bounds the disconnect Close sends.
var (
	WriteWait = 10 * time.Second
	ReadWait  = 15 * time.Second
	CloseWait = 2 * time.Second
)

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
	if c.Dialed != nil {
		c.Dialed(conn.RemoteAddr())
	}
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
	asked := make(map[string]bool, len(names))
	for _, n := range names {
		asked[n] = true
	}
	var unknown []string
	for n, v := range c.Params {
		if v != "" && !asked[n] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return &Error{Message: fmt.Sprintf("guacd takes no %s for %s: it is not core's guacd (logos-guacd), or an older one",
			strings.Join(unknown, ", "), c.Protocol), Status: StatusUnsupported}
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
// which a message may cut anywhere (CutUTF8 keeps characters whole), within
// ReadWait (past it, a net.Error that timed out). Not to be called while
// another Read runs.
func (s *Session) Read(p []byte) (int, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(ReadWait))
	return s.r.Read(p)
}

// Write sends guacd the web app's instructions, whole, within WriteWait.
// Not to be called while another Write, or Close, runs.
func (s *Session) Write(p []byte) (int, error) {
	_ = s.conn.SetWriteDeadline(time.Now().Add(WriteWait))
	return s.conn.Write(p)
}

// Close ends the session: guacd closes its connection to the server, as
// its disconnect says (within CloseWait), or as its socket closes.
func (s *Session) Close() error {
	_ = s.conn.SetWriteDeadline(time.Now().Add(CloseWait))
	_, _ = s.conn.Write([]byte(Instruction{Opcode: "disconnect"}.Encode()))
	return s.conn.Close()
}
