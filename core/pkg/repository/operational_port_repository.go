package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	operationalPortCollection    = "operational_ports"
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
	// ListByOperation is the operation's ports in use, lowest first.
	ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.OperationalPort, error)
	// NextStream counts a new stream of the task's: its number.
	NextStream(ctx context.Context, taskID uuid.UUID) (uint64, error)
	// Release gives the task's port back.
	Release(ctx context.Context, taskID uuid.UUID) error
}

type operationalPortRepository struct{ coll database.Collection }

func NewOperationalPortRepository(db database.Database) IOperationalPortRepository {
	db.EnsureIndexes(context.Background(), operationalPortCollection, []opts.IndexModel{
		{Key: []string{"port"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"task_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "port"}},
	})
	return &operationalPortRepository{coll: db.Collection(operationalPortCollection)}
}

func (r *operationalPortRepository) Insert(ctx context.Context, p *models.OperationalPort) error {
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

func (r *operationalPortRepository) ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.OperationalPort, error) {
	out := []models.OperationalPort{}
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Sort("port").Limit(65535).All(&out)
	return out, err
}

func (r *operationalPortRepository) NextStream(ctx context.Context, taskID uuid.UUID) (uint64, error) {
	var p models.OperationalPort
	err := r.coll.Find(ctx, bson.M{"task_id": taskID}).Apply(qmgo.Change{Update: bson.M{"$inc": bson.M{"streams": 1}}, ReturnNew: true}, &p)
	if err != nil {
		return 0, err
	}
	return p.Streams, nil
}

func (r *operationalPortRepository) Release(ctx context.Context, taskID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"task_id": taskID})
	return err
}

// IMinionTunnelStreamRepository keeps tunnel tasks' streams
// (models.MinionTunnelStream), the operation's record of what was reached
// through its minions.
type IMinionTunnelStreamRepository interface {
	Insert(ctx context.Context, s *models.MinionTunnelStream) error
	// Update writes the stream as it is now: its reply, its end, its byte
	// counts.
	Update(ctx context.Context, s *models.MinionTunnelStream) error
	// ListByTask is the task's streams, the latest first, at most limit.
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]models.MinionTunnelStream, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionTunnelStreamRepository struct{ coll database.Collection }

func NewMinionTunnelStreamRepository(db database.Database) IMinionTunnelStreamRepository {
	db.EnsureIndexes(context.Background(), minionTunnelStreamCollection, []opts.IndexModel{
		{Key: []string{"task_id", "stream"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id"}},
	})
	return &minionTunnelStreamRepository{coll: db.Collection(minionTunnelStreamCollection)}
}

func (r *minionTunnelStreamRepository) Insert(ctx context.Context, s *models.MinionTunnelStream) error {
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

func (r *minionTunnelStreamRepository) Update(ctx context.Context, s *models.MinionTunnelStream) error {
	set := bson.M{"up": s.Up, "down": s.Down}
	if s.Result != nil {
		set["result"], set["bound"], set["msg"] = *s.Result, s.Bound, s.Msg
	}
	if s.EndedAt != nil {
		set["ended_at"], set["end"] = *s.EndedAt, s.End
	}
	return r.coll.UpdateOne(ctx, bson.M{"task_id": s.TaskID, "stream": s.Stream}, bson.M{"$set": set})
}

func (r *minionTunnelStreamRepository) ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]models.MinionTunnelStream, error) {
	out := []models.MinionTunnelStream{}
	err := r.coll.Find(ctx, bson.M{"task_id": taskID}).Sort("-stream").Limit(int64(limit)).All(&out)
	return out, err
}

func (r *minionTunnelStreamRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
