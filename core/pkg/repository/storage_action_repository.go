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

// Why a cancel is refused: the action ended, or ends at once (a grow).
var (
	ErrStorageActionNotRunning     = errors.New("storage action not running")
	ErrStorageActionNotCancellable = errors.New("storage action not cancellable")
)

// IStorageActionRepository keeps the maintenance actions admins run on the
// object store (models.StorageAction).
type IStorageActionRepository interface {
	// Insert starts a running action: ErrStorageActionRunning when one of
	// its kind, for its bucket, runs already.
	Insert(ctx context.Context, a *models.StorageAction) error
	// Get is one action: IsNotFound for none.
	Get(ctx context.Context, id uuid.UUID) (*models.StorageAction, error)
	// Finish ends a running action with status: why it failed, what it did.
	Finish(ctx context.Context, id uuid.UUID, status, why string, freed int64, volumes int, at time.Time) (*models.StorageAction, error)
	// Progress keeps how far a running reclaim or cleanup came.
	Progress(ctx context.Context, id uuid.UUID, p models.StorageActionProgress) error
	// RequestCancel marks a running reclaim or cleanup asked to stop, by
	// whom (the first ask is kept): the action as it is then.
	// ErrStorageActionNotRunning for one that ended,
	// ErrStorageActionNotCancellable for a grow, IsNotFound for none.
	RequestCancel(ctx context.Context, id uuid.UUID, by models.ModuleActor, at time.Time) (*models.StorageAction, error)
	// SetVacuumPaused says whether a reclaim paused SeaweedFS's own vacuum
	// and has not resumed it; VacuumPaused is every action that says so.
	SetVacuumPaused(ctx context.Context, id uuid.UUID, paused bool) error
	VacuumPaused(ctx context.Context) ([]models.StorageAction, error)
	// Last is the latest action of a kind actor ran: nil for none.
	Last(ctx context.Context, action string, actor models.ModuleActor) (*models.StorageAction, error)
	// EndRunning fails every action still running, saying why, and answers
	// them as they were: a core that stopped while they ran no longer knows
	// how they went.
	EndRunning(ctx context.Context, why string, at time.Time) ([]models.StorageAction, error)
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

func (r *storageActionRepository) Get(ctx context.Context, id uuid.UUID) (*models.StorageAction, error) {
	var a models.StorageAction
	if err := r.coll.Find(ctx, bson.M{"action_id": id}).One(&a); err != nil {
		return nil, err
	}
	return &a, nil
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
		Apply(qmgo.Change{Update: bson.M{"$set": set, "$unset": bson.M{"step": "", "step_at": ""}}, ReturnNew: true}, &after)
	if err != nil {
		return nil, err
	}
	return &after, nil
}

func (r *storageActionRepository) Progress(ctx context.Context, id uuid.UUID, p models.StorageActionProgress) error {
	set := bson.M{"step": p.Step, "step_at": p.StepAt, "freed": p.Freed, "recordings": p.Recordings, "bytes": p.Bytes}
	if p.ReclaimVolumes != nil {
		set["reclaim_volumes"] = p.ReclaimVolumes
	}
	return r.coll.UpdateOne(ctx, bson.M{"action_id": id, "status": models.StorageActionRunning}, bson.M{"$set": set})
}

// cancellable are the actions that run long enough to stop.
var cancellable = bson.A{models.StorageActionReclaim, models.StorageActionCleanup}

func (r *storageActionRepository) RequestCancel(ctx context.Context, id uuid.UUID, by models.ModuleActor, at time.Time) (*models.StorageAction, error) {
	var after models.StorageAction
	err := r.coll.Find(ctx, bson.M{"action_id": id, "status": models.StorageActionRunning, "action": bson.M{"$in": cancellable},
		"cancel_requested_at": bson.M{"$exists": false}}).
		Apply(qmgo.Change{Update: bson.M{"$set": bson.M{"cancel_requested_at": at, "cancelled_by": by}}, ReturnNew: true}, &after)
	if err == nil {
		return &after, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}
	a, err := r.Get(ctx, id)
	switch {
	case err != nil:
		return nil, err
	case a.Action != models.StorageActionReclaim && a.Action != models.StorageActionCleanup:
		return nil, ErrStorageActionNotCancellable
	case a.Status != models.StorageActionRunning:
		return nil, ErrStorageActionNotRunning
	}
	// Asked already: it stops as it was asked first.
	return a, nil
}

func (r *storageActionRepository) SetVacuumPaused(ctx context.Context, id uuid.UUID, paused bool) error {
	update := bson.M{"$unset": bson.M{"vacuum_paused": ""}}
	if paused {
		update = bson.M{"$set": bson.M{"vacuum_paused": true}}
	}
	return r.coll.UpdateOne(ctx, bson.M{"action_id": id}, update)
}

func (r *storageActionRepository) VacuumPaused(ctx context.Context) ([]models.StorageAction, error) {
	out := []models.StorageAction{}
	err := r.coll.Find(ctx, bson.M{"vacuum_paused": true}).All(&out)
	return out, err
}

func (r *storageActionRepository) Last(ctx context.Context, action string, actor models.ModuleActor) (*models.StorageAction, error) {
	var a models.StorageAction
	err := r.coll.Find(ctx, bson.M{"action": action, "actor.type": actor.Type, "actor.id": actor.ID}).Sort("-started_at").One(&a)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *storageActionRepository) EndRunning(ctx context.Context, why string, at time.Time) ([]models.StorageAction, error) {
	var running []models.StorageAction
	if err := r.coll.Find(ctx, bson.M{"status": models.StorageActionRunning}).All(&running); err != nil {
		return nil, err
	}
	ended := []models.StorageAction{}
	for _, a := range running {
		err := r.coll.UpdateOne(ctx, bson.M{"action_id": a.ActionID, "status": models.StorageActionRunning},
			bson.M{"$set": bson.M{"status": models.StorageActionFailed, "err": why, "finished_at": at}, "$unset": bson.M{"step": "", "step_at": ""}})
		if IsNotFound(err) {
			continue
		}
		if err != nil {
			return ended, err
		}
		ended = append(ended, a)
	}
	return ended, nil
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
