package repository

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionSessionCollection = "minion_sessions"

// MinionSessionCheckInWindow is how long a session waits for its first
// check-in.
const MinionSessionCheckInWindow = 24 * time.Hour

// IMinionSessionRepository keeps minion sessions (models.MinionSession).
type IMinionSessionRepository interface {
	Insert(ctx context.Context, s *models.MinionSession) error
	// FindBySessionID returns one session; IsNotFound on a miss.
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionSession, error)
	// CheckIn records a message under the session id: the time, and the end
	// of the wait for the first check-in.
	CheckIn(ctx context.Context, sessionID uuid.UUID, at time.Time) error
	// SetHello replaces what the session's hello said (a rehello).
	SetHello(ctx context.Context, sessionID uuid.UUID, hello models.MinionHello) error
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionSession, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionSessionRepository struct{ coll database.Collection }

func NewMinionSessionRepository(db database.Database) IMinionSessionRepository {
	db.EnsureIndexes(context.Background(), minionSessionCollection, []opts.IndexModel{
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at", "-_id"}},
		// A document without expires_at (checked in) never expires.
		{Key: []string{"expires_at"}, IndexOptions: new(options.IndexOptions).SetExpireAfterSeconds(0)},
	})
	return &minionSessionRepository{coll: db.Collection(minionSessionCollection)}
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

// minionSessionListSort orders the sessions list: newest first.
var minionSessionListSort = pagination.SortKey{Field: "created_at"}

// MinionSessionCursor is a session's position in the sessions list.
func MinionSessionCursor(s *models.MinionSession) string {
	return pagination.EncodeCursor(s.CreatedAt, s.Id)
}

// minionSessionListFilter is an operation's sessions, searched (searchPattern)
// over what the Sessions page shows: the host and process, the build and its
// factory, and the ids.
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
	// Ids are binary UUIDs: matched whole, as typed.
	if id, err := uuid.Parse(search); err == nil {
		or = append(or, bson.M{"session_id": id}, bson.M{"build_id": id})
	}
	q["$or"] = or
	return q
}

func (r *minionSessionRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionSession, error) {
	if err := minionSessionListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.MinionSession{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(minionSessionListFilter(operationID, search), cursor, forward, minionSessionListSort)).
		Sort(pagination.SortFieldsKey(forward, minionSessionListSort)...).
		Limit(limit).
		All(&out)
	if !forward {
		slices.Reverse(out)
	}
	return out, err
}

func (r *minionSessionRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	return r.coll.Count(ctx, minionSessionListFilter(operationID, search))
}

func (r *minionSessionRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
