package minionsync

import (
	"sync"
	"time"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
)

// A check-in's change goes to the session's watchers (the web app's tables
// and the session's page) at most once a second for each session. Every
// message under a session is a check-in, and a minion may send many a second
// (a tunnel's streams, a desktop, a download, check-ins with no wait): each
// published a change, every viewer read the session again for each, and a
// viewer that fell behind lost events to the bus, a new session's among
// them. A change that is not only a check-in (a claim, a connection, an
// identity, a schedule) is told at once, where it happens.

// checkInEvery is the most often one session's check-ins are told.
const checkInEvery = time.Second

// checkInEvents tells sessions' check-ins: a session's first at once, those
// that come within the second after it as one at its end, and so on while
// they come.
type checkInEvents struct {
	publish func(eventbus.Event)
	every   time.Duration
	mu      sync.Mutex
	// held are the sessions whose second runs, each with whether a check-in
	// came within it: told as its second ends.
	held map[string]*heldCheckIn
}

type heldCheckIn struct {
	payload eventbus.MinionSessionEventPayload
	more    bool
}

func newCheckInEvents(publish func(eventbus.Event), every time.Duration) *checkInEvents {
	return &checkInEvents{publish: publish, every: every, held: map[string]*heldCheckIn{}}
}

// tell says a session checked in.
func (c *checkInEvents) tell(p eventbus.MinionSessionEventPayload) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h, ok := c.held[p.SessionID]; ok {
		h.payload, h.more = p, true
		return
	}
	c.held[p.SessionID] = &heldCheckIn{payload: p}
	c.publish(eventbus.NewMinionSessionUpdatedEvent(p))
	time.AfterFunc(c.every, func() { c.secondOver(p.SessionID) })
}

// secondOver ends a session's second: a check-in that came within it is told
// and another second starts; with none, the session is let go.
func (c *checkInEvents) secondOver(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.held[id]
	if h == nil {
		return
	}
	if !h.more {
		delete(c.held, id)
		return
	}
	h.more = false
	c.publish(eventbus.NewMinionSessionUpdatedEvent(h.payload))
	time.AfterFunc(c.every, func() { c.secondOver(id) })
}
