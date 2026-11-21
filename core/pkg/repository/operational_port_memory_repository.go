package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	operationalPortUseCollection  = "operational_port_uses"
	operationalPortHeatCollection = "operational_port_heat"
)

// IOperationalPortMemoryRepository keeps what core took operational ports
// for: each session's uses of them (models.OperationalPortUse), which core
// offers it again, and each port's heat (models.OperationalPortHeat), the
// least heated taken first for the rest.
type IOperationalPortMemoryRepository interface {
	// Record counts one taking of u's port, at at: the session's use of it
	// and the port's heat.
	Record(ctx context.Context, u models.OperationalPortUse, at time.Time) error
	// BySession is the session's uses, the latest first.
	BySession(ctx context.Context, sessionID uuid.UUID) ([]models.OperationalPortUse, error)
	// Heat is every port core took, by port.
	Heat(ctx context.Context) (map[int]models.OperationalPortHeat, error)
	// DeleteByOperationID forgets the operation's sessions' uses; the
	// ports' heat stays.
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type operationalPortMemoryRepository struct {
	uses database.Collection
	heat database.Collection
}

func NewOperationalPortMemoryRepository(db database.Database) IOperationalPortMemoryRepository {
	db.EnsureIndexes(context.Background(), operationalPortUseCollection, []opts.IndexModel{
		{Key: []string{"session_id", "purpose", "target", "port"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"session_id", "-last_at"}},
		{Key: []string{"operation_id"}},
	})
	db.EnsureIndexes(context.Background(), operationalPortHeatCollection, []opts.IndexModel{
		{Key: []string{"port"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &operationalPortMemoryRepository{uses: db.Collection(operationalPortUseCollection), heat: db.Collection(operationalPortHeatCollection)}
}

func (r *operationalPortMemoryRepository) Record(ctx context.Context, u models.OperationalPortUse, at time.Time) error {
	uses, err := r.uses.RawCollection()
	if err != nil {
		return err
	}
	upsert := options.Update().SetUpsert(true)
	if _, err := uses.UpdateOne(ctx,
		bson.M{"session_id": u.SessionID, "purpose": u.Purpose, "target": u.Target, "port": u.Port},
		bson.M{"$inc": bson.M{"uses": 1}, "$set": bson.M{"operation_id": u.OperationID, "last_at": at}, "$setOnInsert": bson.M{"first_at": at}},
		upsert); err != nil {
		return err
	}
	heat, err := r.heat.RawCollection()
	if err != nil {
		return err
	}
	_, err = heat.UpdateOne(ctx, bson.M{"port": u.Port}, bson.M{"$inc": bson.M{"uses": 1}, "$set": bson.M{"last_at": at}}, upsert)
	return err
}

func (r *operationalPortMemoryRepository) BySession(ctx context.Context, sessionID uuid.UUID) ([]models.OperationalPortUse, error) {
	out := []models.OperationalPortUse{}
	err := r.uses.Find(ctx, bson.M{"session_id": sessionID}).Sort("-last_at").Limit(1000).All(&out)
	return out, err
}

func (r *operationalPortMemoryRepository) Heat(ctx context.Context) (map[int]models.OperationalPortHeat, error) {
	var all []models.OperationalPortHeat
	if err := r.heat.Find(ctx, bson.M{}).Limit(65536).All(&all); err != nil {
		return nil, err
	}
	out := make(map[int]models.OperationalPortHeat, len(all))
	for _, h := range all {
		out[h.Port] = h
	}
	return out, nil
}

func (r *operationalPortMemoryRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.uses.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
