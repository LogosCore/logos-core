package miniontunnels

import (
	"errors"
	"io"
	"net"
	"time"
)

// A connection inside core's process, both its ends core's: a web
// terminal's stream (terminal.go) holds one end, its WebSocket the other.

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
