package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// attachmentRepository is the storage wiki images and wiki files share: one
// collection of attachment metadata per kind, keyed by that kind's id field,
// with the same lookups, the same sweeper query and the same indexes.
// IWikiImageRepository and IWikiFileRepository are both served by it.
type attachmentRepository[T any] struct {
	coll    database.Collection
	idField string
}

func newAttachmentRepository[T any](db database.Database, collection, idField string) *attachmentRepository[T] {
	db.EnsureIndexes(context.Background(), collection, []opts.IndexModel{
		{Key: []string{idField}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"document_id"}},
		{Key: []string{"operation_id"}},
		{Key: []string{"createAt"}},
	})
	return &attachmentRepository[T]{coll: db.Collection(collection), idField: idField}
}

func (r *attachmentRepository[T]) Create(ctx context.Context, attachment *T) error {
	_, err := r.coll.InsertOne(ctx, attachment)
	return err
}

func (r *attachmentRepository[T]) FindByID(ctx context.Context, id uuid.UUID) (T, error) {
	var attachment T
	err := r.coll.FindOne(ctx, bson.M{r.idField: id}).One(&attachment)
	return attachment, err
}

func (r *attachmentRepository[T]) FindByDocumentID(ctx context.Context, docID uuid.UUID) ([]T, error) {
	var attachments []T
	err := r.coll.Find(ctx, bson.M{"document_id": docID}).All(&attachments)
	return attachments, err
}

func (r *attachmentRepository[T]) FindCandidatesOlderThan(ctx context.Context, cutoff time.Time, limit int64) ([]T, error) {
	var attachments []T
	err := r.coll.Find(ctx, bson.M{
		"createAt": bson.M{"$lt": cutoff},
	}).Limit(limit).All(&attachments)
	return attachments, err
}

func (r *attachmentRepository[T]) HardDelete(ctx context.Context, id uuid.UUID) error {
	return r.coll.Remove(ctx, bson.M{r.idField: id})
}
