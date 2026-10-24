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

// Module management storage — desired config per scope, its audit trail, and
// action invocations. See models/module_management.go.

const (
	moduleConfigCollection           = "module_config"
	moduleConfigChangeCollection     = "module_config_changes"
	moduleActionInvocationCollection = "module_action_invocations"
)

// ErrModuleConfigConflict is returned by IModuleConfigRepository.Apply when
// the scope's revision is not the expected one (someone else wrote first).
var ErrModuleConfigConflict = errors.New("module config revision conflict")

// IModuleConfigRepository stores desired property values, one document per
// (module_name, scope, instance).
type IModuleConfigRepository interface {
	// FindForInstance returns the MODULE-scope document of moduleName and the
	// INSTANCE-scope document of instance; either is nil when absent.
	FindForInstance(ctx context.Context, moduleName, instance string) (module, inst *models.ModuleConfig, err error)
	// Apply sets and unsets values at one scope and increments its revision,
	// atomically and only if the current revision equals expectedRevision (0
	// for a document that does not exist yet). A mismatch returns
	// ErrModuleConfigConflict. instance must be "" for module scope.
	Apply(ctx context.Context, moduleName, scope, instance string, expectedRevision int64,
		set map[string]any, unset []string, actor models.ModuleActor, at time.Time) (*models.ModuleConfig, error)
}

// IModuleConfigChangeRepository is the audit trail of config writes.
type IModuleConfigChangeRepository interface {
	Insert(ctx context.Context, change *models.ModuleConfigChange) error
	// ListForInstance returns the changes that affect instance — its own
	// INSTANCE scope plus its module's MODULE scope — newest first.
	ListForInstance(ctx context.Context, moduleName, instance string, limit int64) ([]models.ModuleConfigChange, error)
}

// IModuleActionInvocationRepository records action invocations.
type IModuleActionInvocationRepository interface {
	Insert(ctx context.Context, inv *models.ModuleActionInvocation) error
	// Finish moves a RUNNING invocation to its terminal state.
	Finish(ctx context.Context, invocationID, status, resultJSON, errMsg string, at time.Time) error
	// ListByInstance returns an instance's invocations, newest first.
	ListByInstance(ctx context.Context, instance string, limit int64) ([]models.ModuleActionInvocation, error)
}

// --- module_config ---

type moduleConfigRepository struct {
	coll database.Collection
}

func NewModuleConfigRepository(db database.Database) IModuleConfigRepository {
	db.EnsureIndexes(context.Background(), moduleConfigCollection, []opts.IndexModel{
		// One document per scope. Also what makes a racing first write (two
		// upserts at expectedRevision 0) fail as a conflict instead of
		// creating a duplicate.
		{Key: []string{"module_name", "scope", "instance"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
	})
	return &moduleConfigRepository{coll: db.Collection(moduleConfigCollection)}
}

// moduleConfigScopeFilter matches one scope document.
func moduleConfigScopeFilter(moduleName, scope, instance string) bson.M {
	return bson.M{"module_name": moduleName, "scope": scope, "instance": instance}
}

// moduleConfigForInstanceFilter matches the two documents that make up an
// instance's desired config.
func moduleConfigForInstanceFilter(moduleName, instance string) bson.M {
	return bson.M{
		"module_name": moduleName,
		"$or": bson.A{
			bson.M{"scope": models.ModuleConfigScopeModule, "instance": ""},
			bson.M{"scope": models.ModuleConfigScopeInstance, "instance": instance},
		},
	}
}

// moduleConfigApplyFilter is the compare half of the compare-and-set: the scope
// document at exactly expectedRevision. At 0 it also matches "no document"
// through the upsert — the equality fields seed the new document, which the
// $inc then takes to revision 1. An existing document at a later revision does
// not match, so the upsert tries to insert and the unique index refuses it.
func moduleConfigApplyFilter(moduleName, scope, instance string, expectedRevision int64) bson.M {
	f := moduleConfigScopeFilter(moduleName, scope, instance)
	f["revision"] = expectedRevision
	return f
}

// moduleConfigApplyUpdate is the set half. Keys are manifest property keys,
// already validated against ^[a-z][a-z0-9_]*$, so they are safe path segments.
func moduleConfigApplyUpdate(set map[string]any, unset []string, actor models.ModuleActor, at time.Time) bson.M {
	setDoc := bson.M{"updated_by": actor, "updated_at": at, "updateAt": at}
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

func (r *moduleConfigRepository) FindForInstance(ctx context.Context, moduleName, instance string) (*models.ModuleConfig, *models.ModuleConfig, error) {
	var docs []models.ModuleConfig
	if err := r.coll.Find(ctx, moduleConfigForInstanceFilter(moduleName, instance)).All(&docs); err != nil {
		return nil, nil, err
	}
	var mod, inst *models.ModuleConfig
	for i := range docs {
		switch docs[i].Scope {
		case models.ModuleConfigScopeModule:
			mod = &docs[i]
		case models.ModuleConfigScopeInstance:
			inst = &docs[i]
		}
	}
	return mod, inst, nil
}

func (r *moduleConfigRepository) Apply(ctx context.Context, moduleName, scope, instance string, expectedRevision int64,
	set map[string]any, unset []string, actor models.ModuleActor, at time.Time,
) (*models.ModuleConfig, error) {
	var out models.ModuleConfig
	err := r.coll.Find(ctx, moduleConfigApplyFilter(moduleName, scope, instance, expectedRevision)).Apply(qmgo.Change{
		Update:    moduleConfigApplyUpdate(set, unset, actor, at),
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

// --- module_config_changes ---

type moduleConfigChangeRepository struct {
	coll database.Collection
}

func NewModuleConfigChangeRepository(db database.Database) IModuleConfigChangeRepository {
	db.EnsureIndexes(context.Background(), moduleConfigChangeCollection, []opts.IndexModel{
		{Key: []string{"module_name", "-at"}},
	})
	return &moduleConfigChangeRepository{coll: db.Collection(moduleConfigChangeCollection)}
}

func moduleConfigChangesForInstanceFilter(moduleName, instance string) bson.M {
	return bson.M{
		"module_name": moduleName,
		"$or": bson.A{
			bson.M{"scope": models.ModuleConfigScopeModule},
			bson.M{"scope": models.ModuleConfigScopeInstance, "instance": instance},
		},
	}
}

func (r *moduleConfigChangeRepository) Insert(ctx context.Context, change *models.ModuleConfigChange) error {
	_, err := r.coll.InsertOne(ctx, change)
	return err
}

func (r *moduleConfigChangeRepository) ListForInstance(ctx context.Context, moduleName, instance string, limit int64) ([]models.ModuleConfigChange, error) {
	var out []models.ModuleConfigChange
	q := r.coll.Find(ctx, moduleConfigChangesForInstanceFilter(moduleName, instance)).Sort("-at", "-_id")
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
		{Key: []string{"instance", "-started_at"}},
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
