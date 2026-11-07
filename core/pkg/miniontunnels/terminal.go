package miniontunnels

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A terminal (docs: contracts/minion-messages/tunnels.md, Terminal): each
// connection to its port, or each web terminal the web app opens
// (OpenTerminal), is a stream to a process on the minion's host, the task's
// program (none: the host's shell) in a terminal. The raw port's terminal
// keeps the task's size; a web terminal says its own, and again as it
// changes (Resize). A process that does not start says why, in the
// terminal; one that ends says its exit there.

// ErrNoTerminal: the task runs no terminal core holds.
var ErrNoTerminal = errors.New("the task runs no terminal")

// errTerminalEnded: the terminal's stream is over.
var errTerminalEnded = errors.New("the terminal ended")

// processRequest is a stream for the tunnel's program, in a terminal of
// rows and cols.
func (t *tunnel) processRequest(rows, cols int) request {
	return request{kind: protocol.TunnelKindProcess, program: t.program, args: t.args, rows: rows, cols: cols}
}

// serveTerminal is one client of a terminal's port: its process, in a
// terminal of the task's size; what it types goes with the open.
func (s *Service) serveTerminal(t *tunnel, c net.Conn) {
	s.mu.Lock()
	delete(t.handshaking, c)
	closing := t.closing
	s.mu.Unlock()
	if closing {
		_ = c.Close()
		return
	}
	st, _ := s.newStream(t, c, t.processRequest(t.rows, t.cols), true)
	if st == nil {
		say(c, "[logos: no terminal: as many streams open as the task takes]")
		_ = c.Close()
		return
	}
	s.runProcess(st)
}

// runProcess carries a process's stream once it is numbered: the client's
// bytes from the open on, the minion's once it started; one that does not
// start says why to its client.
func (s *Service) runProcess(st *stream) {
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		s.readClient(st)
	}()
	timer := time.NewTimer(st.t.connectWait + s.cfg.ConnectSlack)
	defer timer.Stop()
	select {
	case r := <-st.replied:
		if r.Result != protocol.TunnelOpen {
			say(st.client, startNote(st.rec.Program, r))
			s.mu.Lock()
			s.drop(st, models.TunnelEndNotOpened, false)
			s.mu.Unlock()
			return
		}
	case <-timer.C:
		say(st.client, "[logos: the minion did not answer in time]")
		s.mu.Lock()
		s.abort(st, models.TunnelEndNoReply)
		s.mu.Unlock()
		return
	case <-st.done:
		return
	}
	s.done.Add(1)
	go s.writeClient(st)
}

// say writes a line of core's to a terminal's client, giving up after a
// while.
func say(c net.Conn, line string) {
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte("\r\n" + line + "\r\n"))
	_ = c.SetWriteDeadline(time.Time{})
}

// startNote says why a process did not start.
func startNote(program string, r protocol.TunnelReplyItem) string {
	if program == "" {
		program = "the host's shell"
	}
	why := protocol.TunnelResultName(r.Result)
	if r.Msg != "" {
		why += ": " + r.Msg
	}
	return fmt.Sprintf("[logos: %s did not start: %s]", program, why)
}

// Terminal is a web terminal's stream: its bytes through Read and Write,
// its size through Resize, a signal through Signal.
type Terminal struct {
	s    *Service
	st   *stream
	conn *pipeEnd
	once sync.Once
}

// OpenTerminal opens a stream for a web terminal of rows and cols on the
// task's terminal; client names who asked (its streams list it).
// ErrNoTerminal when the task runs none here, or takes no more streams.
func (s *Service) OpenTerminal(task uuid.UUID, client string, rows, cols int) (*Terminal, error) {
	if err := protocol.TunnelProcess("", nil, uint64(max(rows, 0)), uint64(max(cols, 0))); err != nil || rows == 0 {
		return nil, fmt.Errorf("a terminal is 1 to %d rows and cols", protocol.MaxTunnelTerm)
	}
	s.mu.Lock()
	t := s.tunnels[task]
	ok := t != nil && !t.closing && t.purpose == models.OperationalPurposeTerminal
	s.mu.Unlock()
	if !ok {
		return nil, ErrNoTerminal
	}
	near, far := newPipe()
	req := t.processRequest(rows, cols)
	req.client = client
	st, _ := s.newStream(t, near, req, true)
	if st == nil {
		return nil, fmt.Errorf("%w: as many streams open as the task takes", ErrNoTerminal)
	}
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		s.runProcess(st)
	}()
	return &Terminal{s: s, st: st, conn: far}, nil
}

// Stream is its stream's number in the task.
func (tm *Terminal) Stream() uint64 { return tm.st.id }

// Read is what the process sends: an eof once it ended.
func (tm *Terminal) Read(b []byte) (int, error) { return tm.conn.Read(b) }

// Write is what the operator types.
func (tm *Terminal) Write(b []byte) (int, error) { return tm.conn.Write(b) }

// CloseWrite ends what the operator sends: the process's input ends.
func (tm *Terminal) CloseWrite() error { return tm.conn.CloseWrite() }

// Done closes when the stream is over.
func (tm *Terminal) Done() <-chan struct{} { return tm.st.done }

// Resize says the terminal's new size.
func (tm *Terminal) Resize(rows, cols int) error {
	if rows < 1 || cols < 1 || rows > protocol.MaxTunnelTerm || cols > protocol.MaxTunnelTerm {
		return fmt.Errorf("a terminal is 1 to %d rows and cols", protocol.MaxTunnelTerm)
	}
	return tm.ctl(protocol.TunnelCtlItem{Rows: uint64(rows), Cols: uint64(cols)})
}

// Signal sends the process a signal (protocol.TunnelSignalInterrupt, ...).
func (tm *Terminal) Signal(signal int) error {
	if signal < 1 {
		return errors.New("a signal is 1 or more")
	}
	return tm.ctl(protocol.TunnelCtlItem{Signal: signal})
}

func (tm *Terminal) ctl(c protocol.TunnelCtlItem) error {
	tm.s.mu.Lock()
	defer tm.s.mu.Unlock()
	if tm.st.gone {
		return errTerminalEnded
	}
	return tm.s.queueCtl(tm.st, c)
}

// Ended is how the stream ended (its end, "" while it lasts), the
// process's exit once it said it, and whether it had a terminal (nil until
// it started).
func (tm *Terminal) Ended() (end string, exit *int64, terminal *bool) {
	tm.s.mu.Lock()
	defer tm.s.mu.Unlock()
	return tm.st.rec.End, tm.st.rec.Exit, tm.st.rec.Terminal
}

// Close ends the terminal: a stream still going is reset, which hangs its
// process up.
func (tm *Terminal) Close() {
	tm.once.Do(func() {
		_ = tm.conn.Close()
		tm.s.mu.Lock()
		tm.s.abort(tm.st, models.TunnelEndClientReset)
		tm.s.mu.Unlock()
	})
}
