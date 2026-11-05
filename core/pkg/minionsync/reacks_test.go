package minionsync

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// reackOf is the tasks the reack among items names; nil when there is none.
func reackOf(t *testing.T, msg protocol.CoreMessage) []uuid.UUID {
	t.Helper()
	for _, raw := range msg.Items {
		if protocol.ItemType(raw) != protocol.ItemReack {
			continue
		}
		var r protocol.ReackItem
		if err := protocol.UnmarshalCBOR(raw, &r); err != nil || r.Validate() != nil {
			t.Fatalf("a reack that does not read: %v", err)
		}
		ids := make([]uuid.UUID, len(r.Tasks))
		for i, b := range r.Tasks {
			ids[i] = uuid.UUID(b)
		}
		return ids
	}
	return nil
}

// unknownTask is the minion's err about a task it does not have.
func unknownTask(id uuid.UUID) protocol.ErrItem {
	item := 0
	return protocol.ErrItem{T: protocol.ItemErr, Code: protocol.ErrItemUnknownTask, Msg: "no such task running", Item: &item, Task: id[:]}
}

// A new process claims a persisted session: core asks it, in its next
// message, which of the tasks it runs it still has, once. Those it does not
// have end failed.
func TestReackAfterAClaim(t *testing.T) {
	r := newRig(t)
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	reply, _ := r.send(t, protocol.NewMessageID(), hello("web01"))
	sid := sessionID(t, r.open(t, reply))
	now := time.Now()
	running, queued, done := uuid.New(), uuid.New(), uuid.New()
	tasks.rows = []models.MinionTask{
		{TaskID: running, SessionID: sid, State: models.MinionTaskRunning, AckedAt: &now},
		{TaskID: queued, SessionID: sid, State: models.MinionTaskQueued},
		{TaskID: done, SessionID: sid, State: models.MinionTaskDone, AckedAt: &now},
	}
	b2, keys2 := r.persistedBuild(t, sid)
	if _, err := r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("web01-rebooted", sid)); err != nil {
		t.Fatal(err)
	}
	// Its next check-in's reply asks about the running one.
	reply, err := r.sendUnder(t, sid, keys2, protocol.NewMessageID())
	if err != nil {
		t.Fatal(err)
	}
	if got := reackOf(t, r.openUnder(t, sid, keys2, reply)); len(got) != 1 || got[0] != running {
		t.Fatalf("the reack asked about %v, not %s", got, running)
	}
	// It does not have it: the task ends.
	if _, err := r.sendUnder(t, sid, keys2, protocol.NewMessageID(), unknownTask(running)); err != nil {
		t.Fatal(err)
	}
	if got := tasks.find(running); got.State != models.MinionTaskFailed || got.Err == "" {
		t.Fatalf("the lost task: %s %q", got.State, got.Err)
	}
	// Asked once.
	reply, err = r.sendUnder(t, sid, keys2, protocol.NewMessageID())
	if err != nil {
		t.Fatal(err)
	}
	if reply != nil && reackOf(t, r.openUnder(t, sid, keys2, reply)) != nil {
		t.Fatal("asked again")
	}
}

// A channel acting as the minion back over a new connection (a session
// going on, under its id) is asked which tasks it still has, in the reply
// to its hello; it does not have those that ran in the connection that
// went, and they end.
func TestReackOverANewConnection(t *testing.T) {
	b, ch := newAgentlessRig(t, protocol.KindBindPersistent)
	sid, now := uuid.New(), time.Now()
	b.sessions.rows = append(b.sessions.rows, models.MinionSession{SessionID: sid, BuildID: b.build.BuildID, OperationID: b.build.OperationID,
		Conn: 1, ConnKind: protocol.KindBindPersistent, Crypto: *b.build.Crypto, CreatedAt: now, LastSeenAt: &now})
	row := b.targets.rows[b.target]
	row.SessionID = &sid
	b.targets.rows[b.target] = row
	lost := uuid.New()
	b.tasks.rows = []models.MinionTask{{TaskID: lost, SessionID: sid, State: models.MinionTaskRunning, AckedAt: &now}}
	if tg, err := b.svc.ConnectTarget(context.Background(), b.target); err != nil || tg.State != models.TargetConnected {
		t.Fatalf("connect: %+v %v", tg, err)
	}
	ch.settle()
	if ch.unders[0] != sid {
		t.Fatalf("the open said %s", ch.unders[0])
	}
	if got := reackOf(t, openCore(t, b.keys, sid, ch.replies[0])); len(got) != 1 || got[0] != lost {
		t.Fatalf("the reack asked about %v", got)
	}
	if _, err := b.svc.Exchange(context.Background(), ch.connAt(0), sid.String(), "203.0.113.9",
		sealedMinionMessage(t, b.keys, sid, nil, unknownTask(lost))); err != nil {
		t.Fatal(err)
	}
	if got := b.tasks.find(lost); got.State != models.MinionTaskFailed {
		t.Fatalf("the lost task: %s", got.State)
	}
}

// A minion that keeps its tasks between connections (BOB) acks those it
// has: they go on, the new connection's now, which stays open while they
// run.
func TestReackAckedTasksKeepTheConnection(t *testing.T) {
	shortSteady(t, 0)
	b, l := newPersistentRig(t, models.PolicyOnDemand)
	sid, now := uuid.New(), time.Now()
	b.sessions.rows = append(b.sessions.rows, models.MinionSession{SessionID: sid, BuildID: b.build.BuildID, OperationID: b.build.OperationID,
		Conn: 1, ConnKind: protocol.KindBindPersistent, Crypto: *b.build.Crypto, CreatedAt: now, LastSeenAt: &now})
	row := b.targets.rows[b.target]
	row.SessionID = &sid
	b.targets.rows[b.target] = row
	long := uuid.New()
	b.tasks.rows = []models.MinionTask{{TaskID: long, SessionID: sid, State: models.MinionTaskRunning, AckedAt: &now}}
	replies := make(chan []byte, 1)
	l.setSpeak(func(c *Conn) {
		max := uint64(4)
		replies <- b.speak(t, c, sid, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &max})
	})
	if _, err := b.svc.CheckTarget(context.Background(), b.target); err != nil {
		t.Fatal(err)
	}
	l.settle()
	if got := reackOf(t, openCore(t, b.keys, sid, <-replies)); len(got) != 1 || got[0] != long {
		t.Fatalf("the reack asked about %v", got)
	}
	c := l.connAt(0)
	b.speak(t, c, sid, protocol.AckItem{T: protocol.ItemAck, Tasks: [][]byte{long[:]}})
	if got := b.tasks.find(long); got.State != models.MinionTaskRunning {
		t.Fatalf("an acked task: %s", got.State)
	}
	c.touch(time.Now().Add(-11 * time.Minute))
	b.svc.closeIdle(context.Background())
	time.Sleep(50 * time.Millisecond)
	if l.closedCount() != 0 {
		t.Fatal("closed idle while a task it acked runs")
	}
}

// A reack names as many tasks as the message has room for; the rest go in
// the next.
func TestReackFitsTheRoom(t *testing.T) {
	r := newRig(t)
	sid := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	r.svc.reacks.bySession[sid] = reackWait{tasks: ids, at: time.Now()}
	item, asked, err := r.svc.reackItem(sid, itemOverhead+reackHead+2*reackIDSize)
	if err != nil || len(asked) != 2 || len(item)+itemOverhead > itemOverhead+reackHead+2*reackIDSize {
		t.Fatalf("%d asked in %d bytes (%v)", len(asked), len(item), err)
	}
	r.svc.reacked(sid, asked)
	if _, rest, _ := r.svc.reackItem(sid, 1<<16); len(rest) != 2 || rest[0] != ids[2] {
		t.Fatalf("left %v", rest)
	}
	r.svc.reacked(sid, ids[2:])
	if item, _, _ := r.svc.reackItem(sid, 1<<16); item != nil {
		t.Fatal("asked again")
	}
}

// A bind-call minion's claim asks nothing: core holds where its tasks
// stand, and its calls carry them on.
func TestNoReackAfterABindCallClaim(t *testing.T) {
	r := newRig(t)
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	reply, _ := r.send(t, protocol.NewMessageID(), hello("page01"))
	sid := sessionID(t, r.open(t, reply))
	r.sessions.rows[0].ConnKind = protocol.KindBindCall
	now := time.Now()
	tasks.rows = []models.MinionTask{{TaskID: uuid.New(), SessionID: sid, State: models.MinionTaskRunning, AckedAt: &now}}
	b2, keys2 := r.persistedBuild(t, sid)
	if _, err := r.sendUnder(t, b2.BuildID, keys2, protocol.NewMessageID(), claimHello("page01", sid)); err != nil {
		t.Fatal(err)
	}
	reply, err := r.sendUnder(t, sid, keys2, protocol.NewMessageID())
	if err != nil {
		t.Fatal(err)
	}
	if reply != nil && reackOf(t, r.openUnder(t, sid, keys2, reply)) != nil {
		t.Fatal("a bind-call minion asked which tasks it has")
	}
}
