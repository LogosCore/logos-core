package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionFeedbackCollection = "minion_feedback"

// IMinionFeedbackRepository keeps minions' feedback (models.MinionFeedback),
// per session.
type IMinionFeedbackRepository interface {
	// Add keeps an entry; false when the session has one of its minion id
	// already (a message sent again), which changes nothing.
	Add(ctx context.Context, f *models.MinionFeedback) (bool, error)
	// Trim keeps a session's newest max entries.
	Trim(ctx context.Context, sessionID uuid.UUID, max int) error
	// List is a session's entries, newest first, a page at a time
	// (FeedbackQuery); next is the cursor of the next page, "" on the last.
	List(ctx context.Context, sessionID uuid.UUID, q FeedbackQuery) ([]models.MinionFeedback, string, error)
	// ForTask is the entries about a task, newest first, at most limit.
	ForTask(ctx context.Context, taskID uuid.UUID, limit int64) ([]models.MinionFeedback, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

// FeedbackQuery is how a session's feedback is asked for.
type FeedbackQuery struct {
	// Level keeps one level's ("warning", "error"); "" keeps both.
	Level string
	// Search keeps the entries whose message or code holds it, any case.
	Search string
	After  string
	First  int64
}

type minionFeedbackRepository struct {
	coll database.Collection
}

func NewMinionFeedbackRepository(db database.Database) IMinionFeedbackRepository {
	db.EnsureIndexes(context.Background(), minionFeedbackCollection, []opts.IndexModel{
		{Key: []string{"session_id", "minion_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		// A session's page, newest first; and a task's.
		{Key: []string{"session_id", "-received_at", "-feedback_id"}},
		{Key: []string{"task_id", "-received_at"}, IndexOptions: new(options.IndexOptions).SetSparse(true)},
		{Key: []string{"operation_id"}},
	})
	return &minionFeedbackRepository{coll: db.Collection(minionFeedbackCollection)}
}

func (r *minionFeedbackRepository) Add(ctx context.Context, f *models.MinionFeedback) (bool, error) {
	if _, err := r.coll.InsertOne(ctx, f); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *minionFeedbackRepository) Trim(ctx context.Context, sessionID uuid.UUID, max int) error {
	// The oldest one kept: those older go.
	var kept []models.MinionFeedback
	if err := r.coll.Find(ctx, bson.M{"session_id": sessionID}).Sort("-received_at", "-feedback_id").Skip(int64(max - 1)).Limit(1).
		Select(bson.M{"received_at": 1, "feedback_id": 1}).All(&kept); err != nil || len(kept) == 0 {
		return err
	}
	last := kept[0]
	_, err := r.coll.RemoveAll(ctx, bson.M{"session_id": sessionID, "$or": bson.A{
		bson.M{"received_at": bson.M{"$lt": last.ReceivedAt}},
		bson.M{"received_at": last.ReceivedAt, "feedback_id": bson.M{"$lt": last.FeedbackID}},
	}})
	return err
}

// feedbackCursor is where a page ends: its last entry's time and id.
type feedbackCursor struct {
	ReceivedAt time.Time `json:"t"`
	FeedbackID uuid.UUID `json:"id"`
}

func (r *minionFeedbackRepository) List(ctx context.Context, sessionID uuid.UUID, q FeedbackQuery) ([]models.MinionFeedback, string, error) {
	filter := bson.M{"session_id": sessionID}
	if q.Level != "" {
		filter["level"] = q.Level
	}
	var and bson.A
	if s := strings.TrimSpace(q.Search); s != "" {
		re := bson.M{"$regex": regexp.QuoteMeta(s), "$options": "i"}
		and = append(and, bson.M{"$or": bson.A{bson.M{"msg": re}, bson.M{"code": re}}})
	}
	if q.After != "" {
		c, err := decodeFeedbackCursor(q.After)
		if err != nil {
			return nil, "", err
		}
		and = append(and, bson.M{"$or": bson.A{
			bson.M{"received_at": bson.M{"$lt": c.ReceivedAt}},
			bson.M{"received_at": c.ReceivedAt, "feedback_id": bson.M{"$lt": c.FeedbackID}},
		}})
	}
	if len(and) > 0 {
		filter["$and"] = and
	}
	first := q.First
	if first <= 0 || first > 500 {
		first = 200
	}
	var out []models.MinionFeedback
	if err := r.coll.Find(ctx, filter).Sort("-received_at", "-feedback_id").Limit(first + 1).All(&out); err != nil {
		return nil, "", err
	}
	next := ""
	if int64(len(out)) > first {
		out = out[:first]
		last := out[len(out)-1]
		b, err := json.Marshal(feedbackCursor{ReceivedAt: last.ReceivedAt, FeedbackID: last.FeedbackID})
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, next, nil
}

func decodeFeedbackCursor(s string) (feedbackCursor, error) {
	var c feedbackCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil || c.FeedbackID == uuid.Nil {
		return c, errors.New("a bad cursor")
	}
	return c, nil
}

func (r *minionFeedbackRepository) ForTask(ctx context.Context, taskID uuid.UUID, limit int64) ([]models.MinionFeedback, error) {
	var out []models.MinionFeedback
	err := r.coll.Find(ctx, bson.M{"task_id": taskID}).Sort("-received_at").Limit(limit).All(&out)
	return out, err
}

func (r *minionFeedbackRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
