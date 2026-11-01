package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	mopts "go.mongodb.org/mongo-driver/mongo/options"
)

const fsListingCollection = "fs_listings"

// FsListingTTL is how long core keeps where a task's listing stands: its
// pieces come while the task runs, late ones (resent) within the day.
const FsListingTTL = int32(7 * 24 * 60 * 60)

// IFsListingRepository keeps where each task's listings stand as their
// pieces come (models.FsListing), so a listing's pieces share its time and
// what a whole one no longer has is marked gone once all are applied.
type IFsListingRepository interface {
	// Piece takes a piece's word on its listing (the folder, from the
	// first; the last; partial, err), the listing begun at now when it is
	// new; and returns where it stands.
	Piece(ctx context.Context, taskID uuid.UUID, p protocol.ListingPiece, now time.Time) (*models.FsListing, error)
	// Stash keeps a piece that came before its listing's first, until
	// that one names the folder.
	Stash(ctx context.Context, taskID uuid.UUID, p protocol.ListingPiece) error
	// TakePending returns the pieces stashed, and forgets them.
	TakePending(ctx context.Context, taskID uuid.UUID, id uint64) ([]models.FsPendingPiece, error)
	// Applied records pieces in the snapshot, and returns where the
	// listing stands.
	Applied(ctx context.Context, taskID uuid.UUID, id uint64, parts []uint64) (*models.FsListing, error)
	// ClaimWhole: true for one caller only, who marks what the listing no
	// longer has gone.
	ClaimWhole(ctx context.Context, taskID uuid.UUID, id uint64) (bool, error)
}

type fsListingRepository struct {
	coll database.Collection
}

func NewFsListingRepository(db database.Database) IFsListingRepository {
	db.EnsureIndexes(context.Background(), fsListingCollection, []opts.IndexModel{
		{Key: []string{"task_id", "id"}, IndexOptions: new(mopts.IndexOptions).SetUnique(true)},
		{Key: []string{"created_at"}, IndexOptions: new(mopts.IndexOptions).SetExpireAfterSeconds(FsListingTTL)},
	})
	return &fsListingRepository{coll: db.Collection(fsListingCollection)}
}

func listingKey(taskID uuid.UUID, id uint64) bson.M { return bson.M{"task_id": taskID, "id": id} }

// update applies an update to a listing, and returns it as left.
func (r *fsListingRepository) update(ctx context.Context, filter, update bson.M, upsert bool) (*models.FsListing, error) {
	c, err := r.coll.RawCollection()
	if err != nil {
		return nil, err
	}
	o := mopts.FindOneAndUpdate().SetReturnDocument(mopts.After).SetUpsert(upsert)
	var l models.FsListing
	if err := c.FindOneAndUpdate(ctx, filter, update, o).Decode(&l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *fsListingRepository) Piece(ctx context.Context, taskID uuid.UUID, p protocol.ListingPiece, now time.Time) (*models.FsListing, error) {
	set := bson.M{}
	if p.Part == 0 {
		set["path"], set["sep"], set["parent"] = p.Path, p.Sep, p.Parent
		if p.Err != "" {
			set["err"] = p.Err
		}
	}
	if p.Last {
		set["last"] = p.Part
	}
	if p.Partial {
		set["partial"] = true
	}
	update := bson.M{"$setOnInsert": bson.M{"at": now, "created_at": now}}
	if len(set) > 0 {
		update["$set"] = set
	}
	l, err := r.update(ctx, listingKey(taskID, p.ID), update, true)
	if mongo.IsDuplicateKeyError(err) {
		// Two first pieces at once: the other made it; once more.
		return r.update(ctx, listingKey(taskID, p.ID), update, true)
	}
	return l, err
}

func (r *fsListingRepository) Stash(ctx context.Context, taskID uuid.UUID, p protocol.ListingPiece) error {
	_, err := r.update(ctx, listingKey(taskID, p.ID),
		bson.M{"$push": bson.M{"pending": models.FsPendingPiece{Part: p.Part, Entries: p.Entries}}}, false)
	return err
}

func (r *fsListingRepository) TakePending(ctx context.Context, taskID uuid.UUID, id uint64) ([]models.FsPendingPiece, error) {
	c, err := r.coll.RawCollection()
	if err != nil {
		return nil, err
	}
	var l models.FsListing
	err = c.FindOneAndUpdate(ctx, bson.M{"task_id": taskID, "id": id, "pending.0": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"pending": ""}}, mopts.FindOneAndUpdate().SetReturnDocument(mopts.Before)).Decode(&l)
	if IsNotFound(err) {
		return nil, nil
	}
	return l.Pending, err
}

func (r *fsListingRepository) Applied(ctx context.Context, taskID uuid.UUID, id uint64, parts []uint64) (*models.FsListing, error) {
	return r.update(ctx, listingKey(taskID, id), bson.M{"$addToSet": bson.M{"applied": bson.M{"$each": parts}}}, false)
}

func (r *fsListingRepository) ClaimWhole(ctx context.Context, taskID uuid.UUID, id uint64) (bool, error) {
	c, err := r.coll.RawCollection()
	if err != nil {
		return false, err
	}
	res, err := c.UpdateOne(ctx, bson.M{"task_id": taskID, "id": id, "whole": bson.M{"$ne": true}}, bson.M{"$set": bson.M{"whole": true}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}
