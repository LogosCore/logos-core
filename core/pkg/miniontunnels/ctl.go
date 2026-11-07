package miniontunnels

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A stream's tunnel_ctl items (docs: contracts/minion-messages/tunnels.md,
// Controls): what it carries besides its bytes, in order and none lost.
// Core numbers its own from 0 and sends each until the minion's ack (its
// Ctl) covers it: a terminal's size, a signal. It takes the minion's at its
// next, acking with its own acks: a process's exit, who came to an accept.

// maxCtlOut bounds a stream's ctls the minion has not acked.
const maxCtlOut = 64

// errCtlFull: a stream holds maxCtlOut ctls its minion has not acked.
var errCtlFull = errors.New("the minion has not taken the terminal's last controls yet")

// queueCtl puts a ctl of core's on the stream, numbered; a size not yet
// sent gives way to a newer one. Under s.mu.
func (s *Service) queueCtl(st *stream, c protocol.TunnelCtlItem) error {
	if c.Rows != 0 && c.Signal == 0 {
		if n := len(st.ctlOut); n > 0 && st.ctlBase+uint64(n-1) >= st.ctlSent && st.ctlOut[n-1].Signal == 0 {
			st.ctlOut[n-1].Rows, st.ctlOut[n-1].Cols = c.Rows, c.Cols
			return nil
		}
	}
	if len(st.ctlOut) >= maxCtlOut {
		return errCtlFull
	}
	c.T, c.Task, c.Stream, c.Seq = protocol.ItemTunnelCtl, st.t.task[:], st.id, st.ctlBase+uint64(len(st.ctlOut))
	st.ctlOut = append(st.ctlOut, c)
	s.wake(st.t.session)
	return nil
}

// ctlDue: one of core's ctls is to go, once its stream is open: one not
// sent, or sent and not acked for the resend's time.
func (st *stream) ctlDue(now time.Time, resend time.Duration) bool {
	if !st.opened || len(st.ctlOut) == 0 {
		return false
	}
	end := st.ctlBase + uint64(len(st.ctlOut))
	return st.ctlSent < end || now.Sub(st.ctlSentAt) >= resend
}

// putCtls puts what of the stream's ctls is due in f, from the ack again
// once they went unacked too long; true when one went.
func (st *stream) putCtls(f *filler, now time.Time, resend time.Duration) bool {
	if !st.ctlDue(now, resend) {
		return false
	}
	from := st.ctlSent
	if from > st.ctlBase && now.Sub(st.ctlSentAt) >= resend {
		from = st.ctlBase
	}
	went := false
	for seq := from; seq < st.ctlBase+uint64(len(st.ctlOut)); seq++ {
		if !f.put(st.ctlOut[seq-st.ctlBase]) {
			break
		}
		st.ctlSent, went = max(st.ctlSent, seq+1), true
	}
	if went {
		st.ctlSentAt = now
	}
	return went
}

// ctlAcked drops the ctls the minion's ack covers (its Ctl, the next it
// takes); true when that left none.
func (st *stream) ctlAcked(next uint64) bool {
	if next <= st.ctlBase || next > st.ctlBase+uint64(len(st.ctlOut)) {
		return false
	}
	st.ctlOut = st.ctlOut[next-st.ctlBase:]
	st.ctlBase = next
	st.ctlSent = max(st.ctlSent, next)
	if len(st.ctlOut) == 0 {
		st.ctlOut = nil
		return true
	}
	return false
}

// takeCtl takes the minion's ctl at core's next, and acks it (one before is
// acked again; one past waits for the minion to send again): a process's
// exit, an accept's connection.
func (s *Service) takeCtl(session uuid.UUID, c protocol.TunnelCtlItem) ([]cbor.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, reason := s.find(session, c.Task, c.Stream)
	if st == nil {
		return closeItem(c.Task, c.Stream, reason)
	}
	if !st.opened || c.Seq > st.ctlIn {
		return nil, nil
	}
	st.ackDue = true
	defer s.wake(st.t.session)
	if c.Seq < st.ctlIn {
		return nil, nil
	}
	st.ctlIn++
	if c.Exit != nil && st.kind == protocol.TunnelKindProcess {
		exit := *c.Exit
		st.rec.Exit = &exit
		st.t.changed = true
	}
	if c.Host != "" && st.kind == protocol.TunnelKindAccept && st.rec.Came == "" {
		st.rec.Came = net.JoinHostPort(c.Host, strconv.FormatUint(c.Port, 10))
		st.t.changed = true
		select {
		case st.came <- c:
		default:
		}
	}
	return nil, nil
}

// exitNote is a terminal's last line, how its process ended.
func exitNote(code int64) []byte {
	return []byte(fmt.Sprintf("\r\n[the process exited: %d]\r\n", code))
}
