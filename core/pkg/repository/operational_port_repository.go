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
	// FindByOperationIDWithCursor is a page of the operation's ports in
	// use, the latest opened first; search matches SearchText.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.OperationalPort, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	// FindByTaskID is the task's port; IsNotFound for none.
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.OperationalPort, error)
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
		{Key: []string{"operation_id", "-createAt", "-_id"}},
	})
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
	// Page is the task's streams, the latest first: limit at most, before
	// the stream before (0: from the latest), as filter narrows them.
	Page(ctx context.Context, taskID uuid.UUID, filter TunnelStreamFilter, before uint64, limit int64) ([]models.MinionTunnelStream, error)
	// Totals are the task's streams counted, and their bytes each way.
	Totals(ctx context.Context, taskID uuid.UUID) (TunnelTotals, error)
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

// TunnelStreamFilter narrows a task's streams: search matches the tool's
// address and the destination; State "open" (not ended) or "ended".
type TunnelStreamFilter struct {
	Search string
	State  string
}

// The streams' states a filter names.
const (
	TunnelStreamsOpen  = "open"
	TunnelStreamsEnded = "ended"
)

// TunnelTotals are a task's streams counted, the last one's number, and
// their bytes each way.
type TunnelTotals struct {
	Streams  int64
	Last     uint64
	Up, Down int64
}

func (r *minionTunnelStreamRepository) Insert(ctx context.Context, s *models.MinionTunnelStream) error {
	s.SearchText = models.TunnelStreamSearchText(s)
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

func (r *minionTunnelStreamRepository) Page(ctx context.Context, taskID uuid.UUID, filter TunnelStreamFilter, before uint64, limit int64) ([]models.MinionTunnelStream, error) {
	q := bson.M{"task_id": taskID}
	if before > 0 {
		q["stream"] = bson.M{"$lt": before}
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		q["search_text"] = bson.M{"$regex": searchPattern(search), "$options": "i"}
	}
	switch filter.State {
	case TunnelStreamsOpen:
		q["ended_at"] = bson.M{"$exists": false}
	case TunnelStreamsEnded:
		q["ended_at"] = bson.M{"$exists": true}
	}
	out := []models.MinionTunnelStream{}
	err := r.coll.Find(ctx, q).Sort("-stream").Limit(limit).All(&out)
	return out, err
}

func (r *minionTunnelStreamRepository) Totals(ctx context.Context, taskID uuid.UUID) (TunnelTotals, error) {
	var out []struct {
		N    int64  `bson:"n"`
		Last uint64 `bson:"last"`
		Up   int64  `bson:"up"`
		Down int64  `bson:"down"`
	}
	err := r.coll.Aggregate(ctx, bson.A{
		bson.M{"$match": bson.M{"task_id": taskID}},
		bson.M{"$group": bson.M{"_id": nil, "n": bson.M{"$sum": 1}, "last": bson.M{"$max": "$stream"}, "up": bson.M{"$sum": "$up"}, "down": bson.M{"$sum": "$down"}}},
	}).All(&out)
	if err != nil || len(out) == 0 {
		return TunnelTotals{}, err
	}
	return TunnelTotals{Streams: out[0].N, Last: out[0].Last, Up: out[0].Up, Down: out[0].Down}, nil
}

func (r *minionTunnelStreamRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
