package minionsync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeComposites decides as decide says, in time when it returns true.
type fakeComposites struct {
	mu       sync.Mutex
	held     [][]uuid.UUID
	decide   func() bool
	deciding bool
}

func (f *fakeComposites) Hold(_ context.Context, tasks []uuid.UUID, d time.Duration) bool {
	f.mu.Lock()
	f.held = append(f.held, tasks)
	f.mu.Unlock()
	if d != protocol.ComposeReplyHold {
		panic("held for another time")
	}
	return f.decide()
}

func (f *fakeComposites) Deciding(uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deciding
}

// A reverse-call message that ends a composite task's subtask and asks for
// tasks waits for the factory's decision: what it queues goes in the
// reply. Past the wait, the reply says come back soon, as every reply does
// while a call about one of the session's tasks is out (docs: Task
// Outsourcing, Mechanics by connection kind).
func TestComposeHoldsTheReply(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	comp := &fakeComposites{}
	r.svc.WithComposites(comp)
	parent := uuid.New()
	subtask := func() uuid.UUID {
		id := uuid.New()
		tasks.mu.Lock()
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, Command: "ls", Op: 4, State: models.MinionTaskSent,
			ParentID: &parent})
		tasks.mu.Unlock()
		return id
	}
	send := func(items ...any) protocol.CoreMessage {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), items...)
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return protocol.CoreMessage{}
		}
		return r.openUnder(t, sid, r.keys, reply)
	}
	ok := true
	ended := func(id uuid.UUID) protocol.ResultItem {
		return protocol.ResultItem{T: protocol.ItemResult, Task: id[:], Seq: 0, Done: true, OK: &ok}
	}
	get := protocol.GetTasksItem{T: protocol.ItemGetTasks}

	// In time: the next subtask goes in this reply.
	next := uuid.New()
	comp.decide = func() bool {
		tasks.mu.Lock()
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: next, SessionID: sid, Command: "rm", Op: 5, State: models.MinionTaskQueued,
			ParentID: &parent})
		tasks.mu.Unlock()
		return true
	}
	msg := send(ended(subtask()), get)
	if len(comp.held) != 1 || len(comp.held[0]) != 1 || comp.held[0][0] != parent {
		t.Fatalf("held for %v", comp.held)
	}
	if got := itemsOf(msg, protocol.ItemTask); len(got) != 1 || len(itemsOf(msg, protocol.ItemPoll)) != 0 {
		t.Fatalf("in time: %d tasks, %d polls", len(got), len(itemsOf(msg, protocol.ItemPoll)))
	}

	// Too slow: no task yet, and come back soon.
	comp.decide = func() bool { return false }
	msg = send(ended(subtask()), get)
	polls := itemsOf(msg, protocol.ItemPoll)
	var p protocol.PollItem
	if len(polls) != 1 || protocol.UnmarshalCBOR(polls[0], &p) != nil || p.Within != uint64(pollSoon.Milliseconds()) {
		t.Fatalf("too slow: polls %d %+v", len(polls), p)
	}

	// No get_tasks: no wait.
	held := len(comp.held)
	send(ended(subtask()))
	if len(comp.held) != held {
		t.Fatal("a message that asks for no tasks waited")
	}

	// While a call is out, every reply says come back soon.
	comp.mu.Lock()
	comp.deciding = true
	comp.mu.Unlock()
	if msg := send(); len(itemsOf(msg, protocol.ItemPoll)) != 1 {
		t.Fatal("no poll while a call is out")
	}
}
