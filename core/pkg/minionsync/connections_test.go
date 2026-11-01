package minionsync

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// connRig is a persistent connection to the rig's service: what core sends
// on it is kept.
type connRig struct {
	r      *rig
	sid    uuid.UUID
	conn   *Conn
	mu     sync.Mutex
	sent   []protocol.ConnFrame
	closed bool
}

func newConnRig(t *testing.T, r *rig, sid uuid.UUID, name string) *connRig {
	c := &connRig{r: r, sid: sid}
	c.conn = NewConn(name, protocol.KindReversePersistent, "http-1", "10.0.0.9", func(_ context.Context, f protocol.ConnFrame) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed {
			return errors.New("closed")
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

	// A second connection carries a message: pushes go there; the first
	// closes and the session is still connected, through the second.
	c2 := newConnRig(t, r, sid, "c2")
	if _, err := c2.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks}); err != nil {
		t.Fatal(err)
	}
	if p := c2.pushes(t); len(p) != 0 {
		t.Fatal("a reply came as a push")
	}
	r.svc.Closed(ctx, c.conn)
	if !r.svc.Connected(sid) {
		t.Fatal("not connected with the second connection open")
	}
	r.svc.Notify(ctx, sid)
	if p := c2.pushes(t); len(p) != 1 || len(taskIDsIn(t, p[0])) == 0 {
		t.Fatalf("the second connection's push: %+v", p)
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
