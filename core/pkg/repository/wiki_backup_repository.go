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

const wikiDocumentBackupCollection = "wiki_document_backups"

// IWikiDocumentBackupRepository defines the interface for WikiDocumentBackup database operations.
type IWikiDocumentBackupRepository interface {
	Create(ctx context.Context, backup *models.WikiDocumentBackup) error
	FindByID(ctx context.Context, id uuid.UUID) (models.WikiDocumentBackup, error)
	FindByDocumentIDWithCursor(ctx context.Context, docID uuid.UUID, trigger *models.WikiDocumentBackupTrigger, cursor *pagination.Cursor, limit int64, forward bool) ([]models.WikiDocumentBackup, error)
	CountByDocumentID(ctx context.Context, docID uuid.UUID, trigger *models.WikiDocumentBackupTrigger) (int64, error)
	FindLatestByDocumentID(ctx context.Context, docID uuid.UUID) (*models.WikiDocumentBackup, error)
	Delete(ctx context.Context, backup *models.WikiDocumentBackup) error
	DeleteByDocumentID(ctx context.Context, docID uuid.UUID) error
	// DeleteByDocumentIDs is DeleteByDocumentID for many documents in one
	// round trip.
	DeleteByDocumentIDs(ctx context.Context, docIDs []uuid.UUID) error
	DeleteByOperationID(ctx context.Context, opID uuid.UUID) error

	// FilterReferencedImageIDs / FilterReferencedFileIDs return the subset of
	// ids some backup in the operation references — the backup half of the
	// attachment sweepers' liveness check.
	FilterReferencedImageIDs(ctx context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error)
	FilterReferencedFileIDs(ctx context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error)
	// HasUnindexed reports whether any backup in the operation predates
	// reference indexing, in which case nothing it might reference can be
	// ruled out.
	HasUnindexed(ctx context.Context, opID uuid.UUID) (bool, error)
	// FindUnindexed returns up to limit backups still to be indexed.
	FindUnindexed(ctx context.Context, limit int64) ([]models.WikiDocumentBackup, error)
	// SetReferences indexes one backup.
	SetReferences(ctx context.Context, backupID uuid.UUID, images, files []uuid.UUID) error
}

type wikiDocumentBackupRepository struct {
	coll database.Collection
}

func NewWikiDocumentBackupRepository(db database.Database) IWikiDocumentBackupRepository {
	coll := db.Collection(wikiDocumentBackupCollection)

	db.EnsureIndexes(context.Background(), wikiDocumentBackupCollection, []opts.IndexModel{
		{Key: []string{"backup_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"document_id", "-createAt"}},
		{Key: []string{"operation_id"}},
		// The sweepers' liveness lookups, and the "anything not yet indexed
		// here?" check that gates them — the same shapes as on documents.
		{Key: []string{"operation_id", "image_references"}},
		{Key: []string{"operation_id", "file_references"}},
		{Key: []string{"operation_id", "references_indexed"}},
	})

	return &wikiDocumentBackupRepository{coll: coll}
}

func (r *wikiDocumentBackupRepository) Create(ctx context.Context, backup *models.WikiDocumentBackup) error {
	_, err := r.coll.InsertOne(ctx, backup)
	return err
}

func (r *wikiDocumentBackupRepository) FindByID(ctx context.Context, id uuid.UUID) (models.WikiDocumentBackup, error) {
	var backup models.WikiDocumentBackup
	err := r.coll.FindOne(ctx, bson.M{"backup_id": id}).One(&backup)
	return backup, err
}

func (r *wikiDocumentBackupRepository) FindByDocumentIDWithCursor(ctx context.Context, docID uuid.UUID, trigger *models.WikiDocumentBackupTrigger, cursor *pagination.Cursor, limit int64, forward bool) ([]models.WikiDocumentBackup, error) {
	filter := bson.M{"document_id": docID}
	if trigger != nil {
		filter["trigger"] = *trigger
	}

	filter = pagination.ApplyCursorFilter(filter, cursor, forward)

	var backups []models.WikiDocumentBackup
	err := r.coll.Find(ctx, filter).
		Sort(pagination.SortFields(forward)...).
		Limit(limit).
		All(&backups)

	if !forward && len(backups) > 0 {
		for i, j := 0, len(backups)-1; i < j; i, j = i+1, j-1 {
			backups[i], backups[j] = backups[j], backups[i]
		}
	}

	return backups, err
}

func (r *wikiDocumentBackupRepository) CountByDocumentID(ctx context.Context, docID uuid.UUID, trigger *models.WikiDocumentBackupTrigger) (int64, error) {
	filter := bson.M{"document_id": docID}
	if trigger != nil {
		filter["trigger"] = *trigger
	}
	return r.coll.Count(ctx, filter)
}

func (r *wikiDocumentBackupRepository) FindLatestByDocumentID(ctx context.Context, docID uuid.UUID) (*models.WikiDocumentBackup, error) {
	var backup models.WikiDocumentBackup
	err := r.coll.FindOne(ctx, bson.M{"document_id": docID}).Sort("-createAt").One(&backup)
	if err != nil {
		return nil, err
	}
	return &backup, nil
}

func (r *wikiDocumentBackupRepository) Delete(ctx context.Context, backup *models.WikiDocumentBackup) error {
	return r.coll.Remove(ctx, bson.M{"backup_id": backup.BackupID})
}

func (r *wikiDocumentBackupRepository) DeleteByDocumentID(ctx context.Context, docID uuid.UUID) error {
	return r.DeleteByDocumentIDs(ctx, []uuid.UUID{docID})
}

func (r *wikiDocumentBackupRepository) DeleteByDocumentIDs(ctx context.Context, docIDs []uuid.UUID) error {
	if len(docIDs) == 0 {
		return nil
	}
	_, err := r.coll.RemoveAll(ctx, bson.M{"document_id": bson.M{"$in": docIDs}})
	return err
}

func (r *wikiDocumentBackupRepository) DeleteByOperationID(ctx context.Context, opID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": opID})
	return err
}

func (r *wikiDocumentBackupRepository) FilterReferencedImageIDs(ctx context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	return filterReferencedIDs(ctx, r.coll, opID, "image_references", ids)
}

func (r *wikiDocumentBackupRepository) FilterReferencedFileIDs(ctx context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	return filterReferencedIDs(ctx, r.coll, opID, "file_references", ids)
}

// unindexedFilter matches backups written before reference indexing. $ne
// rather than false: on those rows the field is absent, not false.
func unindexedFilter() bson.M {
	return bson.M{"references_indexed": bson.M{"$ne": true}}
}

func (r *wikiDocumentBackupRepository) HasUnindexed(ctx context.Context, opID uuid.UUID) (bool, error) {
	f := unindexedFilter()
	f["operation_id"] = opID
	n, err := r.coll.Find(ctx, f).Limit(1).Count()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *wikiDocumentBackupRepository) FindUnindexed(ctx context.Context, limit int64) ([]models.WikiDocumentBackup, error) {
	var out []models.WikiDocumentBackup
	err := r.coll.Find(ctx, unindexedFilter()).Limit(limit).All(&out)
	return out, err
}

func (r *wikiDocumentBackupRepository) SetReferences(ctx context.Context, backupID uuid.UUID, images, files []uuid.UUID) error {
	// Written as arrays even when empty, so an indexed backup that references
	// nothing reads back the same way as one indexed by Create.
	if images == nil {
		images = []uuid.UUID{}
	}
	if files == nil {
		files = []uuid.UUID{}
	}
	return r.coll.UpdateOne(ctx, bson.M{"backup_id": backupID}, bson.M{"$set": bson.M{
		"image_references":   images,
		"file_references":    files,
		"references_indexed": true,
	}})
}
