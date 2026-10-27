package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const moduleRegistryCollection = "module_registry"

// ErrInstanceBlocked refuses the registration of a blocked instance.
var ErrInstanceBlocked = errors.New("instance is blocked")

// IModuleRegistryRepository is the durable system-of-record for module
// instances and their lifecycle state. Keyed by Instance (globally unique).
type IModuleRegistryRepository interface {
	// Upsert records a registration (idempotent takeover): sets status=registered,
	// refreshes registered_at, and clears any prior dead/deregistered markers so
	// re-registering an instance revives it.
	Upsert(ctx context.Context, reg *models.Module) error
	// Block blocks an instance whatever its state, recording who and why;
	// false when there is no such instance or it is blocked already.
	Block(ctx context.Context, instance, reason string, by models.ModuleActor, at time.Time) (bool, error)
	// Unblock returns a blocked instance to deregistered, so it may register
	// again; false when it is not blocked.
	Unblock(ctx context.Context, instance string, at time.Time) (bool, error)
	// Delete removes a deregistered or dead instance's row; false for any
	// other state, or no such instance.
	Delete(ctx context.Context, instance string) (bool, error)
	// TouchHeartbeat refreshes liveness fields on a registered instance, and
	// the applied config revision when the heartbeat reported one (nil leaves
	// it untouched). The bool is false when no registered instance matched
	// (→ unknown_instance).
	TouchHeartbeat(ctx context.Context, instance, status string, metrics map[string]any, appliedRevision *int64, at time.Time) (bool, error)
	// RecordStatus replaces a registered instance's status values (the
	// last_metrics a heartbeat sets) with ones read from it outside a
	// heartbeat, such as right after an action. Liveness is untouched, and an
	// instance no longer registered is left alone.
	RecordStatus(ctx context.Context, instance string, status map[string]any) error
	// MarkDeregistered transitions a registered instance to deregistered. The
	// bool is false when no registered instance matched (→ unknown_instance).
	MarkDeregistered(ctx context.Context, instance, reason string, at time.Time) (bool, error)
	// FindStaleRegistered returns registered instances whose most recent
	// liveness signal predates cutoff — the reaper's input.
	FindStaleRegistered(ctx context.Context, cutoff time.Time, limit int64) ([]models.Module, error)
	// MarkDead transitions the named instances to dead.
	MarkDead(ctx context.Context, instances []string, at time.Time) error
	FindByInstance(ctx context.Context, instance string) (models.Module, error)
	ListActive(ctx context.Context) ([]models.Module, error)
	// FindWithCursor returns a page of module instances for the Modules
	// list, newest registration first: those filter lets through (an empty
	// filter: every state), past cursor, at most limit. Distinct from
	// ListActive, the registered-only hot path of the active-set lookups.
	FindWithCursor(ctx context.Context, filter ModuleFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Module, error)
	// Count counts the module instances filter lets through.
	Count(ctx context.Context, filter ModuleFilter) (int64, error)
	// LatestConnectionSchema returns the minion connection schema of a channel
	// kind: the highest version any instance of moduleName declared, from the
	// most recent registration among those (rows of every lifecycle state
	// count). Nil when no instance declared one.
	LatestConnectionSchema(ctx context.Context, moduleName string) (*models.ModuleConnectionSchema, error)
	// LatestConnectionSchemas is LatestConnectionSchema for every channel
	// kind that declared one, by module name.
	LatestConnectionSchemas(ctx context.Context) (map[string]*models.ModuleConnectionSchema, error)
	// LatestBuildSchema returns the build schema of a factory kind, picked
	// like LatestConnectionSchema. Nil when no instance declared one.
	LatestBuildSchema(ctx context.Context, moduleName string) (*models.ModuleBuildSchema, error)
	// LatestIcons returns the icon of each kind of a module type, by module
	// name: the one its most recent registration that sent an icon carried,
	// with that registration's light-theme icon. Kinds that never sent one are
	// absent.
	LatestIcons(ctx context.Context, moduleType string) (map[string]models.ModuleIcon, error)
	// LatestBuildSchemas is LatestBuildSchema for every factory kind that
	// declared one, by module name.
	LatestBuildSchemas(ctx context.Context) (map[string]*models.ModuleBuildSchema, error)

	// --- module management ---

	// RecordApplyResult stores a mgmt.config.apply reply: the applied revision,
	// rejected keys and restart-required keys, and clears last_apply_error.
	RecordApplyResult(ctx context.Context, instance string, reply protocol.ConfigApplyReply, at time.Time) error
	// RecordApplyError records a push that failed after all retries.
	RecordApplyError(ctx context.Context, instance, message string, at time.Time) error
}

type moduleRegistryRepository struct {
	coll database.Collection
}

func NewModuleRegistryRepository(db database.Database) IModuleRegistryRepository {
	coll := db.Collection(moduleRegistryCollection)

	db.EnsureIndexes(context.Background(), moduleRegistryCollection, []opts.IndexModel{
		{Key: []string{"instance"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		// Reaper scans by status; module_type for operator queries.
		{Key: []string{"status"}},
		{Key: []string{"module_type"}},
		// The Modules list's order: newest registration first, _id breaking ties.
		{Key: []string{"-registered_at", "-_id"}},
	})

	return &moduleRegistryRepository{coll: coll}
}

// --- pure builders (unit-tested without a broker or Mongo) ---

// instanceFilter matches a single instance regardless of status.
func instanceFilter(instance string) bson.M {
	return bson.M{"instance": instance}
}

// registeredInstanceFilter matches an instance only while it is registered —
// heartbeat/deregister on a dead/deregistered row must report unknown_instance.
func registeredInstanceFilter(instance string) bson.M {
	return bson.M{"instance": instance, "status": models.ModuleStatusRegistered}
}

// heartbeatUpdate sets the liveness fields recorded on each heartbeat, plus the
// applied config revision when the module reported one.
func heartbeatUpdate(status string, metrics map[string]any, appliedRevision *int64, at time.Time) bson.M {
	set := bson.M{"last_heartbeat_at": at}
	if appliedRevision != nil {
		set["applied_revision"] = *appliedRevision
	}
	if status != "" {
		set["last_status"] = status
	}
	if metrics != nil {
		set["last_metrics"] = metrics
	}
	return bson.M{"$set": set}
}

// deregisterUpdate transitions a row to deregistered.
func deregisterUpdate(reason string, at time.Time) bson.M {
	return bson.M{"$set": bson.M{
		"status":            models.ModuleStatusDeregistered,
		"deregistered_at":   at,
		"deregister_reason": reason,
	}}
}

// markDeadUpdate transitions matched rows to dead.
func markDeadUpdate(at time.Time) bson.M {
	return bson.M{"$set": bson.M{
		"status":           models.ModuleStatusDead,
		"declared_dead_at": at,
	}}
}

// takeoverUpdate writes a re-registration over an existing row in place: it
// refreshes the descriptor + registered_at and unsets every prior end-state /
// stale-liveness marker, so a dead or deregistered instance is revived.
//
// The management manifest is replaced by whatever this registration declared
// — and removed when it declared none — and the applied revision is the one
// the (re)started process reports. The previous process's apply outcome
// (rejected / restart_required / last_apply_error) is cleared: the register
// reply hands the new process the current effective config.
func takeoverUpdate(reg *models.Module) bson.M {
	set := bson.M{
		"module_type":      reg.Type,
		"module_name":      reg.Name,
		"version":          reg.Version,
		"rpc_queue":        reg.RPCQueue,
		"description":      reg.Description,
		"status":           models.ModuleStatusRegistered,
		"registered_at":    reg.RegisteredAt,
		"applied_revision": reg.AppliedRevision,
	}
	unset := bson.M{
		"deregistered_at":   "",
		"deregister_reason": "",
		"declared_dead_at":  "",
		"last_heartbeat_at": "",
		"last_status":       "",
		"last_metrics":      "",
		"rejected":          "",
		"restart_required":  "",
		"last_apply_error":  "",
		"blocked_at":        "",
		"blocked_by":        "",
		"block_reason":      "",
	}
	if reg.Manifest != nil {
		set["manifest"] = reg.Manifest
		set["manifest_hash"] = reg.ManifestHash
	} else {
		unset["manifest"] = ""
		unset["manifest_hash"] = ""
	}
	if reg.Connection != nil {
		set["connection"] = reg.Connection
	} else {
		unset["connection"] = ""
	}
	if reg.Build != nil {
		set["build"] = reg.Build
	} else {
		unset["build"] = ""
	}
	if reg.Icon != "" {
		set["icon"] = reg.Icon
	} else {
		unset["icon"] = ""
	}
	if reg.IconLight != "" {
		set["icon_light"] = reg.IconLight
	} else {
		unset["icon_light"] = ""
	}
	return bson.M{"$set": set, "$unset": unset}
}

// applyResultUpdate records a mgmt.config.apply reply.
func applyResultUpdate(reply protocol.ConfigApplyReply, at time.Time) bson.M {
	set := bson.M{
		"applied_revision": reply.AppliedRevision,
		"last_apply_at":    at,
	}
	unset := bson.M{"last_apply_error": ""}
	if len(reply.Rejected) > 0 {
		set["rejected"] = reply.Rejected
	} else {
		unset["rejected"] = ""
	}
	if len(reply.RestartRequired) > 0 {
		set["restart_required"] = reply.RestartRequired
	} else {
		unset["restart_required"] = ""
	}
	return bson.M{"$set": set, "$unset": unset}
}

// listFilter scopes a registry listing to the given statuses. Empty/nil returns
// every row (no status constraint).
// ModuleFilter narrows the Modules list. Search is the app's search
// (searchPattern) over the instance, kind, type, version and description;
// Statuses are lifecycle states, Types module types (channel, factory).
type ModuleFilter struct {
	Search   string
	Statuses []string
	Types    []string
}

// moduleListSort orders the Modules list: newest registration first.
var moduleListSort = pagination.SortKey{Field: "registered_at"}

// ModuleCursor is a module instance's position in the Modules list.
func ModuleCursor(m *models.Module) string { return pagination.EncodeCursor(m.RegisteredAt, m.Id) }

func listFilter(f ModuleFilter) bson.M {
	q := bson.M{}
	if len(f.Statuses) > 0 {
		q["status"] = bson.M{"$in": f.Statuses}
	}
	if len(f.Types) > 0 {
		q["module_type"] = bson.M{"$in": f.Types}
	}
	if f.Search != "" {
		rx := bson.M{"$regex": searchPattern(f.Search), "$options": "i"}
		q["$or"] = bson.A{
			bson.M{"instance": rx},
			bson.M{"module_name": rx},
			bson.M{"module_type": rx},
			bson.M{"version": rx},
			bson.M{"description": rx},
		}
	}
	return q
}

// staleRegisteredFilter matches registered rows whose effective last-seen time
// (last_heartbeat_at, falling back to registered_at for an instance that has
// never beaten) is strictly before cutoff.
func staleRegisteredFilter(cutoff time.Time) bson.M {
	return bson.M{
		"status": models.ModuleStatusRegistered,
		"$expr": bson.M{
			"$lt": bson.A{
				bson.M{"$ifNull": bson.A{"$last_heartbeat_at", "$registered_at"}},
				cutoff,
			},
		},
	}
}

// --- repository methods ---

func (r *moduleRegistryRepository) Upsert(ctx context.Context, reg *models.Module) error {
	reg.Status = models.ModuleStatusRegistered
	if reg.RegisteredAt.IsZero() {
		reg.RegisteredAt = time.Now().UTC()
	}
	// A fresh registration supersedes any prior lifecycle end-state.
	reg.DeregisteredAt = nil
	reg.DeregisterReason = ""
	reg.DeclaredDeadAt = nil

	// Atomic-ish idempotent takeover without whole-document replace (which would
	// fight the immutable _id): update the existing row in place, or insert a
	// new one. The unique index on instance closes the insert race — a losing
	// concurrent insert falls back to update.
	_, err := r.FindByInstance(ctx, reg.Instance)
	if err == nil {
		return r.takeover(ctx, reg)
	}
	if !errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return err
	}

	_, err = r.coll.InsertOne(ctx, reg)
	if err != nil && mongo.IsDuplicateKeyError(err) {
		// Lost the insert race: an instance row appeared between our find and
		// insert. Retry as an in-place update.
		return r.takeover(ctx, reg)
	}
	return err
}

// takeover updates an existing row in place, unless it is blocked: the
// filter makes that check and the update one step, so a block landing between
// a find and an update cannot be undone by a registration.
func (r *moduleRegistryRepository) takeover(ctx context.Context, reg *models.Module) error {
	err := r.coll.UpdateOne(ctx, notBlockedFilter(reg.Instance), takeoverUpdate(reg))
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return ErrInstanceBlocked
	}
	return err
}

func notBlockedFilter(instance string) bson.M {
	return bson.M{"instance": instance, "status": bson.M{"$ne": models.ModuleStatusBlocked}}
}

func (r *moduleRegistryRepository) Block(ctx context.Context, instance, reason string, by models.ModuleActor, at time.Time) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, notBlockedFilter(instance), blockUpdate(reason, by, at)))
}

// blockUpdate blocks an instance. Its liveness fields go: a blocked instance
// is not running as far as core is concerned.
func blockUpdate(reason string, by models.ModuleActor, at time.Time) bson.M {
	set := bson.M{"status": models.ModuleStatusBlocked, "blocked_at": at, "blocked_by": by}
	unset := bson.M{"last_status": "", "last_metrics": ""}
	if reason != "" {
		set["block_reason"] = reason
	} else {
		unset["block_reason"] = ""
	}
	return bson.M{"$set": set, "$unset": unset}
}

func (r *moduleRegistryRepository) Unblock(ctx context.Context, instance string, at time.Time) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, bson.M{"instance": instance, "status": models.ModuleStatusBlocked}, unblockUpdate(at)))
}

// unblockUpdate leaves the instance deregistered: free to register again.
func unblockUpdate(at time.Time) bson.M {
	return bson.M{
		"$set":   bson.M{"status": models.ModuleStatusDeregistered, "deregistered_at": at, "deregister_reason": "unblocked"},
		"$unset": bson.M{"blocked_at": "", "blocked_by": "", "block_reason": ""},
	}
}

func (r *moduleRegistryRepository) Delete(ctx context.Context, instance string) (bool, error) {
	return matched(r.coll.Remove(ctx, deletableFilter(instance)))
}

// deletableFilter matches an instance that is gone: deregistered or dead. A
// registered one would only register again, and a blocked one is the block.
func deletableFilter(instance string) bson.M {
	return bson.M{"instance": instance, "status": bson.M{"$in": []string{models.ModuleStatusDeregistered, models.ModuleStatusDead}}}
}

func (r *moduleRegistryRepository) LatestConnectionSchema(ctx context.Context, moduleName string) (*models.ModuleConnectionSchema, error) {
	var mod models.Module
	err := r.coll.Find(ctx, latestConnectionFilter(moduleName)).
		Sort("-connection.version", "-registered_at").
		Select(bson.M{"connection": 1}).
		One(&mod)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mod.Connection, nil
}

func (r *moduleRegistryRepository) LatestConnectionSchemas(ctx context.Context) (map[string]*models.ModuleConnectionSchema, error) {
	// One row per instance that declared a schema: few, so the newest per
	// kind is picked here rather than in an aggregation.
	var mods []models.Module
	err := r.coll.Find(ctx, bson.M{"connection": bson.M{"$exists": true}}).
		Sort("module_name", "-connection.version", "-registered_at").
		Select(bson.M{"module_name": 1, "connection": 1}).
		All(&mods)
	if err != nil {
		return nil, err
	}
	return latestPerKind(mods), nil
}

// latestPerKind keeps the first schema of each module name from rows sorted
// newest first within a name.
func latestPerKind(mods []models.Module) map[string]*models.ModuleConnectionSchema {
	out := map[string]*models.ModuleConnectionSchema{}
	for i := range mods {
		if _, seen := out[mods[i].Name]; !seen && mods[i].Connection != nil {
			out[mods[i].Name] = mods[i].Connection
		}
	}
	return out
}

func latestConnectionFilter(moduleName string) bson.M {
	return bson.M{"module_name": moduleName, "connection": bson.M{"$exists": true}}
}

func (r *moduleRegistryRepository) LatestIcons(ctx context.Context, moduleType string) (map[string]models.ModuleIcon, error) {
	// Few rows (instances), so the newest per kind is picked here.
	var mods []models.Module
	err := r.coll.Find(ctx, bson.M{"module_type": moduleType, "icon": bson.M{"$exists": true}}).
		Sort("module_name", "-registered_at").
		Select(bson.M{"module_name": 1, "icon": 1, "icon_light": 1}).
		All(&mods)
	if err != nil {
		return nil, err
	}
	return latestIconPerKind(mods), nil
}

// latestIconPerKind keeps the first icon of each module name from rows sorted
// newest first within a name, with that row's light-theme icon: the two are
// always one registration's.
func latestIconPerKind(mods []models.Module) map[string]models.ModuleIcon {
	out := map[string]models.ModuleIcon{}
	for _, m := range mods {
		if _, seen := out[m.Name]; !seen && m.Icon != "" {
			out[m.Name] = models.ModuleIcon{Icon: m.Icon, IconLight: m.IconLight}
		}
	}
	return out
}

func (r *moduleRegistryRepository) LatestBuildSchema(ctx context.Context, moduleName string) (*models.ModuleBuildSchema, error) {
	var mod models.Module
	err := r.coll.Find(ctx, bson.M{"module_name": moduleName, "build": bson.M{"$exists": true}}).
		Sort("-build.version", "-registered_at").
		Select(bson.M{"build": 1}).
		One(&mod)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return mod.Build, nil
}

func (r *moduleRegistryRepository) LatestBuildSchemas(ctx context.Context) (map[string]*models.ModuleBuildSchema, error) {
	var mods []models.Module
	err := r.coll.Find(ctx, bson.M{"build": bson.M{"$exists": true}}).
		Sort("module_name", "-build.version", "-registered_at").
		Select(bson.M{"module_name": 1, "build": 1}).
		All(&mods)
	if err != nil {
		return nil, err
	}
	out := map[string]*models.ModuleBuildSchema{}
	for i := range mods {
		if _, seen := out[mods[i].Name]; !seen && mods[i].Build != nil {
			out[mods[i].Name] = mods[i].Build
		}
	}
	return out, nil
}

func (r *moduleRegistryRepository) TouchHeartbeat(ctx context.Context, instance, status string, metrics map[string]any, appliedRevision *int64, at time.Time) (bool, error) {
	err := r.coll.UpdateOne(ctx, registeredInstanceFilter(instance), heartbeatUpdate(status, metrics, appliedRevision, at))
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *moduleRegistryRepository) RecordStatus(ctx context.Context, instance string, status map[string]any) error {
	err := r.coll.UpdateOne(ctx, registeredInstanceFilter(instance), bson.M{"$set": bson.M{"last_metrics": status}})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil
	}
	return err
}

func (r *moduleRegistryRepository) MarkDeregistered(ctx context.Context, instance, reason string, at time.Time) (bool, error) {
	err := r.coll.UpdateOne(ctx, registeredInstanceFilter(instance), deregisterUpdate(reason, at))
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *moduleRegistryRepository) FindStaleRegistered(ctx context.Context, cutoff time.Time, limit int64) ([]models.Module, error) {
	var out []models.Module
	q := r.coll.Find(ctx, staleRegisteredFilter(cutoff)).Sort("registered_at")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.All(&out)
	return out, err
}

func (r *moduleRegistryRepository) MarkDead(ctx context.Context, instances []string, at time.Time) error {
	if len(instances) == 0 {
		return nil
	}
	_, err := r.coll.UpdateAll(ctx,
		bson.M{"instance": bson.M{"$in": instances}, "status": models.ModuleStatusRegistered},
		markDeadUpdate(at),
	)
	return err
}

func (r *moduleRegistryRepository) FindByInstance(ctx context.Context, instance string) (models.Module, error) {
	var reg models.Module
	err := r.coll.FindOne(ctx, instanceFilter(instance)).One(&reg)
	return reg, err
}

func (r *moduleRegistryRepository) ListActive(ctx context.Context) ([]models.Module, error) {
	var out []models.Module
	err := r.coll.Find(ctx, bson.M{"status": models.ModuleStatusRegistered}).All(&out)
	return out, err
}

func (r *moduleRegistryRepository) FindWithCursor(ctx context.Context, filter ModuleFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Module, error) {
	if err := moduleListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.Module{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(listFilter(filter), cursor, forward, moduleListSort)).
		Sort(pagination.SortFieldsKey(forward, moduleListSort)...).
		Limit(limit).
		All(&out)
	if !forward {
		slices.Reverse(out)
	}
	return out, err
}

func (r *moduleRegistryRepository) Count(ctx context.Context, filter ModuleFilter) (int64, error) {
	return r.coll.Count(ctx, listFilter(filter))
}

// RecordApplyResult and RecordApplyError match the instance in any lifecycle
// state: a push can finish after the instance died, and recording its outcome
// is harmless. A row that vanished is not an error.
func (r *moduleRegistryRepository) RecordApplyResult(ctx context.Context, instance string, reply protocol.ConfigApplyReply, at time.Time) error {
	err := r.coll.UpdateOne(ctx, instanceFilter(instance), applyResultUpdate(reply, at))
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil
	}
	return err
}

func (r *moduleRegistryRepository) RecordApplyError(ctx context.Context, instance, message string, at time.Time) error {
	err := r.coll.UpdateOne(ctx, instanceFilter(instance), bson.M{"$set": bson.M{
		"last_apply_error": message,
		"last_apply_at":    at,
	}})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil
	}
	return err
}
