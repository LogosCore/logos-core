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
