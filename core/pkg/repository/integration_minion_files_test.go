package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// A file a minion sends: one per (session, xfer); taken in order, a chunk
// raced or sent again not advancing it; idle ones found; a task's latest.
func TestIntegrationMinionFiles(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionFileRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("file indexes: %v", err)
	}
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	session, task, op := uuid.New(), uuid.New(), uuid.New()
	xfer := []byte("0123456789abcdef")
	f := &models.MinionFile{FileID: uuid.New(), OperationID: op, SessionID: session, TaskID: task, Xfer: xfer,
		Name: "/etc/hosts", Total: 10, State: models.MinionFileReceiving, LastChunkAt: t0}
	if err := repo.Insert(ctx, f); err != nil {
		t.Fatal(err)
	}
	again := *f
	again.FileID = uuid.New()
	if err := repo.Insert(ctx, &again); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("a second transfer with the xfer: %v", err)
	}
	if got, err := repo.FindByXfer(ctx, session, xfer); err != nil || got.FileID != f.FileID {
		t.Fatalf("by xfer: %v %v", got, err)
	}

	after, err := repo.Advance(ctx, f.FileID, 0, 4, t0)
	if err != nil || after.Received != 4 {
		t.Fatalf("advance: %+v %v", after, err)
	}
	if _, err := repo.Advance(ctx, f.FileID, 0, 4, t0); !IsNotFound(err) {
		t.Fatalf("the same chunk again: %v", err)
	}

	// Idle: no chunk since before.
	if idle, _ := repo.Idle(ctx, t0.Add(time.Minute)); len(idle) != 1 {
		t.Fatalf("idle: %d", len(idle))
	}
	if idle, _ := repo.Idle(ctx, t0.Add(-time.Minute)); len(idle) != 0 {
		t.Fatalf("not idle yet: %d", len(idle))
	}

	moved, err := repo.Move(ctx, f.FileID, []string{models.MinionFileReceiving}, models.MinionFileJoining, bson.M{"sha256": make([]byte, 32)})
	if err != nil || moved.State != models.MinionFileJoining || len(moved.SHA256) != 32 {
		t.Fatalf("move: %+v %v", moved, err)
	}
	if _, err := repo.Advance(ctx, f.FileID, 4, 6, t0); !IsNotFound(err) {
		t.Fatalf("a chunk once joining: %v", err)
	}
	if _, err := repo.Move(ctx, f.FileID, []string{models.MinionFileReceiving}, models.MinionFileFailed, nil); !IsNotFound(err) {
		t.Fatalf("a move from a state it is not in: %v", err)
	}
	if got, err := repo.FindByTask(ctx, task); err != nil || got.FileID != f.FileID {
		t.Fatalf("by task: %v %v", got, err)
	}
	if err := repo.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(ctx, f.FileID); !IsNotFound(err) {
		t.Fatalf("after delete: %v", err)
	}
}

// An operation's files of a kind, a page at a time, newest first: the
// cursor's own second shared by two files splits them by id; another kind
// and another operation's are not on the page. Update sets a file's fields
// whatever its state.
func TestIntegrationMinionFilesByKind(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionFileRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("file indexes: %v", err)
	}
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	op := uuid.New()
	insert := func(op uuid.UUID, kind string, at time.Time) *models.MinionFile {
		t.Helper()
		f := &models.MinionFile{FileID: uuid.New(), Kind: kind, OperationID: op, SessionID: uuid.New(), TaskID: uuid.New(),
			Xfer: []byte(uuid.NewString()[:16]), Name: "screen.png", Total: 10, State: models.MinionFileComplete, LastChunkAt: at}
		f.CreateAt = at
		if err := repo.Insert(ctx, f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	oldest := insert(op, models.MinionFileKindScreenshot, t0)
	a := insert(op, models.MinionFileKindScreenshot, t0.Add(time.Second))
	b := insert(op, models.MinionFileKindScreenshot, t0.Add(time.Second))
	insert(op, "", t0.Add(time.Hour))                                      // a download
	insert(uuid.New(), models.MinionFileKindScreenshot, t0.Add(time.Hour)) // another operation's
	newer, older := a, b
	if a.FileID.String() < b.FileID.String() {
		newer, older = b, a
	}

	page, err := repo.ListByKind(ctx, op, models.MinionFileKindScreenshot, nil, 2)
	if err != nil || len(page) != 2 || page[0].FileID != newer.FileID || page[1].FileID != older.FileID {
		t.Fatalf("first page: %v %v", page, err)
	}
	cursor := &MinionFileCursor{CreateAt: page[1].CreateAt, FileID: page[1].FileID}
	rest, err := repo.ListByKind(ctx, op, models.MinionFileKindScreenshot, cursor, 2)
	if err != nil || len(rest) != 1 || rest[0].FileID != oldest.FileID {
		t.Fatalf("after the cursor: %v %v", rest, err)
	}

	after, err := repo.Update(ctx, oldest.FileID, bson.M{"width": int64(1920), "height": int64(1080), "thumb": true})
	if err != nil || after.Width != 1920 || after.Height != 1080 || !after.Thumb || after.State != models.MinionFileComplete {
		t.Fatalf("update: %+v %v", after, err)
	}
	if _, err := repo.Update(ctx, uuid.New(), bson.M{"thumb": true}); !IsNotFound(err) {
		t.Fatalf("update of no file: %v", err)
	}
}
