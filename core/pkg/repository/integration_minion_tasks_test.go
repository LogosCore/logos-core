package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A session's tasks through their life: delivered oldest first, sent again
// until acked, output kept once per part and in order, done, failed or
// cancelled; listed newest first.
func TestIntegrationMinionTasks(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionTaskRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("task indexes: %v", err)
	}
	op, session, user := uuid.New(), uuid.New(), uuid.New()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	ids := make([]uuid.UUID, 4)
	for i := range ids {
		ids[i] = uuid.New()
		if err := repo.Insert(ctx, &models.MinionTask{TaskID: ids[i], OperationID: op, SessionID: session, Command: "shell",
			IssuedBy: user, State: models.MinionTaskQueued, QueuedAt: t0.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	taskIDs := func(ts []models.MinionTask) []uuid.UUID {
		out := make([]uuid.UUID, len(ts))
		for i, x := range ts {
			out[i] = x.TaskID
		}
		return out
	}

	// The wire args come back as they went in: a duration's milliseconds an
	// unsigned integer, not a document's float.
	wire, _ := protocol.MarshalCBOR(map[string]any{"interval": uint64(2000)})
	argued := uuid.New()
	if err := repo.Insert(ctx, &models.MinionTask{TaskID: argued, OperationID: op, SessionID: uuid.New(), Command: "sleep",
		WireArgs: wire, State: models.MinionTaskQueued, QueuedAt: t0}); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.FindByTaskID(ctx, argued)
	var args map[string]any
	if err := protocol.UnmarshalCBOR(stored.WireArgs, &args); err != nil || args["interval"] != uint64(2000) {
		t.Fatalf("wire args back: %#v %v", args, err)
	}

	// Oldest first, at most the limit; sent ones not yet acked come first next time.
	got, _ := repo.ForDelivery(ctx, session, 2)
	if g := taskIDs(got); len(g) != 2 || g[0] != ids[0] || g[1] != ids[1] {
		t.Fatalf("first delivery %v", g)
	}
	if err := repo.MarkSent(ctx, taskIDs(got), t0); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.ForDelivery(ctx, session, 3)
	if g := taskIDs(got); len(g) != 3 || g[0] != ids[0] || g[1] != ids[1] || g[2] != ids[2] {
		t.Fatalf("resend first: %v, want %v", g, ids[:3])
	}
	if err := repo.Ack(ctx, session, []uuid.UUID{ids[0]}, t0); err != nil {
		t.Fatal(err)
	}
	if g, _ := repo.ForDelivery(ctx, session, 10); len(g) != 3 || g[0].TaskID != ids[1] {
		t.Fatalf("after the ack: %v", taskIDs(g))
	}

	// Parts: kept once each, in seq order; the last says how it went.
	ok, exit := true, int64(0)
	if _, isNew, err := repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 1, Out: []byte("world"), At: t0}); err != nil || !isNew {
		t.Fatalf("part 1: %v %v", isNew, err)
	}
	if _, isNew, _ := repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 1, Out: []byte("again"), At: t0}); isNew {
		t.Fatal("a part sent again was kept")
	}
	task, _, err := repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 0, Out: []byte("hello "), Done: false,
		Data: map[string]any{"entries": []any{"a"}}, At: t0})
	if err != nil || task.State != models.MinionTaskRunning || task.AckedAt == nil || task.StartedAt == nil {
		t.Fatalf("running: %+v %v", task, err)
	}
	task, _, _ = repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 2, Done: true, OK: &ok, Exit: &exit,
		Data: map[string]any{"entries": []any{"b"}}, At: t0})
	if task.State != models.MinionTaskDone || task.Exit == nil || *task.Exit != 0 || task.Parts != 3 || task.OutSize != 11 {
		t.Fatalf("done: %+v", task)
	}
	parts, _ := repo.Parts(ctx, ids[1])
	if len(parts) != 3 || string(parts[0].Out)+string(parts[1].Out) != "hello world" {
		t.Fatalf("parts %+v", parts)
	}
	if d := models.MergeMinionTaskData(parts); len(d["entries"].([]any)) != 2 {
		t.Fatalf("merged data %v", d)
	}
	if _, _, err := repo.AddResult(ctx, uuid.New(), ids[1], MinionTaskResult{Seq: 9, At: t0}); !IsNotFound(err) {
		t.Fatalf("another session's result: %v", err)
	}

	// Output past the cap is dropped.
	big := make([]byte, models.MaxMinionTaskOutput+10)
	task, _, _ = repo.AddResult(ctx, session, ids[0], MinionTaskResult{Seq: 0, Out: big, At: t0})
	if task.OutSize != models.MaxMinionTaskOutput || !task.OutTruncated {
		t.Fatalf("capped: size %d truncated %v", task.OutSize, task.OutTruncated)
	}

	// Cancel: never sent, at once; sent, by cancels until its end, which
	// counts as cancelled when it did not succeed.
	task, _ = repo.RequestCancel(ctx, ids[3], t0)
	if task.State != models.MinionTaskCancelled {
		t.Fatalf("queued, cancelled: %s", task.State)
	}
	task, _ = repo.RequestCancel(ctx, ids[0], t0)
	if task.State != models.MinionTaskRunning || task.CancelRequestedAt == nil {
		t.Fatalf("running, asked: %+v", task)
	}
	send, gaveUp, _ := repo.CancelsToSend(ctx, session, t0)
	if len(send) != 1 || send[0].TaskID != ids[0] || len(gaveUp) != 0 {
		t.Fatalf("cancels %v, gave up %v", taskIDs(send), taskIDs(gaveUp))
	}
	failed := false
	task, _, _ = repo.AddResult(ctx, session, ids[0], MinionTaskResult{Seq: 1, Done: true, OK: &failed, Err: "cancelled", At: t0})
	if task.State != models.MinionTaskCancelled {
		t.Fatalf("stopped: %s", task.State)
	}
	if again, _ := repo.RequestCancel(ctx, ids[1], t0); again.State != models.MinionTaskDone {
		t.Fatalf("a finished task asked to cancel: %s", again.State)
	}
	// Sent, never acked: after MinionCancelGiveUp cancels, counted cancelled.
	if err := repo.MarkSent(ctx, ids[2:3], t0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RequestCancel(ctx, ids[2], t0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < models.MinionCancelGiveUp; i++ {
		if s, g, _ := repo.CancelsToSend(ctx, session, t0); len(s) != 1 || len(g) != 0 {
			t.Fatalf("round %d: send %d gave up %d", i, len(s), len(g))
		}
	}
	if s, g, _ := repo.CancelsToSend(ctx, session, t0); len(s) != 0 || len(g) != 1 || g[0].TaskID != ids[2] {
		t.Fatalf("gave up: send %d gave up %d", len(s), len(g))
	}
	if g, _ := repo.ForDelivery(ctx, session, 10); len(g) != 0 {
		t.Fatalf("nothing left to deliver: %v", taskIDs(g))
	}

	// Newest first, a page at a time.
	page, _ := repo.ListBySession(ctx, session, nil, 3)
	if len(page) != 3 || page[0].TaskID != ids[3] {
		t.Fatalf("first page %v", taskIDs(page))
	}
	last := page[2]
	rest, _ := repo.ListBySession(ctx, session, &MinionTaskCursor{QueuedAt: last.QueuedAt, TaskID: last.TaskID}, 3)
	if len(rest) != 1 || rest[0].TaskID != ids[0] {
		t.Fatalf("second page %v", taskIDs(rest))
	}

	if err := repo.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if left, _ := repo.ListBySession(ctx, session, nil, 10); len(left) != 0 {
		t.Fatal("tasks outlived their operation")
	}
	if left, _ := repo.Parts(ctx, ids[1]); len(left) != 0 {
		t.Fatal("parts outlived their operation")
	}
}
