package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
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
