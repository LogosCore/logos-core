package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const storageActionCollection = "storage_actions"

// ErrStorageActionRunning: the same action (for the same bucket) is running
// already.
var ErrStorageActionRunning = errors.New("storage action running")

// IStorageActionRepository keeps the maintenance actions admins run on the
// object store (models.StorageAction).
type IStorageActionRepository interface {
	// Insert starts a running action: ErrStorageActionRunning when one of
	// its kind, for its bucket, runs already.
	Insert(ctx context.Context, a *models.StorageAction) error
	// Finish ends a running action with status: why it failed, what it did.
	Finish(ctx context.Context, id uuid.UUID, status, why string, freed int64, volumes int, at time.Time) (*models.StorageAction, error)
	// EndRunning fails every action still running, saying why: a core that
	// stopped while they ran no longer knows how they went.
	EndRunning(ctx context.Context, why string, at time.Time) (int64, error)
	// FindWithCursor is a page of the actions, the latest first.
	FindWithCursor(ctx context.Context, cursor *pagination.Cursor, limit int64, forward bool) ([]models.StorageAction, error)
	Count(ctx context.Context) (int64, error)
}

type storageActionRepository struct{ coll database.Collection }

func NewStorageActionRepository(db database.Database) IStorageActionRepository {
	db.EnsureIndexes(context.Background(), storageActionCollection, []opts.IndexModel{
		{Key: []string{"action_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		// One running at a time of an action for a bucket (a reclaim's is
		// every bucket's, "").
		{Key: []string{"action", "bucket"}, IndexOptions: new(options.IndexOptions).SetUnique(true).
			SetPartialFilterExpression(bson.M{"status": models.StorageActionRunning}).SetName("one_running")},
		{Key: []string{"-createAt", "-_id"}},
	})
	return &storageActionRepository{coll: db.Collection(storageActionCollection)}
}

func (r *storageActionRepository) Insert(ctx context.Context, a *models.StorageAction) error {
	_, err := r.coll.InsertOne(ctx, a)
	if mongo.IsDuplicateKeyError(err) {
		return ErrStorageActionRunning
	}
	return err
}

func (r *storageActionRepository) Finish(ctx context.Context, id uuid.UUID, status, why string, freed int64, volumes int, at time.Time) (*models.StorageAction, error) {
	set := bson.M{"status": status, "finished_at": at}
	if why != "" {
		set["err"] = why
	}
	if freed != 0 {
		set["freed"] = freed
	}
	if volumes != 0 {
		set["volumes"] = volumes
	}
	var after models.StorageAction
	err := r.coll.Find(ctx, bson.M{"action_id": id, "status": models.StorageActionRunning}).
		Apply(qmgo.Change{Update: bson.M{"$set": set}, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *storageActionRepository) EndRunning(ctx context.Context, why string, at time.Time) (int64, error) {
	res, err := r.coll.UpdateAll(ctx, bson.M{"status": models.StorageActionRunning},
		bson.M{"$set": bson.M{"status": models.StorageActionFailed, "err": why, "finished_at": at}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// storageActionListSort orders the actions: the latest first.
var storageActionListSort = pagination.SortKey{Field: "createAt", Ascending: false}

// StorageActionCursor is an action's position in the list.
func StorageActionCursor(a *models.StorageAction) string {
	return pagination.EncodeCursor(a.CreateAt, a.Id)
}

func (r *storageActionRepository) FindWithCursor(ctx context.Context, cursor *pagination.Cursor, limit int64, forward bool) ([]models.StorageAction, error) {
	if err := storageActionListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.StorageAction{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(bson.M{}, cursor, forward, storageActionListSort)).
		Sort(pagination.SortFieldsKey(forward, storageActionListSort)...).Limit(limit).All(&out)
	if err != nil {
		return nil, err
	}
	if !forward {
		slices.Reverse(out)
	}
	return out, nil
}

func (r *storageActionRepository) Count(ctx context.Context) (int64, error) {
	return r.coll.Find(ctx, bson.M{}).Count()
}
