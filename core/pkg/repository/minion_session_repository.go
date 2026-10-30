package repository

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	minionSessionCollection = "minion_sessions"
	// minionSessionCounterCollection holds one counter per operation: the
	// last session number it gave.
	minionSessionCounterCollection = "minion_session_counters"
)

// MinionSessionCheckInWindow is how long a session waits for its first
// check-in.
const MinionSessionCheckInWindow = 24 * time.Hour

// IMinionSessionRepository keeps minion sessions (models.MinionSession).
type IMinionSessionRepository interface {
	// NextNumber reserves the operation's next session number: 1, 2, 3, …
	NextNumber(ctx context.Context, operationID uuid.UUID) (int64, error)
	Insert(ctx context.Context, s *models.MinionSession) error
	// FindBySessionID returns one session; IsNotFound on a miss.
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error)
	// CheckIn records a message under the session id: the time, and the end
	// of the wait for the first check-in.
	CheckIn(ctx context.Context, sessionID uuid.UUID, at time.Time) error
	// SetHello replaces what the session's hello said (a rehello).
	SetHello(ctx context.Context, sessionID uuid.UUID, hello models.MinionHello) error
	// FindByOperationIDWithCursor is a page of an operation's sessions as q
	// asks, from after (nil: the start); forward false pages back.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, after *MinionSessionKeyset, limit int64, forward bool) ([]models.MinionSession, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery) (int64, error)
	// Values are the distinct values an operation's sessions have in a
	// column, sorted; missing values are left out.
	Values(ctx context.Context, operationID uuid.UUID, column string) ([]string, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionSessionRepository struct{ coll, counters database.Collection }

func NewMinionSessionRepository(db database.Database) IMinionSessionRepository {
	db.EnsureIndexes(context.Background(), minionSessionCollection, []opts.IndexModel{
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "number"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at", "-_id"}},
		// A document without expires_at (checked in) never expires.
		{Key: []string{"expires_at"}, IndexOptions: new(options.IndexOptions).SetExpireAfterSeconds(0)},
	})
	db.EnsureIndexes(context.Background(), minionSessionCounterCollection, []opts.IndexModel{
		{Key: []string{"operation_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &minionSessionRepository{
		coll: db.Collection(minionSessionCollection), counters: db.Collection(minionSessionCounterCollection),
	}
}

func (r *minionSessionRepository) NextNumber(ctx context.Context, operationID uuid.UUID) (int64, error) {
	raw, err := r.counters.RawCollection()
	if err != nil {
		return 0, err
	}
	var counter struct {
		Seq int64 `bson:"seq"`
	}
	err = raw.FindOneAndUpdate(ctx,
		bson.M{"operation_id": operationID},
		bson.M{"$inc": bson.M{"seq": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&counter)
	return counter.Seq, err
}

func (r *minionSessionRepository) Insert(ctx context.Context, s *models.MinionSession) error {
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

func (r *minionSessionRepository) FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error) {
	var s models.MinionSession
	if err := r.coll.Find(ctx, bson.M{"session_id": sessionID}).One(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *minionSessionRepository) CheckIn(ctx context.Context, sessionID uuid.UUID, at time.Time) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID},
		bson.M{"$set": bson.M{"last_seen_at": at}, "$unset": bson.M{"expires_at": ""}})
}

func (r *minionSessionRepository) SetHello(ctx context.Context, sessionID uuid.UUID, hello models.MinionHello) error {
	return r.coll.UpdateOne(ctx, bson.M{"session_id": sessionID}, bson.M{"$set": bson.M{"hello": hello}})
}

// minionSessionListFilter is an operation's sessions, searched (searchPattern)
// over what the Sessions page shows: the host and process, the build and its
// factory, and the ids. A number ("12", "#12") also matches the session
// numbered so.
func minionSessionListFilter(operationID uuid.UUID, search string) bson.M {
	q := bson.M{"operation_id": operationID}
	if search == "" {
		return q
	}
	rx := bson.M{"$regex": searchPattern(search), "$options": "i"}
	or := bson.A{}
	for _, field := range []string{
		"hello.host", "hello.user", "hello.domain", "hello.proc", "hello.os", "hello.arch", "hello.integrity",
		"hello.addrs", "hello.version", "build_description", "module_name", "target", "platform",
	} {
		or = append(or, bson.M{field: rx})
	}
	if n, ok := ParseSessionSearchNumber(search); ok {
		or = append(or, bson.M{"number": n})
	}
	// Ids are binary UUIDs: matched whole, as typed.
	if id, err := uuid.Parse(search); err == nil {
		or = append(or, bson.M{"session_id": id}, bson.M{"build_id": id})
	}
	q["$or"] = or
	return q
}

func (r *minionSessionRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery, after *MinionSessionKeyset, limit int64, forward bool) ([]models.MinionSession, error) {
	filter := minionSessionFilter(operationID, q, time.Now().UTC())
	if after != nil {
		ascending := !q.Sort.Desc == forward
		filter = bson.M{"$and": bson.A{filter, keysetFilter(models.MinionSessionColumns[q.Sort.Column].Field, after, ascending)}}
	}
	out := []models.MinionSession{}
	err := r.coll.Find(ctx, filter).Sort(sortFields(q.Sort, forward)...).Limit(limit).All(&out)
	if !forward {
		slices.Reverse(out)
	}
	return out, err
}

func (r *minionSessionRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, q MinionSessionQuery) (int64, error) {
	return r.coll.Count(ctx, minionSessionFilter(operationID, q, time.Now().UTC()))
}

func (r *minionSessionRepository) Values(ctx context.Context, operationID uuid.UUID, column string) ([]string, error) {
	col, ok := models.MinionSessionColumns[column]
	if !ok || (col.Kind != models.SessionColumnEnum && col.Kind != models.SessionColumnBool) {
		return nil, fmt.Errorf("%s has no list of values", column)
	}
	raw, err := r.coll.RawCollection()
	if err != nil {
		return nil, err
	}
	found, err := raw.Distinct(ctx, col.Field, bson.M{"operation_id": operationID})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, v := range found {
		switch v := v.(type) {
		case string:
			if v != "" {
				out = append(out, v)
			}
		case bool:
			out = append(out, strconv.FormatBool(v))
		}
	}
	slices.Sort(out)
	return out, nil
}

func (r *minionSessionRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	if _, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID}); err != nil {
		return err
	}
	_, err := r.counters.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
