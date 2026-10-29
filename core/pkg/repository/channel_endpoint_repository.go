package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Channel endpoints: an operation's ready-to-use values for a channel kind's
// minion connection schema. See models/channel_endpoint.go.

// A new name rather than new indexes on the prototype's channel_endpoints
// (unique name across all endpoints): its old unique index would refuse the
// same name in two operations.
const channelEndpointCollection = "operation_channel_endpoints"

// ErrChannelEndpointSlugTaken is returned when another endpoint of the
// operation has the slug.
var ErrChannelEndpointSlugTaken = errors.New("channel endpoint slug taken")

// ErrChannelEndpointConflict is returned by Update when the endpoint's
// revision is not the expected one (someone else wrote first), or the
// endpoint is gone.
var ErrChannelEndpointConflict = errors.New("channel endpoint revision conflict")

// IChannelEndpointRepository stores channel endpoints.
type IChannelEndpointRepository interface {
	// FindByOperationIDWithCursor returns a page of an operation's
	// endpoints by name: those filter lets through, past cursor, at most
	// limit.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, filter ChannelEndpointFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.ChannelEndpoint, error)
	// CountByOperationID counts an operation's endpoints filter lets
	// through.
	CountByOperationID(ctx context.Context, operationID uuid.UUID, filter ChannelEndpointFilter) (int64, error)
	// SlugTaken reports whether an endpoint of the operation has slug.
	SlugTaken(ctx context.Context, operationID uuid.UUID, slug string) (bool, error)
	// FindByEndpointID returns one endpoint; IsNotFound on a miss.
	FindByEndpointID(ctx context.Context, endpointID uuid.UUID) (*models.ChannelEndpoint, error)
	// Insert stores a new endpoint at revision 1, giving it an endpoint id
	// when it has none.
	Insert(ctx context.Context, ep *models.ChannelEndpoint) error
	// Update replaces name, slug, description, schema version and values if the
	// stored revision equals ep.Revision, and increments it (ep.Revision is
	// updated).
	Update(ctx context.Context, ep *models.ChannelEndpoint) error
	// Delete removes one endpoint; false when there was none.
	Delete(ctx context.Context, endpointID uuid.UUID) (bool, error)
	// DeleteByOperationID removes every endpoint of an operation (operation
	// delete cascade).
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	// BackfillEndpointIDs gives the endpoints created before they had ids
	// one.
	BackfillEndpointIDs(ctx context.Context) (int64, error)
	// BackfillSearchText gives the endpoints stored without a search text
	// one.
	BackfillSearchText(ctx context.Context) (int64, error)
	// BackfillSlugs gives the endpoints stored before slugs their name as
	// slug: names were slugs then, unique in their operation.
	BackfillSlugs(ctx context.Context) (int64, error)
}

type channelEndpointRepository struct {
	coll database.Collection
}

// legacyNameIndex is the unique index names had while they were slugs: names
// are free text now, so it goes.
const legacyNameIndex = "operation_id_1_name_1"

func NewChannelEndpointRepository(db database.Database) IChannelEndpointRepository {
	coll := db.Collection(channelEndpointCollection)
	if raw, err := coll.RawCollection(); err == nil {
		// Absent on a fresh database or once dropped: nothing to do.
		_, _ = raw.Indexes().DropOne(context.Background(), legacyNameIndex)
	}
	db.EnsureIndexes(context.Background(), channelEndpointCollection, []opts.IndexModel{
		// Unique among endpoints that have one: those stored before slugs get
		// theirs from the startup backfill.
		{Key: []string{"operation_id", "slug"}, IndexOptions: new(options.IndexOptions).SetUnique(true).
			SetPartialFilterExpression(bson.M{"slug": bson.M{"$type": "string"}})},
		{Key: []string{"operation_id", "module_name", "name"}},
		// The list's order: by name, case aside, _id breaking ties.
		{Key: []string{"operation_id", "name", "_id"}, IndexOptions: new(options.IndexOptions).SetCollation(caseInsensitiveSortCollation)},
		uniqueIDIndex("endpoint_id"),
	})
	return &channelEndpointRepository{coll: coll}
}

// ChannelEndpointFilter narrows an operation's endpoints list. Search is the
// app's search (searchPattern) over the name, description, channel kind and
// values (search_text: models.EndpointSearchText).
type ChannelEndpointFilter struct {
	Search      string
	ModuleNames []string
}

// channelEndpointListSort orders the endpoints list: by name, case aside.
var channelEndpointListSort = pagination.SortKey{Field: "name", Ascending: true, String: true}

// ChannelEndpointCursor is an endpoint's position in the endpoints list.
func ChannelEndpointCursor(ep *models.ChannelEndpoint) string {
	return pagination.EncodeStringCursor(ep.Name, ep.Id)
}

func channelEndpointListFilter(operationID uuid.UUID, f ChannelEndpointFilter) bson.M {
	q := bson.M{"operation_id": operationID}
	if len(f.ModuleNames) > 0 {
		q["module_name"] = bson.M{"$in": f.ModuleNames}
	}
	if f.Search != "" {
		rx := bson.M{"$regex": searchPattern(f.Search), "$options": "i"}
		q["$or"] = bson.A{
			bson.M{"name": rx},
			bson.M{"slug": rx},
			bson.M{"description": rx},
			bson.M{"module_name": rx},
			bson.M{"search_text": rx},
		}
	}
	return q
}

func (r *channelEndpointRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, filter ChannelEndpointFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.ChannelEndpoint, error) {
	if err := channelEndpointListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.ChannelEndpoint{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(channelEndpointListFilter(operationID, filter), cursor, forward, channelEndpointListSort)).
		Collation(caseInsensitiveSortCollation).
		Sort(pagination.SortFieldsKey(forward, channelEndpointListSort)...).
		Limit(limit).
		All(&out)
	if err != nil {
		return nil, err
	}
	if !forward {
		slices.Reverse(out)
	}
	err = fillIDs(ctx, r.coll, "endpoint_id", out, func(ep *models.ChannelEndpoint) (primitive.ObjectID, *uuid.UUID) {
		return ep.Id, &ep.EndpointID
	})
	return out, err
}

func (r *channelEndpointRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, filter ChannelEndpointFilter) (int64, error) {
	return r.coll.Count(ctx, channelEndpointListFilter(operationID, filter))
}

func (r *channelEndpointRepository) SlugTaken(ctx context.Context, operationID uuid.UUID, slug string) (bool, error) {
	n, err := r.coll.Count(ctx, bson.M{"operation_id": operationID, "slug": slug})
	return n > 0, err
}

func (r *channelEndpointRepository) FindByEndpointID(ctx context.Context, endpointID uuid.UUID) (*models.ChannelEndpoint, error) {
	var ep models.ChannelEndpoint
	if err := r.coll.Find(ctx, bson.M{"endpoint_id": endpointID}).One(&ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (r *channelEndpointRepository) Insert(ctx context.Context, ep *models.ChannelEndpoint) error {
	if ep.EndpointID == uuid.Nil {
		ep.EndpointID = uuid.New()
	}
	ep.SearchText = models.EndpointSearchText(ep)
	ep.Revision = 1
	_, err := r.coll.InsertOne(ctx, ep)
	if mongo.IsDuplicateKeyError(err) {
		return ErrChannelEndpointSlugTaken
	}
	return err
}

// channelEndpointUpdate is the set half of Update's compare-and-set.
func channelEndpointUpdate(ep *models.ChannelEndpoint) bson.M {
	set := bson.M{
		"name":           ep.Name,
		"slug":           ep.Slug,
		"schema_version": ep.SchemaVersion,
		"values":         ep.Values,
		"search_text":    models.EndpointSearchText(ep),
		"updated_by":     ep.UpdatedBy,
		"updated_at":     ep.UpdatedAt,
		"updateAt":       ep.UpdatedAt,
	}
	u := bson.M{"$set": set, "$inc": bson.M{"revision": int64(1)}}
	if ep.Description != "" {
		set["description"] = ep.Description
	} else {
		u["$unset"] = bson.M{"description": ""}
	}
	return u
}

func (r *channelEndpointRepository) Update(ctx context.Context, ep *models.ChannelEndpoint) error {
	var out models.ChannelEndpoint
	err := r.coll.Find(ctx, bson.M{"endpoint_id": ep.EndpointID, "revision": ep.Revision}).Apply(qmgo.Change{
		Update:    channelEndpointUpdate(ep),
		ReturnNew: true,
	}, &out)
	switch {
	case err == nil:
		ep.Revision = out.Revision
		return nil
	case errors.Is(err, qmgo.ErrNoSuchDocuments):
		return ErrChannelEndpointConflict
	case mongo.IsDuplicateKeyError(err):
		return ErrChannelEndpointSlugTaken
	default:
		return err
	}
}

func (r *channelEndpointRepository) Delete(ctx context.Context, endpointID uuid.UUID) (bool, error) {
	err := r.coll.Remove(ctx, bson.M{"endpoint_id": endpointID})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (r *channelEndpointRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

func (r *channelEndpointRepository) BackfillSlugs(ctx context.Context) (int64, error) {
	var eps []models.ChannelEndpoint
	missing := bson.M{"$or": []bson.M{{"slug": bson.M{"$exists": false}}, {"slug": ""}}}
	if err := r.coll.Find(ctx, missing).All(&eps); err != nil {
		return 0, fmt.Errorf("find endpoints without a slug: %w", err)
	}
	var n int64
	for i := range eps {
		ok, err := matched(r.coll.UpdateOne(ctx, bson.M{"_id": eps[i].Id, "$or": missing["$or"]},
			bson.M{"$set": bson.M{"slug": eps[i].Name}}))
		if err != nil {
			return n, fmt.Errorf("give endpoint %s its slug: %w", eps[i].Name, err)
		}
		if ok {
			n++
		}
	}
	return n, nil
}

func (r *channelEndpointRepository) BackfillEndpointIDs(ctx context.Context) (int64, error) {
	return backfillIDs(ctx, r.coll, "endpoint_id")
}

// BackfillSearchText gives the endpoints stored before endpoints had a
// search text one. Idempotent: it only reads endpoints without one.
func (r *channelEndpointRepository) BackfillSearchText(ctx context.Context) (int64, error) {
	var eps []models.ChannelEndpoint
	if err := r.coll.Find(ctx, bson.M{"search_text": bson.M{"$exists": false}}).All(&eps); err != nil {
		return 0, fmt.Errorf("find endpoints without a search text: %w", err)
	}
	var n int64
	for i := range eps {
		ok, err := matched(r.coll.UpdateOne(ctx, bson.M{"_id": eps[i].Id, "search_text": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"search_text": models.EndpointSearchText(&eps[i])}}))
		if err != nil {
			return n, fmt.Errorf("give endpoint %s its search text: %w", eps[i].Name, err)
		}
		if ok {
			n++
		}
	}
	return n, nil
}
