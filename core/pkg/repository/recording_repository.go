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
	// SaveKept is Save on a recording that keeps its files: IsNotFound
	// when it is gone, or a retention cleanup took it.
	SaveKept(ctx context.Context, recordingID uuid.UUID, set bson.M) error
	// Stale are the recordings recording with no word from their core
	// since before.
	Stale(ctx context.Context, before time.Time) ([]models.Recording, error)
	// EndStale sets a recording's fields if it is still recording with no
	// word since before: false when it is not (its core spoke, or ended
	// it).
	EndStale(ctx context.Context, recordingID uuid.UUID, before time.Time, set bson.M) (bool, error)
	// ClaimToConvert takes the oldest desktop recording that ended and has
	// no video yet (none, or a conversion whose lease ran out), due now
	// (VideoNextAt), and keeps its files, marking it converting until
	// lease: nil for none.
	ClaimToConvert(ctx context.Context, now, lease time.Time) (*models.Recording, error)
	// ToClean are the recordings a retention cleanup takes (docs: Storage,
	// Retention cleanups): saved before before, holding something, their
	// files kept (no cleanup took them), not converting at now (a lease
	// running), and their task not marked important; of one operation (nil:
	// of every one), the oldest first, at most limit.
	ToClean(ctx context.Context, before, now time.Time, operationID *uuid.UUID, limit int64) ([]models.Recording, error)
	// ToCleanByOperation sums ToClean's, of every operation, by operation.
	ToCleanByOperation(ctx context.Context, before, now time.Time) ([]models.RecordingsToClean, error)
	// MarkCleaned marks a recording taken by cleanup at at, if a cleanup
	// still takes it (saved, its files kept, not converting at at): the
	// recording as it is then, nil when a cleanup does not take it.
	MarkCleaned(ctx context.Context, recordingID, cleanup uuid.UUID, at time.Time) (*models.Recording, error)
	// UnmarkCleaned takes the mark off again: its files were not all
	// deleted.
	UnmarkCleaned(ctx context.Context, recordingID uuid.UUID) error
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
		// A retention cleanup's: saved, by when they ended.
		{Key: []string{"state", "ended_at"}},
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

func (r *recordingRepository) SaveKept(ctx context.Context, recordingID uuid.UUID, set bson.M) error {
	s := bson.M{"updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	return r.coll.UpdateOne(ctx, bson.M{"recording_id": recordingID, "cleaned_at": bson.M{"$exists": false}}, bson.M{"$set": s})
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
		"state":      models.RecordingStateSaved,
		"format":     bson.M{"$ne": models.RecordingFormatAsciicast},
		"cleaned_at": bson.M{"$exists": false},
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

// recordingBytes is a recording's bytes in its bucket, as an aggregation
// says it (models.Recording.StoredBytes): a converted one's video, else its
// parts.
var recordingBytes = bson.M{"$cond": bson.A{bson.M{"$gt": bson.A{"$video_size", 0}}, "$video_size", "$size"}}

// toCleanMatch is what a cleanup of those saved before before takes, as
// the recording says (ToClean): its task is asked after it.
func toCleanMatch(before, now time.Time) bson.M {
	return bson.M{
		"state":      models.RecordingStateSaved,
		"ended_at":   bson.M{"$lte": before},
		"cleaned_at": bson.M{"$exists": false},
		"$and": bson.A{
			bson.M{"$or": bson.A{bson.M{"size": bson.M{"$gt": 0}}, bson.M{"video_size": bson.M{"$gt": 0}}}},
			notConverting(now),
		},
	}
}

// notConverting matches a recording no core converts at now: none has
// it, or its lease ran out.
func notConverting(now time.Time) bson.M {
	return bson.M{"$or": bson.A{
		bson.M{"video": bson.M{"$ne": models.RecordingVideoConverting}},
		bson.M{"video_lease": bson.M{"$lt": now}},
	}}
}

// notImportant are the aggregation's stages that keep the recordings whose
// task is not marked important (a task gone is not).
var notImportant = bson.A{
	bson.M{"$lookup": bson.M{"from": minionTaskCollection, "localField": "task_id", "foreignField": "task_id", "as": "important",
		"pipeline": bson.A{bson.M{"$match": bson.M{"important_at": bson.M{"$exists": true}}}, bson.M{"$project": bson.M{"_id": 1}}}}},
	bson.M{"$match": bson.M{"important": bson.M{"$size": 0}}},
}

func (r *recordingRepository) ToClean(ctx context.Context, before, now time.Time, operationID *uuid.UUID, limit int64) ([]models.Recording, error) {
	match := toCleanMatch(before, now)
	if operationID != nil {
		match["operation_id"] = *operationID
	}
	pipeline := bson.A{bson.M{"$match": match}, bson.M{"$sort": bson.D{{Key: "ended_at", Value: 1}}}}
	pipeline = append(pipeline, notImportant...)
	pipeline = append(pipeline, bson.M{"$limit": limit}, bson.M{"$project": bson.M{"important": 0}})
	out := []models.Recording{}
	err := r.coll.Aggregate(ctx, pipeline).All(&out)
	return out, err
}

func (r *recordingRepository) ToCleanByOperation(ctx context.Context, before, now time.Time) ([]models.RecordingsToClean, error) {
	pipeline := bson.A{bson.M{"$match": toCleanMatch(before, now)}}
	pipeline = append(pipeline, notImportant...)
	pipeline = append(pipeline, bson.M{"$group": bson.M{"_id": "$operation_id", "recordings": bson.M{"$sum": 1},
		"bytes": bson.M{"$sum": bson.M{"$ifNull": bson.A{recordingBytes, 0}}}}})
	out := []models.RecordingsToClean{}
	err := r.coll.Aggregate(ctx, pipeline).All(&out)
	return out, err
}

func (r *recordingRepository) MarkCleaned(ctx context.Context, recordingID, cleanup uuid.UUID, at time.Time) (*models.Recording, error) {
	var rec models.Recording
	err := r.coll.Find(ctx, bson.M{"recording_id": recordingID, "state": models.RecordingStateSaved,
		"cleaned_at": bson.M{"$exists": false}, "$and": bson.A{notConverting(at)}}).Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"cleaned_at": at, "cleaned_by": cleanup, "updateAt": at}},
		ReturnNew: true,
	}, &rec)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *recordingRepository) UnmarkCleaned(ctx context.Context, recordingID uuid.UUID) error {
	err := r.coll.UpdateOne(ctx, bson.M{"recording_id": recordingID},
		bson.M{"$unset": bson.M{"cleaned_at": "", "cleaned_by": ""}, "$set": bson.M{"updateAt": time.Now().UTC()}})
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (r *recordingRepository) ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.Recording, error) {
	var out []models.Recording
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Select(bson.M{"recording_id": 1, "parts": 1, "cleaned_at": 1}).All(&out)
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
