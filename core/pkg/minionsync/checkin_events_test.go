package minionsync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// told records what checkInEvents publishes.
type told struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (t *told) publish(e eventbus.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, e)
}

func (t *told) count(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, e := range t.events {
		if p, ok := e.Payload.(eventbus.MinionSessionEventPayload); ok && p.SessionID == sessionID && e.Topic == eventbus.TopicMinionSessionUpdated {
			n++
		}
	}
	return n
}

// A session's first check-in is told at once; those within its second, as
// one at its end; a session quiet for a second is let go, and its next
// check-in is told at once again. Sessions do not hold each other back.
func TestCheckInEventsOncePerSecondEach(t *testing.T) {
	const every = 60 * time.Millisecond
	var got told
	c := newCheckInEvents(got.publish, every)
	a := eventbus.MinionSessionEventPayload{SessionID: "a", OperationID: "op"}
	b := eventbus.MinionSessionEventPayload{SessionID: "b", OperationID: "op"}

	c.tell(a)
	for range 5 {
		c.tell(a)
	}
	c.tell(b)
	if got.count("a") != 1 || got.count("b") != 1 {
		t.Fatalf("at once: a %d, b %d; want each session's first told", got.count("a"), got.count("b"))
	}
	time.Sleep(every / 3)
	if got.count("a") != 1 {
		t.Fatalf("within the second: a told %d times, want the rest held", got.count("a"))
	}
	time.Sleep(every) // a's second is over: the held ones as one; b had none
	if got.count("a") != 2 || got.count("b") != 1 {
		t.Fatalf("after the second: a %d, b %d; want a's held check-ins as one, none for b", got.count("a"), got.count("b"))
	}
	time.Sleep(3 * every) // a quiet second after that: a is let go
	c.mu.Lock()
	left := len(c.held)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d sessions still held after a quiet second", left)
	}
	c.tell(a)
	if got.count("a") != 3 {
		t.Fatalf("a check-in after a quiet second: a told %d times, want 3 (at once)", got.count("a"))
	}
}

// Under a session, every message is a check-in: three in a row tell the
// session's watchers once at once, not three times.
func TestCheckInsToldOncePerSecond(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	before := 0
	for _, e := range r.bus.published() {
		if e.Topic == eventbus.TopicMinionSessionUpdated {
			before++
		}
	}
	for range 3 {
		if _, err := r.svc.Handle(context.Background(), sid.String(), "", sealedMinionMessage(t, r.keys, sid, nil,
			protocol.GetTasksItem{T: protocol.ItemGetTasks})); err != nil {
			t.Fatal(err)
		}
	}
	updated := 0
	for _, e := range r.bus.published() {
		if p, ok := e.Payload.(eventbus.MinionSessionEventPayload); ok && e.Topic == eventbus.TopicMinionSessionUpdated && p.SessionID == sid.String() {
			updated++
		}
	}
	if updated-before != 1 {
		t.Fatalf("three check-ins told %d times at once, want 1", updated-before)
	}
}
