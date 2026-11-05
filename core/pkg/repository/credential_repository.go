package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	opts "github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const credentialCollection = "credentials"

// CredentialSearchField identifies a single Mongo field path that the text
// search may target. The string value is the field path used directly in the
// query, so adding a member is enough to make it searchable.
type CredentialSearchField string

const (
	CredentialSearchFieldName       CredentialSearchField = "name"
	CredentialSearchFieldUsername   CredentialSearchField = "username"
	CredentialSearchFieldRealm      CredentialSearchField = "realm"
	CredentialSearchFieldPassword   CredentialSearchField = "password"
	CredentialSearchFieldHash       CredentialSearchField = "hash"
	CredentialSearchFieldHex        CredentialSearchField = "hex"
	CredentialSearchFieldService    CredentialSearchField = "service"
	CredentialSearchFieldAlgorithm  CredentialSearchField = "algorithm"
	CredentialSearchFieldProperties CredentialSearchField = "properties.value"
)

// defaultCredentialSearchFields is the field set used when the caller does not
// restrict the search: every one-line field and the property values. The
// blobs (key, certificate, private key, ticket) are left out: a search term
// matching inside a kilobyte of base64 is noise, not a find.
var defaultCredentialSearchFields = []CredentialSearchField{
	CredentialSearchFieldName,
	CredentialSearchFieldUsername,
	CredentialSearchFieldRealm,
	CredentialSearchFieldPassword,
	CredentialSearchFieldHash,
	CredentialSearchFieldHex,
	CredentialSearchFieldService,
	CredentialSearchFieldAlgorithm,
	CredentialSearchFieldProperties,
}

// CredentialSortField identifies a Mongo column the credentials list can be
// ordered by. The string value is the field path used in the sort and in the
// keyset cursor filter.
type CredentialSortField string

const (
	CredentialSortFieldCreatedAt CredentialSortField = "createAt"
	CredentialSortFieldName      CredentialSortField = "name"
	CredentialSortFieldUsername  CredentialSortField = "username"
)

// CredentialSort bundles the sort column and direction for the credentials
// list queries. The zero value is NOT valid — use DefaultCredentialSort()
// (createAt descending, the historical order) when the caller doesn't choose.
type CredentialSort struct {
	Field     CredentialSortField
	Ascending bool
}

// DefaultCredentialSort returns the historical list order: newest first.
func DefaultCredentialSort() CredentialSort {
	return CredentialSort{Field: CredentialSortFieldCreatedAt, Ascending: false}
}

// SortKey maps the credential sort to the pagination layer's representation.
// name/username are string columns, so their cursors carry the string sort
// key; createAt keeps the legacy time-keyed cursor shape.
func (s CredentialSort) SortKey() pagination.SortKey {
	return pagination.SortKey{
		Field:     string(s.Field),
		Ascending: s.Ascending,
		String:    s.Field != CredentialSortFieldCreatedAt,
	}
}

// Cursor encodes the edge cursor for a credential row under this sort —
// the value of the active sort column plus the _id tiebreaker.
func (s CredentialSort) Cursor(c *models.Credential) string {
	switch s.Field {
	case CredentialSortFieldName:
		return pagination.EncodeStringCursor(c.Name, c.Id)
	case CredentialSortFieldUsername:
		return pagination.EncodeStringCursor(c.Username, c.Id)
	default:
		return pagination.EncodeCursor(c.CreateAt, c.Id)
	}
}

// CredentialFilter bundles the optional list filters for credentials.
// All fields are independent — combining them ANDs them together at the
// MongoDB query level.
type CredentialFilter struct {
	// Search matches case-insensitively against the fields named in
	// SearchFields (defaulting to name, username, password, and the values of
	// operator-defined properties). Property *names* are intentionally
	// excluded — they're labels, not content, and matching them would surface
	// false positives whenever a generic label like "port" or "url" appears in
	// a query.
	Search string
	// SearchFields restricts which fields Search matches against. Empty means
	// "all default fields" (see defaultCredentialSearchFields). Ignored when
	// Search is empty.
	SearchFields []CredentialSearchField
	// Type, if non-nil, restricts to credentials of this type.
	Type *models.CredentialType
	// Types, if non-empty, restricts to credentials of any of these types
	// (with Type, both apply).
	Types []models.CredentialType
	// Tags, if non-empty, requires every listed tag to be present ($all).
	Tags []string
	// Validity, if non-empty, restricts to credentials in one of these
	// states. Empty means every state — the caller decides what the default
	// view hides, since "not yet tried" and "known not to work" are both
	// real answers and only the second is worth hiding.
	Validity []models.CredentialValidity
}

// ICredentialRepository defines the interface for Credential database operations.
type ICredentialRepository interface {
	Create(ctx context.Context, c *models.Credential) error
	FindByID(ctx context.Context, id uuid.UUID) (models.Credential, error)
	FindByOperationIDWithCursor(ctx context.Context, opID uuid.UUID, filter CredentialFilter, sort CredentialSort, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Credential, error)
	CountByOperationID(ctx context.Context, opID uuid.UUID, filter CredentialFilter) (int64, error)
	DistinctTagsByOperationID(ctx context.Context, opID uuid.UUID) ([]string, error)
	// FindNamesByIDs returns only credential_id and name for the given ids,
	// in one round trip. For naming references without loading secrets.
	FindNamesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Credential, error)
	// FindByIDs returns the full credentials for the given ids in one round
	// trip, in unspecified order — callers that need the caller's ordering
	// index the result themselves. Use FindNamesByIDs instead when only the
	// label is needed, so secrets are not read.
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Credential, error)

	// Multi-operation variants — used by the "global / cross-operation" Findings
	// view where the caller has selected several operations to search across.
	// Each method matches `{operation_id: {$in: opIDs}}` and otherwise behaves
	// identically to its single-op sibling. An empty opIDs slice MUST short-circuit
	// to a no-match result without hitting the DB; callers rely on that to model
	// "explicit empty selection".
	//
	// Index note: the existing {operation_id, -createAt, -_id} compound index
	// supports these queries via index union. Acceptable for moderate fan-out
	// (~tens of ops); revisit for very large op sets.
	FindByOperationIDsWithCursor(ctx context.Context, opIDs []uuid.UUID, filter CredentialFilter, sort CredentialSort, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Credential, error)
	CountByOperationIDs(ctx context.Context, opIDs []uuid.UUID, filter CredentialFilter) (int64, error)
	DistinctTagsByOperationIDs(ctx context.Context, opIDs []uuid.UUID) ([]string, error)

	Update(ctx context.Context, c *models.Credential, updates map[string]interface{}) error
	Delete(ctx context.Context, c *models.Credential) error
	DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error

	// BackfillValidity stamps the three-state validity field on credentials
	// that predate it. Returns the number of rows touched.
	BackfillValidity(ctx context.Context) (int64, error)
	// MarkValid sets a credential of the operation VALID unless it is
	// already, and reports whether it changed: a login with it worked.
	MarkValid(ctx context.Context, operationID, credentialID uuid.UUID) (bool, error)
	// BackfillCredentialTypes rewrites credentials recorded under the old
	// types (schema version 1) as the current ones. Returns how many became
	// PLAINTEXT and how many OTHER.
	BackfillCredentialTypes(ctx context.Context) (plaintext, other int64, err error)

	// Embedded comment operations
	AddComment(ctx context.Context, credentialID uuid.UUID, comment models.CredentialComment) error
	UpdateComment(ctx context.Context, credentialID, commentID uuid.UUID, text string, updatedAt time.Time) error
	RemoveComment(ctx context.Context, credentialID, commentID uuid.UUID) error
}

type credentialRepository struct {
	coll database.Collection
}

func NewCredentialRepository(db database.Database) ICredentialRepository {
	coll := db.Collection(credentialCollection)

	db.EnsureIndexes(context.Background(), credentialCollection, []opts.IndexModel{
		{Key: []string{"credential_id"}, IndexOptions: new(options.IndexOptions).SetUnique(true)},
		{Key: []string{"operation_id"}},
		{Key: []string{"operation_id", "tags"}},
		{Key: []string{"operation_id", "type"}},
		{Key: []string{"operation_id", "validity"}},
		{Key: []string{"operation_id", "-createAt", "-_id"}}, // Supports cursor-based pagination
		// Collated indexes backing the name / username column sorts. The
		// collation must match caseInsensitiveSortCollation exactly or Mongo won't
		// use the index for those queries (it would fall back to an in-memory
		// sort, still correct but slower — result order always follows the
		// query's collation regardless of index use). One index per column
		// serves both directions: a sort that is the exact reverse of the
		// index pattern walks the index backwards, so no -name/-_id mirror
		// indexes are needed.
		{Key: []string{"operation_id", "name", "_id"}, IndexOptions: new(options.IndexOptions).SetCollation(caseInsensitiveSortCollation)},
		{Key: []string{"operation_id", "username", "_id"}, IndexOptions: new(options.IndexOptions).SetCollation(caseInsensitiveSortCollation)},
	})

	return &credentialRepository{coll: coll}
}

// Create writes a new credential. Every row written here is in the current
// shape, so it is stamped so: a row without the stamp is one the startup
// backfill rewrites.
func (r *credentialRepository) Create(ctx context.Context, c *models.Credential) error {
	c.Rev = 1
	c.SchemaVersion = models.CredentialSchemaVersion
	_, err := r.coll.InsertOne(ctx, c)
	return err
}

func (r *credentialRepository) FindByID(ctx context.Context, id uuid.UUID) (models.Credential, error) {
	var c models.Credential
	err := r.coll.FindOne(ctx, bson.M{"credential_id": id}).One(&c)
	if err == nil {
		c.UpgradeLegacy()
	}
	return c, err
}

// upgradeLegacy reads rows the startup backfill has not reached (it failed,
// or an older core wrote them since) as the current types, so an old type
// never reaches a caller. Drop it with the backfill, a release after.
func upgradeLegacy(creds []models.Credential) {
	for i := range creds {
		creds[i].UpgradeLegacy()
	}
}

func (r *credentialRepository) FindByOperationIDWithCursor(ctx context.Context, opID uuid.UUID, filter CredentialFilter, sort CredentialSort, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Credential, error) {
	return r.findWithCursor(ctx, buildCredentialFilter(opID, filter), sort, cursor, limit, forward)
}

// findWithCursor is the shared list body for the single-op and multi-op
// variants: keyset cursor filter + sort derived from the CredentialSort, with
// the case-insensitive collation switched on for string-sorted columns.
func (r *credentialRepository) findWithCursor(ctx context.Context, base bson.M, sort CredentialSort, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Credential, error) {
	key := sort.SortKey()
	if err := key.ValidateCursor(cursor); err != nil {
		return nil, err
	}

	q := r.coll.Find(ctx, pagination.ApplyCursorFilterKey(base, cursor, forward, key))
	if key.String {
		q = q.Collation(caseInsensitiveSortCollation)
	}

	var creds []models.Credential
	err := q.
		Sort(pagination.SortFieldsKey(forward, key)...).
		Limit(limit).
		All(&creds)

	if !forward && len(creds) > 0 {
		for i, j := 0, len(creds)-1; i < j; i, j = i+1, j-1 {
			creds[i], creds[j] = creds[j], creds[i]
		}
	}
	upgradeLegacy(creds)

	return creds, err
}

func (r *credentialRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Credential, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var creds []models.Credential
	if err := r.coll.Find(ctx, bson.M{"credential_id": bson.M{"$in": ids}}).All(&creds); err != nil {
		return nil, fmt.Errorf("find by ids: %w", err)
	}
	upgradeLegacy(creds)
	return creds, nil
}

func (r *credentialRepository) FindNamesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Credential, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var creds []models.Credential
	err := r.coll.Find(ctx, bson.M{"credential_id": bson.M{"$in": ids}}).
		Select(bson.M{"credential_id": 1, "name": 1}).All(&creds)
	if err != nil {
		return nil, fmt.Errorf("find names by ids: %w", err)
	}
	return creds, nil
}

func (r *credentialRepository) CountByOperationID(ctx context.Context, opID uuid.UUID, filter CredentialFilter) (int64, error) {
	return r.coll.Count(ctx, buildCredentialFilter(opID, filter))
}

// DistinctTagsByOperationID returns the deduplicated tag set across all
// credentials in the operation, used to drive the tag autocomplete UI.
func (r *credentialRepository) DistinctTagsByOperationID(ctx context.Context, opID uuid.UUID) ([]string, error) {
	var tags []string
	err := r.coll.Find(ctx, bson.M{"operation_id": opID}).Distinct("tags", &tags)
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// FindByOperationIDsWithCursor lists credentials across multiple operations.
// Returns an empty slice (and no error) when opIDs is empty so callers can
// model "explicit empty selection" without a DB round-trip.
func (r *credentialRepository) FindByOperationIDsWithCursor(ctx context.Context, opIDs []uuid.UUID, filter CredentialFilter, sort CredentialSort, cursor *pagination.Cursor, limit int64, forward bool) ([]models.Credential, error) {
	if len(opIDs) == 0 {
		return []models.Credential{}, nil
	}
	return r.findWithCursor(ctx, buildCredentialFilterMulti(opIDs, filter), sort, cursor, limit, forward)
}

// CountByOperationIDs counts credentials matching the filter across multiple
// operations. Empty opIDs ⇒ 0 with no DB call.
func (r *credentialRepository) CountByOperationIDs(ctx context.Context, opIDs []uuid.UUID, filter CredentialFilter) (int64, error) {
	if len(opIDs) == 0 {
		return 0, nil
	}
	return r.coll.Count(ctx, buildCredentialFilterMulti(opIDs, filter))
}

// DistinctTagsByOperationIDs returns the deduplicated tag set across all
// credentials in the given operations. Empty opIDs ⇒ empty slice, no DB call.
func (r *credentialRepository) DistinctTagsByOperationIDs(ctx context.Context, opIDs []uuid.UUID) ([]string, error) {
	if len(opIDs) == 0 {
		return []string{}, nil
	}
	var tags []string
	err := r.coll.Find(ctx, bson.M{"operation_id": bson.M{"$in": opIDs}}).Distinct("tags", &tags)
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func (r *credentialRepository) Update(ctx context.Context, c *models.Credential, updates map[string]interface{}) error {
	// Defense-in-depth: filter includes operation_id so a resolver bug cannot
	// accidentally mutate a credential belonging to a different operation.
	return r.coll.UpdateOne(ctx,
		bson.M{"credential_id": c.CredentialID, "operation_id": c.OperationID},
		revised(bson.M{"$set": bson.M(updates)}),
	)
}

func (r *credentialRepository) MarkValid(ctx context.Context, operationID, credentialID uuid.UUID) (bool, error) {
	err := r.coll.UpdateOne(ctx,
		bson.M{"credential_id": credentialID, "operation_id": operationID, "validity": bson.M{"$ne": models.CredentialValidityValid}},
		revised(bson.M{"$set": bson.M{"validity": models.CredentialValidityValid}}),
	)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *credentialRepository) Delete(ctx context.Context, c *models.Credential) error {
	return r.coll.Remove(ctx,
		bson.M{"credential_id": c.CredentialID, "operation_id": c.OperationID},
	)
}

func (r *credentialRepository) DeleteByOperationID(ctx context.Context, operationID uuid.UUID) error {
	_, err := r.coll.RemoveAll(ctx, bson.M{"operation_id": operationID})
	return err
}

// AddComment appends a comment to a credential's embedded comments array.
func (r *credentialRepository) AddComment(ctx context.Context, credentialID uuid.UUID, comment models.CredentialComment) error {
	return r.coll.UpdateOne(ctx,
		bson.M{"credential_id": credentialID},
		revised(bson.M{"$push": bson.M{"comments": comment}}),
	)
}

// UpdateComment changes a single comment's text via the positional $ operator.
func (r *credentialRepository) UpdateComment(ctx context.Context, credentialID, commentID uuid.UUID, text string, updatedAt time.Time) error {
	return r.coll.UpdateOne(ctx,
		bson.M{
			"credential_id":       credentialID,
			"comments.comment_id": commentID,
		},
		revised(bson.M{"$set": bson.M{
			"comments.$.text":       text,
			"comments.$.updated_at": updatedAt,
		}}),
	)
}

// RemoveComment pulls a comment out of the embedded comments array.
func (r *credentialRepository) RemoveComment(ctx context.Context, credentialID, commentID uuid.UUID) error {
	return r.coll.UpdateOne(ctx,
		bson.M{"credential_id": credentialID},
		revised(bson.M{"$pull": bson.M{"comments": bson.M{"comment_id": commentID}}}),
	)
}

// buildCredentialFilter composes a MongoDB filter from a CredentialFilter,
// always scoped to the given operation.
func buildCredentialFilter(opID uuid.UUID, f CredentialFilter) bson.M {
	return applyCredentialFilter(bson.M{"operation_id": opID}, f)
}

// buildCredentialFilterMulti is the multi-operation variant of
// buildCredentialFilter. The operation predicate becomes {$in: opIDs} and the
// rest of the filter shape is identical.
func buildCredentialFilterMulti(opIDs []uuid.UUID, f CredentialFilter) bson.M {
	return applyCredentialFilter(bson.M{"operation_id": bson.M{"$in": opIDs}}, f)
}

// applyCredentialFilter layers Type / Validity / Tags / Search constraints
// on top of a base filter (operation predicate). Shared between the single-op
// and multi-op builders.
func applyCredentialFilter(q bson.M, f CredentialFilter) bson.M {
	switch {
	case f.Type != nil && len(f.Types) > 0:
		q["type"] = bson.M{"$eq": *f.Type, "$in": f.Types}
	case f.Type != nil:
		q["type"] = *f.Type
	case len(f.Types) > 0:
		q["type"] = bson.M{"$in": f.Types}
	}
	if len(f.Validity) > 0 {
		q["validity"] = bson.M{"$in": f.Validity}
	}
	if len(f.Tags) > 0 {
		q["tags"] = bson.M{"$all": f.Tags}
	}
	if f.Search != "" {
		rx := bson.M{"$regex": searchPattern(f.Search), "$options": "i"}
		fields := f.SearchFields
		if len(fields) == 0 {
			fields = defaultCredentialSearchFields
		}
		or := make(bson.A, 0, len(fields))
		for _, field := range fields {
			or = append(or, bson.M{string(field): rx})
		}
		q["$or"] = or
	}

	return q
}

// BackfillValidity stamps `validity` on credentials written before the field
// existed, where the state was a bool named `is_valid`.
//
// `true` becomes VALID. `false` becomes UNKNOWN, not INVALID: the bool could
// not say "nobody has tried this yet", and the agent guidance of the time told
// agents to set it true the moment a credential worked — so a false row is
// overwhelmingly untested rather than known-bad. Calling the untested ones
// INVALID would both hide them and assert something false about them, which is
// the failure this field was split up to fix. The handful of genuinely bad
// ones resurface as UNKNOWN and an operator re-marks them in one click.
//
// Idempotent: it only matches rows that have no `validity` yet, so subsequent
// boots update nothing.
func (r *credentialRepository) BackfillValidity(ctx context.Context) (int64, error) {
	var n int64
	for _, step := range []struct {
		legacy bool
		to     models.CredentialValidity
	}{
		{true, models.CredentialValidityValid},
		{false, models.CredentialValidityUnknown},
	} {
		res, err := r.coll.UpdateAll(ctx,
			bson.M{"validity": nil, "is_valid": step.legacy},
			bson.M{
				"$set":   bson.M{"validity": step.to},
				"$unset": bson.M{"is_valid": ""},
			},
		)
		if err != nil {
			return n, fmt.Errorf("backfill validity: %s: %w", step.to, err)
		}
		if res != nil {
			n += res.ModifiedCount
		}
	}
	return n, nil
}

// BackfillCredentialTypes rewrites the credentials recorded before the types
// had fields of their own (no schema_version: the old types PASSWORD, SSH_KEY,
// API_KEY, TOKEN, HASH and OTHER, each a username, a password and keys) as
// the current ones, the way models.Credential.UpgradeLegacy reads them:
//
//   - a PASSWORD with a password and no keys is exactly a PLAINTEXT, and
//     becomes one;
//   - everything else becomes OTHER, which has the old shape, so nothing
//     moves between fields; its old type goes to legacy_type, for whoever
//     converts it (an OTHER stays plain OTHER).
//
// An old HASH goes to OTHER too: its hash sat in the password, and the new
// HASH keeps it in a field of its own. Both steps are one update inside Mongo
// and match only unstamped rows, so a later boot does nothing; the PLAINTEXT
// step runs first, or the second would take its rows too. updateAt stays:
// nobody edited these.
func (r *credentialRepository) BackfillCredentialTypes(ctx context.Context) (plaintext, other int64, err error) {
	old := bson.M{"schema_version": bson.M{"$not": bson.M{"$gte": models.CredentialSchemaVersion}}}

	lossless := bson.M{
		"type":     "PASSWORD",
		"password": bson.M{"$nin": bson.A{nil, ""}},
		"$or":      bson.A{bson.M{"keys": nil}, bson.M{"keys": bson.M{"$size": 0}}},
	}
	for k, v := range old {
		lossless[k] = v
	}
	res, err := r.coll.UpdateAll(ctx, lossless, bson.M{
		"$set": bson.M{"type": models.CredentialTypePlaintext, "schema_version": models.CredentialSchemaVersion},
		"$inc": bson.M{"rev": 1},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("backfill credential types: plaintext: %w", err)
	}
	if res != nil {
		plaintext = res.ModifiedCount
	}

	// A pipeline update, so the old type can be read while it is replaced.
	res, err = r.coll.UpdateAll(ctx, old, bson.A{bson.M{"$set": bson.M{
		"legacy_type": bson.M{"$cond": bson.A{
			bson.M{"$in": bson.A{bson.M{"$ifNull": bson.A{"$type", ""}}, bson.A{string(models.CredentialTypeOther), ""}}},
			"$$REMOVE",
			"$type",
		}},
		"type":           models.CredentialTypeOther,
		"schema_version": models.CredentialSchemaVersion,
		"rev":            bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$rev", 0}}, 1}},
	}}})
	if err != nil {
		return plaintext, 0, fmt.Errorf("backfill credential types: other: %w", err)
	}
	if res != nil {
		other = res.ModifiedCount
	}
	return plaintext, other, nil
}
