package minionsync

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/minionstreams"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// connRig is a persistent connection to the rig's service: what core sends
// on it is kept, but for the next fail sends, which fail with the socket
// still open.
type connRig struct {
	r      *rig
	sid    uuid.UUID
	conn   *Conn
	mu     sync.Mutex
	sent   []protocol.ConnFrame
	closed bool
	fail   int
}

func newConnRig(t *testing.T, r *rig, sid uuid.UUID, name string) *connRig {
	c := &connRig{r: r, sid: sid}
	c.conn = NewConn(name, protocol.KindReversePersistent, "http-1", "10.0.0.9", func(_ context.Context, f protocol.ConnFrame) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed {
			return errors.New("closed")
		}
		if c.fail > 0 {
			c.fail--
			return errors.New("the write failed")
		}
		c.sent = append(c.sent, f)
		return nil
	}, func() { c.mu.Lock(); c.closed = true; c.mu.Unlock() })
	return c
}

// exchange sends a message over the connection, a request (id) or an
// answer (re).
func (c *connRig) exchange(t *testing.T, id, re []byte, items ...any) ([]byte, error) {
	t.Helper()
	msg := protocol.MinionMessage{V: protocol.MinionMessageV3, ID: id, Re: re}
	for _, it := range items {
		raw, err := protocol.MarshalCBOR(it)
		if err != nil {
			t.Fatal(err)
		}
		msg.Items = append(msg.Items, raw)
	}
	plain, _ := protocol.MarshalCBOR(msg)
	frame, err := protocol.SealFrame(c.r.keys, c.sid, protocol.FrameMinionToCore, plain)
	if err != nil {
		t.Fatal(err)
	}
	return c.r.svc.Exchange(context.Background(), c.conn, c.sid.String(), "10.0.0.9", frame)
}

// pushes takes what core sent on the connection since the last call, opened.
func (c *connRig) pushes(t *testing.T) []protocol.CoreMessage {
	t.Helper()
	c.mu.Lock()
	sent := c.sent
	c.sent = nil
	c.mu.Unlock()
	var out []protocol.CoreMessage
	for _, f := range sent {
		if !bytes.Equal(f.ID, c.sid[:]) {
			t.Fatalf("a push under %x, not the session's id", f.ID)
		}
		msg := c.r.openUnder(t, c.sid, c.r.keys, f.Data)
		if msg.ID == nil || msg.Re != nil {
			t.Fatalf("a push that does not start an exchange: %+v", msg)
		}
		out = append(out, msg)
	}
	return out
}

func taskIDsIn(t *testing.T, msg protocol.CoreMessage) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	for _, raw := range itemsOf(msg, protocol.ItemTask) {
		var task protocol.TaskItem
		if err := protocol.UnmarshalCBOR(raw, &task); err != nil {
			t.Fatal(err)
		}
		id, _ := uuid.FromBytes(task.Task)
		ids = append(ids, id)
	}
	return ids
}

// A persistent connection: the minion's get_tasks is an allowance core
// pushes tasks within, as they are queued and as tasks end; its answers to
// core's pushes are taken; max 0 pauses; closing the socket leaves the
// session unconnected.
func TestPersistentConnection(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := newConnRig(t, r, sid, "c1")
	ctx := context.Background()
	queue := func(cmd string, op int) uuid.UUID {
		id := uuid.New()
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
			Command: cmd, Op: op, State: models.MinionTaskQueued})
		return id
	}

	if r.svc.Connected(sid) {
		t.Fatal("connected before any message")
	}
	// A check-in over the connection with an allowance of 1, nothing
	// queued: connected, nothing sent.
	one := uint64(1)
	reply, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &one})
	if err != nil || reply == nil {
		t.Fatalf("check-in: %x %v", reply, err)
	}
	// Over a socket a request always gets its reply: here an empty one.
	if msg := r.openUnder(t, sid, r.keys, reply); msg.Re == nil || len(msg.Items) != 0 {
		t.Fatalf("check-in's reply %+v", msg)
	}
	if !r.svc.Connected(sid) {
		t.Fatal("not connected after a message over the connection")
	}

	// Two tasks queued: one push, the first, within the allowance.
	first, second := queue("shell", 11), queue("ls", 4)
	r.svc.Notify(ctx, sid)
	pushed := c.pushes(t)
	if len(pushed) != 1 || len(taskIDsIn(t, pushed[0])) != 1 || taskIDsIn(t, pushed[0])[0] != first {
		t.Fatalf("pushes %+v", pushed)
	}
	// The minion answers it with an ack: taken, no reply; its task runs, so
	// no room.
	if reply, err := c.exchange(t, nil, pushed[0].ID, protocol.AckItem{T: protocol.ItemAck, Tasks: [][]byte{first[:]}}); err != nil || reply != nil {
		t.Fatalf("answer: %x %v", reply, err)
	}
	if tasks.find(first).AckedAt == nil {
		t.Fatal("the answer's ack not taken")
	}
	r.svc.Notify(ctx, sid)
	if p := c.pushes(t); len(p) != 0 {
		t.Fatalf("a push past the allowance: %+v", p)
	}
	// An answer to nothing core waits for (a replay) is refused.
	if _, err := c.exchange(t, nil, pushed[0].ID, protocol.AckItem{T: protocol.ItemAck, Tasks: [][]byte{first[:]}}); !errors.Is(err, ErrUnopened) {
		t.Fatalf("a replayed answer: %v", err)
	}

	// The first task ends: room again, and the second goes.
	ok := true
	if _, err := c.exchange(t, protocol.NewMessageID(), nil,
		protocol.ResultItem{T: protocol.ItemResult, Task: first[:], Seq: 0, Done: true, OK: &ok}); err != nil {
		t.Fatal(err)
	}
	tasks.find(first).State = models.MinionTaskDone // the fake does not end tasks
	r.svc.Notify(ctx, sid)
	if p := c.pushes(t); len(p) != 1 || len(taskIDsIn(t, p[0])) != 1 || taskIDsIn(t, p[0])[0] != second {
		t.Fatalf("after the end: %+v", p)
	}

	// max 0 pauses: a third task waits.
	zero := uint64(0)
	if _, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &zero}); err != nil {
		t.Fatal(err)
	}
	third := queue("shell", 11)
	r.svc.Notify(ctx, sid)
	if p := c.pushes(t); len(p) != 0 {
		t.Fatalf("a push while paused: %+v", p)
	}
	if tasks.find(third).State != models.MinionTaskQueued {
		t.Fatal("the third task left while paused")
	}

	// A second connection carries a message: its reply brings what waits
	// (the third task, and the second, on its way over the first and never
	// acked), and pushes go there; the first closes and the session is
	// still connected, through the second.
	c2 := newConnRig(t, r, sid, "c2")
	reply, err = c2.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDsIn(t, r.openUnder(t, sid, r.keys, reply)); len(got) != 2 {
		t.Fatalf("the second connection's reply: %v", got)
	}
	if p := c2.pushes(t); len(p) != 0 {
		t.Fatal("a reply came as a push")
	}
	r.svc.Closed(ctx, c.conn)
	if !r.svc.Connected(sid) {
		t.Fatal("not connected with the second connection open")
	}
	// What the reply carried is on its way over the second: news pushes
	// it no second time.
	r.svc.Notify(ctx, sid)
	if p := c2.pushes(t); len(p) != 0 {
		t.Fatalf("pushed again over the connection that carries it: %+v", p)
	}
	r.svc.Closed(ctx, c2.conn)
	if r.svc.Connected(sid) {
		t.Fatal("connected after every connection closed")
	}
}

// A session keeps maxSessionConns connections: one more closes the oldest.
func TestSessionConnectionsCap(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	var conns []*connRig
	for i := range maxSessionConns + 1 {
		c := newConnRig(t, r, sid, "c"+string(rune('a'+i)))
		if _, err := c.exchange(t, protocol.NewMessageID(), nil); err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	if !conns[0].closed || conns[1].closed || len(r.svc.conns.of(sid)) != maxSessionConns {
		t.Fatalf("the oldest closed: %v, the next: %v, open %d", conns[0].closed, conns[1].closed, len(r.svc.conns.of(sid)))
	}
}

// fakeStreams answers Acks with acks, and soon; the rest is not used here.
type fakeStreams struct {
	acks []protocol.AckItem
	soon bool
}

func (f *fakeStreams) TakeChunk(context.Context, *models.MinionSession, protocol.ChunkItem) (minionstreams.Reply, bool, error) {
	return minionstreams.Reply{}, false, nil
}

func (f *fakeStreams) Acks(context.Context, *models.MinionSession, map[string]bool) ([]protocol.AckItem, bool, error) {
	return f.acks, f.soon, nil
}

func (f *fakeStreams) Pull(context.Context, *models.MinionTask, int64, int) (*protocol.ChunkItem, *protocol.AckItem, error) {
	return nil, nil, nil
}

// A downloader comes for a stream the minion holds: the stream's ack goes
// over the persistent connection at once, without waiting for a message.
func TestWakeStreams(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	next, xfer := uint64(0), bytes.Repeat([]byte{7}, 16)
	r.svc.WithStreams(&fakeStreams{acks: []protocol.AckItem{{T: protocol.ItemAck, Xfer: xfer, Next: &next}}})
	c := newConnRig(t, r, sid, "c1")

	// Not connected: nothing to push it on.
	r.svc.pushNow(context.Background(), sid, true)
	if p := c.pushes(t); len(p) != 0 {
		t.Fatalf("a push with no connection: %+v", p)
	}

	if _, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks}); err != nil {
		t.Fatal(err)
	}
	r.svc.pushNow(context.Background(), sid, true)
	p := c.pushes(t)
	if len(p) != 1 {
		t.Fatalf("pushes %+v", p)
	}
	acks := itemsOf(p[0], protocol.ItemAck)
	var a protocol.AckItem
	if len(acks) != 1 || protocol.UnmarshalCBOR(acks[0], &a) != nil || !bytes.Equal(a.Xfer, xfer) || a.Hold || a.Next == nil || *a.Next != 0 {
		t.Fatalf("the pushed ack %+v", a)
	}
}

// A session is recorded connected while a persistent connection serving it
// is open, for the Sessions list's filters; core clears it as it starts.
func TestConnectedRecorded(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	c := newConnRig(t, r, sid, "c1")
	if _, err := c.exchange(t, protocol.NewMessageID(), nil); err != nil {
		t.Fatal(err)
	}
	if !r.sessionRow(t, sid).Connected {
		t.Fatal("not recorded connected")
	}
	r.svc.Closed(context.Background(), c.conn)
	if r.sessionRow(t, sid).Connected {
		t.Fatal("still recorded connected once its connection closed")
	}
	r.sessionRow(t, sid).Connected = true
	r.svc.resetConnected(context.Background())
	if r.sessionRow(t, sid).Connected {
		t.Fatal("still connected after core started")
	}
}

// connected is a connection of the session that said its standing
// get_tasks, no max: pushes go there.
func connected(t *testing.T, r *rig, sid uuid.UUID, name string) *connRig {
	t.Helper()
	c := newConnRig(t, r, sid, name)
	if _, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks}); err != nil {
		t.Fatal(err)
	}
	return c
}

// queueTask queues a shell task for the session.
func queueTask(r *rig, sid uuid.UUID, tasks *fakeTasks) uuid.UUID {
	id := uuid.New()
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
		Command: "shell", Op: 11, State: models.MinionTaskQueued})
	return id
}

// carried is how many times msgs carry task id.
func carried(t *testing.T, msgs []protocol.CoreMessage, id uuid.UUID) int {
	t.Helper()
	n := 0
	for _, m := range msgs {
		for _, x := range taskIDsIn(t, m) {
			if x == id {
				n++
			}
		}
	}
	return n
}

// liveBus gives r's service a bus that delivers, WatchTasks on it: as in
// core, a change to a task is news for its session.
func liveBus(t *testing.T, r *rig) {
	bus := eventbus.NewEventBus(zap.NewNop())
	r.svc.bus = bus
	unwatch := r.svc.WatchTasks()
	bus.Start()
	t.Cleanup(func() {
		unwatch()
		bus.Stop(context.Background())
	})
}

// A task that goes out is news for the console, none for the minion's
// connections: no push for it. Any other change to it is.
func TestSentIsNoNews(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	liveBus(t, r)
	c := connected(t, r, sid, "c1")
	id := queueTask(r, sid, tasks)
	task := models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID}
	r.svc.publishSent(&task)
	time.Sleep(3 * notifyDelay)
	if p := c.pushes(t); len(p) != 0 {
		t.Fatalf("a push for a task going out: %+v", p)
	}
	r.svc.publishTask(&task)
	time.Sleep(3 * notifyDelay)
	if n := carried(t, c.pushes(t), id); n != 1 {
		t.Fatalf("the change pushed the task %d times", n)
	}
}

// A task pushed over a slow link goes once while its ack is on its way
// (round trips of seconds): neither its going out nor other news sends it
// again.
func TestPushedOnceWhileItsAckIsOnItsWay(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	liveBus(t, r)
	c := connected(t, r, sid, "c1")
	id := queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid)
	time.Sleep(5 * notifyDelay)
	if n := carried(t, c.pushes(t), id); n != 1 {
		t.Fatalf("pushed %d times while its ack is on its way, not once", n)
	}
}

// A push that fails while its connection stays open goes again by itself:
// no news may come to nudge it, and the minion sends nothing meanwhile.
func TestFailedPushGoesAgain(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := connected(t, r, sid, "c1")
	c.mu.Lock()
	c.fail = 1
	c.mu.Unlock()
	id := queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid)
	if p := c.pushes(t); len(p) != 0 {
		t.Fatalf("a failed push went: %+v", p)
	}
	n := 0
	for end := time.Now().Add(pushRetryWait + 2*time.Second); n == 0 && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
		n = carried(t, c.pushes(t), id)
	}
	if n != 1 {
		t.Fatalf("the retry carried the task %d times", n)
	}
}

// A failed push leaves its tasks free: the next push over the same
// connection carries them at once.
func TestFailedPushLetsItsTasksGo(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := connected(t, r, sid, "c1")
	c.mu.Lock()
	c.fail = 1
	c.mu.Unlock()
	id := queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid)
	r.svc.Notify(context.Background(), sid)
	if n := carried(t, c.pushes(t), id); n != 1 {
		t.Fatalf("after a failed push the task went %d times", n)
	}
}

// Tasks one push could not carry go in the reply to the minion's ack (its
// get_tasks with it), and nothing on its way is pushed again.
func TestLeftoversGoInTheAcksReply(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	liveBus(t, r)
	c := connected(t, r, sid, "c1")
	queued := map[uuid.UUID]bool{}
	for range models.MaxMinionTasksPerReply + 3 {
		queued[queueTask(r, sid, tasks)] = true
	}
	r.svc.Notify(context.Background(), sid)
	pushed := c.pushes(t)
	if len(pushed) != 1 || len(taskIDsIn(t, pushed[0])) != models.MaxMinionTasksPerReply {
		t.Fatalf("the first push: %d messages", len(pushed))
	}
	var acks [][]byte
	for _, id := range taskIDsIn(t, pushed[0]) {
		delete(queued, id)
		acks = append(acks, id[:])
	}
	reply, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.AckItem{T: protocol.ItemAck, Tasks: acks},
		protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if err != nil {
		t.Fatal(err)
	}
	rest := taskIDsIn(t, r.openUnder(t, sid, r.keys, reply))
	for _, id := range rest {
		delete(queued, id)
	}
	if len(rest) != 3 || len(queued) != 0 {
		t.Fatalf("the ack's reply: %d tasks, %d never went", len(rest), len(queued))
	}
	// The acks are news: the push they nudge has nothing to carry.
	time.Sleep(5 * notifyDelay)
	if p := c.pushes(t); len(p) != 0 {
		t.Fatalf("pushed again: %+v", p)
	}
}

// News for the session pushes what is new, not a task on its way over the
// same connection.
func TestNotAgainOverTheSameConnection(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := connected(t, r, sid, "c1")
	first := queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid)
	if n := carried(t, c.pushes(t), first); n != 1 {
		t.Fatalf("the first push carried the task %d times", n)
	}
	second := queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid)
	p := c.pushes(t)
	if carried(t, p, first) != 0 || carried(t, p, second) != 1 {
		t.Fatalf("the second push: %+v", p)
	}
}

// A task on its way over one connection goes again over another: the
// minion switched, or connected again.
func TestAgainOverANewConnection(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c1 := connected(t, r, sid, "c1")
	id := queueTask(r, sid, tasks)
	r.svc.Notify(context.Background(), sid)
	if n := carried(t, c1.pushes(t), id); n != 1 {
		t.Fatalf("the first push carried the task %d times", n)
	}
	c2 := newConnRig(t, r, sid, "c2")
	reply, err := c2.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if err != nil {
		t.Fatal(err)
	}
	if n := carried(t, []protocol.CoreMessage{r.openUnder(t, sid, r.keys, reply)}, id); n != 1 {
		t.Fatalf("the new connection's reply carried the task %d times", n)
	}
}

// A task whose ack never comes goes again over the same connection once
// its wait is over (a frame lost on the way), the wait doubling each time.
func TestAgainOverTheSameConnectionAfterItsWait(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := connected(t, r, sid, "c1")
	start := time.Now().UTC()
	var mu sync.Mutex
	at := start
	r.svc.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return at
	}
	id := queueTask(r, sid, tasks)
	// pushes is how many times a push at after carries the task.
	pushes := func(after time.Duration) int {
		mu.Lock()
		at = start.Add(after)
		mu.Unlock()
		r.svc.Notify(context.Background(), sid)
		return carried(t, c.pushes(t), id)
	}
	for _, step := range []struct {
		after time.Duration
		want  int
	}{
		{0, 1},
		{resendWait - time.Second, 0},
		{resendWait, 1},
		{resendWait + 2*resendWait - time.Second, 0},
		{resendWait + 2*resendWait, 1},
	} {
		if n := pushes(step.after); n != step.want {
			t.Fatalf("at %s the task went %d times, not %d", step.after, n, step.want)
		}
	}
}
