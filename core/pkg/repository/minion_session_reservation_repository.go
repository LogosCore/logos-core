package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionSessionReservationCollection = "minion_session_reservations"

// IMinionSessionReservationRepository keeps the session ids reserved with
// builds made with a new persisted session (models.MinionSessionReservation).
type IMinionSessionReservationRepository interface {
	Insert(ctx context.Context, r *models.MinionSessionReservation) error
	// FindBySessionID returns one reservation; IsNotFound on a miss.
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSessionReservation, error)
	// Delete removes a reservation its claim turned into a session;
	// IsNotFound when another claim took it first.
	Delete(ctx context.Context, sessionID uuid.UUID) error
	// DeleteByBuildID and DeleteByOperationID drop unclaimed reservations
	// with their build or operation.
	DeleteByBuildID(ctx context.Context, buildID uuid.UUID) error
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionSessionReservationRepository struct{ coll database.Collection }

func NewMinionSessionReservationRepository(db database.Database) IMinionSessionReservationRepository {
	db.EnsureIndexes(context.Background(), minionSessionReservationCollection, []opts.IndexModel{
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"build_id"}},
		{Key: []string{"operation_id"}},
	})
	return &minionSessionReservationRepository{coll: db.Collection(minionSessionReservationCollection)}
}

func (r *minionSessionReservationRepository) Insert(ctx context.Context, res *models.MinionSessionReservation) error {
	_, err := r.coll.InsertOne(ctx, res)
	return err
}

func (r *minionSessionReservationRepository) FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSessionReservation, error) {
	var res models.MinionSessionReservation
	if err := r.coll.Find(ctx, bson.M{"session_id": sessionID}).One(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *minionSessionReservationRepository) Delete(ctx context.Context, sessionID uuid.UUID) error {
	return r.coll.Remove(ctx, bson.M{"session_id": sessionID})
}

func (r *minionSessionReservationRepository) DeleteByBuildID(ctx context.Context, buildID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"build_id": buildID})
	return err
}

func (r *minionSessionReservationRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
