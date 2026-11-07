package miniontunnels

import (
	"time"

	"github.com/google/uuid"
)

// Timing that follows the link (docs: contracts/minion-messages/tunnels.md,
// Bytes, in order, within a window): core sends again what the minion did
// not ack once the session's round trip has passed, as TCP does (RFC 6298),
// never sooner than cfg.ResendAfter, and wakes the session then itself; and
// a stream's window, the room core gives the minion, grows while the
// minion fills it and the client keeps up.

const (
	// maxResend bounds how long core waits for an ack before it sends
	// again, however slow the link.
	maxResend = time.Minute
	// maxDownWindow is the most a stream's window grows to.
	maxDownWindow = 4 << 20
	// maxTunnelExtra bounds what a tunnel's windows grew past downWindow,
	// together: what core may hold of its streams for slow clients.
	maxTunnelExtra = 32 << 20
)

// rttState is a session's round trip, as its streams' acks measure it.
type rttState struct {
	srtt, rttvar time.Duration
	have         bool
	// wake is the timer that wakes the session when a send is due again;
	// wakeAt when it fires (zero: none set).
	wake   *time.Timer
	wakeAt time.Time
}

// sample takes one round trip.
func (r *rttState) sample(d time.Duration) {
	if !r.have {
		r.srtt, r.rttvar, r.have = d, d/2, true
		return
	}
	diff := r.srtt - d
	if diff < 0 {
		diff = -diff
	}
	r.rttvar = (3*r.rttvar + diff) / 4
	r.srtt = (7*r.srtt + d) / 8
}

// rttOf is the session's round trip; under s.mu.
func (s *Service) rttOf(session uuid.UUID) *rttState {
	r := s.rtt[session]
	if r == nil {
		r = &rttState{}
		s.rtt[session] = r
	}
	return r
}

// resendAfter is how long core waits for an ack from the session's minion
// before it sends again: its round trip and four times its spread, never
// less than cfg.ResendAfter, nor more than maxResend; under s.mu.
func (s *Service) resendAfter(session uuid.UUID) time.Duration {
	r := s.rtt[session]
	if r == nil || !r.have {
		return s.cfg.ResendAfter
	}
	return min(max(s.cfg.ResendAfter, r.srtt+4*r.rttvar), maxResend)
}

// measure starts a round trip on what the stream sends, unless one runs:
// its ack ends it. A send again (resent) ends any, as it would mislead
// (Karn's rule).
func (st *stream) measure(end uint64, resent bool, now time.Time) {
	switch {
	case resent:
		st.rttAt = time.Time{}
	case st.rttAt.IsZero():
		st.rttEnd, st.rttAt = end, now
	}
}

// measured takes the stream's round trip when the ack covers it; under
// s.mu.
func (s *Service) measured(st *stream, next uint64) {
	if st.rttAt.IsZero() || next < st.rttEnd {
		return
	}
	if d := s.now().Sub(st.rttAt); d >= 0 {
		s.rttOf(st.t.session).sample(d)
	}
	st.rttAt = time.Time{}
}

// wakeAt has the session's next message made at when (a send again due),
// earlier than any wake already set; under s.mu. Fill makes it, and sets
// the next.
func (s *Service) wakeAt(session uuid.UUID, when time.Time) {
	r := s.rttOf(session)
	if !r.wakeAt.IsZero() && !when.Before(r.wakeAt) {
		return
	}
	if r.wake != nil {
		r.wake.Stop()
	}
	r.wakeAt = when
	r.wake = time.AfterFunc(max(when.Sub(s.now()), time.Millisecond), func() {
		s.mu.Lock()
		if r.wakeAt.Equal(when) {
			r.wakeAt = time.Time{}
		}
		s.mu.Unlock()
		s.wake(session)
	})
}

// dueAt is when the stream sends again unasked, should no ack come (its
// open, its unacked bytes, a probe of a window shut): zero for never.
func (st *stream) dueAt(resend time.Duration) time.Time {
	switch {
	case st.udp != nil:
		return time.Time{}
	case !st.opened && !st.t.reverse && !st.openSent.IsZero() && st.rec.Result == nil:
		return st.openSent.Add(resend)
	case !st.opened && !st.early:
		return time.Time{}
	case st.upDone:
		return time.Time{}
	}
	end := st.upBase + uint64(len(st.up))
	waiting := st.upSent > st.upBase || (st.upSent < end && st.upSent >= st.upLimit)
	if !waiting || st.upSentAt.IsZero() {
		return time.Time{}
	}
	return st.upSentAt.Add(resend)
}

// grow doubles the stream's window when the minion sent up to what core's
// acks let it and the client has taken all of it: the window, not the
// client, holds the stream back (on a capped tunnel the cap does: no
// growing there). Up to maxDownWindow, and the tunnel's maxTunnelExtra;
// true when it grew (an ack says so).
func (st *stream) grow() bool {
	switch {
	case st.window >= maxDownWindow, len(st.down) > 0, st.downEOF, st.t.cap > 0:
		return false
	case st.downNext+uint64(st.window/8) < st.ackedEnd:
		return false
	case st.t.extra+st.window > maxTunnelExtra:
		return false
	}
	st.t.extra += st.window
	st.window *= 2
	st.ackDue = true
	return true
}

// shrink gives back what the stream's window grew, as it goes.
func (st *stream) shrink() {
	st.t.extra -= st.window - downWindow
	st.window = downWindow
}
