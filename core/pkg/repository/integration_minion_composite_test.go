package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A composite command's task against a real server (docs:
// contracts/task-outsourcing): its own op's end, by every writer that ends
// a task, ends the op and leaves the task waiting in core; its record's
// changes; its claim; its subtasks.

func compositeTask(session uuid.UUID, ownOp protocol.OwnOpState) *models.MinionTask {
	return &models.MinionTask{TaskID: uuid.New(), OperationID: uuid.New(), SessionID: session, Command: "archive", Op: 40,
		Args: models.JSONMap{"path": "/x"}, State: models.MinionTaskComposing, QueuedAt: time.Now().UTC(),
		Composite: &models.MinionComposite{OwnOp: string(ownOp)}}
}

func TestIntegrationCompositeOwnOp(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	session := uuid.New()
	task := compositeTask(session, protocol.OwnOpNotRun)
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	// While it waits in core, nothing of it goes to the minion.
	if got, err := repo.ForDelivery(ctx, session, 10); err != nil || len(got) != 0 {
		t.Fatalf("delivered while composing: %v %v", got, err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := repo.SendOwnOp(ctx, task.TaskID, 40, []byte{0xa0}, nil, nil, models.JSONMap{"path": "/y"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SendOwnOp(ctx, task.TaskID, 40, nil, nil, nil, nil, now); !IsNotFound(err) {
		t.Fatalf("sent twice: %v", err)
	}
	got, err := repo.ForDelivery(ctx, session, 10)
	if err != nil || len(got) != 1 || got[0].Composite.OwnOp != string(protocol.OwnOpOut) || got[0].Composite.OwnArgs["path"] != "/y" {
		t.Fatalf("own op not queued: %+v %v", got, err)
	}
	if err := repo.MarkSent(ctx, []uuid.UUID{task.TaskID}, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Ack(ctx, session, []uuid.UUID{task.TaskID}, now, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.CountSlots(ctx, session); n != 1 {
		t.Fatalf("its own op out takes %d slots", n)
	}
	if _, _, err := repo.AddResult(ctx, session, task.TaskID, MinionTaskResult{Seq: 0, Out: []byte("a"), At: now}); err != nil {
		t.Fatal(err)
	}
	ok := false
	after, fresh, err := repo.AddResult(ctx, session, task.TaskID, MinionTaskResult{Seq: 1, Done: true, OK: &ok, Err: "no space", At: now})
	if err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	c := after.Composite
	if after.State != models.MinionTaskComposing || after.DoneAt != nil || after.OK != nil || after.Err != "" || after.DoneSeq == nil ||
		c.OwnOp != string(protocol.OwnOpEnded) || c.OwnEnd == nil || c.OwnEnd.State != string(protocol.EndDone) || c.OwnEnd.OK ||
		c.OwnEnd.Err != "no space" || len(c.Pending) != 1 || !c.Pending[0].OwnOp || c.WakeAt == nil {
		t.Fatalf("its own op's done part: %s %+v", after.State, c)
	}
	if n, _ := repo.CountSlots(ctx, session); n != 0 {
		t.Fatalf("an ended own op takes %d slots", n)
	}
	// A part after its own op's end is late.
	if _, _, err := repo.AddResult(ctx, session, task.TaskID, MinionTaskResult{Seq: 2, Out: []byte("b"), At: now}); !errors.Is(err, ErrMinionTaskEnded) {
		t.Fatalf("a part after the own op's end: %v", err)
	}
	if _, err := repo.EndComposite(ctx, task.TaskID, models.MinionTaskDone, &[]bool{true}[0], "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EndComposite(ctx, task.TaskID, models.MinionTaskDone, nil, "", now); !IsNotFound(err) {
		t.Fatalf("ended twice: %v", err)
	}
}

// Every other writer that ends a task ends a composite's own op alone.
func TestIntegrationCompositeOwnOpEnds(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	session := uuid.New()
	now := time.Now().UTC()
	out := func(sent, acked bool) *models.MinionTask {
		t.Helper()
		task := compositeTask(session, protocol.OwnOpNotRun)
		if err := repo.Insert(ctx, task); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SendOwnOp(ctx, task.TaskID, 40, nil, nil, nil, nil, now); err != nil {
			t.Fatal(err)
		}
		if sent {
			if err := repo.MarkSent(ctx, []uuid.UUID{task.TaskID}, now); err != nil {
				t.Fatal(err)
			}
		}
		if acked {
			if err := repo.Ack(ctx, session, []uuid.UUID{task.TaskID}, now, nil); err != nil {
				t.Fatal(err)
			}
		}
		return task
	}
	check := func(name string, after *models.MinionTask, err error, state protocol.EndState) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if after.State != models.MinionTaskComposing || after.Composite.OwnEnd == nil || after.Composite.OwnEnd.State != string(state) ||
			len(after.Composite.Pending) != 1 || after.CancelRequestedAt != nil {
			t.Fatalf("%s: %s %+v", name, after.State, after.Composite)
		}
	}
	task := out(false, false)
	after, err := repo.FailUndelivered(ctx, session, task.TaskID, "too large", now)
	check("too large", after, err, protocol.EndFailed)

	task = out(true, true)
	after, err = repo.EndUnknown(ctx, session, task.TaskID, "the minion does not have the task", now)
	check("lost", after, err, protocol.EndFailed)

	task = out(false, false)
	after, err = repo.RequestCancel(ctx, task.TaskID, now)
	check("cancelled before it went", after, err, protocol.EndCancelled)

	task = out(true, false)
	if _, err := repo.CancelOwnOp(ctx, task.TaskID, now); err != nil {
		t.Fatal(err)
	}
	for range models.MinionCancelGiveUp + 1 {
		if _, _, err := repo.CancelsToSend(ctx, session, now); err != nil {
			t.Fatal(err)
		}
	}
	after, err = repo.FindByTaskID(ctx, task.TaskID)
	check("cancels given up", after, err, protocol.EndCancelled)

	// An ordinary task ends as it did.
	plain := &models.MinionTask{TaskID: uuid.New(), SessionID: session, Command: "ls", Op: 1, State: models.MinionTaskQueued, QueuedAt: now}
	if err := repo.Insert(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if after, err := repo.FailUndelivered(ctx, session, plain.TaskID, "too large", now); err != nil || after.State != models.MinionTaskFailed {
		t.Fatalf("an ordinary task too large: %v %+v", err, after)
	}
}

// The record's changes, the claim, and the subtasks' queries.
func TestIntegrationCompositeRecord(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	session := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := compositeTask(session, "")
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	id := task.TaskID
	sub := uuid.New()
	after, err := repo.UpdateComposite(ctx, id, ComposeUpdate{Set: map[string]any{"seq": 1, "call_at": now, "wake_at": now,
		"cancel": models.MinionComposeCancel{Keys: []string{"a"}}}, Subtasks: 3,
		Ends:    []models.MinionComposeEnd{{TaskID: &sub, At: now}, {Command: "rm", State: "refused", Err: "no", At: now}},
		Waiting: &[]models.MinionSubtask{{TaskID: uuid.New(), Command: "ls"}}})
	if err != nil || after.Composite.Seq != 1 || after.Composite.Subtasks != 3 || len(after.Composite.Pending) != 2 ||
		len(after.Composite.Waiting) != 1 || after.Composite.Cancel == nil || after.Composite.CallAt == nil {
		t.Fatalf("update: %v %+v", err, after.Composite)
	}
	wrong, running := 7, ""
	if _, err := repo.UpdateComposite(ctx, id, ComposeUpdate{IfSeq: &wrong, Set: map[string]any{"answered": 7}}); !IsNotFound(err) {
		t.Fatalf("a stale seq: %v", err)
	}
	seq := 1
	after, err = repo.UpdateComposite(ctx, id, ComposeUpdate{IfSeq: &seq, IfPhase: &running, PopEnd: true,
		Set: map[string]any{"answered": 1}, Unset: []string{"call_at", "cancel"}})
	if err != nil || after.Composite.Answered != 1 || len(after.Composite.Pending) != 1 || after.Composite.Pending[0].Command != "rm" ||
		after.Composite.CallAt != nil || after.Composite.Cancel != nil {
		t.Fatalf("answered: %v %+v", err, after.Composite)
	}
	if _, err := repo.UpdateComposite(ctx, id, ComposeUpdate{Set: map[string]any{"phase": models.ComposeStopping}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateComposite(ctx, id, ComposeUpdate{IfPhase: &running, Set: map[string]any{"answered": 2}}); !IsNotFound(err) {
		t.Fatalf("an answer after a stop changed more than its ctx: %v", err)
	}
	if due, err := repo.ComposeDue(ctx, now, 10); err != nil || len(due) != 1 || due[0] != id {
		t.Fatalf("due: %v %v", due, err)
	}

	if _, err := repo.ClaimComposite(ctx, id, "a", now.Add(time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimComposite(ctx, id, "b", now.Add(time.Minute), now); !IsNotFound(err) {
		t.Fatalf("claimed twice: %v", err)
	}
	if _, err := repo.ClaimComposite(ctx, id, "b", now.Add(3*time.Minute), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("a claim past its time: %v", err)
	}

	add := func(state, key string, cleanup bool) uuid.UUID {
		t.Helper()
		s := &models.MinionTask{TaskID: uuid.New(), SessionID: session, Command: "ls", Op: 1, State: state, QueuedAt: time.Now().UTC(),
			ParentID: &id, SubtaskKey: key, Cleanup: cleanup}
		if state == models.MinionTaskDone {
			s.DoneAt = &now
		}
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatal(err)
		}
		return s.TaskID
	}
	done := add(models.MinionTaskDone, "a", false)
	add(models.MinionTaskRunning, "b", false)
	add(models.MinionTaskQueued, "c", true)
	if n, _ := repo.CountOpenSubtasks(ctx, id); n != 2 {
		t.Fatalf("open: %d", n)
	}
	if subs, _ := repo.OpenSubtasks(ctx, id, []string{"b"}); len(subs) != 1 || subs[0].SubtaskKey != "b" {
		t.Fatalf("open with key b: %+v", subs)
	}
	if subs, _ := repo.UnheardSubtasks(ctx, id, 10); len(subs) != 1 || subs[0].TaskID != done {
		t.Fatalf("unheard: %+v", subs)
	}
	if parents, _ := repo.UnheardParents(ctx, 10); len(parents) != 1 || parents[0] != id {
		t.Fatalf("unheard parents: %v", parents)
	}
	if err := repo.MarkHeard(ctx, done); err != nil {
		t.Fatal(err)
	}
	if subs, _ := repo.UnheardSubtasks(ctx, id, 10); len(subs) != 0 {
		t.Fatalf("heard: %+v", subs)
	}
	if subs, _ := repo.ListSubtasks(ctx, id); len(subs) != 3 {
		t.Fatalf("all: %d", len(subs))
	}
	if _, err := repo.Subtask(ctx, uuid.New(), done); !IsNotFound(err) {
		t.Fatalf("another task's subtask: %v", err)
	}
	// The session's list, narrowed to the composite task and its subtasks,
	// a page at a time.
	other := &models.MinionTask{TaskID: uuid.New(), SessionID: session, Command: "ls", Op: 1, State: models.MinionTaskQueued,
		QueuedAt: time.Now().UTC()}
	if err := repo.Insert(ctx, other); err != nil {
		t.Fatal(err)
	}
	family, err := repo.ListBySession(ctx, session, MinionTaskFilter{Composite: &id}, nil, 10)
	if err != nil || len(family) != 4 || family[3].TaskID != id {
		t.Fatalf("the composite task and its subtasks: %d %v", len(family), err)
	}
	page, err := repo.ListBySession(ctx, session, MinionTaskFilter{Composite: &id}, &MinionTaskCursor{QueuedAt: family[1].QueuedAt,
		TaskID: family[1].TaskID}, 10)
	if err != nil || len(page) != 2 || page[1].TaskID != id {
		t.Fatalf("its next page: %d %v", len(page), err)
	}
	if all, _ := repo.ListBySession(ctx, session, MinionTaskFilter{}, nil, 10); len(all) != 5 {
		t.Fatalf("the whole list: %d", len(all))
	}

	// A finish takes a subtask's result: its parts and sizes.
	ok := true
	if _, _, err := repo.AddResult(ctx, session, done, MinionTaskResult{Seq: 0, Done: true, OK: &ok, Out: []byte("listed"), At: now}); err != nil &&
		!errors.Is(err, ErrMinionTaskEnded) {
		t.Fatal(err)
	}
	from, _ := repo.FindByTaskID(ctx, done)
	from.Parts, from.OutSize = 1, 6
	if _, err := repo.FinishComposite(ctx, id, CompositeResult{State: models.MinionTaskDone, OK: true, At: now, From: from}); err != nil {
		t.Fatal(err)
	}
	finished, _ := repo.FindByTaskID(ctx, id)
	if finished.State != models.MinionTaskDone || finished.ResultOf == nil || *finished.ResultOf != done || finished.Composite.WakeAt != nil {
		t.Fatalf("finished: %+v", finished)
	}
}
