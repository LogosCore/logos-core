package repository

import (
	"context"
	"slices"
	"strings"
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

const (
	vpnTunnelCollection = "vpn_tunnels"
	vpnKeyCollection    = "vpn_keys"
)

// IVPNTunnelRepository keeps VPN tunnels (models.VPNTunnel): one a task,
// while it runs.
type IVPNTunnelRepository interface {
	Insert(ctx context.Context, v *models.VPNTunnel) error
	// All is every VPN tunnel kept, to take up again after a restart.
	All(ctx context.Context) ([]models.VPNTunnel, error)
	// FindByOperationIDWithCursor is a page of the operation's VPN tunnels,
	// the latest opened first; search matches SearchText.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.VPNTunnel, error)
	CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error)
	// FindByTaskID is the task's VPN tunnel; IsNotFound for none.
	FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.VPNTunnel, error)
	// ReserveStreams takes n more stream numbers for the task: the last
	// one taken.
	ReserveStreams(ctx context.Context, taskID uuid.UUID, n uint64) (uint64, error)
	SetCounts(ctx context.Context, taskID uuid.UUID, up, down int64, opened uint64) error
	SetCap(ctx context.Context, taskID uuid.UUID, limit int64) error
	// SetRoutes keeps the tunnel's routes, changed.
	SetRoutes(ctx context.Context, taskID uuid.UUID, routes []string) error
	Release(ctx context.Context, taskID uuid.UUID) error
}

type vpnTunnelRepository struct{ coll database.Collection }

func NewVPNTunnelRepository(db database.Database) IVPNTunnelRepository {
	db.EnsureIndexes(context.Background(), vpnTunnelCollection, []opts.IndexModel{
		{Key: []string{"task_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-createAt", "-_id"}},
	})
	return &vpnTunnelRepository{coll: db.Collection(vpnTunnelCollection)}
}

func (r *vpnTunnelRepository) Insert(ctx context.Context, v *models.VPNTunnel) error {
	v.SearchText = models.VPNTunnelSearchText(v)
	_, err := r.coll.InsertOne(ctx, v)
	return err
}

func (r *vpnTunnelRepository) All(ctx context.Context) ([]models.VPNTunnel, error) {
	out := []models.VPNTunnel{}
	err := r.coll.Find(ctx, bson.M{}).Sort("createAt").Limit(65535).All(&out)
	return out, err
}

// vpnTunnelListSort orders the Ports page's VPN list: the latest first.
var vpnTunnelListSort = pagination.SortKey{Field: "createAt", Ascending: false}

// VPNTunnelCursor is a VPN tunnel's position in its list.
func VPNTunnelCursor(v *models.VPNTunnel) string {
	return pagination.EncodeCursor(v.CreateAt, v.Id)
}

func vpnTunnelFilter(operationID uuid.UUID, search string) bson.M {
	q := bson.M{"operation_id": operationID}
	if search = strings.TrimSpace(search); search != "" {
		q["search_text"] = bson.M{"$regex": searchPattern(search), "$options": "i"}
	}
	return q
}

func (r *vpnTunnelRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, search string, cursor *pagination.Cursor, limit int64, forward bool) ([]models.VPNTunnel, error) {
	if err := vpnTunnelListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.VPNTunnel{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(vpnTunnelFilter(operationID, search), cursor, forward, vpnTunnelListSort)).
		Sort(pagination.SortFieldsKey(forward, vpnTunnelListSort)...).Limit(limit).All(&out)
	if err != nil {
		return nil, err
	}
	if !forward {
		slices.Reverse(out)
	}
	return out, nil
}

func (r *vpnTunnelRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, search string) (int64, error) {
	return r.coll.Find(ctx, vpnTunnelFilter(operationID, search)).Count()
}

func (r *vpnTunnelRepository) FindByTaskID(ctx context.Context, taskID uuid.UUID) (*models.VPNTunnel, error) {
	var v models.VPNTunnel
	if err := r.coll.Find(ctx, bson.M{"task_id": taskID}).One(&v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *vpnTunnelRepository) ReserveStreams(ctx context.Context, taskID uuid.UUID, n uint64) (uint64, error) {
	var v models.VPNTunnel
	err := r.coll.Find(ctx, bson.M{"task_id": taskID}).Apply(qmgo.Change{Update: bson.M{"$inc": bson.M{"streams": int64(n)}}, ReturnNew: true}, &v)
	if err != nil {
		return 0, err
	}
	return v.Streams, nil
}

func (r *vpnTunnelRepository) SetCounts(ctx context.Context, taskID uuid.UUID, up, down int64, opened uint64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"up": up, "down": down, "opened": int64(opened)}})
}

func (r *vpnTunnelRepository) SetCap(ctx context.Context, taskID uuid.UUID, limit int64) error {
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID}, bson.M{"$set": bson.M{"cap": limit}})
}

func (r *vpnTunnelRepository) SetRoutes(ctx context.Context, taskID uuid.UUID, routes []string) error {
	v, err := r.FindByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	v.Routes = routes
	return r.coll.UpdateOne(ctx, bson.M{"task_id": taskID},
		bson.M{"$set": bson.M{"routes": routes, "search_text": models.VPNTunnelSearchText(v)}})
}

func (r *vpnTunnelRepository) Release(ctx context.Context, taskID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"task_id": taskID})
	return err
}

// IVPNKeyRepository keeps the keys logos-vpn clients connect with
// (models.VPNKey).
type IVPNKeyRepository interface {
	Insert(ctx context.Context, k *models.VPNKey) error
	// FindByKeyID is a key by its id; IsNotFound for none.
	FindByKeyID(ctx context.Context, keyID string) (*models.VPNKey, error)
	// Take gives the key to a client's instance: one no client took and
	// not lapsed at now, or one this instance took already. IsNotFound
	// when it cannot (another's, lapsed, gone).
	Take(ctx context.Context, keyID, instance string, now time.Time) (*models.VPNKey, error)
	// Release drops a key; ReleaseTask every key of a task.
	Release(ctx context.Context, keyID string) error
	ReleaseTask(ctx context.Context, taskID uuid.UUID) error
}

type vpnKeyRepository struct{ coll database.Collection }

func NewVPNKeyRepository(db database.Database) IVPNKeyRepository {
	db.EnsureIndexes(context.Background(), vpnKeyCollection, []opts.IndexModel{
		{Key: []string{"key_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"task_id"}},
		// An unused key goes once it lapsed; a taken one has no lapses_at.
		{Key: []string{"lapses_at"}, IndexOptions: new(options.IndexOptions).SetExpireAfterSeconds(0)},
	})
	return &vpnKeyRepository{coll: db.Collection(vpnKeyCollection)}
}

func (r *vpnKeyRepository) Insert(ctx context.Context, k *models.VPNKey) error {
	_, err := r.coll.InsertOne(ctx, k)
	return err
}

func (r *vpnKeyRepository) FindByKeyID(ctx context.Context, keyID string) (*models.VPNKey, error) {
	var k models.VPNKey
	if err := r.coll.Find(ctx, bson.M{"key_id": keyID}).One(&k); err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *vpnKeyRepository) Take(ctx context.Context, keyID, instance string, now time.Time) (*models.VPNKey, error) {
	var k models.VPNKey
	filter := bson.M{"key_id": keyID, "$or": []bson.M{
		{"instance": bson.M{"$exists": false}, "lapses_at": bson.M{"$gt": now}},
		{"instance": instance},
	}}
	err := r.coll.Find(ctx, filter).Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"instance": instance, "taken_at": now}, "$unset": bson.M{"lapses_at": ""}},
		ReturnNew: true,
	}, &k)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *vpnKeyRepository) Release(ctx context.Context, keyID string) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"key_id": keyID})
	return err
}

func (r *vpnKeyRepository) ReleaseTask(ctx context.Context, taskID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"task_id": taskID})
	return err
}
