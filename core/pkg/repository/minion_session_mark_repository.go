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
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionSessionMarkCollection = "minion_session_marks"

// IMinionSessionMarkRepository keeps the operators' own marks on sessions
// (models.MinionSessionMark): one per operator and session.
type IMinionSessionMarkRepository interface {
	// SetMany marks these sessions for the operator, all alike, in one write;
	// an empty mark removes theirs.
	SetMany(ctx context.Context, userID, operationID uuid.UUID, sessionIDs []uuid.UUID, color, icon, emoji string) error
	// ForSessions returns the operator's marks on these sessions, by session.
	ForSessions(ctx context.Context, userID uuid.UUID, sessionIDs []uuid.UUID) (map[uuid.UUID]models.MinionSessionMark, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	// RemoveOrphans removes the marks whose session or operator is gone, and
	// says how many.
	RemoveOrphans(ctx context.Context) (int64, error)
}

type minionSessionMarkRepository struct{ coll database.Collection }

func NewMinionSessionMarkRepository(db database.Database) IMinionSessionMarkRepository {
	db.EnsureIndexes(context.Background(), minionSessionMarkCollection, []opts.IndexModel{
		{Key: []string{"user_id", "session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id"}},
	})
	return &minionSessionMarkRepository{coll: db.Collection(minionSessionMarkCollection)}
}

func (r *minionSessionMarkRepository) SetMany(ctx context.Context, userID, operationID uuid.UUID, sessionIDs []uuid.UUID, color, icon, emoji string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	in := make(bson.A, len(sessionIDs))
	for i, id := range sessionIDs {
		in[i] = id
	}
	if (models.MinionSessionMark{Color: color, Icon: icon, Emoji: emoji}).Empty() {
		_, err := r.coll.RemoveAll(ctx, bson.M{"user_id": userID, "session_id": bson.M{"$in": in}})
		return err
	}
	raw, err := r.coll.RawCollection()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	set := bson.M{"operation_id": operationID, "updated_at": now}
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
	writes := make([]mongo.WriteModel, len(sessionIDs))
	for i, id := range sessionIDs {
		writes[i] = mongo.NewUpdateOneModel().
			SetFilter(bson.M{"user_id": userID, "session_id": id}).
			SetUpdate(update).
			SetUpsert(true)
	}
	_, err = raw.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false))
	return err
}

func (r *minionSessionMarkRepository) ForSessions(ctx context.Context, userID uuid.UUID, sessionIDs []uuid.UUID) (map[uuid.UUID]models.MinionSessionMark, error) {
	out := map[uuid.UUID]models.MinionSessionMark{}
	if len(sessionIDs) == 0 {
		return out, nil
	}
	in := make(bson.A, len(sessionIDs))
	for i, id := range sessionIDs {
		in[i] = id
	}
	var marks []models.MinionSessionMark
	if err := r.coll.Find(ctx, bson.M{"user_id": userID, "session_id": bson.M{"$in": in}}).All(&marks); err != nil {
		return nil, err
	}
	for _, m := range marks {
		out[m.SessionID] = m
	}
	return out, nil
}

func (r *minionSessionMarkRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

// RemoveOrphans removes the marks whose session is gone (a closed session
// leaves by its TTL, which nothing hears) or whose operator is (deleting a
// user cascades nothing). Marks are only read for sessions on screen, so an
// orphan shows nowhere; unswept, they would only pile up.
func (r *minionSessionMarkRepository) RemoveOrphans(ctx context.Context) (int64, error) {
	exists := func(from, field string) bson.M {
		return bson.M{"$lookup": bson.M{"from": from, "localField": field, "foreignField": field, "as": from,
			"pipeline": bson.A{bson.M{"$project": bson.M{"_id": 1}}, bson.M{"$limit": 1}}}}
	}
	var gone []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	err := r.coll.Aggregate(ctx, bson.A{
		exists(minionSessionCollection, "session_id"),
		exists(userCollection, "user_id"),
		bson.M{"$match": bson.M{"$or": bson.A{
			bson.M{minionSessionCollection: bson.M{"$size": 0}},
			bson.M{userCollection: bson.M{"$size": 0}},
		}}},
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
