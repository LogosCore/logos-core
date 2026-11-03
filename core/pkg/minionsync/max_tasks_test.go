package minionsync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// maxTasksIn is the limit a core message sets, if it sets one.
func maxTasksIn(t *testing.T, items []cbor.RawMessage) *uint64 {
	t.Helper()
	var out *uint64
	for _, it := range items {
		if protocol.ItemType(it) != protocol.ItemMaxTasks {
			continue
		}
		var m protocol.MaxTasksItem
		if err := protocol.UnmarshalCBOR(it, &m); err != nil {
			t.Fatal(err)
		}
		if out != nil {
			t.Fatal("two max_tasks in one message")
		}
		out = &m.Max
	}
	return out
}

// helloWith opens a session whose hello says limit (nil: none), and
// returns its stored row.
func helloWith(t *testing.T, r *rig, limit *uint64) (uuid.UUID, *models.MinionSession) {
	t.Helper()
	r.svc.WithTasks(&fakeTasks{})
	h := hello("web01")
	h.MaxTasks = limit
	reply, err := r.send(t, protocol.NewMessageID(), h)
	if err != nil {
		t.Fatal(err)
	}
	var resp protocol.HelloResponseItem
	if err := protocol.UnmarshalCBOR(r.open(t, reply).Items[0], &resp); err != nil {
		t.Fatal(err)
	}
	sid, err := uuid.FromBytes(resp.SID)
	if err != nil {
		t.Fatal(err)
	}
	return sid, r.sessionRow(t, sid)
}

// checkInOf checks a session in with items and returns core's reply's items.
func checkInOf(t *testing.T, r *rig, sid uuid.UUID) func(items ...any) []cbor.RawMessage {
	return func(items ...any) []cbor.RawMessage {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), items...)
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return nil
		}
		return r.openUnder(t, sid, r.keys, reply).Items
	}
}

// The hello says the limit a minion starts with; for a build whose factory
// declares max tasks, the operator's goes with every message core sends,
// whatever the minion asks for (a busy minion asks for no tasks), until the
// minion says it has it.
func TestMaxTasks(t *testing.T) {
	r := newRig(t)
	five := uint64(5)
	build := 5
	r.build.MaxTasks = &build
	sid, row := helloWith(t, r, &five)
	if row.MaxTasks == nil || *row.MaxTasks != 5 || !row.MaxTasksSettable || row.MaxTasksDue() != nil {
		t.Fatalf("after the hello: %v %v settable %v", row.MaxTasks, row.MaxTasksSet, row.MaxTasksSettable)
	}
	checkIn := checkInOf(t, r, sid)
	if m := maxTasksIn(t, checkIn()); m != nil {
		t.Fatalf("a limit sent with none set: %d", *m)
	}

	// The operator's: with each reply, no get_tasks needed, until the
	// minion says it has it.
	eight := 8
	row.MaxTasksSet = &eight
	for range 2 {
		if m := maxTasksIn(t, checkIn()); m == nil || *m != 8 {
			t.Fatalf("the operator's limit not sent: %v", m)
		}
	}
	if m := maxTasksIn(t, checkIn(protocol.MaxTasksItem{T: protocol.ItemMaxTasks, Max: 8})); m != nil {
		t.Fatalf("sent again once the minion has it: %d", *m)
	}
	if *row.MaxTasks != 8 || row.MaxTasksDue() != nil {
		t.Fatalf("after the minion's: %v", *row.MaxTasks)
	}

	// A process that claims the session with the build's limit (a rehello
	// here) gets the operator's again.
	h := hello("web01")
	h.MaxTasks = &five
	if m := maxTasksIn(t, checkIn(h)); m == nil || *m != 8 || *row.MaxTasks != 5 {
		t.Fatalf("after a hello with another limit: %v, the minion's %d", m, *row.MaxTasks)
	}

	// A minion's own that does not read is refused; one over the limit too.
	errs := 0
	for _, it := range checkIn(protocol.MaxTasksItem{T: protocol.ItemMaxTasks, Max: protocol.MaxTasksLimit + 1}) {
		if protocol.ItemType(it) == protocol.ItemErr {
			errs++
		}
	}
	if errs != 1 {
		t.Fatalf("%d errs for a limit over the most", errs)
	}

	// Over a socket: pushed at once, with no allowance at all.
	c := newConnRig(t, r, sid, "c1")
	if _, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.MaxTasksItem{T: protocol.ItemMaxTasks, Max: 8}); err != nil {
		t.Fatal(err)
	}
	three := 3
	row.MaxTasksSet, row.MaxTasksSends = &three, 0
	r.svc.Notify(context.Background(), sid)
	pushed := c.pushes(t)
	if len(pushed) != 1 {
		t.Fatalf("%d pushes", len(pushed))
	}
	if m := maxTasksIn(t, pushed[0].Items); m == nil || *m != 3 {
		t.Fatalf("the push: %+v", pushed[0])
	}
}

// A minion that never says it has the operator's limit (one that does not
// know the item) is sent it with MaxTasksTries messages, then no more: the
// session says it was not taken; setting it again, or the minion saying a
// limit (a rehello), tries again.
func TestMaxTasksNotTaken(t *testing.T) {
	r := newRig(t)
	five, build, eight := uint64(5), 5, 8
	r.build.MaxTasks = &build
	sid, row := helloWith(t, r, &five)
	checkIn := checkInOf(t, r, sid)
	row.MaxTasksSet = &eight
	sent := 0
	for range models.MaxTasksTries + 3 {
		if maxTasksIn(t, checkIn()) != nil {
			sent++
		}
	}
	if sent != models.MaxTasksTries || row.MaxTasksDue() != nil || row.MaxTasksNotTaken() == nil || *row.MaxTasksNotTaken() != 8 {
		t.Fatalf("sent %d times; due %v, not taken %v", sent, row.MaxTasksDue(), row.MaxTasksNotTaken())
	}
	h := hello("web01")
	h.MaxTasks = &five
	if m := maxTasksIn(t, checkIn(h)); m == nil || *m != 8 {
		t.Fatalf("a rehello did not try again: %v", m)
	}
}

// A minion whose build's factory sets its limit (no max tasks declared)
// may say it, and is never sent one, whatever is set.
func TestMaxTasksFixedByTheFactory(t *testing.T) {
	r := newRig(t)
	one, three := uint64(1), 3
	sid, row := helloWith(t, r, &one)
	if row.MaxTasksSettable || row.MaxTasks == nil || *row.MaxTasks != 1 {
		t.Fatalf("a fixed limit: settable %v, %v", row.MaxTasksSettable, row.MaxTasks)
	}
	row.MaxTasksSet = &three
	if row.MaxTasksDue() != nil || row.MaxTasksNotTaken() != nil || maxTasksIn(t, checkInOf(t, r, sid)()) != nil {
		t.Fatal("a limit sent to a minion whose factory sets it")
	}
	// None said at all: nothing either.
	r2 := newRig(t)
	_, none := helloWith(t, r2, nil)
	if none.MaxTasks != nil || (models.MinionSession{MaxTasksSet: &three, MaxTasksSettable: true}).MaxTasksDue() != nil {
		t.Fatal("a limit for a minion that says none")
	}
}

// A max_tasks command (core's own, kept as a task) ends done once the
// minion says its limit, at once when it has it already; failed when a
// newer limit replaces it, or the minion does not take it. It takes none
// of the minion's room.
func TestMaxTasksCommand(t *testing.T) {
	r := newRig(t)
	five, build := uint64(5), 5
	r.build.MaxTasks = &build
	sid, row := helloWith(t, r, &five)
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	ctx := context.Background()
	command := func(max int) uuid.UUID {
		t.Helper()
		id := uuid.New()
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
			Command: protocol.CommandMaxTasks, Args: map[string]any{"max": max}, State: models.MinionTaskRunning})
		if err := r.svc.SetMaxTasks(ctx, row, max); err != nil {
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

	// Replaced before the minion had it, then had: one failed, one done.
	first := command(8)
	second := command(3)
	if e := end(first); e == nil || *e.OK || !strings.Contains(e.Err, "replaced by max tasks 3") {
		t.Fatalf("the replaced one: %+v", e)
	}
	if end(second) != nil {
		t.Fatal("ended before the minion had it")
	}
	checkIn := checkInOf(t, r, sid)
	checkIn(protocol.MaxTasksItem{T: protocol.ItemMaxTasks, Max: 3})
	if e := end(second); e == nil || !*e.OK || !strings.Contains(string(e.Out), "runs 3 at once") {
		t.Fatalf("the one the minion has: %+v", e)
	}

	// The limit it has already: done at once.
	if e := end(command(3)); e == nil || !*e.OK {
		t.Fatalf("the limit it had: %+v", e)
	}

	// Never answered: failed once it counts as not taken.
	last := command(0)
	for range models.MaxTasksTries + 1 {
		checkIn()
	}
	if e := end(last); e == nil || *e.OK || !strings.Contains(e.Err, "did not take it") {
		t.Fatalf("the one not taken: %+v", e)
	}

	// Refused: a limit past the most, a fixed limit.
	if err := r.svc.SetMaxTasks(ctx, row, protocol.MaxTasksLimit+1); !errors.Is(err, models.ErrMaxTasksRange) {
		t.Fatalf("past the most: %v", err)
	}
	row.MaxTasksSettable = false
	if err := r.svc.SetMaxTasks(ctx, row, 2); !errors.Is(err, models.ErrMaxTasksFixed) {
		t.Fatalf("a fixed limit: %v", err)
	}
}
