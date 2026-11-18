package minionsync

import (
	"slices"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Tasks on their way over a connection kept open (docs:
// contracts/minion-messages/tasks-and-results/, Sending tasks). A socket
// delivers what core sends over it or closes, so a task sent over one is on
// its way until its ack comes back, a round trip later (seconds over a slow
// link). Meanwhile the connection does not carry it again, whatever news
// nudges the session: a task picked for a push, or for the reply to the
// minion's message, is marked in flight there as it is picked. One whose
// ack has not come once resendWait has passed goes again over the same
// connection (a frame lost on the way: a delegate's queue full), its wait
// doubling each time to resendMost. Another connection (the minion
// switched, or connected again), a request's reply and a call carry it as
// before: what the minion did not acknowledge goes again.
//
// What a push does not get across (every connection failed, or its codec
// wrote part of the message) is no longer on its way, and goes again by
// itself (retryPush): with nothing new, no news would nudge the session,
// and a minion on a connection kept open sends nothing while it waits.

const (
	// resendWait is how long a task sent over a connection waits for its
	// ack before the same connection carries it again; each time it goes
	// again its wait doubles, to resendMost.
	resendWait = 30 * time.Second
	resendMost = 5 * time.Minute
	// pushRetryWait is how soon a push that did not get across goes again;
	// the wait doubles while pushes fail, to pushRetryMost, and starts over
	// once one goes.
	pushRetryWait = time.Second
	pushRetryMost = 30 * time.Second
)

// flight is a task a connection carried: when it last went over it, and how
// long the connection waits before it carries it again. seed: a tunnel
// task's first send, whose ack's round trip seeds its session's before its
// streams measure one (Tunnels.SeedRTT); never one sent again, whose ack
// could answer either send (Karn's rule).
type flight struct {
	at   time.Time
	wait time.Duration
	seed bool
}

// fly reports whether c may carry task id now, and marks it on its way over
// c when it may: a task in flight over c goes again only once its wait is
// over. seed marks a tunnel task's first send (flight). undo puts the mark
// back as it was, for a task that did not fit in the message after all.
func (c *Conn) fly(id uuid.UUID, now time.Time, seed bool) (ok bool, undo func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flights == nil {
		c.flights = map[uuid.UUID]flight{}
	}
	was, flying := c.flights[id]
	if flying && now.Sub(was.at) < was.wait {
		return false, nil
	}
	c.sweepFlightsLocked(now)
	wait := resendWait
	if flying {
		wait, seed = min(2*was.wait, resendMost), false
	}
	c.flights[id] = flight{at: now, wait: wait, seed: seed}
	return true, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if flying {
			c.flights[id] = was
		} else {
			delete(c.flights, id)
		}
	}
}

// unfly takes the tasks of items, as core wrote them for session's minion
// (packed or not), off what is on its way over c: the message that carried
// them did not go, or not that part of it.
func (c *Conn) unfly(session *models.MinionSession, items []cbor.RawMessage) {
	ids := taskIDsOf(session, items)
	if len(ids) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		delete(c.flights, id)
	}
}

// landed takes off what is on its way over c the tasks the minion's items
// say it has (an ack) or runs (a result, the last one too): their acks came
// back, at now. rtts are the round trips the acks of first sent tunnel
// tasks among them took (flight.seed); a result's says how long it ran.
func (c *Conn) landed(items []cbor.RawMessage, now time.Time) (rtts []time.Duration) {
	var ids []uuid.UUID
	acks := 0
	for _, raw := range items {
		switch protocol.ItemType(raw) {
		case protocol.ItemAck:
			var a struct {
				Tasks [][]byte `cbor:"1,keyasint,omitempty"`
			}
			if protocol.UnmarshalCBOR(raw, &a) != nil {
				continue
			}
			for _, t := range a.Tasks {
				if id, err := uuid.FromBytes(t); err == nil {
					// The acks first: they seed.
					ids = slices.Insert(ids, acks, id)
					acks++
				}
			}
		case protocol.ItemResult:
			if id, ok := itemTask(raw); ok {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, id := range ids {
		if f, ok := c.flights[id]; ok && f.seed && i < acks {
			if d := now.Sub(f.at); d > 0 {
				rtts = append(rtts, d)
			}
		}
		delete(c.flights, id)
	}
	return rtts
}

// sweepFlightsLocked lets go of the flights no message picked again long
// past their wait: their tasks left (acked another way, ended, cancelled).
// At most once a resendWait.
func (c *Conn) sweepFlightsLocked(now time.Time) {
	if now.Sub(c.flightsSwept) < resendWait {
		return
	}
	c.flightsSwept = now
	for id, f := range c.flights {
		if now.Sub(f.at) > f.wait+resendMost {
			delete(c.flights, id)
		}
	}
}

// taskIDsOf are the ids of the tasks items carry, as core wrote them for
// session's minion: packed (packTask) or not.
func taskIDsOf(session *models.MinionSession, items []cbor.RawMessage) []uuid.UUID {
	env := unpackEnvelope(session)
	var ids []uuid.UUID
	for _, raw := range items {
		if protocol.ItemType(raw) == protocol.ItemPacked {
			inner, err := protocol.UnpackIn(env, raw, protocol.MaxMinionMessageSize)
			if err != nil {
				continue
			}
			raw = inner
		}
		if protocol.ItemType(raw) != protocol.ItemTask {
			continue
		}
		if id, ok := itemTask(raw); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// itemTask is the task a task or a result item names (its key 1).
func itemTask(raw cbor.RawMessage) (uuid.UUID, bool) {
	var it struct {
		Task []byte `cbor:"1,keyasint"`
	}
	if protocol.UnmarshalCBOR(raw, &it) != nil {
		return uuid.UUID{}, false
	}
	id, err := uuid.FromBytes(it.Task)
	return id, err == nil
}

// retryPush pushes to the session again by itself, after its wait: its
// push did not get across, or not all of it. The wait doubles while pushes
// fail, to pushRetryMost; one push at a time is due for a session
// (pushAfter), whatever asks for it.
func (s *Service) retryPush(session uuid.UUID, streams bool) {
	s.conns.mu.Lock()
	wait, ok := s.conns.retryWait[session]
	if !ok {
		wait = pushRetryWait
	}
	if _, due := s.conns.scheduled[session]; !due {
		s.conns.retryWait[session] = min(2*wait, pushRetryMost)
	}
	// News meanwhile does not bring the retry sooner (pushAfter).
	s.conns.retryAt[session] = time.Now().Add(wait)
	s.conns.mu.Unlock()
	s.pushAfter(session, streams, wait)
}

// pushWent: a push to the session got across whole, or it has nothing to
// send: the next retry waits the least again.
func (s *Service) pushWent(session uuid.UUID) {
	s.conns.mu.Lock()
	defer s.conns.mu.Unlock()
	delete(s.conns.retryWait, session)
	delete(s.conns.retryAt, session)
}
