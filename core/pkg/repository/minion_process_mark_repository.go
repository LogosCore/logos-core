package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionProcessMarkCollection = "minion_process_marks"

// IMinionProcessMarkRepository keeps the users' own marks on processes by
// name (models.MinionProcessMark): one per user and name, whatever its case.
type IMinionProcessMarkRepository interface {
	// Set keeps the user's own mark on a name as given: an empty one too,
	// which shows none (it hides the name's built-in mark).
	Set(ctx context.Context, userID uuid.UUID, name, color, icon, emoji string) error
	// Remove takes the user's own mark on a name away: the name shows its
	// built-in mark again, if it has one.
	Remove(ctx context.Context, userID uuid.UUID, name string) error
	// ForUser is the user's marks, by key (models.ProcessMarkKey), the
	// empty ones too.
	ForUser(ctx context.Context, userID uuid.UUID) (map[string]models.MinionProcessMark, error)
	// RemoveOrphans removes the marks of users gone, and says how many.
	RemoveOrphans(ctx context.Context) (int64, error)
}

type minionProcessMarkRepository struct{ coll database.Collection }

func NewMinionProcessMarkRepository(db database.Database) IMinionProcessMarkRepository {
	db.EnsureIndexes(context.Background(), minionProcessMarkCollection, []opts.IndexModel{
		{Key: []string{"user_id", "key"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &minionProcessMarkRepository{coll: db.Collection(minionProcessMarkCollection)}
}

func (r *minionProcessMarkRepository) Set(ctx context.Context, userID uuid.UUID, name, color, icon, emoji string) error {
	filter := bson.M{"user_id": userID, "key": models.ProcessMarkKey(name)}
	set := bson.M{"name": name, "updated_at": time.Now().UTC()}
	unset := bson.M{}
	for k, v := range map[string]string{"color": color, "icon": icon, "emoji": emoji} {
		if v == "" {
			unset[k] = ""
		} else {
			set[k] = v
		}
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	// An update upserted, not qmgo's Upsert: that one replaces the document.
	raw, err := r.coll.RawCollection()
	if err != nil {
		return err
	}
	_, err = raw.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *minionProcessMarkRepository) Remove(ctx context.Context, userID uuid.UUID, name string) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"user_id": userID, "key": models.ProcessMarkKey(name)})
	return err
}

func (r *minionProcessMarkRepository) ForUser(ctx context.Context, userID uuid.UUID) (map[string]models.MinionProcessMark, error) {
	var marks []models.MinionProcessMark
	if err := r.coll.Find(ctx, bson.M{"user_id": userID}).All(&marks); err != nil {
		return nil, err
	}
	out := make(map[string]models.MinionProcessMark, len(marks))
	for _, m := range marks {
		out[m.Key] = m
	}
	return out, nil
}

// RemoveOrphans removes the marks of users gone (deleting a user cascades
// nothing). A process mark outlives every process of its name on purpose: it
// is for the next list that has one. A clearing stays too, though its name
// has no built-in mark now: the deployment's file may have that mark again at
// a later start (or its mount have been missing at this one), and the user
// cleared it; meanwhile it shows nothing.
func (r *minionProcessMarkRepository) RemoveOrphans(ctx context.Context) (int64, error) {
	var gone []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	err := r.coll.Aggregate(ctx, bson.A{
		bson.M{"$lookup": bson.M{"from": userCollection, "localField": "user_id", "foreignField": "user_id", "as": userCollection,
			"pipeline": bson.A{bson.M{"$project": bson.M{"_id": 1}}, bson.M{"$limit": 1}}}},
		bson.M{"$match": bson.M{userCollection: bson.M{"$size": 0}}},
		bson.M{"$project": bson.M{"_id": 1}},
	}).All(&gone)
	if err != nil || len(gone) == 0 {
		return 0, err
	}
	ids := make(bson.A, len(gone))
	for i, g := range gone {
		ids[i] = g.ID
	}
	res, err := r.coll.RemoveAll(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
