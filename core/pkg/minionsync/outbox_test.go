package minionsync

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// sessionRow is the session's stored row, to change in place.
func (r *rig) sessionRow(t *testing.T, sid uuid.UUID) *models.MinionSession {
	t.Helper()
	for i := range r.sessions.rows {
		if r.sessions.rows[i].SessionID == sid {
			return &r.sessions.rows[i]
		}
	}
	t.Fatalf("no session %s", sid)
	return nil
}

// A minion that polls is told to come back soon while a stream of its is
// held with its downloader (or uploader) there; one over a socket is not:
// core pushes to it.
func TestPollWhenAStreamMayGoSoon(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	streams := &fakeStreams{}
	r.svc.WithStreams(streams)
	checkIn := func() protocol.CoreMessage {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID())
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return protocol.CoreMessage{}
		}
		return r.openUnder(t, sid, r.keys, reply)
	}
	if msg := checkIn(); len(itemsOf(msg, protocol.ItemPoll)) != 0 {
		t.Fatal("a poll with no stream held")
	}
	streams.soon = true
	polls := itemsOf(checkIn(), protocol.ItemPoll)
	var p protocol.PollItem
	if len(polls) != 1 || protocol.UnmarshalCBOR(polls[0], &p) != nil || p.Within != uint64(pollSoon.Milliseconds()) {
		t.Fatalf("polls %d: %+v", len(polls), p)
	}
	c := newConnRig(t, r, sid, "c1")
	reply, err := c.exchange(t, protocol.NewMessageID(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg := r.openUnder(t, sid, r.keys, reply); len(itemsOf(msg, protocol.ItemPoll)) != 0 {
		t.Fatal("a poll over a socket")
	}
}

// An err about an item that said a ctx carries the ctx back: a page that
// keeps nothing finds there what to clean up.
func TestErrEchoesTheItemsCtx(t *testing.T) {
	r := newRig(t)
	sid, _ := openSession(t, r)
	reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(),
		protocol.ChunkItem{T: protocol.ItemChunk, Xfer: bytes.Repeat([]byte{9}, 16), Data: []byte("x"), Ctx: []byte("part=/tmp/a.part")})
	if err != nil {
		t.Fatal(err)
	}
	errs := itemsOf(r.openUnder(t, sid, r.keys, reply), protocol.ItemErr)
	var e protocol.ErrItem
	if len(errs) != 1 || protocol.UnmarshalCBOR(errs[0], &e) != nil || e.Item == nil || *e.Item != 0 || string(e.Ctx) != "part=/tmp/a.part" {
		t.Fatalf("errs %d: %+v", len(errs), e)
	}
}

// What core sends keeps within the session's connection's messages: tasks
// that do not fit wait for the next message, and one no message of the
// connection could carry ends failed, saying so.
func TestOutboxKeepsWithinTheConnection(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	r.sessionRow(t, sid).ConnMaxMessage = 2048
	queue := func(argBytes int) uuid.UUID {
		id := uuid.New()
		wire, _ := protocol.MarshalCBOR(protocol.Args{1: strings.Repeat("a", argBytes)})
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
			Command: "shell", Op: 11, WireArgs: wire, State: models.MinionTaskQueued})
		return id
	}
	huge := queue(4000)
	var small []uuid.UUID
	for range 5 {
		small = append(small, queue(500))
	}
	ask := func() []protocol.TaskItem {
		t.Helper()
		reply, err := r.sendUnder(t, sid, r.keys, protocol.NewMessageID(), protocol.GetTasksItem{T: protocol.ItemGetTasks})
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return nil // nothing left: 204
		}
		if len(reply) > 2048+protocol.MinFrameSize {
			t.Fatalf("a reply of %d bytes over a connection of 2048", len(reply))
		}
		var out []protocol.TaskItem
		for _, raw := range itemsOf(r.openUnder(t, sid, r.keys, reply), protocol.ItemTask) {
			var ti protocol.TaskItem
			_ = protocol.UnmarshalCBOR(raw, &ti)
			out = append(out, ti)
		}
		return out
	}
	first := ask()
	if n := len(first); n == 0 || n >= len(small) {
		t.Fatalf("%d tasks in a 2048-byte reply", n)
	}
	if h := tasks.find(huge); h.State != models.MinionTaskFailed || !strings.Contains(h.Err, "too large for the connection") {
		t.Fatalf("the task no message carries: %s %q", h.State, h.Err)
	}
	// The rest come in later replies, those sent and not acked first.
	ids := map[uuid.UUID]bool{}
	for _, ti := range first {
		ids[uuid.UUID(ti.Task)] = true
		_ = tasks.Ack(context.Background(), sid, []uuid.UUID{uuid.UUID(ti.Task)}, time.Now(), nil)
	}
	for range 5 {
		for _, ti := range ask() {
			ids[uuid.UUID(ti.Task)] = true
			_ = tasks.Ack(context.Background(), sid, []uuid.UUID{uuid.UUID(ti.Task)}, time.Now(), nil)
		}
	}
	for _, id := range small {
		if !ids[id] {
			t.Fatalf("task %s never went", id)
		}
	}
}

// A push says core's time (at), as a call does.
func TestPushSaysAt(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	c := newConnRig(t, r, sid, "c1")
	if _, err := c.exchange(t, protocol.NewMessageID(), nil, protocol.GetTasksItem{T: protocol.ItemGetTasks}); err != nil {
		t.Fatal(err)
	}
	tasks.rows = append(tasks.rows, models.MinionTask{TaskID: uuid.New(), SessionID: sid, OperationID: r.build.OperationID,
		Command: "ls", Op: 4, State: models.MinionTaskQueued})
	r.svc.Notify(context.Background(), sid)
	p := c.pushes(t)
	if len(p) != 1 || p[0].At == 0 || time.Since(time.Unix(int64(p[0].At), 0)) > time.Minute {
		t.Fatalf("pushes %+v", p)
	}
}

// A minion on a connection that says whether it is there (bind-persistent)
// is never late by its schedule; on any other it is.
func TestLateBySchedule(t *testing.T) {
	now, sleep := time.Now(), &models.MinionSessionSleep{Interval: 60000, Jitter: 10}
	if c := checkIn(now, "", nil, sleep, protocol.KindBindPersistent); !c.Untimed || c.LateAt != nil {
		t.Fatalf("bind-persistent: %+v", c)
	}
	for _, kind := range []string{protocol.KindReverseCall, protocol.KindReversePersistent, protocol.KindBindCall, ""} {
		if c := checkIn(now, "", nil, sleep, kind); c.Untimed || c.LateAt == nil {
			t.Fatalf("%q: %+v", kind, c)
		}
	}
	// Moving to a listening connection clears what was late.
	r := newRig(t)
	sid, _ := openSession(t, r)
	late := now.Add(-time.Hour)
	r.sessionRow(t, sid).LateAt = &late
	if err := r.sessions.SetConn(context.Background(), sid, models.MinionSessionConn{Place: 2, Kind: protocol.KindBindPersistent}, nil); err != nil {
		t.Fatal(err)
	}
	if r.sessionRow(t, sid).LateAt != nil {
		t.Fatal("still late on a bind-persistent connection")
	}
}

// A page that refuses a call as stale (its clock far from core's) leaves
// its target refused, saying so.
func TestBindStalePage(t *testing.T) {
	b := newBindRig(t, true)
	tg, err := b.svc.CheckTarget(context.Background(), b.target)
	if err != nil || tg.SessionID == nil {
		t.Fatalf("%+v %v", tg, err)
	}
	b.page.more = func(protocol.CoreMessage) []any {
		return []any{protocol.ErrItem{T: protocol.ItemErr, Code: protocol.ErrItemStale, Msg: "the page's time is 1700000000"}}
	}
	tg, err = b.svc.CheckTarget(context.Background(), b.target)
	if err != nil {
		t.Fatal(err)
	}
	if tg.State != models.TargetRefused || !strings.Contains(tg.StateDetail, "stale") {
		t.Fatalf("target %s: %s", tg.State, tg.StateDetail)
	}
}
