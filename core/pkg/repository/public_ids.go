package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Public ids. Every record the app names has a UUIDv4 under <entity>_id,
// stored as binary; Mongo's _id never leaves this package (it is the keyset
// tiebreaker). Channel endpoints, module config changes, wiki visits and API
// keys got theirs after rows existed. Those rows get one at startup
// (backfillIDs), and a row an older core writes while it runs next to this
// one during a rolling upgrade gets one when it is read (fillIDs). The unique
// index on the id covers only rows that have one, so the older core's inserts
// are not refused meanwhile.

// uniqueIDIndex is the unique index of a public id field. It is partial: rows
// still without an id do not collide.
func uniqueIDIndex(field string) opts.IndexModel {
	return opts.IndexModel{
		Key: []string{field},
		IndexOptions: new(options.IndexOptions).SetUnique(true).
			SetPartialFilterExpression(bson.M{field: bson.M{"$exists": true}}),
	}
}

// backfillIDs gives every row of coll without a field one, and returns how
// many rows it gave one.
func backfillIDs(ctx context.Context, coll database.Collection, field string) (int64, error) {
	var rows []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := coll.Find(ctx, bson.M{field: bson.M{"$exists": false}}).Select(bson.M{"_id": 1}).All(&rows); err != nil {
		return 0, fmt.Errorf("find rows without %s: %w", field, err)
	}
	var n int64
	for _, row := range rows {
		ok, err := giveID(ctx, coll, row.ID, field, uuid.New())
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// fillIDs gives the rows that were read without a public id one. id returns
// a row's _id and where its public id goes.
func fillIDs[T any](ctx context.Context, coll database.Collection, field string, rows []T, id func(*T) (primitive.ObjectID, *uuid.UUID)) error {
	for i := range rows {
		oid, dst := id(&rows[i])
		if *dst != uuid.Nil {
			continue
		}
		got, err := fillID(ctx, coll, oid, field)
		if err != nil {
			return err
		}
		*dst = got
	}
	return nil
}

// fillID returns the public id of the row with _id oid, giving it one when it
// has none. One another instance gave it meanwhile is kept.
func fillID(ctx context.Context, coll database.Collection, oid primitive.ObjectID, field string) (uuid.UUID, error) {
	id := uuid.New()
	ok, err := giveID(ctx, coll, oid, field, id)
	if err != nil || ok {
		return id, err
	}
	var row bson.Raw
	if err := coll.Find(ctx, bson.M{"_id": oid}).Select(bson.M{field: 1}).One(&row); err != nil {
		return uuid.Nil, fmt.Errorf("read %s: %w", field, err)
	}
	var got uuid.UUID
	if err := row.Lookup(field).Unmarshal(&got); err != nil {
		return uuid.Nil, fmt.Errorf("read %s: %w", field, err)
	}
	return got, nil
}

// giveID sets field to id on the row with _id oid unless it has one already.
func giveID(ctx context.Context, coll database.Collection, oid primitive.ObjectID, field string, id uuid.UUID) (bool, error) {
	ok, err := matched(coll.UpdateOne(ctx, bson.M{"_id": oid, field: bson.M{"$exists": false}}, bson.M{"$set": bson.M{field: id}}))
	if err != nil {
		return false, fmt.Errorf("give a row its %s: %w", field, err)
	}
	return ok, nil
}
