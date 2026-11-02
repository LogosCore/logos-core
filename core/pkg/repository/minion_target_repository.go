package repository

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Targets of bind minions: see models/minion_target.go.

const minionTargetCollection = "minion_targets"

// MinionTargetFilter narrows an operation's targets list. Search is the
// app's search (searchPattern) over the name, the channel and the state.
type MinionTargetFilter struct {
	Search    string
	BuildID   *uuid.UUID
	SessionID *uuid.UUID
	States    []string
}

// minionTargetListSort orders the targets list: newest first.
var minionTargetListSort = pagination.SortKey{Field: "created_at", Ascending: false}

// MinionTargetCursor is a target's position in the targets list.
func MinionTargetCursor(t *models.MinionTarget) string {
	return pagination.EncodeCursor(t.CreatedAt, t.Id)
}

// IMinionTargetRepository keeps targets (models.MinionTarget).
type IMinionTargetRepository interface {
	Insert(ctx context.Context, t *models.MinionTarget) error
	// FindByTargetID returns one target; IsNotFound on a miss.
	FindByTargetID(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error)
	// FindBySessionID returns the target linked to a session; IsNotFound
	// for a session no target opened.
	FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionTarget, error)
	// FindByOperationIDWithCursor returns a page of an operation's targets,
	// newest first.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, filter MinionTargetFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionTarget, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, filter MinionTargetFilter) (int64, error)
	// SetValues replaces what the operator gave: the values, the name and
	// retries.
	SetValues(ctx context.Context, targetID uuid.UUID, values models.JSONMap, name string, retries bool, at time.Time) error
	// SetCallState writes what calls left: the state and its detail, the
	// session, the allowance, the failures in a row, when it was unreachable
	// since, when core calls next, the last call and the last good one, and
	// the calls made.
	SetCallState(ctx context.Context, t *models.MinionTarget) error
	// ClaimDue takes one target whose next call is due at now, clearing its
	// next call, so that one caller makes it; IsNotFound when none is due.
	ClaimDue(ctx context.Context, now time.Time) (*models.MinionTarget, error)
	// Delete removes one target; false when there was none.
	Delete(ctx context.Context, targetID uuid.UUID) (bool, error)
	// DeleteByOperationID removes an operation's targets (operation delete
	// cascade).
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionTargetRepository struct{ coll database.Collection }

func NewMinionTargetRepository(db database.Database) IMinionTargetRepository {
	db.EnsureIndexes(context.Background(), minionTargetCollection, []opts.IndexModel{
		uniqueIDIndex("target_id"),
		{Key: []string{"operation_id", "-created_at", "-_id"}},
		{Key: []string{"build_id"}},
		{Key: []string{"session_id"}, IndexOptions: new(options.IndexOptions).
			SetPartialFilterExpression(bson.M{"session_id": bson.M{"$exists": true}})},
		{Key: []string{"next_call_at"}, IndexOptions: new(options.IndexOptions).
			SetPartialFilterExpression(bson.M{"next_call_at": bson.M{"$exists": true}})},
	})
	return &minionTargetRepository{coll: db.Collection(minionTargetCollection)}
}

func (r *minionTargetRepository) Insert(ctx context.Context, t *models.MinionTarget) error {
	if t.TargetID == uuid.Nil {
		t.TargetID = uuid.New()
	}
	_, err := r.coll.InsertOne(ctx, t)
	return err
}

func (r *minionTargetRepository) FindByTargetID(ctx context.Context, targetID uuid.UUID) (*models.MinionTarget, error) {
	var t models.MinionTarget
	if err := r.coll.Find(ctx, bson.M{"target_id": targetID}).One(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *minionTargetRepository) FindBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.MinionTarget, error) {
	var t models.MinionTarget
	if err := r.coll.Find(ctx, bson.M{"session_id": sessionID}).Sort("-created_at").One(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func minionTargetListFilter(operationID uuid.UUID, f MinionTargetFilter) bson.M {
	q := bson.M{"operation_id": operationID}
	if f.BuildID != nil {
		q["build_id"] = *f.BuildID
	}
	if f.SessionID != nil {
		q["session_id"] = *f.SessionID
	}
	if len(f.States) > 0 {
		q["state"] = bson.M{"$in": f.States}
	}
	if f.Search != "" {
		rx := bson.M{"$regex": searchPattern(f.Search), "$options": "i"}
		q["$or"] = bson.A{bson.M{"name": rx}, bson.M{"module_name": rx}, bson.M{"state": rx}, bson.M{"state_detail": rx}}
	}
	return q
}

func (r *minionTargetRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, filter MinionTargetFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.MinionTarget, error) {
	if err := minionTargetListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.MinionTarget{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(minionTargetListFilter(operationID, filter), cursor, forward, minionTargetListSort)).
		Sort(pagination.SortFieldsKey(forward, minionTargetListSort)...).
		Limit(limit).
		All(&out)
	if err != nil {
		return nil, err
	}
	if !forward {
		slices.Reverse(out)
	}
	return out, nil
}

func (r *minionTargetRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, filter MinionTargetFilter) (int64, error) {
	return r.coll.Count(ctx, minionTargetListFilter(operationID, filter))
}

func (r *minionTargetRepository) SetValues(ctx context.Context, targetID uuid.UUID, values models.JSONMap, name string, retries bool, at time.Time) error {
	return r.coll.UpdateOne(ctx, bson.M{"target_id": targetID}, bson.M{"$set": bson.M{
		"values": values, "name": name, "retries": retries, "updated_at": at, "updateAt": at,
	}})
}

func (r *minionTargetRepository) SetCallState(ctx context.Context, t *models.MinionTarget) error {
	set := bson.M{"state": t.State, "state_at": t.StateAt, "failures": t.Failures, "calls": t.Calls, "updated_at": t.UpdatedAt}
	unset := bson.M{}
	opt := func(key string, v any, present bool) {
		if present {
			set[key] = v
		} else {
			unset[key] = ""
		}
	}
	opt("state_detail", t.StateDetail, t.StateDetail != "")
	opt("session_id", t.SessionID, t.SessionID != nil)
	opt("allowance", t.Allowance, t.Allowance != nil)
	opt("unreachable_since", t.UnreachableSince, t.UnreachableSince != nil)
	opt("next_call_at", t.NextCallAt, t.NextCallAt != nil)
	opt("last_call_at", t.LastCallAt, t.LastCallAt != nil)
	opt("last_ok_at", t.LastOKAt, t.LastOKAt != nil)
	u := bson.M{"$set": set}
	if len(unset) > 0 {
		u["$unset"] = unset
	}
	return r.coll.UpdateOne(ctx, bson.M{"target_id": t.TargetID}, u)
}

func (r *minionTargetRepository) ClaimDue(ctx context.Context, now time.Time) (*models.MinionTarget, error) {
	var t models.MinionTarget
	err := r.coll.Find(ctx, bson.M{"next_call_at": bson.M{"$lte": now}}).Sort("next_call_at").Apply(qmgo.Change{
		Update: bson.M{"$unset": bson.M{"next_call_at": ""}}, ReturnNew: false,
	}, &t)
	if err != nil {
		return nil, err
	}
	t.NextCallAt = nil
	return &t, nil
}

func (r *minionTargetRepository) Delete(ctx context.Context, targetID uuid.UUID) (bool, error) {
	res, err := r.coll.RemoveAll(ctx, bson.M{"target_id": targetID})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (r *minionTargetRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
