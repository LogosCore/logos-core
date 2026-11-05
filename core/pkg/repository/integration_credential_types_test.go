package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The startup backfill that rewrites credentials recorded under the old types.
// Against a real server, because the filters (a keys array that is missing,
// null or empty; a password that is missing or "") and the pipeline's $cond
// are what can be wrong while the counts still look plausible. And because
// the read-time fallback (models.Credential.UpgradeLegacy) must read every row
// as the backfill then writes it: the test reads each row before and after.
func TestIntegrationBackfillCredentialTypes(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewCredentialRepository(db)
	coll := db.Collection(credentialCollection)
	op := uuid.New()
	edited := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// Rows as an older core wrote them: no schema_version, the old types.
	legacy := func(fields bson.M) uuid.UUID {
		id := uuid.New()
		doc := bson.M{"credential_id": id, "operation_id": op, "name": "n", "rev": int64(3),
			"username": "u", "validity": "UNKNOWN", "properties": bson.A{}, "tags": bson.A{},
			"createAt": edited, "updateAt": edited}
		for k, v := range fields {
			doc[k] = v
		}
		if _, err := coll.InsertOne(ctx, doc); err != nil {
			t.Fatal(err)
		}
		return id
	}
	key := bson.A{bson.M{"name": "id_rsa", "content": "KEY"}}
	rows := map[string]struct {
		id         uuid.UUID
		wantType   models.CredentialType
		wantLegacy models.CredentialType
	}{
		"password, empty keys": {legacy(bson.M{"type": "PASSWORD", "password": "p", "keys": bson.A{}}), models.CredentialTypePlaintext, ""},
		"password, no keys":    {legacy(bson.M{"type": "PASSWORD", "password": "p"}), models.CredentialTypePlaintext, ""},
		"password, null keys":  {legacy(bson.M{"type": "PASSWORD", "password": "p", "keys": nil}), models.CredentialTypePlaintext, ""},
		"password with a key":  {legacy(bson.M{"type": "PASSWORD", "password": "p", "keys": key}), models.CredentialTypeOther, "PASSWORD"},
		"password type, none":  {legacy(bson.M{"type": "PASSWORD", "password": "", "keys": bson.A{}}), models.CredentialTypeOther, "PASSWORD"},
		"password type, unset": {legacy(bson.M{"type": "PASSWORD"}), models.CredentialTypeOther, "PASSWORD"},
		"ssh key":              {legacy(bson.M{"type": "SSH_KEY", "password": "", "keys": key}), models.CredentialTypeOther, "SSH_KEY"},
		"old hash":             {legacy(bson.M{"type": "HASH", "password": "aad3b435", "keys": bson.A{}}), models.CredentialTypeOther, "HASH"},
		"old other":            {legacy(bson.M{"type": "OTHER", "password": "x"}), models.CredentialTypeOther, ""},
		"no type at all":       {legacy(bson.M{"password": "x"}), models.CredentialTypeOther, ""},
	}

	// A current HASH: what the backfill must never take for an old one.
	current := &models.Credential{CredentialID: uuid.New(), OperationID: op, Name: "nt",
		Type: models.CredentialTypeHash, Hash: "31d6cfe0", Keys: []models.CredentialKey{}}
	if err := repo.Create(ctx, current); err != nil {
		t.Fatal(err)
	}

	// Before: the fallback's reading.
	for name, row := range rows {
		c, err := repo.FindByID(ctx, row.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Type != row.wantType || c.LegacyType != row.wantLegacy {
			t.Errorf("%s read before the backfill as %s (legacy %q), want %s (%q)", name, c.Type, c.LegacyType, row.wantType, row.wantLegacy)
		}
	}

	plaintext, other, err := repo.BackfillCredentialTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != 3 || other != 7 {
		t.Errorf("backfill: %d plaintext, %d other; want 3 and 7", plaintext, other)
	}

	// After: what it wrote, stamped, read without the fallback's help.
	for name, row := range rows {
		var raw bson.M
		if err := coll.FindOne(ctx, bson.M{"credential_id": row.id}).One(&raw); err != nil {
			t.Fatal(err)
		}
		legacyType, hasLegacy := raw["legacy_type"]
		if raw["type"] != string(row.wantType) || raw["schema_version"] != int64(models.CredentialSchemaVersion) &&
			raw["schema_version"] != int32(models.CredentialSchemaVersion) {
			t.Errorf("%s: type %v, schema_version %v", name, raw["type"], raw["schema_version"])
		}
		if row.wantLegacy == "" && hasLegacy || row.wantLegacy != "" && legacyType != string(row.wantLegacy) {
			t.Errorf("%s: legacy_type %v (present %v), want %q", name, legacyType, hasLegacy, row.wantLegacy)
		}
		if raw["rev"] != int64(4) {
			t.Errorf("%s: rev %v, want 4", name, raw["rev"])
		}
		if at, _ := raw["updateAt"].(primitive.DateTime); !at.Time().Equal(edited) {
			t.Errorf("%s: updateAt moved to %v: nobody edited it", name, at.Time())
		}
		c, err := repo.FindByID(ctx, row.id)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.CheckFields(); err != nil {
			t.Errorf("%s: rewritten into a credential that does not fit: %v", name, err)
		}
	}
	if c, err := repo.FindByID(ctx, current.CredentialID); err != nil || c.Type != models.CredentialTypeHash || c.Hash != "31d6cfe0" || c.Rev != 1 {
		t.Errorf("the current HASH was touched: %+v %v", c, err)
	}

	// A second boot finds nothing to do.
	if plaintext, other, err := repo.BackfillCredentialTypes(ctx); err != nil || plaintext+other != 0 {
		t.Errorf("second run: %d plaintext, %d other (err %v), want none", plaintext, other, err)
	}
}
