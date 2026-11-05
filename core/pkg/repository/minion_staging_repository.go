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

const minionStagingCollection = "minion_stagings"

// MinionStagingTTL is how long a staging waits for the minion's hello.
const MinionStagingTTL = 24 * 60 * 60 // seconds

// IMinionStagingRepository keeps what a minion's stage leaves for its next
// message (models.MinionStaging). MongoDB removes an entry a day after it is
// made.
type IMinionStagingRepository interface {
	Insert(ctx context.Context, s *models.MinionStaging) error
	// FindByTempID returns one entry; IsNotFound on a miss.
	FindByTempID(ctx context.Context, tempID uuid.UUID) (*models.MinionStaging, error)
	// BeginHello marks the entry's hello begun at at, once: first is true
	// for the hello that did, else served is the session the first opened
	// (nil while it is under way). IsNotFound when the entry is gone.
	BeginHello(ctx context.Context, tempID uuid.UUID, at time.Time) (first bool, served *uuid.UUID, err error)
	// Served records the session the entry's hello opened.
	Served(ctx context.Context, tempID, sessionID uuid.UUID) error
}

type minionStagingRepository struct{ coll database.Collection }

func NewMinionStagingRepository(db database.Database) IMinionStagingRepository {
	db.EnsureIndexes(context.Background(), minionStagingCollection, []opts.IndexModel{
		{Key: []string{"temp_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"created_at"}, IndexOptions: new(options.IndexOptions).SetExpireAfterSeconds(MinionStagingTTL)},
	})
	return &minionStagingRepository{coll: db.Collection(minionStagingCollection)}
}

func (r *minionStagingRepository) Insert(ctx context.Context, s *models.MinionStaging) error {
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

func (r *minionStagingRepository) FindByTempID(ctx context.Context, tempID uuid.UUID) (*models.MinionStaging, error) {
	var s models.MinionStaging
	if err := r.coll.Find(ctx, bson.M{"temp_id": tempID}).One(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *minionStagingRepository) BeginHello(ctx context.Context, tempID uuid.UUID, at time.Time) (bool, *uuid.UUID, error) {
	err := r.coll.UpdateOne(ctx, bson.M{"temp_id": tempID, "hello_at": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"hello_at": at}})
	switch {
	case err == nil:
		return true, nil, nil
	case !IsNotFound(err):
		return false, nil, err
	}
	s, err := r.FindByTempID(ctx, tempID)
	if err != nil {
		return false, nil, err
	}
	return false, s.SessionID, nil
}

func (r *minionStagingRepository) Served(ctx context.Context, tempID, sessionID uuid.UUID) error {
	return r.coll.UpdateOne(ctx, bson.M{"temp_id": tempID}, bson.M{"$set": bson.M{"session_id": sessionID}})
}
