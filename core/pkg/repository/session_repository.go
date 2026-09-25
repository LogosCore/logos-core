package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const sessionCollection = "sessions"

// ISessionRepository is the insert-once creation log for sessions. Every
// successful login writes one row; nothing else is ever written, updated,
// or deleted by application code. Active state lives in Redis (TokenStore);
// this repository is never consulted during authorization.
//
// The resolver reads from this collection to display historical sessions
// in the UI, and decorates each row with `is_active` and `last_activity_at`
// from Redis at query time.
type ISessionRepository interface {
	// Insert persists a new session creation row. Called once on login.
	Insert(ctx context.Context, session *models.Session) error

	// FindByID looks up a session row by its session UUID. Used by
	// AdminRevokeSession to learn the owning user_id from a session_id.
	FindByID(ctx context.Context, id uuid.UUID) (models.Session, error)

	// Count returns the number of rows matching the filter.
	Count(ctx context.Context, filter SessionFilter) (int64, error)

	// FindWithCursor returns one page of rows matching the filter, newest
	// first.
	FindWithCursor(ctx context.Context, filter SessionFilter,
		cursor *pagination.Cursor, limit int64, forward bool) ([]models.Session, error)
}

// SessionFilter narrows the session log. The zero value matches every row.
type SessionFilter struct {
	// UserIDs keeps rows owned by any of these users. Empty: every user.
	UserIDs []uuid.UUID
	// SessionIDs keeps only these sessions — on the activeOnly path, the
	// live set from Redis. Nil: no constraint. Non-nil but empty matches
	// nothing, so an empty live set can never widen into every session.
	SessionIDs []uuid.UUID
}

type sessionRepository struct {
	coll database.Collection
}

// NewSessionRepository creates the session creation-log repository.
func NewSessionRepository(db database.Database) ISessionRepository {
	coll := db.Collection(sessionCollection)

	db.EnsureIndexes(context.Background(), sessionCollection, []opts.IndexModel{
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"user_id", "-createAt"}},
		{Key: []string{"-createAt", "-_id"}}, // cursor pagination
	})

	return &sessionRepository{coll: coll}
}

func (r *sessionRepository) Insert(ctx context.Context, session *models.Session) error {
	_, err := r.coll.InsertOne(ctx, session)
	return err
}

func (r *sessionRepository) FindByID(ctx context.Context, id uuid.UUID) (models.Session, error) {
	var session models.Session
	err := r.coll.FindOne(ctx, bson.M{"session_id": id}).One(&session)
	return session, err
}

func (r *sessionRepository) Count(ctx context.Context, filter SessionFilter) (int64, error) {
	return r.coll.Count(ctx, buildSessionFilter(filter))
}

func (r *sessionRepository) FindWithCursor(ctx context.Context, f SessionFilter,
	cursor *pagination.Cursor, limit int64, forward bool) ([]models.Session, error) {

	filter := pagination.ApplyCursorFilter(buildSessionFilter(f), cursor, forward)

	var sessions []models.Session
	err := r.coll.Find(ctx, filter).
		Sort(pagination.SortFields(forward)...).
		Limit(limit).
		All(&sessions)

	if !forward && len(sessions) > 0 {
		for i, j := 0, len(sessions)-1; i < j; i, j = i+1, j-1 {
			sessions[i], sessions[j] = sessions[j], sessions[i]
		}
	}

	return sessions, err
}

func buildSessionFilter(f SessionFilter) bson.M {
	filter := bson.M{}
	if len(f.UserIDs) == 1 {
		filter["user_id"] = f.UserIDs[0]
	} else if len(f.UserIDs) > 1 {
		filter["user_id"] = bson.M{"$in": f.UserIDs}
	}
	if f.SessionIDs != nil {
		// $in with an empty list matches nothing, which is the point.
		filter["session_id"] = bson.M{"$in": f.SessionIDs}
	}
	return filter
}
