package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
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

// ErrChannelEndpointNameTaken is returned when another endpoint of the
// operation has the name.
var ErrChannelEndpointNameTaken = errors.New("channel endpoint name taken")

// ErrChannelEndpointConflict is returned by Update when the endpoint's
// revision is not the expected one (someone else wrote first), or the
// endpoint is gone.
var ErrChannelEndpointConflict = errors.New("channel endpoint revision conflict")

// IChannelEndpointRepository stores channel endpoints.
type IChannelEndpointRepository interface {
	// List returns an operation's endpoints by kind and name, all kinds or
	// one (moduleName "" for all).
	List(ctx context.Context, operationID uuid.UUID, moduleName string) ([]models.ChannelEndpoint, error)
	// FindByID returns one endpoint; IsNotFound on a miss.
	FindByID(ctx context.Context, id primitive.ObjectID) (*models.ChannelEndpoint, error)
	// Insert stores a new endpoint at revision 1.
	Insert(ctx context.Context, ep *models.ChannelEndpoint) error
	// Update replaces name, description, schema version and values if the
	// stored revision equals ep.Revision, and increments it (ep.Revision is
	// updated).
	Update(ctx context.Context, ep *models.ChannelEndpoint) error
	// Delete removes one endpoint; false when there was none.
	Delete(ctx context.Context, id primitive.ObjectID) (bool, error)
	// DeleteByOperationID removes every endpoint of an operation (operation
	// delete cascade).
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type channelEndpointRepository struct {
	coll database.Collection
}

func NewChannelEndpointRepository(db database.Database) IChannelEndpointRepository {
	db.EnsureIndexes(context.Background(), channelEndpointCollection, []opts.IndexModel{
		{Key: []string{"operation_id", "name"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "module_name", "name"}},
	})
	return &channelEndpointRepository{coll: db.Collection(channelEndpointCollection)}
}

// channelEndpointListFilter selects an operation's endpoints, of one kind
// when moduleName is set.
func channelEndpointListFilter(operationID uuid.UUID, moduleName string) bson.M {
	filter := bson.M{"operation_id": operationID}
	if moduleName != "" {
		filter["module_name"] = moduleName
	}
	return filter
}

func (r *channelEndpointRepository) List(ctx context.Context, operationID uuid.UUID, moduleName string) ([]models.ChannelEndpoint, error) {
	out := []models.ChannelEndpoint{}
	err := r.coll.Find(ctx, channelEndpointListFilter(operationID, moduleName)).Sort("module_name", "name").All(&out)
	return out, err
}

func (r *channelEndpointRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.ChannelEndpoint, error) {
	var ep models.ChannelEndpoint
	if err := r.coll.Find(ctx, bson.M{"_id": id}).One(&ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (r *channelEndpointRepository) Insert(ctx context.Context, ep *models.ChannelEndpoint) error {
	ep.Revision = 1
	_, err := r.coll.InsertOne(ctx, ep)
	if mongo.IsDuplicateKeyError(err) {
		return ErrChannelEndpointNameTaken
	}
	return err
}

// channelEndpointUpdate is the set half of Update's compare-and-set.
func channelEndpointUpdate(ep *models.ChannelEndpoint) bson.M {
	set := bson.M{
		"name":           ep.Name,
		"schema_version": ep.SchemaVersion,
		"values":         ep.Values,
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
	err := r.coll.Find(ctx, bson.M{"_id": ep.Id, "revision": ep.Revision}).Apply(qmgo.Change{
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
		return ErrChannelEndpointNameTaken
	default:
		return err
	}
}

func (r *channelEndpointRepository) Delete(ctx context.Context, id primitive.ObjectID) (bool, error) {
	err := r.coll.Remove(ctx, bson.M{"_id": id})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (r *channelEndpointRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
