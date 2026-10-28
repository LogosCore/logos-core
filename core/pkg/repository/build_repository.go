package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/qiniu/qmgo"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Minion builds: build files, builds and minions. See
// models/build.go. A build's state only moves through compare-and-set updates
// on minion_id + state, so concurrent core instances (and a cancel racing a
// result) never both win.

const (
	buildFileCollection = "build_files"
	buildCollection     = "builds"
	minionCollection    = "minions"
)

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

// --- builds --------------------------------------------------------------

// BuildOutcome is what Finish records with a final state.
type BuildOutcome struct {
	State       string
	Error       *models.BuildError
	Artifact    *models.BuildArtifact
	Warnings    []string
	Log         string
	CancelledBy *models.ModuleActor
	At          time.Time
}

// IBuildRepository stores builds. State changes are compare-and-set.
type IBuildRepository interface {
	Insert(ctx context.Context, b *models.Build) error
	// FindByMinionID returns one build; IsNotFound on a miss.
	FindByMinionID(ctx context.Context, minionID uuid.UUID) (*models.Build, error)
	// List returns an operation's builds, newest first, at most limit (0:
	// all).
	List(ctx context.Context, operationID uuid.UUID, limit int64) ([]models.Build, error)
	// FindByOperationIDWithCursor returns a page of an operation's builds,
	// newest first: those filter lets through, past cursor, at most limit.
	FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, filter BuildFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Build, error)
	// CountByOperationID counts an operation's builds filter lets through.
	CountByOperationID(ctx context.Context, operationID uuid.UUID, filter BuildFilter) (int64, error)
	// BackfillSearchText gives the builds stored without a search text one.
	BackfillSearchText(ctx context.Context) (int64, error)
	// ListUnfinished returns every queued or building build: the
	// scheduler's input.
	ListUnfinished(ctx context.Context) ([]models.Build, error)
	// ClaimQueued claims a queued build no other core instance holds, oldest
	// first, until leaseUntil. Nil when there is none.
	ClaimQueued(ctx context.Context, now, leaseUntil time.Time) (*models.Build, error)
	// ReleaseClaim ends a claim that placed the build nowhere.
	ReleaseClaim(ctx context.Context, minionID uuid.UUID) error
	// MarkBuilding moves a queued build to BUILDING on instance. Core does it
	// before offering the build there, so a report the instance sends at
	// once already finds the build its own.
	MarkBuilding(ctx context.Context, minionID uuid.UUID, instance, factoryVersion string, deadline, at time.Time) (bool, error)
	// Unoffer puts a build that instance did not take back in the queue,
	// claimed until leaseUntil, unless the instance already reported a
	// result. False when it is no longer building there.
	Unoffer(ctx context.Context, minionID uuid.UUID, instance string, leaseUntil time.Time) (bool, error)
	// SetPlan stores the factory's plan for the build while instance builds
	// it. False when it is not building there.
	SetPlan(ctx context.Context, minionID uuid.UUID, instance string, plan []models.BuildPlanStep) (bool, error)
	// ReportStep records that instance is on step (an id of the plan, a slug
	// without dots) at at: the step's first report time is kept, its message
	// replaced. False when the build is not building there.
	ReportStep(ctx context.Context, minionID uuid.UUID, instance, step, message string, at time.Time) (bool, error)
	// RecordResult stores a succeeded result, received at at, whose artifact
	// is still to be checked. False when the build is not building on
	// instance, or a result was already recorded.
	RecordResult(ctx context.Context, minionID uuid.UUID, instance string, artifact models.BuildArtifact, warnings []string, log string, at time.Time) (bool, error)
	// Finish moves a build in one of the from states to a final one. False
	// when it was in none of them.
	Finish(ctx context.Context, minionID uuid.UUID, from []string, out BuildOutcome) (bool, error)
	// MarkArtifactDeleted records that the artifact's bytes were removed.
	MarkArtifactDeleted(ctx context.Context, minionID uuid.UUID) error
	// CountFileReferences counts builds whose values name a build file.
	CountFileReferences(ctx context.Context, fileID uuid.UUID) (int64, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

type buildRepository struct{ coll database.Collection }

func NewBuildRepository(db database.Database) IBuildRepository {
	db.EnsureIndexes(context.Background(), buildCollection, []opts.IndexModel{
		{Key: []string{"minion_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		// -_id matches the list's tiebreak, so the index serves the sort.
		{Key: []string{"operation_id", "-created_at", "-_id"}},
		{Key: []string{"state", "created_at"}},
		{Key: []string{"file_ids"}},
	})
	return &buildRepository{coll: db.Collection(buildCollection)}
}

var unfinishedStates = []string{models.BuildStateQueued, models.BuildStateBuilding}

func (r *buildRepository) Insert(ctx context.Context, b *models.Build) error {
	b.SearchText = models.BuildSearchText(b)
	_, err := r.coll.InsertOne(ctx, b)
	return err
}

func (r *buildRepository) FindByMinionID(ctx context.Context, minionID uuid.UUID) (*models.Build, error) {
	var b models.Build
	if err := r.coll.Find(ctx, bson.M{"minion_id": minionID}).One(&b); err != nil {
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

// BuildFilter narrows an operation's builds list. Search is the app's
// search (searchPattern) over what the Builds page shows of a build (factory,
// target, description, platform, state, factory instance, who started it, its
// plan's steps), its error and artifact name, the operators' notes on its
// connections, and its search text: its minion id, values, commands and
// connections (models.BuildSearchText).
type BuildFilter struct {
	Search      string
	States      []string
	ModuleNames []string
}

// buildListSort orders the builds list: newest first.
var buildListSort = pagination.SortKey{Field: "created_at"}

// BuildCursor is a build's position in the builds list.
func BuildCursor(b *models.Build) string { return pagination.EncodeCursor(b.CreatedAt, b.Id) }

func buildListFilter(operationID uuid.UUID, f BuildFilter) bson.M {
	q := bson.M{"operation_id": operationID}
	if len(f.States) > 0 {
		q["state"] = bson.M{"$in": f.States}
	}
	if len(f.ModuleNames) > 0 {
		q["module_name"] = bson.M{"$in": f.ModuleNames}
	}
	if f.Search != "" {
		rx := bson.M{"$regex": searchPattern(f.Search), "$options": "i"}
		or := bson.A{}
		for _, field := range []string{
			"description", "module_name", "target", "platform", "state", "instance", "created_by.name",
			"plan.label", "error.code", "error.message", "artifact.filename", "connection_descriptions", "search_text",
		} {
			or = append(or, bson.M{field: rx})
		}
		q["$or"] = or
	}
	return q
}

func (r *buildRepository) FindByOperationIDWithCursor(ctx context.Context, operationID uuid.UUID, filter BuildFilter, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Build, error) {
	if err := buildListSort.ValidateCursor(cursor); err != nil {
		return nil, err
	}
	out := []models.Build{}
	err := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(buildListFilter(operationID, filter), cursor, forward, buildListSort)).
		Sort(pagination.SortFieldsKey(forward, buildListSort)...).
		Limit(limit).
		All(&out)
	if !forward {
		slices.Reverse(out)
	}
	return out, err
}

func (r *buildRepository) CountByOperationID(ctx context.Context, operationID uuid.UUID, filter BuildFilter) (int64, error) {
	return r.coll.Count(ctx, buildListFilter(operationID, filter))
}

// BackfillSearchText gives the builds stored before builds had a search text
// one. Idempotent: it only reads builds without one.
func (r *buildRepository) BackfillSearchText(ctx context.Context) (int64, error) {
	var bs []models.Build
	if err := r.coll.Find(ctx, bson.M{"search_text": bson.M{"$exists": false}}).All(&bs); err != nil {
		return 0, fmt.Errorf("find builds without a search text: %w", err)
	}
	var n int64
	for i := range bs {
		ok, err := matched(r.coll.UpdateOne(ctx, bson.M{"_id": bs[i].Id, "search_text": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"search_text": models.BuildSearchText(&bs[i])}}))
		if err != nil {
			return n, fmt.Errorf("give build %s its search text: %w", bs[i].MinionID, err)
		}
		if ok {
			n++
		}
	}
	return n, nil
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

func (r *buildRepository) ReleaseClaim(ctx context.Context, minionID uuid.UUID) error {
	err := r.coll.UpdateOne(ctx, bson.M{"minion_id": minionID, "state": models.BuildStateQueued},
		bson.M{"$unset": bson.M{"lease_until": ""}})
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return nil
	}
	return err
}

func (r *buildRepository) MarkBuilding(ctx context.Context, minionID uuid.UUID, instance, factoryVersion string, deadline, at time.Time) (bool, error) {
	return matched(r.coll.UpdateOne(ctx,
		bson.M{"minion_id": minionID, "state": models.BuildStateQueued},
		bson.M{
			"$set": bson.M{
				"state": models.BuildStateBuilding, "instance": instance, "factory_version": factoryVersion,
				"deadline": deadline, "started_at": at, "updateAt": at,
			},
			"$unset": bson.M{"lease_until": ""},
		}))
}

func (r *buildRepository) Unoffer(ctx context.Context, minionID uuid.UUID, instance string, leaseUntil time.Time) (bool, error) {
	filter := buildingOn(minionID, instance)
	filter["result_received"] = bson.M{"$ne": true}
	return matched(r.coll.UpdateOne(ctx, filter, bson.M{
		"$set":   bson.M{"state": models.BuildStateQueued, "lease_until": leaseUntil},
		"$unset": bson.M{"instance": "", "factory_version": "", "started_at": "", "plan": "", "step_reports": ""},
	}))
}

// buildingOn matches a build while instance builds it.
func buildingOn(minionID uuid.UUID, instance string) bson.M {
	return bson.M{"minion_id": minionID, "state": models.BuildStateBuilding, "instance": instance}
}

func (r *buildRepository) SetPlan(ctx context.Context, minionID uuid.UUID, instance string, plan []models.BuildPlanStep) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, buildingOn(minionID, instance), bson.M{"$set": bson.M{"plan": plan}}))
}

func (r *buildRepository) ReportStep(ctx context.Context, minionID uuid.UUID, instance, step, message string, at time.Time) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, buildingOn(minionID, instance), reportStepUpdate(step, message, at)))
}

// reportStepUpdate is the update of ReportStep. Reports of different steps
// touch different keys, and $min keeps a step's first time whatever the
// order they arrive in.
func reportStepUpdate(step, message string, at time.Time) bson.M {
	key := "step_reports." + step
	u := bson.M{"$min": bson.M{key + ".started_at": at}}
	if message != "" {
		u["$set"] = bson.M{key + ".message": message}
	} else {
		u["$unset"] = bson.M{key + ".message": ""}
	}
	return u
}

func (r *buildRepository) RecordResult(ctx context.Context, minionID uuid.UUID, instance string, artifact models.BuildArtifact, warnings []string, log string, at time.Time) (bool, error) {
	filter := buildingOn(minionID, instance)
	filter["result_received"] = bson.M{"$ne": true}
	set := bson.M{"result_received": true, "result_at": at, "artifact": artifact, "log": log}
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

func (r *buildRepository) Finish(ctx context.Context, minionID uuid.UUID, from []string, out BuildOutcome) (bool, error) {
	return matched(r.coll.UpdateOne(ctx, bson.M{"minion_id": minionID, "state": bson.M{"$in": from}}, finishUpdate(out)))
}

func (r *buildRepository) MarkArtifactDeleted(ctx context.Context, minionID uuid.UUID) error {
	return r.coll.UpdateOne(ctx, bson.M{"minion_id": minionID}, bson.M{"$set": bson.M{"artifact.deleted": true}})
}

func (r *buildRepository) CountFileReferences(ctx context.Context, fileID uuid.UUID) (int64, error) {
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
