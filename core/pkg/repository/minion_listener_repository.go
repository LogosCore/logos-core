package repository

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionListenerCollection = "minion_listeners"

// IMinionListenerRepository keeps reverse port forwards' listeners
// (models.MinionListener): one a task, while it runs.
type IMinionListenerRepository interface {
	Insert(ctx context.Context, l *models.MinionListener) error
	// All is every listener kept, to take up again after a restart.
	All(ctx context.Context) ([]models.MinionListener, error)
	// FindByOperationIDWithCursor is a page of the operation's listeners,
	// the latest opened first; search matches SearchText.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionListener, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	// FindByTaskID is the task's listener; IsNotFound for none.
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionListener, error)
	// SetBound keeps where the minion says it listens.
	SetBound(ctx context.Context, taskID uuid.UUID, bound string) error
	// SeenStream raises the highest stream number the minion opened to n.
	SeenStream(ctx context.Context, taskID uuid.UUID, n uint64) error
	// SetCounts keeps the task's tunnel's bytes so far, each way, and how
	// many streams it opened.
	SetCounts(ctx context.Context, taskID uuid.UUID, up, down int64, opened uint64) error
	// SetCap keeps the task's tunnel's bandwidth cap (0: none).
	SetCap(ctx context.Context, taskID uuid.UUID, limit int64) error
	// Release forgets the task's listener.
	Release(ctx context.Context, taskID uuid.UUID) error
}

type minionListenerRepository struct{ coll database.Collection }

func NewMinionListenerRepository(db database.Database) IMinionListenerRepository {
	db.EnsureIndexes(context.Background(), minionListenerCollection, []opts.IndexModel{
		{Key: []string{"task_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-createAt", "-_id"}},
	})
	return &minionListenerRepository{coll: db.Collection(minionListenerCollection)}
}

func (r *minionListenerRepository) Insert(ctx context.Context, l *models.MinionListener) error {
	l.SearchText = models.MinionListenerSearchText(l)
	_, err := r.coll.InsertOne(ctx, l)
	return err
}

func (r *minionListenerRepository) All(ctx context.Context) ([]models.MinionListener, error) {
	out := []models.MinionListener{}
	err := r.coll.Find(ctx, bson.M{}).Sort("createAt").Limit(65535).All(&out)
	return out, err
}

// minionListenerListSort orders the Ports page's minions' list: the latest
// opened first.
var minionListenerListSort = pagination.SortKey{Field: "createAt", Ascending: false}

// MinionListenerCursor is a listener's position in its list.
func MinionListenerCursor(l *models.MinionListener) string {
	return pagination.EncodeCursor(l.CreateAt, l.Id)
}

func minionListenerFilter(operationID uuid.UUID, search string) bson.M {
	q := bson.M{"operation_id": operationID}
	if search = strings.TrimSpace(search); search != "" {
		q["search_text"] = bson.M{"$regex": searchPattern(search), "$options": "i"}
	}
	return q
}

func (r *minionListenerRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionListener, error) {
	if err := minionListenerListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.MinionListener{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(minionListenerFilter(operationID, search), cursor, forward, minionListenerListSort)).
		Sort(pagination.SortFieldsKey(forward, minionListenerListSort)...).Limit(limit).All(&out)
	if err != nil {
		return nil, err
	}
	if !forward {
		slices.Reverse(out)
	}
	return out, nil
}

func (r *minionListenerRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	return r.coll.Find(ctx, minionListenerFilter(operationID, search)).Count()
}

func (r *minionListenerRepository) FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.MinionListener, error) {
	var l models.MinionListener
	if err := r.coll.Find(ctx, bson.M{"task_id": taskID}).One(&l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *minionListenerRepository) SetBound(ctx context.Context, taskID uuid.UUID, bound string) error {
	l, err := r.FindByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	l.Bound = bound
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID},
		bson.M{"$set": bson.M{"bound": bound, "search_text": models.MinionListenerSearchText(l)}})
}

func (r *minionListenerRepository) SeenStream(ctx context.Context, taskID uuid.UUID, n uint64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$max": bson.M{"streams": n}})
}

func (r *minionListenerRepository) SetCounts(ctx context.Context, taskID uuid.UUID, up, down int64, opened uint64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"up": up, "down": down, "opened": int64(opened)}})
}

func (r *minionListenerRepository) SetCap(ctx context.Context, taskID uuid.UUID, limit int64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"cap": limit}})
}

func (r *minionListenerRepository) Release(ctx context.Context, taskID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"task_id": taskID})
	return err
}
