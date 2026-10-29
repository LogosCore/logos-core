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
