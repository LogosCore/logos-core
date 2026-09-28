package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// IUserPreferencesRepository writes the per-operator picker history kept on
// the user document: models.User.FrequentIcons and RecentOperations.
//
// It is separate from IUserRepository because its writes cannot go through
// IUserRepository.Update. That one is read-modify-write — the resolver loads
// the user, edits a field in Go, and $sets it back — which is fine for a flag
// but loses increments when two tabs record a use at the same moment. Each
// method here is a single update-with-pipeline, so the whole change (bump,
// re-rank, cap) happens inside Mongo against the current document.
type IUserPreferencesRepository interface {
	// RecordIconUse bumps an icon's count and recency, inserting it at count
	// 1 if absent, then re-ranks and caps the list at models.MaxFrequentIcons.
	RecordIconUse(ctx context.Context, userID uuid.UUID, name string, at time.Time) error
	// TouchRecentOperation moves an operation to the front of the recent
	// list, dropping any earlier occurrence, capped at models.MaxRecentOperations.
	TouchRecentOperation(ctx context.Context, userID, operationID uuid.UUID) error
	// SeedFrequentIcons and SeedRecentOperations write a whole list, but only
	// when the stored one is missing or empty. They exist for the one-time
	// import of what the SPA used to keep in localStorage: the condition is in
	// the filter, so two tabs importing at once cannot both win, and an import
	// arriving after the operator has already used the server-side list is a
	// no-op rather than an overwrite. Neither reports whether it wrote.
	SeedFrequentIcons(ctx context.Context, userID uuid.UUID, icons []models.IconUsage) error
	SeedRecentOperations(ctx context.Context, userID uuid.UUID, operationIDs []uuid.UUID) error
}

type userPreferencesRepository struct {
	coll database.Collection
}

// NewUserPreferencesRepository shares the users collection, whose indexes
// NewUserRepository declares; the only one these writes need is the unique
// user_id index.
func NewUserPreferencesRepository(db database.Database) IUserPreferencesRepository {
	return &userPreferencesRepository{coll: db.Collection(userCollection)}
}

func (r *userPreferencesRepository) RecordIconUse(ctx context.Context, userID uuid.UUID, name string, at time.Time) error {
	return r.coll.UpdateOne(ctx, bson.M{"user_id": userID}, recordIconUsePipeline(name, at))
}

func (r *userPreferencesRepository) TouchRecentOperation(ctx context.Context, userID, operationID uuid.UUID) error {
	return r.coll.UpdateOne(ctx, bson.M{"user_id": userID}, touchRecentOperationPipeline(operationID))
}

func (r *userPreferencesRepository) SeedFrequentIcons(ctx context.Context, userID uuid.UUID, icons []models.IconUsage) error {
	return r.seed(ctx, userID, "frequent_icons", icons)
}

func (r *userPreferencesRepository) SeedRecentOperations(ctx context.Context, userID uuid.UUID, operationIDs []uuid.UUID) error {
	return r.seed(ctx, userID, "recent_operations", operationIDs)
}

func (r *userPreferencesRepository) seed(ctx context.Context, userID uuid.UUID, field string, value any) error {
	err := r.coll.UpdateOne(ctx, seedFilter(userID, field), bson.M{"$set": bson.M{field: value}})
	// No match means the list was already populated — the expected outcome
	// for every import after the first, not a failure.
	if IsNotFound(err) {
		return nil
	}
	return err
}

// seedFilter matches the user only while field is missing, null or empty.
// {$in: [null, []]} covers all three: null matches missing and null, and []
// matches an empty array by equality.
func seedFilter(userID uuid.UUID, field string) bson.M {
	return bson.M{"user_id": userID, field: bson.M{"$in": bson.A{nil, bson.A{}}}}
}

// recordIconUsePipeline is the update for RecordIconUse:
//
//	cur  = frequent_icons ?? []
//	old  = the entry named name, if any
//	rest = cur without it
//	frequent_icons = slice(sort(rest + {name, old.count+1, at}), MaxFrequentIcons)
//
// The name goes in through $literal: a pipeline reads any string starting with
// "$" as a field path, and this one comes from the client.
func recordIconUsePipeline(name string, at time.Time) bson.A {
	lit := bson.M{"$literal": name}
	byName := func(op string) bson.M {
		return bson.M{"$filter": bson.M{
			"input": "$$cur",
			"cond":  bson.M{op: bson.A{"$$this.name", lit}},
		}}
	}
	return bson.A{bson.M{"$set": bson.M{"frequent_icons": bson.M{"$let": bson.M{
		"vars": bson.M{"cur": bson.M{"$ifNull": bson.A{"$frequent_icons", bson.A{}}}},
		"in": bson.M{"$let": bson.M{
			"vars": bson.M{
				"old":  bson.M{"$first": byName("$eq")},
				"rest": byName("$ne"),
			},
			"in": bson.M{"$slice": bson.A{
				bson.M{"$sortArray": bson.M{
					"input": bson.M{"$concatArrays": bson.A{"$$rest", bson.A{bson.M{
						"name":      lit,
						"count":     bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$$old.count", 0}}, 1}},
						"last_used": at,
					}}}},
					// bson.D: the sort key order is the ranking.
					"sortBy": bson.D{{Key: "count", Value: -1}, {Key: "last_used", Value: -1}},
				}},
				models.MaxFrequentIcons,
			}},
		}},
	}}}}}
}

// touchRecentOperationPipeline is the update for TouchRecentOperation:
// recent_operations = slice([id] + (recent_operations ?? []).without(id), Max).
func touchRecentOperationPipeline(operationID uuid.UUID) bson.A {
	return bson.A{bson.M{"$set": bson.M{"recent_operations": bson.M{"$slice": bson.A{
		bson.M{"$concatArrays": bson.A{
			bson.A{operationID},
			bson.M{"$filter": bson.M{
				"input": bson.M{"$ifNull": bson.A{"$recent_operations", bson.A{}}},
				"cond":  bson.M{"$ne": bson.A{"$$this", operationID}},
			}},
		}},
		models.MaxRecentOperations,
	}}}}}
}
