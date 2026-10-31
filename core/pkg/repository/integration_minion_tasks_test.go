package repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/taskdata"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func cborData(t *testing.T, v any) []byte {
	t.Helper()
	b, err := protocol.ResultData(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

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
		Data: cborData(t, map[string]any{"entries": []any{"a"}}), At: t0})
	if err != nil || task.State != models.MinionTaskRunning || task.AckedAt == nil || task.StartedAt == nil {
		t.Fatalf("running: %+v %v", task, err)
	}
	task, _, _ = repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 2, Done: true, OK: &ok, Exit: &exit,
		Data: cborData(t, map[string]any{"entries": []any{"b"}}), At: t0})
	if task.State != models.MinionTaskDone || task.Exit == nil || *task.Exit != 0 || task.Parts != 3 || task.OutSize != 11 ||
		task.DoneSeq == nil || *task.DoneSeq != 2 || task.MissingParts() != 0 || task.DataSize == 0 {
		t.Fatalf("done: %+v", task)
	}
	// The done part is the last: a later part, or another done part, changes nothing.
	failedToo := false
	if _, _, err := repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 3, Out: []byte("late"), At: t0}); !errors.Is(err, ErrMinionTaskEnded) {
		t.Fatalf("a part after the end: %v", err)
	}
	if _, _, err := repo.AddResult(ctx, session, ids[1], MinionTaskResult{Seq: 7, Done: true, OK: &failedToo, At: t0}); !errors.Is(err, ErrMinionTaskEnded) {
		t.Fatalf("a second done part: %v", err)
	}
	if after, _ := repo.FindByTaskID(ctx, ids[1]); after.State != models.MinionTaskDone || after.Parts != 3 {
		t.Fatalf("after the late parts: %+v", after)
	}
	parts, _ := repo.Parts(ctx, ids[1])
	if len(parts) != 3 || string(parts[0].Out)+string(parts[1].Out) != "hello world" {
		t.Fatalf("parts %+v", parts)
	}
	// The data stays the CBOR it came in, and joins as the contract says.
	if len(parts[0].DataCBOR) == 0 || parts[0].Data != nil {
		t.Fatalf("part 0 data kept as %+v", parts[0])
	}
	if d, err := taskdata.Join(*task, parts); err != nil || len(d.(map[string]any)["entries"].([]any)) != 2 {
		t.Fatalf("joined data %v %v", d, err)
	}
	if _, _, err := repo.AddResult(ctx, uuid.New(), ids[1], MinionTaskResult{Seq: 9, At: t0}); !IsNotFound(err) {
		t.Fatalf("another session's result: %v", err)
	}

	// Output past the cap is dropped, and data past it too: they share it.
	big := make([]byte, models.MaxMinionTaskOutput+10)
	task, _, _ = repo.AddResult(ctx, session, ids[0], MinionTaskResult{Seq: 0, Out: big, At: t0})
	if task.OutSize != models.MaxMinionTaskOutput || !task.OutTruncated {
		t.Fatalf("capped: size %d truncated %v", task.OutSize, task.OutTruncated)
	}
	task, _, _ = repo.AddResult(ctx, session, ids[0], MinionTaskResult{Seq: 1, Data: cborData(t, map[string]any{"n": 1}), At: t0})
	if task.DataSize != 0 || !task.DataTruncated || task.Parts != 2 {
		t.Fatalf("data past the cap: %+v", task)
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
	task, _, _ = repo.AddResult(ctx, session, ids[0], MinionTaskResult{Seq: 2, Done: true, OK: &failed, Err: "cancelled", At: t0})
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

// A dir_listing task: each part's data is checked as it comes, the first
// that does not fit flags the task; a part lost before the done part shows
// as missing until it comes late, which only fills the gap.
func TestIntegrationMinionTaskParts(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionTaskRepository(db)
	session, t0 := uuid.New(), time.Now().UTC().Truncate(time.Millisecond)
	id := uuid.New()
	if err := repo.Insert(ctx, &models.MinionTask{TaskID: id, OperationID: uuid.New(), SessionID: session, Command: "ls",
		Handler: protocol.HandlerDirListing, State: models.MinionTaskSent, QueuedAt: t0}); err != nil {
		t.Fatal(err)
	}
	add := func(r MinionTaskResult) *models.MinionTask {
		t.Helper()
		r.At, r.Check = t0, taskdata.Check
		task, _, err := repo.AddResult(ctx, session, id, r)
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	task := add(MinionTaskResult{Seq: 0, Data: cborData(t, protocol.DirListing{Path: "/tmp", Sep: "/",
		Entries: []protocol.DirEntry{{Name: "a", Type: protocol.DirEntryFile}}})})
	if task.DataProblem != "" || task.Handler != protocol.HandlerDirListing {
		t.Fatalf("a fitting part: %+v", task)
	}
	// Part 1 is lost; part 2 names the directory again, which only the first may.
	ok := true
	task = add(MinionTaskResult{Seq: 2, Done: true, OK: &ok, Data: cborData(t, protocol.DirListing{Path: "/again"})})
	if !strings.HasPrefix(task.DataProblem, "part 2: dir_listing: only the first part") || task.MissingParts() != 1 {
		t.Fatalf("a misfit, a gap: problem %q, missing %d", task.DataProblem, task.MissingParts())
	}
	// Late, part 1 fills the gap and changes nothing else; the first problem stays.
	task = add(MinionTaskResult{Seq: 1, Data: cborData(t, map[string]any{"entries": "nope"})})
	if task.MissingParts() != 0 || task.State != models.MinionTaskDone || !strings.HasPrefix(task.DataProblem, "part 2:") {
		t.Fatalf("the gap filled: %+v", task)
	}
}
