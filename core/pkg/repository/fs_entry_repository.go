package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const fsEntryCollection = "fs_entries"

// IFsEntryRepository keeps the file browser's snapshots (models.FsEntry;
// docs: contracts/file-browser.md). Every change takes a time, its task's
// end: an entry takes a change only newer than the one it holds.
type IFsEntryRepository interface {
	// Listing applies a folder's listing at at: the folder listed by ref,
	// its children the entries given (seen), and, when the listing is
	// whole, those it no longer has gone. False when the folder holds a
	// newer listing, and nothing changed.
	Listing(ctx context.Context, op uuid.UUID, machine string, folder models.FsEntry, children []models.FsEntry, whole bool, at time.Time, ref models.FsTaskRef) (bool, error)
	// Gone marks an entry, and what is under it, gone at at.
	Gone(ctx context.Context, op uuid.UUID, machine string, e models.FsEntry, how string, at time.Time, ref models.FsTaskRef) error
	// Transfer records a file a task moved: the entry seen (a file of its
	// size), and the transfer in its downloaded or uploaded.
	Transfer(ctx context.Context, op uuid.UUID, machine string, e models.FsEntry, up bool, t models.FsTransfer, ref models.FsTaskRef) error
	// Ensure makes the entries on the way to one (its parents), as folders,
	// where there are none.
	Ensure(ctx context.Context, op uuid.UUID, machine string, entries []models.FsEntry) error

	Get(ctx context.Context, op uuid.UUID, machine, pathKey string) (*models.FsEntry, error)
	// Children are a folder's entries, a page at a time (FsPage).
	Children(ctx context.Context, op uuid.UUID, machine, parentKey string, q FsQuery) ([]models.FsEntry, string, error)
	// Search is the machine's entries whose names match pattern (a
	// regular expression over the lowercase name), by path, a page at a time.
	Search(ctx context.Context, op uuid.UUID, machine string, pattern string, showDeleted bool, after string, first int64) ([]models.FsEntry, string, error)
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error
}

// FsQuery is how a folder's entries are asked for.
type FsQuery struct {
	// Sort: "name" (default), "size" or "modified"; Desc reverses it.
	// Folders come first either way.
	Sort string
	Desc bool
	// Search keeps the entries whose names hold it, any case.
	Search      string
	ShowDeleted bool
	// FoldersOnly: the folders (the tree).
	FoldersOnly bool
	After       string
	First       int64
}

type fsEntryRepository struct {
	coll database.Collection
}

func NewFsEntryRepository(db database.Database) IFsEntryRepository {
	db.EnsureIndexes(context.Background(), fsEntryCollection, []opts.IndexModel{
		{Key: []string{"operation_id", "machine", "path_key"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id", "machine", "parent_key", "dir_rank", "name_key", "path_key"}},
		{Key: []string{"operation_id", "machine", "parent_key", "dir_rank", "size_key", "path_key"}},
		{Key: []string{"operation_id", "machine", "parent_key", "dir_rank", "mod_key", "path_key"}},
		{Key: []string{"operation_id", "machine", "parent_key", "changed_at"}},
	})
	return &fsEntryRepository{coll: db.Collection(fsEntryCollection)}
}

// identity is what an entry is, set when it is made.
func identity(op uuid.UUID, machine string, e models.FsEntry) bson.M {
	return bson.M{"operation_id": op, "machine": machine, "path": e.Path, "path_key": e.PathKey, "parent": e.Parent,
		"parent_key": e.ParentKey, "name": e.Name, "name_key": e.NameKey, "sep": e.Sep}
}

// olderThan matches an entry holding no change newer than at.
func olderThan(at time.Time) bson.M {
	return bson.M{"$or": bson.A{bson.M{"changed_at": bson.M{"$lt": at}}, bson.M{"changed_at": bson.M{"$exists": false}}}}
}

// described is what a listing entry says of a path.
func described(e models.FsEntry) bson.M {
	size, mod := int64(-1), int64(math.MinInt64)
	if e.Size != nil {
		size = *e.Size
	}
	if e.Modified != nil {
		mod = *e.Modified
	}
	rank := 1
	if e.Type == "dir" {
		rank = 0
	}
	return bson.M{"type": e.Type, "dir_rank": rank, "size": e.Size, "modified": e.Modified, "mode": e.Mode, "owner": e.Owner,
		"group": e.Group, "target": e.Target, "hidden": e.Hidden, "size_key": size, "mod_key": mod, "known": true}
}

func (r *fsEntryRepository) raw() (*mongo.Collection, error) { return r.coll.RawCollection() }

func (r *fsEntryRepository) Listing(ctx context.Context, op uuid.UUID, machine string, folder models.FsEntry, children []models.FsEntry, whole bool, at time.Time, ref models.FsTaskRef) (bool, error) {
	c, err := r.raw()
	if err != nil {
		return false, err
	}
	// The folder, unless a newer listing holds it.
	folderSet := bson.M{"type": "dir", "dir_rank": 0, "known": true, "listed_at": at, "listed_by": ref, "seen_at": at, "seen_by": ref}
	_, err = c.UpdateOne(ctx, bson.M{"operation_id": op, "machine": machine, "path_key": folder.PathKey,
		"$or": bson.A{bson.M{"listed_at": bson.M{"$lt": at}}, bson.M{"listed_at": bson.M{"$exists": false}}}},
		bson.M{"$set": folderSet, "$unset": bson.M{"gone_at": "", "gone_by": "", "gone_how": ""},
			"$setOnInsert": withKeys(identity(op, machine, folder), bson.M{"size_key": int64(-1), "mod_key": int64(math.MinInt64), "changed_at": at})},
		options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("the listed folder: %w", err)
	}

	// Its children: seen at at, unless a newer change holds them.
	for start := 0; start < len(children); start += 1000 {
		var models_ []mongo.WriteModel
		for _, e := range children[start:min(start+1000, len(children))] {
			filter := withKeys(bson.M{"operation_id": op, "machine": machine, "path_key": e.PathKey}, olderThan(at))
			set := withKeys(described(e), bson.M{"changed_at": at, "seen_at": at, "seen_by": ref})
			models_ = append(models_, mongo.NewUpdateOneModel().SetFilter(filter).SetUpsert(true).SetUpdate(bson.M{
				"$set": set, "$unset": bson.M{"gone_at": "", "gone_by": "", "gone_how": ""},
				"$setOnInsert": identity(op, machine, e),
			}))
		}
		if _, err := c.BulkWrite(ctx, models_, options.BulkWrite().SetOrdered(false)); err != nil && !onlyDuplicates(err) {
			return false, fmt.Errorf("the listed entries: %w", err)
		}
	}
	// What it no longer has: gone, unless a newer change holds it. A
	// listing cut short says nothing of what it lacks.
	if !whole {
		return true, nil
	}
	_, err = c.UpdateMany(ctx, bson.M{"operation_id": op, "machine": machine, "parent_key": folder.PathKey,
		"changed_at": bson.M{"$lt": at}, "gone_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"gone_at": at, "gone_by": ref, "gone_how": models.FsGoneNotListed, "changed_at": at}})
	if err != nil {
		return false, fmt.Errorf("the entries no longer listed: %w", err)
	}
	return true, nil
}

// onlyDuplicates: a bulk write's errors are all duplicate keys (an upsert
// whose entry holds a newer change: nothing to do).
func onlyDuplicates(err error) bool {
	var bwe mongo.BulkWriteException
	if !errors.As(err, &bwe) || bwe.WriteConcernError != nil {
		return false
	}
	for _, we := range bwe.WriteErrors {
		if we.Code != 11000 {
			return false
		}
	}
	return true
}

func withKeys(a, b bson.M) bson.M {
	out := bson.M{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func (r *fsEntryRepository) Gone(ctx context.Context, op uuid.UUID, machine string, e models.FsEntry, how string, at time.Time, ref models.FsTaskRef) error {
	c, err := r.raw()
	if err != nil {
		return err
	}
	set := bson.M{"gone_at": at, "gone_by": ref, "gone_how": how, "changed_at": at}
	_, err = c.UpdateOne(ctx, withKeys(bson.M{"operation_id": op, "machine": machine, "path_key": e.PathKey}, olderThan(at)),
		bson.M{"$set": set, "$setOnInsert": withKeys(identity(op, machine, e),
			bson.M{"type": "other", "dir_rank": 1, "size_key": int64(-1), "mod_key": int64(math.MinInt64)})},
		options.Update().SetUpsert(true))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	// What is under it: the paths that start with it and a separator (a
	// root ends with one already).
	under := e.PathKey
	if !strings.HasSuffix(under, e.Sep) {
		under += e.Sep
	}
	prefix := "^" + regexp.QuoteMeta(under)
	_, err = c.UpdateMany(ctx, bson.M{"operation_id": op, "machine": machine, "path_key": bson.M{"$regex": prefix},
		"gone_at": bson.M{"$exists": false}, "changed_at": bson.M{"$lt": at}}, bson.M{"$set": set})
	return err
}

func (r *fsEntryRepository) Transfer(ctx context.Context, op uuid.UUID, machine string, e models.FsEntry, up bool, t models.FsTransfer, ref models.FsTaskRef) error {
	c, err := r.raw()
	if err != nil {
		return err
	}
	size := t.Size
	e.Type, e.Size = "file", &size
	set := withKeys(described(e), bson.M{"changed_at": t.At, "seen_at": t.At, "seen_by": ref})
	delete(set, "modified")
	delete(set, "mod_key")
	delete(set, "mode")
	delete(set, "owner")
	delete(set, "group")
	delete(set, "hidden")
	_, err = c.UpdateOne(ctx, withKeys(bson.M{"operation_id": op, "machine": machine, "path_key": e.PathKey}, olderThan(t.At)),
		bson.M{"$set": set, "$unset": bson.M{"gone_at": "", "gone_by": "", "gone_how": ""},
			"$setOnInsert": withKeys(identity(op, machine, e), bson.M{"mod_key": int64(math.MinInt64)})},
		options.Update().SetUpsert(true))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	field := "downloaded"
	if up {
		field = "uploaded"
	}
	_, err = c.UpdateOne(ctx, bson.M{"operation_id": op, "machine": machine, "path_key": e.PathKey},
		bson.M{"$push": bson.M{field: t}})
	return err
}

func (r *fsEntryRepository) Ensure(ctx context.Context, op uuid.UUID, machine string, entries []models.FsEntry) error {
	if len(entries) == 0 {
		return nil
	}
	c, err := r.raw()
	if err != nil {
		return err
	}
	var models_ []mongo.WriteModel
	for _, e := range entries {
		models_ = append(models_, mongo.NewUpdateOneModel().SetUpsert(true).
			SetFilter(bson.M{"operation_id": op, "machine": machine, "path_key": e.PathKey}).
			SetUpdate(bson.M{"$setOnInsert": withKeys(identity(op, machine, e), bson.M{"type": "dir", "dir_rank": 0,
				"size_key": int64(-1), "mod_key": int64(math.MinInt64), "changed_at": time.Time{}})}))
	}
	_, err = c.BulkWrite(ctx, models_, options.BulkWrite().SetOrdered(false))
	if err != nil && !onlyDuplicates(err) {
		return err
	}
	return nil
}

func (r *fsEntryRepository) Get(ctx context.Context, op uuid.UUID, machine, pathKey string) (*models.FsEntry, error) {
	var e models.FsEntry
	if err := r.coll.Find(ctx, bson.M{"operation_id": op, "machine": machine, "path_key": pathKey}).One(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// fsCursor is where a page ends: the last entry's sort keys.
type fsCursor struct {
	Rank int    `json:"r"`
	Name string `json:"n,omitempty"`
	Num  int64  `json:"k,omitempty"`
	Path string `json:"p"`
}

func encodeFsCursor(c fsCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeFsCursor(s string) (*fsCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c fsCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *fsEntryRepository) Children(ctx context.Context, op uuid.UUID, machine, parentKey string, q FsQuery) ([]models.FsEntry, string, error) {
	filter := bson.M{"operation_id": op, "machine": machine, "parent_key": parentKey}
	if !q.ShowDeleted {
		filter["gone_at"] = bson.M{"$exists": false}
	}
	if q.Search != "" {
		filter["name_key"] = bson.M{"$regex": regexp.QuoteMeta(strings.ToLower(q.Search))}
	}
	if q.FoldersOnly {
		filter["dir_rank"] = 0
	}
	field := map[string]string{"size": "size_key", "modified": "mod_key"}[q.Sort]
	if field == "" {
		field = "name_key"
	}
	dir, op2 := 1, "$gt"
	if q.Desc {
		dir, op2 = -1, "$lt"
	}
	if q.After != "" {
		c, err := decodeFsCursor(q.After)
		if err != nil {
			return nil, "", fmt.Errorf("a bad cursor")
		}
		var k any = c.Name
		if field != "name_key" {
			k = c.Num
		}
		filter["$or"] = bson.A{
			bson.M{"dir_rank": bson.M{"$gt": c.Rank}},
			bson.M{"dir_rank": c.Rank, field: bson.M{op2: k}},
			bson.M{"dir_rank": c.Rank, field: k, "path_key": bson.M{op2: c.Path}},
		}
	}
	first := q.First
	if first <= 0 || first > 500 {
		first = 200
	}
	var out []models.FsEntry
	sortKeys := []string{"dir_rank", sign(dir) + field, sign(dir) + "path_key"}
	if err := r.coll.Find(ctx, filter).Sort(sortKeys...).Limit(first + 1).All(&out); err != nil {
		return nil, "", err
	}
	next := ""
	if int64(len(out)) > first {
		out = out[:first]
		last := out[len(out)-1]
		c := fsCursor{Rank: last.DirRank, Path: last.PathKey}
		switch field {
		case "size_key":
			c.Num = last.SizeKey
		case "mod_key":
			c.Num = last.ModKey
		default:
			c.Name = last.NameKey
		}
		next = encodeFsCursor(c)
	}
	return out, next, nil
}

func sign(dir int) string {
	if dir < 0 {
		return "-"
	}
	return ""
}

func (r *fsEntryRepository) Search(ctx context.Context, op uuid.UUID, machine string, pattern string, showDeleted bool, after string, first int64) ([]models.FsEntry, string, error) {
	filter := bson.M{"operation_id": op, "machine": machine, "name_key": bson.M{"$regex": pattern}}
	if !showDeleted {
		filter["gone_at"] = bson.M{"$exists": false}
	}
	if after != "" {
		c, err := decodeFsCursor(after)
		if err != nil {
			return nil, "", fmt.Errorf("a bad cursor")
		}
		filter["path_key"] = bson.M{"$gt": c.Path}
	}
	if first <= 0 || first > 500 {
		first = 200
	}
	var out []models.FsEntry
	if err := r.coll.Find(ctx, filter).Sort("path_key").Limit(first + 1).All(&out); err != nil {
		return nil, "", err
	}
	next := ""
	if int64(len(out)) > first {
		out = out[:first]
		next = encodeFsCursor(fsCursor{Path: out[len(out)-1].PathKey})
	}
	return out, next, nil
}

func (r *fsEntryRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}
