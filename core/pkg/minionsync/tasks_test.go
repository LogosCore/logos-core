package minionsync

import (
	"context"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

// fakeTasks keeps tasks in memory, by the repository's rules as far as the
// service uses them (the real ones: TestIntegrationMinionTasks).
type fakeTasks struct {
	repository.IMinionTaskRepository
	rows    []models.MinionTask
	results map[uuid.UUID][]repository.MinionTaskResult
}

func (f *fakeTasks) find(id uuid.UUID) *models.MinionTask {
	for i := range f.rows {
		if f.rows[i].TaskID == id {
			return &f.rows[i]
		}
	}
	return nil
}

func (f *fakeTasks) ForDelivery(_ context.Context, sid uuid.UUID, limit int) ([]models.MinionTask, error) {
	var resend, queued []models.MinionTask
	for _, t := range f.rows {
		if t.SessionID != sid || t.CancelRequestedAt != nil {
			continue
		}
		if t.State == models.MinionTaskSent && t.AckedAt == nil {
			resend = append(resend, t)
		} else if t.State == models.MinionTaskQueued {
			queued = append(queued, t)
		}
	}
	out := append(resend, queued...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeTasks) MarkSent(_ context.Context, ids []uuid.UUID, at time.Time) error {
	for _, id := range ids {
		t := f.find(id)
		if t.State == models.MinionTaskQueued {
			t.State, t.SentAt = models.MinionTaskSent, &at
		}
		t.SendCount++
	}
	return nil
}

func (f *fakeTasks) Ack(_ context.Context, _ uuid.UUID, ids []uuid.UUID, at time.Time) error {
	for _, id := range ids {
		if t := f.find(id); t != nil && t.AckedAt == nil {
			t.AckedAt = &at
		}
	}
	return nil
}

func (f *fakeTasks) AddResult(_ context.Context, sid, id uuid.UUID, r repository.MinionTaskResult) (*models.MinionTask, bool, error) {
	t := f.find(id)
	if t == nil || t.SessionID != sid {
		return nil, false, qmgo.ErrNoSuchDocuments
	}
	f.results[id] = append(f.results[id], r)
	t.AckedAt, t.State = &r.At, models.MinionTaskRunning
	if r.Done {
		t.State = models.MinionTaskDone
	}
	return t, true, nil
}

func (f *fakeTasks) CancelsToSend(_ context.Context, sid uuid.UUID, _ time.Time) ([]models.MinionTask, []models.MinionTask, error) {
	var send []models.MinionTask
	for _, t := range f.rows {
		if t.SessionID == sid && t.CancelRequestedAt != nil && !t.Finished() {
			send = append(send, t)
		}
	}
	return send, nil, nil
}

// openSession opens a session with a hello straight under the build id, and
// gives the service a task store.
func openSession(t *testing.T, r *rig) (uuid.UUID, *fakeTasks) {
	t.Helper()
	tasks := &fakeTasks{results: map[uuid.UUID][]repository.MinionTaskResult{}}
	r.svc.WithTasks(tasks)
	reply, err := r.send(t, protocol.NewMessageID(), hello("web01"))
	if err != nil {
		t.Fatal(err)
	}
	var resp protocol.HelloResponseItem
	if err := protocol.UnmarshalCBOR(r.open(t, reply).Items[0], &resp); err != nil {
		t.Fatal(err)
	}
	sid, _ := uuid.FromBytes(resp.SID)
	return sid, tasks
}

func itemsOf(msg protocol.CoreMessage, kind string) []cbor.RawMessage {
	var out []cbor.RawMessage
	for _, it := range msg.Items {
		if protocol.ItemType(it) == kind {
			out = append(out, it)
		}
	}
	return out
}

func TestTasksUnderSession(t *testing.T) {
	r := newRig(t)
	sid, tasks := openSession(t, r)
	queue := func(cmd string, args map[string]any) uuid.UUID {
		id := uuid.New()
		var wire []byte
		if args != nil {
			wire, _ = protocol.MarshalCBOR(args)
		}
		tasks.rows = append(tasks.rows, models.MinionTask{TaskID: id, SessionID: sid, OperationID: r.build.OperationID,
			Command: cmd, WireArgs: wire, State: models.MinionTaskQueued})
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
	one := uint64(1)
	shell := queue("shell", map[string]any{"command": "whoami", "timeout": uint64(30000)})
	ls := queue("ls", nil)

	// No get_tasks, no task: an empty check-in gets nothing (204).
	if msg := send(); len(msg.Items) != 0 {
		t.Fatalf("an empty check-in got %d items", len(msg.Items))
	}
	// Asked for one: the oldest, with its args.
	msg := send(protocol.GetTasksItem{T: protocol.ItemGetTasks, Max: &one})
	got := itemsOf(msg, protocol.ItemTask)
	var task protocol.TaskItem
	if len(got) != 1 || protocol.UnmarshalCBOR(got[0], &task) != nil || task.Cmd != "shell" || task.Args["command"] != "whoami" ||
		task.Args["timeout"] != uint64(30000) {
		t.Fatalf("first delivery: %+v", task)
	}
	if id, _ := uuid.FromBytes(task.Task); id != shell || tasks.find(shell).State != models.MinionTaskSent {
		t.Fatalf("the shell task, sent: %s", tasks.find(shell).State)
	}
	// Not acked: sent again first, with the next.
	msg = send(protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if got = itemsOf(msg, protocol.ItemTask); len(got) != 2 {
		t.Fatalf("resend: %d tasks", len(got))
	}
	// Acked with a result in the same message: nothing more to send.
	ok := true
	msg = send(protocol.AckItem{T: protocol.ItemAck, Tasks: [][]byte{shell[:], ls[:]}},
		protocol.ResultItem{T: protocol.ItemResult, Task: shell[:], Seq: 0, Done: true, OK: &ok, Out: []byte("root\n")},
		protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if len(itemsOf(msg, protocol.ItemTask)) != 0 || len(itemsOf(msg, protocol.ItemErr)) != 0 {
		t.Fatalf("after the ack: %d items", len(msg.Items))
	}
	if tasks.find(shell).State != models.MinionTaskDone || string(tasks.results[shell][0].Out) != "root\n" {
		t.Fatalf("the shell task: %s", tasks.find(shell).State)
	}
	// A result for a task core does not have.
	stray := uuid.New()
	msg = send(protocol.ResultItem{T: protocol.ItemResult, Task: stray[:], Done: true, OK: &ok})
	var e protocol.ErrItem
	if errs := itemsOf(msg, protocol.ItemErr); len(errs) != 1 || protocol.UnmarshalCBOR(errs[0], &e) != nil || e.Code != protocol.ErrItemUnknownTask {
		t.Fatalf("stray result: %+v", e)
	}
	// An operator's stop: a cancel even with no get_tasks.
	now := time.Now()
	tasks.find(ls).CancelRequestedAt = &now
	msg = send()
	var c protocol.CancelItem
	if cancels := itemsOf(msg, protocol.ItemCancel); len(cancels) != 1 || protocol.UnmarshalCBOR(cancels[0], &c) != nil {
		t.Fatalf("cancel: %d items", len(msg.Items))
	}
	if id, _ := uuid.FromBytes(c.Task); id != ls {
		t.Fatalf("cancelled %s", id)
	}
	// Two get_tasks in one message: the second is refused.
	msg = send(protocol.GetTasksItem{T: protocol.ItemGetTasks}, protocol.GetTasksItem{T: protocol.ItemGetTasks})
	if errs := itemsOf(msg, protocol.ItemErr); len(errs) != 1 {
		t.Fatalf("two get_tasks: %d errs", len(errs))
	}
}
