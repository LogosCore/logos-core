package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const minionSessionFolderCollection = "minion_session_folders"

// IMinionSessionFolderRepository keeps operations' session folders
// (models.MinionSessionFolder). Which session is in which is the session's
// (MinionSession.FolderID).
type IMinionSessionFolderRepository interface {
	Create(ctx context.Context, f *models.MinionSessionFolder) error
	FindByID(ctx context.Context, folderID uuid.UUID) (*models.MinionSessionFolder, error)
	// ListByOperation is the operation's folders in their order.
	ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.MinionSessionFolder, error)
	Count(ctx context.Context, operationID uuid.UUID) (int64, error)
	// NextOrder is the order a folder added last takes.
	NextOrder(ctx context.Context, operationID uuid.UUID) (int, error)
	Update(ctx context.Context, folderID uuid.UUID, name, icon, emoji, color string) error
	SetOrder(ctx context.Context, folderID uuid.UUID, order int) error
	Delete(ctx context.Context, folderID uuid.UUID) error
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionSessionFolderRepository struct{ coll database.Collection }

func NewMinionSessionFolderRepository(db database.Database) IMinionSessionFolderRepository {
	db.EnsureIndexes(context.Background(), minionSessionFolderCollection, []opts.IndexModel{
		{Key: []string{"folder_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "order"}},
	})
	return &minionSessionFolderRepository{coll: db.Collection(minionSessionFolderCollection)}
}

func (r *minionSessionFolderRepository) Create(ctx context.Context, f *models.MinionSessionFolder) error {
	_, err := r.coll.InsertOne(ctx, f)
	return err
}

func (r *minionSessionFolderRepository) FindByID(ctx context.Context, folderID uuid.UUID) (*models.MinionSessionFolder, error) {
	var f models.MinionSessionFolder
	if err := r.coll.Find(ctx, bson.M{"folder_id": folderID}).One(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *minionSessionFolderRepository) ListByOperation(ctx context.Context, operationID uuid.UUID) ([]models.MinionSessionFolder, error) {
	out := []models.MinionSessionFolder{}
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Sort("order", "created_at").Limit(models.MaxSessionFolders).All(&out)
	return out, err
}

func (r *minionSessionFolderRepository) Count(ctx context.Context, operationID uuid.UUID) (int64, error) {
	return r.coll.Find(ctx, bson.M{"operation_id": operationID}).Count()
}

func (r *minionSessionFolderRepository) NextOrder(ctx context.Context, operationID uuid.UUID) (int, error) {
	var last []models.MinionSessionFolder
	if err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Sort("-order").Limit(1).All(&last); err != nil {
		return 0, err
	}
	if len(last) == 0 {
		return 0, nil
	}
	return last[0].Order + 1, nil
}

func (r *minionSessionFolderRepository) Update(ctx context.Context, folderID uuid.UUID, name, icon, emoji, color string) error {
	return r.coll.UpdateOne(ctx, bson.M{"folder_id": folderID},
		bson.M{"$set": bson.M{"name": name, "icon": icon, "emoji": emoji, "color": color}})
}

func (r *minionSessionFolderRepository) SetOrder(ctx context.Context, folderID uuid.UUID, order int) error {
	return r.coll.UpdateOne(ctx, bson.M{"folder_id": folderID}, bson.M{"$set": bson.M{"order": order}})
}

func (r *minionSessionFolderRepository) Delete(ctx context.Context, folderID uuid.UUID) error {
	return r.coll.Remove(ctx, bson.M{"folder_id": folderID})
}

func (r *minionSessionFolderRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
