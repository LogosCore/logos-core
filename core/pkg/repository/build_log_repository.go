package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// buildLogCollection holds builds' live logs, a document per chunk the
// factory sent (models.BuildLogChunk).
const buildLogCollection = "build_logs"

// IBuildLogRepository stores builds' live logs.
type IBuildLogRepository interface {
	// Append stores a chunk; false when the build has one of its Seq
	// already (the factory sent it again), which is kept.
	Append(ctx context.Context, c *models.BuildLogChunk) (bool, error)
	// Page returns a build's chunks after Seq after, in order, at most
	// limit.
	Page(ctx context.Context, buildID uuid.UUID, after int64, limit int64) ([]models.BuildLogChunk, error)
	// TrimStart removes a build's oldest chunks, at least bytes of text of
	// them, and returns what it removed.
	TrimStart(ctx context.Context, buildID uuid.UUID, bytes int64) (removedBytes, removedLines int64, err error)
	DeleteByBuildID(ctx context.Context, buildID uuid.UUID) error
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type buildLogRepository struct{ coll database.Collection }

func NewBuildLogRepository(db database.Database) IBuildLogRepository {
	db.EnsureIndexes(context.Background(), buildLogCollection, []opts.IndexModel{
		{Key: []string{"build_id", "seq"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id"}},
	})
	return &buildLogRepository{coll: db.Collection(buildLogCollection)}
}

func (r *buildLogRepository) Append(ctx context.Context, c *models.BuildLogChunk) (bool, error) {
	_, err := r.coll.InsertOne(ctx, c)
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *buildLogRepository) Page(ctx context.Context, buildID uuid.UUID, after int64, limit int64) ([]models.BuildLogChunk, error) {
	var out []models.BuildLogChunk
	err := r.coll.Find(ctx, bson.M{"build_id": buildID, "seq": bson.M{"$gt": after}}).Sort("seq").Limit(limit).All(&out)
	return out, err
}

// trimBatch is how many chunks TrimStart looks at a time.
const trimBatch = 64

func (r *buildLogRepository) TrimStart(ctx context.Context, buildID uuid.UUID, bytes int64) (int64, int64, error) {
	var removedBytes, removedLines int64
	for removedBytes < bytes {
		var oldest []struct {
			Seq   int64 `bson:"seq"`
			Bytes int64 `bson:"bytes"`
			Lines int64 `bson:"lines"`
		}
		err := r.coll.Aggregate(ctx, []bson.M{
			{"$match": bson.M{"build_id": buildID}},
			{"$sort": bson.M{"seq": 1}},
			{"$limit": trimBatch},
			{"$project": bson.M{"seq": 1, "bytes": 1, "lines": bson.M{"$size": "$lines"}}},
		}).All(&oldest)
		if err != nil {
			return removedBytes, removedLines, fmt.Errorf("find the oldest chunks: %w", err)
		}
		if len(oldest) == 0 {
			break
		}
		last := oldest[len(oldest)-1].Seq
		var b, l int64
		for _, c := range oldest {
			b, l = b+c.Bytes, l+c.Lines
			if removedBytes+b >= bytes {
				last = c.Seq
				break
			}
		}
		if _, err := r.coll.RemoveAll(ctx, bson.M{"build_id": buildID, "seq": bson.M{"$lte": last}}); err != nil {
			return removedBytes, removedLines, fmt.Errorf("remove the oldest chunks: %w", err)
		}
		removedBytes, removedLines = removedBytes+b, removedLines+l
	}
	return removedBytes, removedLines, nil
}

func (r *buildLogRepository) DeleteByBuildID(ctx context.Context, buildID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"build_id": buildID})
	return err
}

func (r *buildLogRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
