package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const recordingCollection = "desktop_recordings"

// IRecordingRepository keeps the desktops' and the terminals' recordings
// (models.Recording): who opened each and how far it came; what it
// recorded is in the desktop recordings bucket.
type IRecordingRepository interface {
	Insert(ctx context.Context, r *models.Recording) error
	FindByID(ctx context.Context, recordingID uuid.UUID) (*models.Recording, error)
	// ListByTask is a page of a task's recordings, the latest first: those
	// numbered below before (0: from the latest), at most limit.
	ListByTask(ctx context.Context, taskID uuid.UUID, before, limit int64) ([]models.Recording, error)
	// Save sets a recording's fields: IsNotFound when it is gone.
	Save(ctx context.Context, recordingID uuid.UUID, set bson.M) error
	// Stale are the recordings recording with no word from their core
	// since before.
	Stale(ctx context.Context, before time.Time) ([]models.Recording, error)
	// EndStale sets a recording's fields if it is still recording with no
	// word since before: false when it is not (its core spoke, or ended
	// it).
	EndStale(ctx context.Context, recordingID uuid.UUID, before time.Time, set bson.M) (bool, error)
	// ClaimToConvert takes the oldest desktop recording that ended and has
	// no video yet (none, or a conversion whose lease ran out), due now
	// (VideoNextAt), marking it converting until lease: nil for none.
	ClaimToConvert(ctx context.Context, now, lease time.Time) (*models.Recording, error)
	ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.Recording, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	Delete(ctx context.Context, recordingID uuid.UUID) error
}

type recordingRepository struct {
	coll database.Collection
}

func NewRecordingRepository(db database.Database) IRecordingRepository {
	db.EnsureIndexes(context.Background(), recordingCollection, []opts.IndexModel{
		{Key: []string{"recording_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"task_id", "-number"}},
		{Key: []string{"state", "seen_at"}},
		{Key: []string{"operation_id"}},
		// The next to convert: saved, by age.
		{Key: []string{"state", "started_at"}},
	})
	return &recordingRepository{coll: db.Collection(recordingCollection)}
}

func (r *recordingRepository) Insert(ctx context.Context, rec *models.Recording) error {
	_, err := r.coll.InsertOne(ctx, rec)
	return err
}

func (r *recordingRepository) FindByID(ctx context.Context, recordingID uuid.UUID) (*models.Recording, error) {
	var rec models.Recording
	if err := r.coll.Find(ctx, bson.M{"recording_id": recordingID}).One(&rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *recordingRepository) ListByTask(ctx context.Context, taskID uuid.UUID, before, limit int64) ([]models.Recording, error) {
	filter := bson.M{"task_id": taskID}
	if before > 0 {
		filter["number"] = bson.M{"$lt": before}
	}
	out := []models.Recording{}
	err := r.coll.Find(ctx, filter).Sort("-number").Limit(limit).All(&out)
	return out, err
}

func (r *recordingRepository) Save(ctx context.Context, recordingID uuid.UUID, set bson.M) error {
	s := bson.M{"updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	return r.coll.UpdateOne(ctx, bson.M{"recording_id": recordingID}, bson.M{"$set": s})
}

func (r *recordingRepository) Stale(ctx context.Context, before time.Time) ([]models.Recording, error) {
	var out []models.Recording
	err := r.coll.Find(ctx, bson.M{"state": models.RecordingStateRecording, "seen_at": bson.M{"$lt": before}}).All(&out)
	return out, err
}

func (r *recordingRepository) EndStale(ctx context.Context, recordingID uuid.UUID, before time.Time, set bson.M) (bool, error) {
	s := bson.M{"updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	err := r.coll.UpdateOne(ctx, bson.M{"recording_id": recordingID, "state": models.RecordingStateRecording,
		"seen_at": bson.M{"$lt": before}}, bson.M{"$set": s})
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *recordingRepository) ClaimToConvert(ctx context.Context, now, lease time.Time) (*models.Recording, error) {
	var rec models.Recording
	err := r.coll.Find(ctx, bson.M{
		"state":  models.RecordingStateSaved,
		"format": bson.M{"$ne": models.RecordingFormatAsciicast},
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"video": bson.M{"$exists": false}},
				bson.M{"video": ""},
				bson.M{"video": models.RecordingVideoConverting, "video_lease": bson.M{"$lt": now}},
			}},
			bson.M{"$or": bson.A{
				bson.M{"video_next_at": bson.M{"$exists": false}},
				bson.M{"video_next_at": bson.M{"$lte": now}},
			}},
		},
	}).Sort("started_at").Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"video": models.RecordingVideoConverting, "video_lease": lease, "updateAt": now}},
		ReturnNew: true,
	}, &rec)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *recordingRepository) ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.Recording, error) {
	var out []models.Recording
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Select(bson.M{"recording_id": 1, "parts": 1}).All(&out)
	return out, err
}

func (r *recordingRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

func (r *recordingRepository) Delete(ctx context.Context, recordingID uuid.UUID) error {
	err := r.coll.Remove(ctx, bson.M{"recording_id": recordingID})
	if IsNotFound(err) {
		return nil
	}
	return err
}
