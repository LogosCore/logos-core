package repository

import (
	"context"

	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const storageRetentionCollection = "storage_retention"

// storageRetentionKey is the one policy's document: every operation's.
const storageRetentionKey = "recordings"

// IStorageRetentionRepository keeps the retention policy
// (models.StorageRetention): one for every operation.
type IStorageRetentionRepository interface {
	// Get is the policy: off, at models.DefaultRetentionDays, before an
	// admin set it.
	Get(ctx context.Context) (models.StorageRetention, error)
	Set(ctx context.Context, p models.StorageRetention) error
}

type storageRetentionRepository struct{ coll database.Collection }

func NewStorageRetentionRepository(db database.Database) IStorageRetentionRepository {
	db.EnsureIndexes(context.Background(), storageRetentionCollection, []opts.IndexModel{
		{Key: []string{"key"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &storageRetentionRepository{coll: db.Collection(storageRetentionCollection)}
}

// storageRetentionDoc is the policy as it is kept.
type storageRetentionDoc struct {
	Key                     string `bson:"key"`
	models.StorageRetention `bson:",inline"`
}

func (r *storageRetentionRepository) Get(ctx context.Context) (models.StorageRetention, error) {
	var doc storageRetentionDoc
	err := r.coll.Find(ctx, bson.M{"key": storageRetentionKey}).One(&doc)
	if IsNotFound(err) {
		return models.StorageRetention{OlderThanDays: models.DefaultRetentionDays}, nil
	}
	if err != nil {
		return models.StorageRetention{}, err
	}
	return doc.StorageRetention, nil
}

func (r *storageRetentionRepository) Set(ctx context.Context, p models.StorageRetention) error {
	_, err := r.coll.Upsert(ctx, bson.M{"key": storageRetentionKey}, storageRetentionDoc{Key: storageRetentionKey, StorageRetention: p})
	return err
}
