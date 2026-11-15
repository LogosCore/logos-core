package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const desktopRecordingCollection = "desktop_recordings"

// IDesktopRecordingRepository keeps the desktops' recordings
// (models.DesktopRecording): who opened each and how far it came; what it
// recorded is in the desktop recordings bucket.
type IDesktopRecordingRepository interface {
	Insert(ctx context.Context, r *models.DesktopRecording) error
	FindByID(ctx context.Context, recordingID uuid.UUID) (*models.DesktopRecording, error)
	// ListByTask is a page of a task's recordings, the latest first: those
	// numbered below before (0: from the latest), at most limit.
	ListByTask(ctx context.Context, taskID uuid.UUID, before, limit int64) ([]models.DesktopRecording, error)
	// Save sets a recording's fields: IsNotFound when it is gone.
	Save(ctx context.Context, recordingID uuid.UUID, set bson.M) error
	// Stale are the recordings recording with no word from their core
	// since before.
	Stale(ctx context.Context, before time.Time) ([]models.DesktopRecording, error)
	// EndStale sets a recording's fields if it is still recording with no
	// word since before: false when it is not (its core spoke, or ended
	// it).
	EndStale(ctx context.Context, recordingID uuid.UUID, before time.Time, set bson.M) (bool, error)
	ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.DesktopRecording, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	Delete(ctx context.Context, recordingID uuid.UUID) error
}

type desktopRecordingRepository struct {
	coll database.Collection
}

func NewDesktopRecordingRepository(db database.Database) IDesktopRecordingRepository {
	db.EnsureIndexes(context.Background(), desktopRecordingCollection, []opts.IndexModel{
		{Key: []string{"recording_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"task_id", "-number"}},
		{Key: []string{"state", "seen_at"}},
		{Key: []string{"operation_id"}},
	})
	return &desktopRecordingRepository{coll: db.Collection(desktopRecordingCollection)}
}

func (r *desktopRecordingRepository) Insert(ctx context.Context, rec *models.DesktopRecording) error {
	_, err := r.coll.InsertOne(ctx, rec)
	return err
}

func (r *desktopRecordingRepository) FindByID(ctx context.Context, recordingID uuid.UUID) (*models.DesktopRecording, error) {
	var rec models.DesktopRecording
	if err := r.coll.Find(ctx, bson.M{"recording_id": recordingID}).One(&rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *desktopRecordingRepository) ListByTask(ctx context.Context, taskID uuid.UUID, before, limit int64) ([]models.DesktopRecording, error) {
	filter := bson.M{"task_id": taskID}
	if before > 0 {
		filter["number"] = bson.M{"$lt": before}
	}
	out := []models.DesktopRecording{}
	err := r.coll.Find(ctx, filter).Sort("-number").Limit(limit).All(&out)
	return out, err
}

func (r *desktopRecordingRepository) Save(ctx context.Context, recordingID uuid.UUID, set bson.M) error {
	s := bson.M{"updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	return r.coll.UpdateOne(ctx, bson.M{"recording_id": recordingID}, bson.M{"$set": s})
}

func (r *desktopRecordingRepository) Stale(ctx context.Context, before time.Time) ([]models.DesktopRecording, error) {
	var out []models.DesktopRecording
	err := r.coll.Find(ctx, bson.M{"state": models.DesktopRecordingRecording, "seen_at": bson.M{"$lt": before}}).All(&out)
	return out, err
}

func (r *desktopRecordingRepository) EndStale(ctx context.Context, recordingID uuid.UUID, before time.Time, set bson.M) (bool, error) {
	s := bson.M{"updateAt": time.Now().UTC()}
	for k, v := range set {
		s[k] = v
	}
	err := r.coll.UpdateOne(ctx, bson.M{"recording_id": recordingID, "state": models.DesktopRecordingRecording,
		"seen_at": bson.M{"$lt": before}}, bson.M{"$set": s})
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *desktopRecordingRepository) ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.DesktopRecording, error) {
	var out []models.DesktopRecording
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Select(bson.M{"recording_id": 1, "parts": 1}).All(&out)
	return out, err
}

func (r *desktopRecordingRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

func (r *desktopRecordingRepository) Delete(ctx context.Context, recordingID uuid.UUID) error {
	err := r.coll.Remove(ctx, bson.M{"recording_id": recordingID})
	if IsNotFound(err) {
		return nil
	}
	return err
}
