package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionIdentityCollection = "minion_session_identities"

// IMinionIdentityRepository keeps sessions' identity histories
// (models.MinionIdentityEvent).
type IMinionIdentityRepository interface {
	// Add keeps a change of the process's or the session's identity.
	Add(ctx context.Context, e *models.MinionIdentityEvent) error
	// PutTask keeps who a task ran as: one entry a task, the last said.
	PutTask(ctx context.Context, e *models.MinionIdentityEvent) error
	// DropTask forgets a task's entry (it ran as the process's own after
	// all).
	DropTask(ctx context.Context, sessionID, taskID uuid.UUID) error
	// List is a session's history, newest first, a page at a time; next is
	// the cursor of the next page, "" on the last.
	List(ctx context.Context, sessionID uuid.UUID, after string, first int64) ([]models.MinionIdentityEvent, string, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionIdentityRepository struct {
	coll database.Collection
}

func NewMinionIdentityRepository(db database.Database) IMinionIdentityRepository {
	db.EnsureIndexes(context.Background(), minionIdentityCollection, []opts.IndexModel{
		{Key: []string{"session_id", "-at", "-event_id"}},
		// One entry a task.
		{Key: []string{"session_id", "task_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true).
			SetPartialFilterExpression(bson.M{"scope": models.IdentityScopeTask})},
		{Key: []string{"operation_id"}},
	})
	return &minionIdentityRepository{coll: db.Collection(minionIdentityCollection)}
}

func (r *minionIdentityRepository) Add(ctx context.Context, e *models.MinionIdentityEvent) error {
	_, err := r.coll.InsertOne(ctx, e)
	return err
}

func (r *minionIdentityRepository) PutTask(ctx context.Context, e *models.MinionIdentityEvent) error {
	_, err := r.coll.Upsert(ctx, bson.M{"session_id": e.SessionID, "task_id": e.TaskID, "scope": models.IdentityScopeTask}, e)
	return err
}

func (r *minionIdentityRepository) DropTask(ctx context.Context, sessionID, taskID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"session_id": sessionID, "task_id": taskID, "scope": models.IdentityScopeTask})
	return err
}

// identityCursor is where a page ends: its last entry's time and id.
type identityCursor struct {
	At      time.Time `json:"t"`
	EventID uuid.UUID `json:"id"`
}

func (r *minionIdentityRepository) List(ctx context.Context, sessionID uuid.UUID, after string, first int64) ([]models.MinionIdentityEvent, string, error) {
	filter := bson.M{"session_id": sessionID}
	if after != "" {
		var c identityCursor
		b, err := base64.RawURLEncoding.DecodeString(after)
		if err == nil {
			err = json.Unmarshal(b, &c)
		}
		if err != nil || c.EventID == uuid.Nil {
			return nil, "", errors.New("a bad cursor")
		}
		filter["$or"] = bson.A{
			bson.M{"at": bson.M{"$lt": c.At}},
			bson.M{"at": c.At, "event_id": bson.M{"$lt": c.EventID}},
		}
	}
	if first <= 0 || first > 500 {
		first = 100
	}
	var out []models.MinionIdentityEvent
	if err := r.coll.Find(ctx, filter).Sort("-at", "-event_id").Limit(first + 1).All(&out); err != nil {
		return nil, "", err
	}
	next := ""
	if int64(len(out)) > first {
		out = out[:first]
		last := out[len(out)-1]
		b, err := json.Marshal(identityCursor{At: last.At, EventID: last.EventID})
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, next, nil
}

func (r *minionIdentityRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
