package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A factory command's task against a real server (docs:
// contracts/factory-commands): it never goes to the minion nor takes its
// room; its offer is claimed once, marked before it goes, put back unless the
// instance reported; its output is kept once per seq within the task's
// budget; its result's data is the done part; core's own end; the task files
// it names are in use while it runs.

func factoryTask(session uuid.UUID, at time.Time) *models.MinionTask {
	return &models.MinionTask{TaskID: uuid.New(), OperationID: uuid.New(), SessionID: session, Command: "whois",
		Args: models.JSONMap{"address": "x"}, State: models.MinionTaskFactory, QueuedAt: at,
		Factory: &models.MinionFactoryRun{Module: "example", Args: models.JSONMap{"address": "x", "depth": 2}, OfferAt: &at}}
}

func TestIntegrationFactoryOffer(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	session := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := factoryTask(session, now)
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	// Nothing of it goes to the minion, nor takes its room.
	if got, err := repo.ForDelivery(ctx, session, 10); err != nil || len(got) != 0 {
		t.Fatalf("delivered: %v %v", got, err)
	}
	if n, _ := repo.CountRunning(ctx, session); n != 0 {
		t.Fatalf("it takes %d of the minion's room", n)
	}
	if got, err := repo.OpenOfCommand(ctx, session, "whois"); err != nil || len(got) != 1 {
		t.Fatalf("not open: %v %v", got, err)
	}

	// Claimed once while the lease holds; not before its offer is due.
	if got, err := repo.ClaimFactoryOffer(ctx, "core-a", now.Add(time.Minute), now.Add(-time.Second)); err != nil || got != nil {
		t.Fatalf("claimed before it was due: %+v %v", got, err)
	}
	got, err := repo.ClaimFactoryOffer(ctx, "core-a", now.Add(time.Minute), now)
	if err != nil || got == nil || got.TaskID != task.TaskID || got.Factory.LeaseBy != "core-a" {
		t.Fatalf("claim: %+v %v", got, err)
	}
	if again, err := repo.ClaimFactoryOffer(ctx, "core-b", now.Add(time.Minute), now); err != nil || again != nil {
		t.Fatalf("claimed twice: %+v %v", again, err)
	}
	// No instance took it: it waits, saying why, until next.
	next := now.Add(2 * time.Second)
	waited, err := repo.WaitFactory(ctx, task.TaskID, "core-a", next, "no instance", true, now)
	if err != nil || waited.Factory.Waiting != "no instance" || waited.Factory.WaitingSince == nil || waited.Factory.LeaseBy != "" {
		t.Fatalf("wait: %+v %v", waited, err)
	}
	if again, err := repo.WaitFactory(ctx, task.TaskID, "core-a", next, "x", false, now); !IsNotFound(err) {
		t.Fatalf("waited unclaimed: %+v %v", again, err)
	}
	if got, err = repo.ClaimFactoryOffer(ctx, "core-a", next.Add(time.Minute), next); err != nil || got == nil {
		t.Fatalf("claim again: %+v %v", got, err)
	}

	// Offered there first; put back while the instance has not reported.
	if ok, err := repo.OfferFactory(ctx, task.TaskID, "core-b", "example-1", next); err != nil || ok {
		t.Fatalf("offered by another core: %v %v", ok, err)
	}
	if ok, err := repo.OfferFactory(ctx, task.TaskID, "core-a", "example-1", next); err != nil || !ok {
		t.Fatalf("offer: %v %v", ok, err)
	}
	if fresh, err := repo.ClaimFactoryOffer(ctx, "core-a", next.Add(time.Minute), next.Add(time.Hour)); err != nil || fresh != nil {
		t.Fatalf("claimed while offered: %+v %v", fresh, err)
	}
	if ok, err := repo.UnofferFactory(ctx, task.TaskID, "example-1"); err != nil || !ok {
		t.Fatalf("put back: %v %v", ok, err)
	}
	if ok, err := repo.OfferFactory(ctx, task.TaskID, "core-a", "example-2", next); err != nil || !ok {
		t.Fatalf("offer to the next: %v %v", ok, err)
	}
	// The instance reports before its reply: it took the task, and is not
	// put back.
	if _, kept, err := repo.FactoryOutput(ctx, task.TaskID, "example-2", 1, []byte("looking up\n"), next); err != nil || !kept {
		t.Fatalf("report: %v %v", kept, err)
	}
	if ok, err := repo.UnofferFactory(ctx, task.TaskID, "example-2"); err != nil || ok {
		t.Fatalf("put back after a report: %v %v", ok, err)
	}
	accepted, err := repo.AcceptFactory(ctx, task.TaskID, "example-2", next.Add(time.Second))
	switch {
	case err != nil:
		t.Fatal(err)
	case !accepted.Factory.AcceptedAt.Equal(next) || accepted.StartedAt == nil || !accepted.Factory.ReportAt.Equal(next.Add(time.Second)) ||
		accepted.Factory.Waiting != "" || accepted.Factory.OfferAt != nil || accepted.Factory.LeaseBy != "":
		t.Fatalf("accepted: %+v", accepted.Factory)
	}
	if _, err := repo.AcceptFactory(ctx, task.TaskID, "example-1", next); !IsNotFound(err) {
		t.Fatalf("accepted by another: %v", err)
	}
	under, err := repo.FactoryUnderWay(ctx, 10)
	if err != nil || len(under) != 1 || under[0].TaskID != task.TaskID {
		t.Fatalf("under way: %v %v", under, err)
	}
}

func TestIntegrationFactoryOutputAndResult(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := factoryTask(uuid.New(), now)
	task.Factory.Instance, task.Factory.OfferAt, task.Factory.AcceptedAt = "example-1", nil, &now
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		seq  int64
		out  string
		kept bool
	}{{1, "a", true}, {2, "b", true}, {2, "b", false}, {1, "a", false}, {4, "d", true}, {5, "", true}} {
		if _, kept, err := repo.FactoryOutput(ctx, task.TaskID, "example-1", r.seq, []byte(r.out), now); err != nil || kept != r.kept {
			t.Fatalf("seq %d: kept %v, %v", r.seq, kept, err)
		}
	}
	if _, _, err := repo.FactoryOutput(ctx, task.TaskID, "example-2", 6, []byte("x"), now); !IsNotFound(err) {
		t.Fatalf("another instance's: %v", err)
	}
	mid, err := repo.FindByTaskID(ctx, task.TaskID)
	if err != nil || mid.Parts != 3 || mid.OutSize != 3 || mid.Factory.Seq != 5 {
		t.Fatalf("kept: %+v %v", mid, err)
	}
	out, err := repo.OutParts(ctx, task.TaskID)
	if err != nil || len(out) != 3 || out[2].Seq != 3 {
		t.Fatalf("parts: %+v %v", out, err)
	}

	exit := int64(0)
	done, err := repo.FinishFactory(ctx, task.TaskID, "example-1", FactoryResult{State: models.MinionTaskDone, OK: true, Exit: &exit,
		Data: []byte{0xa1, 0x61, 0x61, 0x01}, At: now.Add(time.Second)})
	switch {
	case err != nil:
		t.Fatal(err)
	case done.State != models.MinionTaskDone || done.OK == nil || !*done.OK || done.DoneSeq == nil || *done.DoneSeq != 5 ||
		done.Factory.ResultAt == nil || done.DataParts != 1 || done.Parts != 4:
		t.Fatalf("finished: %+v", done)
	case done.MissingParts() != 2:
		// Seqs 3 and 5 never came: parts 2 and 4 are missing (5 printed
		// nothing).
		t.Fatalf("missing parts: %d", done.MissingParts())
	}
	if _, err := repo.FinishFactory(ctx, task.TaskID, "example-1", FactoryResult{State: models.MinionTaskDone, At: now}); !IsNotFound(err) {
		t.Fatalf("finished twice: %v", err)
	}
	if _, err := repo.EndFactory(ctx, task.TaskID, models.MinionTaskCancelled, "", now); !IsNotFound(err) {
		t.Fatalf("ended after its result: %v", err)
	}

	stopped := factoryTask(uuid.New(), now)
	if err := repo.Insert(ctx, stopped); err != nil {
		t.Fatal(err)
	}
	ended, err := repo.EndFactory(ctx, stopped.TaskID, models.MinionTaskFailed, "its factory sent no report", now)
	if err != nil || ended.State != models.MinionTaskFailed || ended.OK == nil || *ended.OK || ended.Err != "its factory sent no report" ||
		ended.Factory.OfferAt != nil {
		t.Fatalf("ended: %+v %v", ended, err)
	}
}

// The output budget is the task's, shared with its data.
func TestIntegrationFactoryOutputBudget(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	task := factoryTask(uuid.New(), now)
	task.Factory.Instance, task.Factory.OfferAt = "example-1", nil
	task.OutSize = models.MaxMinionTaskOutput - 2
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	after, kept, err := repo.FactoryOutput(ctx, task.TaskID, "example-1", 1, []byte("abcd"), now)
	if err != nil || !kept || !after.OutTruncated || after.OutSize != models.MaxMinionTaskOutput {
		t.Fatalf("cut: %+v %v", after, err)
	}
}

// A task file a factory command's args name is in use while the task runs.
func TestIntegrationFactoryFileUse(t *testing.T) {
	repo := NewMinionTaskRepository(integrationDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	file := uuid.New()
	task := factoryTask(uuid.New(), now)
	task.Factory.Files = []uuid.UUID{file}
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	if open, named, err := repo.UploadFileUse(ctx, file); err != nil || !open || !named {
		t.Fatalf("in use while it runs: %v %v %v", open, named, err)
	}
	if _, err := repo.EndFactory(ctx, task.TaskID, models.MinionTaskCancelled, "", now); err != nil {
		t.Fatal(err)
	}
	if open, named, err := repo.UploadFileUse(ctx, file); err != nil || open || !named {
		t.Fatalf("after its end: %v %v %v", open, named, err)
	}
}

// Files a factory put for a task become the task's own, as a download is.
func TestIntegrationAdoptFactoryFiles(t *testing.T) {
	db := integrationDB(t)
	files := NewMinionFileRepository(db)
	ctx := context.Background()
	task, session := uuid.New(), uuid.New()
	mk := func(forTask uuid.UUID, state string) *models.MinionFile {
		f := &models.MinionFile{FileID: uuid.New(), Kind: models.MinionFileKindTask, OperationID: uuid.New(), ForTask: forTask,
			Xfer: uuid.New().NodeID(), Name: "report.txt", State: state, LastChunkAt: time.Now().UTC()}
		if err := files.Insert(ctx, f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	mine, other, partial := mk(task, models.MinionFileComplete), mk(uuid.New(), models.MinionFileComplete), mk(task, models.MinionFileReceiving)
	n, err := files.AdoptForTask(ctx, []uuid.UUID{mine.FileID, other.FileID, partial.FileID}, task, session)
	if err != nil || n != 1 {
		t.Fatalf("adopted %d, %v", n, err)
	}
	got, err := files.FindByID(ctx, mine.FileID)
	if err != nil || got.Kind != "" || got.TaskID != task || got.SessionID != session || got.ForTask != uuid.Nil {
		t.Fatalf("the task's own: %+v %v", got, err)
	}
	if f, err := files.FindByTask(ctx, task); err != nil || f.FileID != mine.FileID {
		t.Fatalf("the task's file: %+v %v", f, err)
	}
	if got, err := files.ListByTask(ctx, task, 10); err != nil || len(got) != 1 || got[0].FileID != mine.FileID {
		t.Fatalf("the task's files: %+v %v", got, err)
	}
}

// A task's files, oldest first: those it got and those it took as its own,
// none of another task's, at most limit.
func TestIntegrationFilesOfTask(t *testing.T) {
	db := integrationDB(t)
	files := NewMinionFileRepository(db)
	ctx := context.Background()
	task, other := uuid.New(), uuid.New()
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	mk := func(name string, of uuid.UUID, also []uuid.UUID, minute int) uuid.UUID {
		f := &models.MinionFile{FileID: uuid.New(), OperationID: uuid.New(), TaskID: of, AlsoTasks: also, Xfer: uuid.New().NodeID(),
			Name: name, State: models.MinionFileComplete, LastChunkAt: at}
		f.CreateAt = at.Add(time.Duration(minute) * time.Minute)
		if err := files.Insert(ctx, f); err != nil {
			t.Fatal(err)
		}
		return f.FileID
	}
	second := mk("second", task, nil, 2)
	first := mk("first", task, nil, 1)
	taken := mk("taken", other, []uuid.UUID{task}, 3)
	mk("another task's", other, nil, 0)
	names := func(fs []models.MinionFile) []string {
		out := make([]string, len(fs))
		for i, f := range fs {
			out[i] = f.Name
		}
		return out
	}

	got, err := files.ListByTask(ctx, task, 10)
	if err != nil || len(got) != 3 || got[0].FileID != first || got[1].FileID != second || got[2].FileID != taken {
		t.Fatalf("the task's files: %v %v", names(got), err)
	}
	if got, err := files.ListByTask(ctx, task, 2); err != nil || len(got) != 2 || got[1].FileID != second {
		t.Fatalf("at most 2: %v %v", names(got), err)
	}
	if got, err := files.ListByTask(ctx, uuid.New(), 10); err != nil || len(got) != 0 {
		t.Fatalf("a task with none: %v %v", names(got), err)
	}
}
