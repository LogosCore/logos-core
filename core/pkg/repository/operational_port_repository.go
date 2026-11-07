package repository

import (
	"context"
	"errors"
	"slices"
	"strings"

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

const (
	operationalPortCollection = "operational_ports"
	// minionTunnelStreamCollection held tunnel tasks' streams, which core
	// no longer stores (it holds a running tunnel's in memory): an older
	// core's collection is dropped.
	minionTunnelStreamCollection = "minion_tunnel_streams"
)

// ErrPortTaken: another task holds the port (or the task one already).
var ErrPortTaken = errors.New("operational port taken")

// IOperationalPortRepository keeps which operational port serves which task
// (models.OperationalPort): a port serves one task at a time, a task one
// port.
type IOperationalPortRepository interface {
	// Insert takes p's port for its task: ErrPortTaken when either is in
	// use.
	Insert(ctx context.Context, p *models.OperationalPort) error
	// All is every port in use, to open again after a restart.
	All(ctx context.Context) ([]models.OperationalPort, error)
	// FindByOperationIDWithCursor is a page of the operation's ports in
	// use, the latest opened first; search matches SearchText.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.OperationalPort, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	// FindByTaskID is the task's port; IsNotFound for none.
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.OperationalPort, error)
	// ReserveStreams takes n more stream numbers for the task: the last of
	// them (the highest the task has taken).
	ReserveStreams(ctx context.Context, taskID uuid.UUID, n uint64) (uint64, error)
	// SetCounts keeps the task's tunnel's bytes so far, each way, and how
	// many streams it opened.
	SetCounts(ctx context.Context, taskID uuid.UUID, up, down int64, opened uint64) error
	// SetCap keeps the task's tunnel's bandwidth cap (0: none).
	SetCap(ctx context.Context, taskID uuid.UUID, limit int64) error
	// Release gives the task's port back.
	Release(ctx context.Context, taskID uuid.UUID) error
}

type operationalPortRepository struct{ coll database.Collection }

func NewOperationalPortRepository(db database.Database) IOperationalPortRepository {
	db.EnsureIndexes(context.Background(), operationalPortCollection, []opts.IndexModel{
		{Key: []string{"port"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"task_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-createAt", "-_id"}},
	})
	// Dropping a collection that is not there is no error.
	_ = db.Collection(minionTunnelStreamCollection).DropCollection(context.Background())
	return &operationalPortRepository{coll: db.Collection(operationalPortCollection)}
}

func (r *operationalPortRepository) Insert(ctx context.Context, p *models.OperationalPort) error {
	p.SearchText = models.OperationalPortSearchText(p)
	_, err := r.coll.InsertOne(ctx, p)
	if mongo.IsDuplicateKeyError(err) {
		return ErrPortTaken
	}
	return err
}

func (r *operationalPortRepository) All(ctx context.Context) ([]models.OperationalPort, error) {
	out := []models.OperationalPort{}
	err := r.coll.Find(ctx, bson.M{}).Sort("port").Limit(65535).All(&out)
	return out, err
}

// operationalPortListSort orders the Ports list: the latest opened first.
var operationalPortListSort = pagination.SortKey{Field: "createAt", Ascending: false}

// OperationalPortCursor is a port's position in the Ports list.
func OperationalPortCursor(p *models.OperationalPort) string {
	return pagination.EncodeCursor(p.CreateAt, p.Id)
}

func operationalPortFilter(operationID uuid.UUID, search string) bson.M {
	q := bson.M{"operation_id": operationID}
	if search = strings.TrimSpace(search); search != "" {
		q["search_text"] = bson.M{"$regex": searchPattern(search), "$options": "i"}
	}
	return q
}

func (r *operationalPortRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.OperationalPort, error) {
	if err := operationalPortListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.OperationalPort{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(operationalPortFilter(operationID, search), cursor, forward, operationalPortListSort)).
		Sort(pagination.SortFieldsKey(forward, operationalPortListSort)...).Limit(limit).All(&out)
	if err != nil {
		return nil, err
	}
	if !forward {
		slices.Reverse(out)
	}
	return out, nil
}

func (r *operationalPortRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	return r.coll.Find(ctx, operationalPortFilter(operationID, search)).Count()
}

func (r *operationalPortRepository) FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.OperationalPort, error) {
	var p models.OperationalPort
	if err := r.coll.Find(ctx, bson.M{"task_id": taskID}).One(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *operationalPortRepository) ReserveStreams(ctx context.Context, taskID uuid.UUID, n uint64) (uint64, error) {
	var p models.OperationalPort
	err := r.coll.Find(ctx, bson.M{"task_id": taskID}).Apply(qmgo.Change{Update: bson.M{"$inc": bson.M{"streams": int64(n)}}, ReturnNew: true}, &p)
	if err != nil {
		return 0, err
	}
	return p.Streams, nil
}

func (r *operationalPortRepository) SetCounts(ctx context.Context, taskID uuid.UUID, up, down int64, opened uint64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"up": up, "down": down, "opened": int64(opened)}})
}

func (r *operationalPortRepository) SetCap(ctx context.Context, taskID uuid.UUID, limit int64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"cap": limit}})
}

func (r *operationalPortRepository) Release(ctx context.Context, taskID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"task_id": taskID})
	return err
}
