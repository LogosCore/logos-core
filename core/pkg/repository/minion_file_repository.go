package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionFileCollection = "minion_files"

// IMinionFileRepository keeps the files minions send (models.MinionFile):
// what came and how far; the bytes are in the minion files bucket.
type IMinionFileRepository interface {
	// Insert starts a transfer: IsDuplicateKey when the session has one
	// with this xfer already.
	Insert(ctx context.Context, f *models.MinionFile) error
	FindByID(ctx context.Context, fileID uuid.UUID) (*models.MinionFile, error)
	// FindByXfer is a session's transfer by its id.
	FindByXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionFile, error)
	// FindByTask is a task's file, its latest when there were several: one
	// it got, or one it took as its own (AlsoTasks).
	FindByTask(ctx context.Context, taskID uuid.UUID) (*models.MinionFile, error)
	// AlsoForTask makes the files task from got to's as well.
	AlsoForTask(ctx context.Context, from, to uuid.UUID) error
	// AdoptForTask makes task files put for a task its own, as a file its
	// minion sent is (docs: contracts/factory-commands, a result's files):
	// no longer task files, kept as the task's, of its session. It counts
	// those it changed.
	AdoptForTask(ctx context.Context, fileIDs []uuid.UUID, task, session uuid.UUID) (int64, error)
	// Advance takes n bytes from off for a file receiving them, when off is
	// how far it got: IsNotFound when it is not (a chunk sent again, or one
	// raced). It returns the file after.
	Advance(ctx context.Context, fileID uuid.UUID, off, n int64, at time.Time) (*models.MinionFile, error)
	// Move takes the file from one of from to state, with set's fields:
	// IsNotFound when it is in none of from. It returns the file after.
	Move(ctx context.Context, fileID uuid.UUID, from []string, state string, set bson.M) (*models.MinionFile, error)
	// Idle are files receiving with no chunk since before, and files joining
	// since before (a core that stopped on the way).
	Idle(ctx context.Context, before time.Time) ([]models.MinionFile, error)
	// Update sets fields of a file, whatever its state (a screenshot's
	// size in pixels, its thumbnail). It returns the file after.
	Update(ctx context.Context, fileID uuid.UUID, set bson.M) (*models.MinionFile, error)
	ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.MinionFile, error)
	// ListByKind is an operation's files of a kind (a screenshot's),
	// newest first, before a page's cursor, at most limit.
	ListByKind(ctx context.Context, operationID uuid.UUID, kind string, before *MinionFileCursor, limit int64) ([]models.MinionFile, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	// TaskFiles are the task files uploaded before before.
	TaskFiles(ctx context.Context, before time.Time) ([]models.MinionFile, error)
	Delete(ctx context.Context, fileID uuid.UUID) error
}

type minionFileRepository struct {
	coll database.Collection
}

func NewMinionFileRepository(db database.Database) IMinionFileRepository {
	db.EnsureIndexes(context.Background(), minionFileCollection, []opts.IndexModel{
		{Key: []string{"file_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"session_id", "xfer"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"task_id", "-createAt"}},
		{Key: []string{"also_tasks"}, IndexOptions: new(options.IndexOptions).SetSparse(true)},
		{Key: []string{"state", "last_chunk_at"}},
		{Key: []string{"operation_id"}},
		{Key: []string{"kind", "createAt"}, IndexOptions: new(options.IndexOptions).SetSparse(true)},
		// An operation's files of a kind, a page at a time (ListByKind).
		{Key: []string{"operation_id", "kind", "-createAt", "-file_id"}},
	})
	return &minionFileRepository{coll: db.Collection(minionFileCollection)}
}

func (r *minionFileRepository) Insert(ctx context.Context, f *models.MinionFile) error {
	_, err := r.coll.InsertOne(ctx, f)
	return err
}

func (r *minionFileRepository) one(ctx context.Context, filter bson.M) (*models.MinionFile, error) {
	var f models.MinionFile
	if err := r.coll.Find(ctx, filter).One(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *minionFileRepository) FindByID(ctx context.Context, fileID uuid.UUID) (*models.MinionFile, error) {
	return r.one(ctx, bson.M{"file_id": fileID})
}

func (r *minionFileRepository) FindByXfer(ctx context.Context, sessionID uuid.UUID, xfer []byte) (*models.MinionFile, error) {
	return r.one(ctx, bson.M{"session_id": sessionID, "xfer": xfer})
}

func (r *minionFileRepository) FindByTask(ctx context.Context, taskID uuid.UUID) (*models.MinionFile, error) {
	var f models.MinionFile
	if err := r.coll.Find(ctx, bson.M{"$or": bson.A{bson.M{"task_id": taskID}, bson.M{"also_tasks": taskID}}}).
		Sort("-createAt").Limit(1).One(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *minionFileRepository) AlsoForTask(ctx context.Context, from, to uuid.UUID) error {
	_, err := r.coll.UpdateAll(ctx, bson.M{"task_id": from}, bson.M{"$addToSet": bson.M{"also_tasks": to}})
	return err
}

func (r *minionFileRepository) AdoptForTask(ctx context.Context, fileIDs []uuid.UUID, task, session uuid.UUID) (int64, error) {
	if len(fileIDs) == 0 {
		return 0, nil
	}
	ids := make(bson.A, len(fileIDs))
	for i, id := range fileIDs {
		ids[i] = id
	}
	res, err := r.coll.UpdateAll(ctx, bson.M{"file_id": bson.M{"$in": ids}, "kind": models.MinionFileKindTask, "for_task": task,
		"state": models.MinionFileComplete},
		bson.M{"$set": bson.M{"task_id": task, "session_id": session}, "$unset": bson.M{"kind": "", "for_task": ""}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *minionFileRepository) apply(ctx context.Context, filter, update bson.M) (*models.MinionFile, error) {
	var after models.MinionFile
	if err := r.coll.Find(ctx, filter).Apply(qmgo.Change{Update: update, ReturnNew: true}, &after); err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *minionFileRepository) Advance(ctx context.Context, fileID uuid.UUID, off, n int64, at time.Time) (*models.MinionFile, error) {
	return r.apply(ctx, bson.M{"file_id": fileID, "state": models.MinionFileReceiving, "received": off},
		bson.M{"$inc": bson.M{"received": n}, "$set": bson.M{"last_chunk_at": at, "updateAt": at}})
}

func (r *minionFileRepository) Move(ctx context.Context, fileID uuid.UUID, from []string, state string, set bson.M) (*models.MinionFile, error) {
	s := bson.M{"state": state, "updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	return r.apply(ctx, bson.M{"file_id": fileID, "state": bson.M{"$in": from}}, bson.M{"$set": s})
}

func (r *minionFileRepository) Idle(ctx context.Context, before time.Time) ([]models.MinionFile, error) {
	var out []models.MinionFile
	err := r.coll.Find(ctx, bson.M{"$or": bson.A{
		bson.M{"state": models.MinionFileReceiving, "last_chunk_at": bson.M{"$lt": before}},
		bson.M{"state": models.MinionFileJoining, "updateAt": bson.M{"$lt": before}},
	}}).All(&out)
	return out, err
}

func (r *minionFileRepository) Update(ctx context.Context, fileID uuid.UUID, set bson.M) (*models.MinionFile, error) {
	s := bson.M{"updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	return r.apply(ctx, bson.M{"file_id": fileID}, bson.M{"$set": s})
}

func (r *minionFileRepository) ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.MinionFile, error) {
	var out []models.MinionFile
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).All(&out)
	return out, err
}

// MinionFileCursor is where a page of files starts: after the file made at
// CreateAt with FileID, newest first.
type MinionFileCursor struct {
	CreateAt time.Time
	FileID   uuid.UUID
}

// minionFilesByKind is ListByKind's filter: as its index has it, so the
// page is read from it.
func minionFilesByKind(operationID uuid.UUID, kind string, before *MinionFileCursor) bson.M {
	filter := bson.M{"operation_id": operationID, "kind": kind}
	if before != nil {
		filter["$or"] = bson.A{
			bson.M{"createAt": bson.M{"$lt": before.CreateAt}},
			bson.M{"createAt": before.CreateAt, "file_id": bson.M{"$lt": before.FileID}},
		}
	}
	return filter
}

func (r *minionFileRepository) ListByKind(ctx context.Context, operationID uuid.UUID, kind string, before *MinionFileCursor, limit int64) ([]models.MinionFile, error) {
	var out []models.MinionFile
	err := r.coll.Find(ctx, minionFilesByKind(operationID, kind, before)).Sort("-createAt", "-file_id").Limit(limit).All(&out)
	return out, err
}

func (r *minionFileRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

func (r *minionFileRepository) TaskFiles(ctx context.Context, before time.Time) ([]models.MinionFile, error) {
	var out []models.MinionFile
	err := r.coll.Find(ctx, bson.M{"kind": models.MinionFileKindTask, "createAt": bson.M{"$lt": before}}).All(&out)
	return out, err
}

func (r *minionFileRepository) Delete(ctx context.Context, fileID uuid.UUID) error {
	err := r.coll.Remove(ctx, bson.M{"file_id": fileID})
	if IsNotFound(err) {
		return nil
	}
	return err
}
