package repository

import (
	"context"
	"errors"
	"time"

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

// Minion builds: build files, presets, builds and minions. See
// models/build.go. A build's state only moves through compare-and-set updates
// on build_id + state, so concurrent core instances (and a cancel racing a
// result) never both win.

const (
	buildFileCollection   = "build_files"
	buildPresetCollection = "build_presets"
	buildCollection       = "builds"
	minionCollection      = "minions"
)

// ErrBuildPresetNameTaken is returned when another preset of the operation
// has the name.
var ErrBuildPresetNameTaken = errors.New("build preset name taken")

// ErrBuildPresetConflict is returned by Update when the preset's revision is
// not the expected one, or the preset is gone.
var ErrBuildPresetConflict = errors.New("build preset revision conflict")

// --- build files ---------------------------------------------------------

// IBuildFileRepository stores build file metadata (the bytes are in the build
// bucket).
type IBuildFileRepository interface {
	Insert(ctx context.Context, f *models.BuildFile) error
	// FindByFileID returns one file; IsNotFound on a miss.
	FindByFileID(ctx context.Context, fileID uuid.UUID) (*models.BuildFile, error)
	// List returns an operation's files, newest first.
	List(ctx context.Context, operationID uuid.UUID) ([]models.BuildFile, error)
	// ListCreatedBefore returns files uploaded before cutoff, oldest first:
	// the sweeper's candidates.
	ListCreatedBefore(ctx context.Context, cutoff time.Time, limit int64) ([]models.BuildFile, error)
	Delete(ctx context.Context, fileID uuid.UUID) (bool, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type buildFileRepository struct{ coll database.Collection }

func NewBuildFileRepository(db database.Database) IBuildFileRepository {
	db.EnsureIndexes(context.Background(), buildFileCollection, []opts.IndexModel{
		{Key: []string{"file_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at"}},
		{Key: []string{"created_at"}},
	})
	return &buildFileRepository{coll: db.Collection(buildFileCollection)}
}

func (r *buildFileRepository) Insert(ctx context.Context, f *models.BuildFile) error {
	_, err := r.coll.InsertOne(ctx, f)
	return err
}

func (r *buildFileRepository) FindByFileID(ctx context.Context, fileID uuid.UUID) (*models.BuildFile, error) {
	var f models.BuildFile
	if err := r.coll.Find(ctx, bson.M{"file_id": fileID}).One(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *buildFileRepository) List(ctx context.Context, operationID uuid.UUID) ([]models.BuildFile, error) {
	out := []models.BuildFile{}
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Sort("-created_at").All(&out)
	return out, err
}

func (r *buildFileRepository) ListCreatedBefore(ctx context.Context, cutoff time.Time, limit int64) ([]models.BuildFile, error) {
	out := []models.BuildFile{}
	q := r.coll.Find(ctx, bson.M{"created_at": bson.M{"$lt": cutoff}}).Sort("created_at")
	if limit > 0 {
		q = q.Limit(limit)
	}
	return out, q.All(&out)
}

func (r *buildFileRepository) Delete(ctx context.Context, fileID uuid.UUID) (bool, error) {
	err := r.coll.Remove(ctx, bson.M{"file_id": fileID})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (r *buildFileRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

// --- presets -------------------------------------------------------------

// IBuildPresetRepository stores build presets.
type IBuildPresetRepository interface {
	// List returns an operation's presets by kind and name, all kinds or one
	// (moduleName "" for all).
	List(ctx context.Context, operationID uuid.UUID, moduleName string) ([]models.BuildPreset, error)
	// FindByID returns one preset; IsNotFound on a miss.
	FindByID(ctx context.Context, id primitive.ObjectID) (*models.BuildPreset, error)
	// Insert stores a new preset at revision 1.
	Insert(ctx context.Context, p *models.BuildPreset) error
	// Update replaces the settings if the stored revision equals p.Revision,
	// and increments it (p.Revision is updated).
	Update(ctx context.Context, p *models.BuildPreset) error
	Delete(ctx context.Context, id primitive.ObjectID) (bool, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
	// CountFileReferences counts presets whose values name a build file.
	CountFileReferences(ctx context.Context, fileID string) (int64, error)
}

type buildPresetRepository struct{ coll database.Collection }

func NewBuildPresetRepository(db database.Database) IBuildPresetRepository {
	db.EnsureIndexes(context.Background(), buildPresetCollection, []opts.IndexModel{
		{Key: []string{"operation_id", "name"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "module_name", "name"}},
		{Key: []string{"file_ids"}},
	})
	return &buildPresetRepository{coll: db.Collection(buildPresetCollection)}
}

func (r *buildPresetRepository) List(ctx context.Context, operationID uuid.UUID, moduleName string) ([]models.BuildPreset, error) {
	filter := bson.M{"operation_id": operationID}
	if moduleName != "" {
		filter["module_name"] = moduleName
	}
	out := []models.BuildPreset{}
	err := r.coll.Find(ctx, filter).Sort("module_name", "name").All(&out)
	return out, err
}

func (r *buildPresetRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.BuildPreset, error) {
	var p models.BuildPreset
	if err := r.coll.Find(ctx, bson.M{"_id": id}).One(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *buildPresetRepository) Insert(ctx context.Context, p *models.BuildPreset) error {
	p.Revision = 1
	_, err := r.coll.InsertOne(ctx, p)
	if mongo.IsDuplicateKeyError(err) {
		return ErrBuildPresetNameTaken
	}
	return err
}

// buildPresetUpdate is the set half of Update's compare-and-set.
func buildPresetUpdate(p *models.BuildPreset) bson.M {
	set := bson.M{
		"name":           p.Name,
		"target":         p.Target,
		"schema_version": p.SchemaVersion,
		"values":         p.Values,
		"connections":    p.Connections,
		"updated_by":     p.UpdatedBy,
		"updated_at":     p.UpdatedAt,
		"updateAt":       p.UpdatedAt,
	}
	unset := bson.M{}
	for key, v := range map[string]string{"description": p.Description, "platform": p.Platform} {
		if v != "" {
			set[key] = v
		} else {
			unset[key] = ""
		}
	}
	if len(p.FileIDs) > 0 {
		set["file_ids"] = p.FileIDs
	} else {
		unset["file_ids"] = ""
	}
	u := bson.M{"$set": set, "$inc": bson.M{"revision": int64(1)}}
	if len(unset) > 0 {
		u["$unset"] = unset
	}
	return u
}

func (r *buildPresetRepository) Update(ctx context.Context, p *models.BuildPreset) error {
	var out models.BuildPreset
	err := r.coll.Find(ctx, bson.M{"_id": p.Id, "revision": p.Revision}).Apply(qmgo.Change{
		Update:    buildPresetUpdate(p),
		ReturnNew: true,
	}, &out)
	switch {
	case err == nil:
		p.Revision = out.Revision
		return nil
	case errors.Is(err, qmgo.ErrNoSuchDocuments):
		return ErrBuildPresetConflict
	case mongo.IsDuplicateKeyError(err):
		return ErrBuildPresetNameTaken
	default:
		return err
	}
}

func (r *buildPresetRepository) Delete(ctx context.Context, id primitive.ObjectID) (bool, error) {
	err := r.coll.Remove(ctx, bson.M{"_id": id})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (r *buildPresetRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

func (r *buildPresetRepository) CountFileReferences(ctx context.Context, fileID string) (int64, error) {
	return r.coll.Find(ctx, bson.M{"file_ids": fileID}).Count()
}

// --- builds --------------------------------------------------------------

// BuildOutcome is what Finish records with a final state.
type BuildOutcome struct {
	State        string
	Error        *models.BuildError
	Artifact     *models.BuildArtifact
	Capabilities []string
	Warnings     []string
	Log          string
	CancelledBy  *models.ModuleActor
	At           time.Time
}

// IBuildRepository stores builds. State changes are compare-and-set.
type IBuildRepository interface {
	Insert(ctx context.Context, b *models.Build) error
	// FindByBuildID returns one build; IsNotFound on a miss.
	FindByBuildID(ctx context.Context, buildID string) (*models.Build, error)
	// List returns an operation's builds, newest first, at most limit (0:
	// all).
	List(ctx context.Context, operationID uuid.UUID, limit int64) ([]models.Build, error)
	// ListUnfinished returns every queued or building build: the
	// scheduler's input.
	ListUnfinished(ctx context.Context) ([]models.Build, error)
	// ClaimQueued claims a queued build no other core instance holds, oldest
	// first, until leaseUntil. Nil when there is none.
	ClaimQueued(ctx context.Context, now, leaseUntil time.Time) (*models.Build, error)
	// ReleaseClaim ends a claim that placed the build nowhere.
	ReleaseClaim(ctx context.Context, buildID string) error
	// MarkBuilding moves a queued build to BUILDING on instance. Core does it
	// before offering the build there, so a report the instance sends at
	// once already finds the build its own.
	MarkBuilding(ctx context.Context, buildID, instance, factoryVersion string, deadline, at time.Time) (bool, error)
	// Unoffer puts a build that instance did not take back in the queue,
	// claimed until leaseUntil, unless the instance already reported a
	// result. False when it is no longer building there.
	Unoffer(ctx context.Context, buildID, instance string, leaseUntil time.Time) (bool, error)
	// AppendStep records progress while instance builds it, keeping the
	// last keep steps. False when it is not building there.
	AppendStep(ctx context.Context, buildID, instance string, step models.BuildStep, keep int) (bool, error)
	// RecordResult stores a succeeded result whose artifact is still to be
	// checked. False when the build is not building on instance, or a
	// result was already recorded.
	RecordResult(ctx context.Context, buildID, instance string, artifact models.BuildArtifact, capabilities, warnings []string, log string) (bool, error)
	// Finish moves a build in one of the from states to a final one. False
	// when it was in none of them.
	Finish(ctx context.Context, buildID string, from []string, out BuildOutcome) (bool, error)
	// MarkArtifactDeleted records that the artifact's bytes were removed.
	MarkArtifactDeleted(ctx context.Context, buildID string) error
	// CountFileReferences counts builds whose values name a build file.
	CountFileReferences(ctx context.Context, fileID string) (int64, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type buildRepository struct{ coll database.Collection }

func NewBuildRepository(db database.Database) IBuildRepository {
	db.EnsureIndexes(context.Background(), buildCollection, []opts.IndexModel{
		{Key: []string{"build_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at"}},
		{Key: []string{"state", "created_at"}},
		{Key: []string{"file_ids"}},
	})
	return &buildRepository{coll: db.Collection(buildCollection)}
}

var unfinishedStates = []string{models.BuildStateQueued, models.BuildStateBuilding}

func (r *buildRepository) Insert(ctx context.Context, b *models.Build) error {
	_, err := r.coll.InsertOne(ctx, b)
	return err
}

func (r *buildRepository) FindByBuildID(ctx context.Context, buildID string) (*models.Build, error) {
	var b models.Build
	if err := r.coll.Find(ctx, bson.M{"build_id": buildID}).One(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *buildRepository) List(ctx context.Context, operationID uuid.UUID, limit int64) ([]models.Build, error) {
	out := []models.Build{}
	q := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Sort("-created_at")
	if limit > 0 {
		q = q.Limit(limit)
	}
	return out, q.All(&out)
}

func (r *buildRepository) ListUnfinished(ctx context.Context) ([]models.Build, error) {
	out := []models.Build{}
	err := r.coll.Find(ctx, bson.M{"state": bson.M{"$in": unfinishedStates}}).Sort("created_at").All(&out)
	return out, err
}

// claimFilter matches a queued build no live claim holds.
func claimFilter(now time.Time) bson.M {
	return bson.M{
		"state": models.BuildStateQueued,
		"$or": []bson.M{
			{"lease_until": bson.M{"$exists": false}},
			{"lease_until": bson.M{"$lt": now}},
		},
	}
}

func (r *buildRepository) ClaimQueued(ctx context.Context, now, leaseUntil time.Time) (*models.Build, error) {
	var b models.Build
	err := r.coll.Find(ctx, claimFilter(now)).Sort("created_at").Apply(qmgo.Change{
		Update:    bson.M{"$set": bson.M{"lease_until": leaseUntil}},
		ReturnNew: true,
	}, &b)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *buildRepository) ReleaseClaim(ctx context.Context, buildID string) error {
	err := r.coll.UpdateOne(ctx, bson.M{"build_id": buildID, "state": models.BuildStateQueued},
		bson.M{"$unset": bson.M{"lease_until": ""}})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil
	}
	return err
}

func (r *buildRepository) MarkBuilding(ctx context.Context, buildID, instance, factoryVersion string, deadline, at time.Time) (bool, error) {
	return matched(r.coll.UpdateOne(ctx,
		bson.M{"build_id": buildID, "state": models.BuildStateQueued},
		bson.M{
			"$set": bson.M{
				"state": models.BuildStateBuilding, "instance": instance, "factory_version": factoryVersion,
				"deadline": deadline, "started_at": at, "updateAt": at,
			},
			"$unset": bson.M{"lease_until": ""},
		}))
}

func (r *buildRepository) Unoffer(ctx context.Context, buildID, instance string, leaseUntil time.Time) (bool, error) {
	filter := buildingOn(buildID, instance)
	filter["result_received"] = bson.M{"$ne": true}
	return matched(r.coll.UpdateOne(ctx, filter, bson.M{
		"$set":   bson.M{"state": models.BuildStateQueued, "lease_until": leaseUntil},
		"$unset": bson.M{"instance": "", "factory_version": "", "started_at": "", "steps": ""},
	}))
}

// buildingOn matches a build while instance builds it.
func buildingOn(buildID, instance string) bson.M {
	return bson.M{"build_id": buildID, "state": models.BuildStateBuilding, "instance": instance}
}

func (r *buildRepository) AppendStep(ctx context.Context, buildID, instance string, step models.BuildStep, keep int) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, buildingOn(buildID, instance), bson.M{
		"$push": bson.M{"steps": bson.M{"$each": []models.BuildStep{step}, "$slice": -keep}},
	}))
}

func (r *buildRepository) RecordResult(ctx context.Context, buildID, instance string, artifact models.BuildArtifact, capabilities, warnings []string, log string) (bool, error) {
	filter := buildingOn(buildID, instance)
	filter["result_received"] = bson.M{"$ne": true}
	set := bson.M{"result_received": true, "artifact": artifact, "log": log}
	if len(capabilities) > 0 {
		set["capabilities"] = capabilities
	}
	if len(warnings) > 0 {
		set["warnings"] = warnings
	}
	return matched(r.coll.UpdateOne(ctx, filter, bson.M{"$set": set}))
}

// finishUpdate is the update of Finish.
func finishUpdate(out BuildOutcome) bson.M {
	set := bson.M{"state": out.State, "finished_at": out.At, "updateAt": out.At}
	if out.Error != nil {
		set["error"] = out.Error
	}
	if out.Artifact != nil {
		set["artifact"] = out.Artifact
	}
	if len(out.Capabilities) > 0 {
		set["capabilities"] = out.Capabilities
	}
	if len(out.Warnings) > 0 {
		set["warnings"] = out.Warnings
	}
	if out.Log != "" {
		set["log"] = out.Log
	}
	if out.CancelledBy != nil {
		set["cancelled_by"] = out.CancelledBy
	}
	return bson.M{"$set": set, "$unset": bson.M{"lease_until": ""}}
}

func (r *buildRepository) Finish(ctx context.Context, buildID string, from []string, out BuildOutcome) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, bson.M{"build_id": buildID, "state": bson.M{"$in": from}}, finishUpdate(out)))
}

func (r *buildRepository) MarkArtifactDeleted(ctx context.Context, buildID string) error {
	return r.coll.UpdateOne(ctx, bson.M{"build_id": buildID}, bson.M{"$set": bson.M{"artifact.deleted": true}})
}

func (r *buildRepository) CountFileReferences(ctx context.Context, fileID string) (int64, error) {
	return r.coll.Find(ctx, bson.M{"file_ids": fileID}).Count()
}

func (r *buildRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

// matched turns qmgo's no-match error into false.
func matched(err error) (bool, error) {
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return false, nil
	}
	return err == nil, err
}

// --- minions -------------------------------------------------------------

// IMinionRepository stores minions.
type IMinionRepository interface {
	// Insert stores a minion; storing the same minion id again is a no-op.
	Insert(ctx context.Context, m *models.Minion) error
	// List returns an operation's minions, newest first.
	List(ctx context.Context, operationID uuid.UUID) ([]models.Minion, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type minionRepository struct{ coll database.Collection }

func NewMinionRepository(db database.Database) IMinionRepository {
	db.EnsureIndexes(context.Background(), minionCollection, []opts.IndexModel{
		{Key: []string{"minion_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "-created_at"}},
	})
	return &minionRepository{coll: db.Collection(minionCollection)}
}

func (r *minionRepository) Insert(ctx context.Context, m *models.Minion) error {
	_, err := r.coll.InsertOne(ctx, m)
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

func (r *minionRepository) List(ctx context.Context, operationID uuid.UUID) ([]models.Minion, error) {
	out := []models.Minion{}
	err := r.coll.Find(ctx, bson.M{"operation_id": operationID}).Sort("-created_at").All(&out)
	return out, err
}

func (r *minionRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
