package repository

import (
	"context"
	"errors"
	"time"

	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Module management storage — desired config per instance, its audit trail,
// and action invocations. See models/module_management.go.

// The config collections were renamed when config became per instance only
// (the prototype's module_config / module_config_changes held a module-wide
// scope too). New names rather than a changed unique index on the old ones, so
// a stale index in an existing database cannot fail EnsureIndexes at boot.
const (
	moduleConfigCollection           = "module_instance_config"
	moduleConfigChangeCollection     = "module_instance_config_changes"
	moduleActionInvocationCollection = "module_action_invocations"
)

// ErrModuleConfigConflict is returned by IModuleConfigRepository.Apply when
// the instance's revision is not the expected one (someone else wrote first).
var ErrModuleConfigConflict = errors.New("module config revision conflict")

// IModuleConfigRepository stores desired property values, one document per
// instance.
type IModuleConfigRepository interface {
	// FindByInstance returns the instance's config document, nil when absent.
	FindByInstance(ctx context.Context, instance string) (*models.ModuleConfig, error)
	// Apply sets and unsets values and increments the revision, atomically and
	// only if the current revision equals expectedRevision (0 for a document
	// that does not exist yet). A mismatch returns ErrModuleConfigConflict.
	Apply(ctx context.Context, moduleName, instance string, expectedRevision int64,
		set map[string]any, unset []string, actor models.ModuleActor, at time.Time) (*models.ModuleConfig, error)
	// RaiseRevision lifts the revision to at least minRevision without
	// touching the values (creating an empty document if none exists), and
	// returns the resulting document. Atomic and monotonic: a revision already
	// at or above minRevision is left alone.
	RaiseRevision(ctx context.Context, moduleName, instance string, minRevision int64, at time.Time) (*models.ModuleConfig, error)
}

// IModuleConfigChangeRepository is the audit trail of config writes.
type IModuleConfigChangeRepository interface {
	Insert(ctx context.Context, change *models.ModuleConfigChange) error
	// ListByInstance returns an instance's config changes, newest first.
	ListByInstance(ctx context.Context, instance string, limit int64) ([]models.ModuleConfigChange, error)
}

// IModuleActionInvocationRepository records action invocations.
type IModuleActionInvocationRepository interface {
	Insert(ctx context.Context, inv *models.ModuleActionInvocation) error
	// Finish moves a RUNNING invocation to its terminal state.
	Finish(ctx context.Context, invocationID, status, resultJSON, errMsg string, at time.Time) error
	// ListByInstance returns an instance's invocations, newest first.
	ListByInstance(ctx context.Context, instance string, limit int64) ([]models.ModuleActionInvocation, error)
}

// --- module_instance_config ---

type moduleConfigRepository struct {
	coll database.Collection
}

func NewModuleConfigRepository(db database.Database) IModuleConfigRepository {
	db.EnsureIndexes(context.Background(), moduleConfigCollection, []opts.IndexModel{
		// One document per instance. Also what makes a racing first write (two
		// upserts at expectedRevision 0) fail as a conflict instead of
		// creating a duplicate.
		{Key: []string{"instance"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &moduleConfigRepository{coll: db.Collection(moduleConfigCollection)}
}

// moduleConfigApplyFilter is the compare half of the compare-and-set: the
// instance's document at exactly expectedRevision. At 0 it also matches "no
// document" through the upsert — the equality fields seed the new document,
// which the $inc then takes to revision 1. An existing document at a later
// revision does not match, so the upsert tries to insert and the unique index
// refuses it.
func moduleConfigApplyFilter(instance string, expectedRevision int64) bson.M {
	return bson.M{"instance": instance, "revision": expectedRevision}
}

// moduleConfigApplyUpdate is the set half. Keys are manifest property keys,
// already validated against ^[a-z][a-z0-9_]*$, so they are safe path segments.
func moduleConfigApplyUpdate(moduleName string, set map[string]any, unset []string, actor models.ModuleActor, at time.Time) bson.M {
	setDoc := bson.M{"module_name": moduleName, "updated_by": actor, "updated_at": at, "updateAt": at}
	for k, v := range set {
		setDoc["values."+k] = v
	}
	u := bson.M{
		"$set":         setDoc,
		"$inc":         bson.M{"revision": int64(1)},
		"$setOnInsert": bson.M{"createAt": at},
	}
	if len(unset) > 0 {
		unsetDoc := bson.M{}
		for _, k := range unset {
			unsetDoc["values."+k] = ""
		}
		u["$unset"] = unsetDoc
	}
	return u
}

// moduleConfigRaiseUpdate lifts the revision with $max, which is what makes
// RaiseRevision monotonic under concurrent writers. Values are untouched; a
// document created by the upsert starts with none.
func moduleConfigRaiseUpdate(moduleName string, minRevision int64, at time.Time) bson.M {
	return bson.M{
		"$max":         bson.M{"revision": minRevision},
		"$set":         bson.M{"updateAt": at},
		"$setOnInsert": bson.M{"module_name": moduleName, "values": bson.M{}, "createAt": at},
	}
}

func (r *moduleConfigRepository) FindByInstance(ctx context.Context, instance string) (*models.ModuleConfig, error) {
	var doc models.ModuleConfig
	err := r.coll.Find(ctx, bson.M{"instance": instance}).One(&doc)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *moduleConfigRepository) Apply(ctx context.Context, moduleName, instance string, expectedRevision int64,
	set map[string]any, unset []string, actor models.ModuleActor, at time.Time,
) (*models.ModuleConfig, error) {
	var out models.ModuleConfig
	err := r.coll.Find(ctx, moduleConfigApplyFilter(instance, expectedRevision)).Apply(qmgo.Change{
		Update:    moduleConfigApplyUpdate(moduleName, set, unset, actor, at),
		Upsert:    expectedRevision == 0,
		ReturnNew: true,
	}, &out)
	switch {
	case err == nil:
		return &out, nil
	case errors.Is(err, qmgo.ErrNoSuchDocuments), mongo.IsDuplicateKeyError(err):
		return nil, ErrModuleConfigConflict
	default:
		return nil, err
	}
}

func (r *moduleConfigRepository) RaiseRevision(ctx context.Context, moduleName, instance string, minRevision int64, at time.Time) (*models.ModuleConfig, error) {
	var out models.ModuleConfig
	err := r.coll.Find(ctx, bson.M{"instance": instance}).Apply(qmgo.Change{
		Update:    moduleConfigRaiseUpdate(moduleName, minRevision, at),
		Upsert:    true,
		ReturnNew: true,
	}, &out)
	if mongo.IsDuplicateKeyError(err) {
		// Two racing upserts on a missing document: the other one created it,
		// so retry as a plain update.
		err = r.coll.Find(ctx, bson.M{"instance": instance}).Apply(qmgo.Change{
			Update:    moduleConfigRaiseUpdate(moduleName, minRevision, at),
			ReturnNew: true,
		}, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// --- module_instance_config_changes ---

type moduleConfigChangeRepository struct {
	coll database.Collection
}

func NewModuleConfigChangeRepository(db database.Database) IModuleConfigChangeRepository {
	db.EnsureIndexes(context.Background(), moduleConfigChangeCollection, []opts.IndexModel{
		// -_id matches the query's tiebreak, so the index serves the sort.
		{Key: []string{"instance", "-at", "-_id"}},
	})
	return &moduleConfigChangeRepository{coll: db.Collection(moduleConfigChangeCollection)}
}

func (r *moduleConfigChangeRepository) Insert(ctx context.Context, change *models.ModuleConfigChange) error {
	_, err := r.coll.InsertOne(ctx, change)
	return err
}

func (r *moduleConfigChangeRepository) ListByInstance(ctx context.Context, instance string, limit int64) ([]models.ModuleConfigChange, error) {
	var out []models.ModuleConfigChange
	q := r.coll.Find(ctx, bson.M{"instance": instance}).Sort("-at", "-_id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.All(&out)
	return out, err
}

// --- module_action_invocations ---

type moduleActionInvocationRepository struct {
	coll database.Collection
}

func NewModuleActionInvocationRepository(db database.Database) IModuleActionInvocationRepository {
	db.EnsureIndexes(context.Background(), moduleActionInvocationCollection, []opts.IndexModel{
		{Key: []string{"invocation_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"instance", "-started_at", "-_id"}},
	})
	return &moduleActionInvocationRepository{coll: db.Collection(moduleActionInvocationCollection)}
}

// invocationFinishUpdate moves an invocation to its terminal state.
func invocationFinishUpdate(status, resultJSON, errMsg string, at time.Time) bson.M {
	set := bson.M{"status": status, "finished_at": at, "updateAt": at}
	if resultJSON != "" {
		set["result"] = resultJSON
	}
	if errMsg != "" {
		set["error"] = errMsg
	}
	return bson.M{"$set": set}
}

func (r *moduleActionInvocationRepository) Insert(ctx context.Context, inv *models.ModuleActionInvocation) error {
	_, err := r.coll.InsertOne(ctx, inv)
	return err
}

func (r *moduleActionInvocationRepository) Finish(ctx context.Context, invocationID, status, resultJSON, errMsg string, at time.Time) error {
	return r.coll.UpdateOne(ctx,
		bson.M{"invocation_id": invocationID, "status": models.ModuleActionRunning},
		invocationFinishUpdate(status, resultJSON, errMsg, at))
}

func (r *moduleActionInvocationRepository) ListByInstance(ctx context.Context, instance string, limit int64) ([]models.ModuleActionInvocation, error) {
	var out []models.ModuleActionInvocation
	q := r.coll.Find(ctx, bson.M{"instance": instance}).Sort("-started_at", "-_id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.All(&out)
	return out, err
}
