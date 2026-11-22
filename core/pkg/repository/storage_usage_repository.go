package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// IStorageUsageRepository sums what core's records say it keeps in the
// object store (the Storage page): by purpose, operation and kind, each
// record's bytes as its object holds them.
type IStorageUsageRepository interface {
	Usage(ctx context.Context) ([]models.StorageUsage, error)
}

type storageUsageRepository struct{ db database.Database }

func NewStorageUsageRepository(db database.Database) IStorageUsageRepository {
	return &storageUsageRepository{db: db}
}

// storageUsageSource is one kind of record's sum: its collection, which of
// its documents hold an object, and the kind and bytes of each (aggregation
// expressions).
type storageUsageSource struct {
	purpose    string
	collection string
	match      bson.M
	kind       any
	bytes      any
	// noOperation: its records are no operation's (skills).
	noOperation bool
}

// storageUsageSources are core's records of what it keeps in its buckets.
var storageUsageSources = []storageUsageSource{
	// A minion's files: a task file an operator uploaded (kind task), a
	// screenshot's picture, or one the minion sent from its host (no kind):
	// those kept, and the bytes of those coming. A file that failed or was
	// stopped holds nothing: its chunks were dropped.
	{purpose: models.StoragePurposeMinionFiles, collection: minionFileCollection,
		match: bson.M{"state": bson.M{"$in": bson.A{models.MinionFileReceiving, models.MinionFileJoining, models.MinionFileComplete}}},
		kind: bson.M{"$switch": bson.M{"branches": bson.A{
			bson.M{"case": bson.M{"$eq": bson.A{"$kind", models.MinionFileKindTask}}, "then": "upload"},
			bson.M{"case": bson.M{"$eq": bson.A{"$kind", models.MinionFileKindScreenshot}}, "then": "screenshot"},
		}, "default": "download"}},
		bytes: "$received"},
	// The desktops' and the terminals' recordings: a converted one's video,
	// else its parts.
	{purpose: models.StoragePurposeRecordings, collection: recordingCollection, match: bson.M{},
		kind:  bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$front", "terminal"}}, "terminal", "desktop"}},
		bytes: bson.M{"$cond": bson.A{bson.M{"$gt": bson.A{"$video_size", 0}}, "$video_size", "$size"}}},
	// A build's files, and its artifact until an operator deleted its bytes.
	{purpose: models.StoragePurposeBuilds, collection: buildFileCollection, match: bson.M{}, kind: "file", bytes: "$size"},
	{purpose: models.StoragePurposeBuilds, collection: buildCollection,
		match: bson.M{"artifact.object_key": bson.M{"$exists": true, "$ne": ""}, "artifact.deleted": bson.M{"$ne": true}},
		kind:  "artifact", bytes: "$artifact.size"},
	// The wiki's attachments not deleted.
	{purpose: models.StoragePurposeWikiImages, collection: wikiImageCollection, match: bson.M{"deleted_at": bson.M{"$exists": false}},
		kind: "image", bytes: "$size_bytes"},
	{purpose: models.StoragePurposeWikiFiles, collection: wikiFileCollection, match: bson.M{"deleted_at": bson.M{"$exists": false}},
		kind: "file", bytes: "$size_bytes"},
	// Every version of every skill: each a bundle of its own.
	{purpose: models.StoragePurposeSkills, collection: skillVersionCollection, match: bson.M{}, kind: "version", bytes: "$size_bytes",
		noOperation: true},
}

func (r *storageUsageRepository) Usage(ctx context.Context) ([]models.StorageUsage, error) {
	out := []models.StorageUsage{}
	for _, src := range storageUsageSources {
		group := bson.M{"kind": src.kind}
		if !src.noOperation {
			group["op"] = "$operation_id"
		}
		var rows []struct {
			ID struct {
				Op   uuid.UUID `bson:"op"`
				Kind string    `bson:"kind"`
			} `bson:"_id"`
			Objects int64 `bson:"objects"`
			Bytes   int64 `bson:"bytes"`
		}
		err := r.db.Collection(src.collection).Aggregate(ctx, bson.A{
			bson.M{"$match": src.match},
			bson.M{"$group": bson.M{"_id": group, "objects": bson.M{"$sum": 1}, "bytes": bson.M{"$sum": bson.M{"$ifNull": bson.A{src.bytes, 0}}}}},
		}).All(&rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.collection, err)
		}
		for _, row := range rows {
			out = append(out, models.StorageUsage{Purpose: src.purpose, Kind: row.ID.Kind, OperationID: row.ID.Op,
				Objects: row.Objects, Bytes: row.Bytes})
		}
	}
	return out, nil
}
