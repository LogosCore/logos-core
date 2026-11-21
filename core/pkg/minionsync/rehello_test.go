package minionsync

import (
	"context"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// rehellos counts the rehello items in a core message.
func rehellos(items []cbor.RawMessage) int {
	n := 0
	for _, it := range items {
		if protocol.ItemType(it) == protocol.ItemRehello {
			n++
		}
	}
	return n
}

// An operator's rehello goes with every message core sends, whatever the
// minion asks for, until a hello comes under the session: the rehello
// commands then end done, saying the hello and what changed; asked again
// and never answered, they end failed after RehelloTries messages, and the
// ask is forgotten.
func TestRehelloCommand(t *testing.T) {
	r := newRig(t)
	sid, row := helloWith(t, r, nil)
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	ctx := context.Background()
	checkIn := checkInOf(t, r, sid)
	if n := rehellos(checkIn()); n != 0 {
		t.Fatalf("a rehello no one asked for: %d", n)
	}
	command := func() uuid.UUID {
		t.Helper()
		id := uuid.New()
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
			Command: models.CommandRehello, State: models.MinionTaskRunning})
		if err := r.svc.AskRehello(ctx, row); err != nil {
			t.Fatal(err)
		}
		return id
	}
	end := func(id uuid.UUID) *repository.MinionTaskResult {
		for _, res := range tasks.results[id] {
			if res.Done {
				return &res
			}
		}
		return nil
	}
	if n, _ := tasks.CountRunning(ctx, sid); n != 0 {
		t.Fatalf("core's command counted as the minion's: %d", n)
	}

	asked := command()
	for range 2 {
		if n := rehellos(checkIn()); n != 1 {
			t.Fatalf("%d rehellos while asked", n)
		}
	}
	if end(asked) != nil || row.RehelloAt == nil || row.RehelloSends != 2 {
		t.Fatalf("ended before a hello: %+v, at %v, sends %d", end(asked), row.RehelloAt, row.RehelloSends)
	}
	// The hello again, another user now: done, saying so, with its round
	// trip.
	h := hello("web01")
	h.User = "root"
	h.RoundtripTime = &[]uint64{142}[0]
	if n := rehellos(checkIn(h)); n != 0 {
		t.Fatalf("asked again in the hello's reply: %d", n)
	}
	e := end(asked)
	if e == nil || !*e.OK || !strings.Contains(string(e.Out), "hello: web01 · root · pid 4242") ||
		!strings.Contains(string(e.Out), "linux/amd64 · round trip 142 ms\n") || !strings.Contains(string(e.Out), "user: (none) → root") {
		t.Fatalf("the answered one: %+v %q", e, e.Out)
	}
	if row.RehelloAt != nil || rehellos(checkIn()) != 0 {
		t.Fatal("still asked after the hello")
	}
	// The same hello again: nothing changed.
	again := command()
	checkIn(h)
	if e := end(again); e == nil || !strings.Contains(string(e.Out), "nothing changed") {
		t.Fatalf("an unchanged hello: %+v", e)
	}

	// Never answered: failed, after RehelloTries messages, and no more sent.
	lost := command()
	sent := 0
	for range models.RehelloTries + 3 {
		sent += rehellos(checkIn())
	}
	if e := end(lost); sent != models.RehelloTries || e == nil || *e.OK || !strings.Contains(e.Err, "no hello came") {
		t.Fatalf("sent %d; the unanswered one: %+v", sent, e)
	}
	if row.RehelloAt != nil {
		t.Fatal("the unanswered ask is kept")
	}

	// Over a socket: pushed at once.
	c := newConnRig(t, r, sid, "c1")
	if _, err := c.exchange(t, protocol.NewMessageID(), nil); err != nil {
		t.Fatal(err)
	}
	command()
	r.svc.Notify(ctx, sid)
	pushed := c.pushes(t)
	if len(pushed) != 1 || rehellos(pushed[0].Items) != 1 {
		t.Fatalf("the push: %+v", pushed)
	}
}

// A new process claiming the session answers an operator's rehello: its
// hello is the session's now, and the commands waiting end done, saying it
// came from a new process; the ask is forgotten.
func TestRehelloAnsweredByAClaim(t *testing.T) {
	r := newRig(t)
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	reply, _ := r.send(t, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.open(t, reply))
	row := r.sessionRow(t, sid)
	id := uuid.New()
	tasks.rows = []models.MinionTask{{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
		Command: models.CommandRehello, State: models.MinionTaskRunning}}
	if err := r.svc.AskRehello(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	b2, keys2 := r.persistedBuild(t, sid)
	if _, err := r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("web01-rebooted", sid)); err != nil {
		t.Fatal(err)
	}
	var done *repository.MinionTaskResult
	for _, res := range tasks.results[id] {
		if res.Done {
			done = &res
		}
	}
	if done == nil || !*done.OK || !strings.Contains(string(done.Out), "new process") || !strings.Contains(string(done.Out), "host: web01 → web01-rebooted") {
		t.Fatalf("the claim's answer: %+v", done)
	}
	if after := r.sessionRow(t, sid); after.RehelloAt != nil {
		t.Fatal("still asked after the claim")
	}
}
