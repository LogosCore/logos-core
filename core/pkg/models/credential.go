package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/qiniu/qmgo/field"
)

// CredentialType says what kind of secret a credential is, and so which
// fields it has (credentialTypeSpecs, in credential_fields.go). OTHER keeps
// the shape every credential had before the types had fields of their own: a
// username, a password and named keys.
type CredentialType string

const (
	CredentialTypePlaintext   CredentialType = "PLAINTEXT"
	CredentialTypeKey         CredentialType = "KEY"
	CredentialTypeCertificate CredentialType = "CERTIFICATE"
	CredentialTypeTicket      CredentialType = "TICKET"
	CredentialTypeHash        CredentialType = "HASH"
	CredentialTypeHex         CredentialType = "HEX"
	CredentialTypeOther       CredentialType = "OTHER"
)

// legacyCredentialTypePassword is the one schema-1 type with a lossless
// successor (UpgradeLegacy). The others (SSH_KEY, API_KEY, TOKEN, and HASH,
// whose hash sat in the password) became OTHER, remembering the name.
const legacyCredentialTypePassword CredentialType = "PASSWORD"

// IsValid reports whether the type matches one of the known enum members.
func (t CredentialType) IsValid() bool {
	_, ok := CredentialTypeSpecFor(t)
	return ok
}

// CredentialSchemaVersion is the shape credentials are written in. 2 since
// the types got fields of their own; a row without it (version 1) carries one
// of the old types, and UpgradeLegacy is how it reads in this one. Without
// the marker an old HASH row and a new one would look the same.
const CredentialSchemaVersion = 2

// CredentialValidity records whether the credential has actually been used
// against the target. It is deliberately three-state: before this existed the
// field was a bool, so "nobody has tried it yet" and "tried it, it failed"
// were the same value — and since the list hides what does not work, every
// freshly recorded credential disappeared from the default view.
type CredentialValidity string

const (
	// CredentialValidityUnknown is the default: recorded but not yet tried.
	CredentialValidityUnknown CredentialValidity = "UNKNOWN"
	CredentialValidityValid   CredentialValidity = "VALID"
	CredentialValidityInvalid CredentialValidity = "INVALID"
)

// IsValid reports whether the validity matches one of the known enum members.
func (v CredentialValidity) IsValid() bool {
	switch v {
	case CredentialValidityUnknown,
		CredentialValidityValid,
		CredentialValidityInvalid:
		return true
	}
	return false
}

// CredentialComment is an operator note attached to a credential.
// Stored as an embedded sub-document within Credential (not a separate collection).
type CredentialComment struct {
	CommentID uuid.UUID `bson:"comment_id" json:"comment_id"`
	AuthorID  uuid.UUID `bson:"author_id"  json:"author_id"`
	Text      string    `bson:"text"       json:"text"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

// CredentialKey is a named blob of key material attached to a credential
// (e.g. an SSH private key, an API certificate). Stored as an embedded
// sub-document so a credential can carry several labelled keys without
// spawning a separate collection.
type CredentialKey struct {
	Name    string `bson:"name"    json:"name"`
	Content string `bson:"content" json:"content"`
}

// CredentialProperty is an operator-defined key/value pair carrying ad-hoc
// metadata about a credential — things that don't fit Username/Password/Keys
// but are worth recording (e.g. port=2222, mfa=enabled, source=kerberoast).
//
// Properties are *not* secret material. They render as visible text and are
// included in the credential's search index. If a value is genuinely
// sensitive, it belongs in one of the type's secret fields, which render
// masked. Order matters (operators choose the layout) so this is a slice,
// not a map.
type CredentialProperty struct {
	Name  string `bson:"name"  json:"name"`
	Value string `bson:"value" json:"value"`
}

// Credential is a discovered secret recorded by operators against an operation's
// target systems. Passwords and keys here are *target* secrets — they are
// stored as plain data, not application secrets, and are not redacted.
//
// Which of the typed fields (Username to Keys) a credential has depends on its
// Type: credentialTypeSpecs says which each type takes and requires, and
// CheckFields holds a credential to it. A field the type does not take is
// empty, and the newer ones are left out of the document then.
type Credential struct {
	field.DefaultField `bson:",inline"`
	Rev                int64          `bson:"rev" json:"rev"` // writes counted (repository.revised): a client keeps the higher copy
	CredentialID       uuid.UUID      `bson:"credential_id" json:"credential_id"`
	OperationID        uuid.UUID      `bson:"operation_id"  json:"operation_id"`
	Name               string         `bson:"name"          json:"name"`
	Type               CredentialType `bson:"type"          json:"type"`
	// LegacyType is the schema-1 type a credential the migration moved to
	// OTHER was recorded under (SSH_KEY, TOKEN...): what the UI offers to
	// convert it to. Cleared when its type changes.
	LegacyType CredentialType `bson:"legacy_type,omitempty" json:"legacy_type,omitempty"`
	// SchemaVersion is CredentialSchemaVersion on every row written since
	// the types had fields; repository.Create stamps it.
	SchemaVersion int `bson:"schema_version" json:"schema_version"`

	Username    string               `bson:"username"              json:"username"`
	Realm       string               `bson:"realm,omitempty"       json:"realm,omitempty"`
	Password    string               `bson:"password"              json:"password"`
	Key         string               `bson:"key,omitempty"         json:"key,omitempty"`
	Passphrase  string               `bson:"passphrase,omitempty"  json:"passphrase,omitempty"`
	Certificate string               `bson:"certificate,omitempty" json:"certificate,omitempty"`
	PrivateKey  string               `bson:"private_key,omitempty" json:"private_key,omitempty"`
	Ticket      string               `bson:"ticket,omitempty"      json:"ticket,omitempty"`
	Service     string               `bson:"service,omitempty"     json:"service,omitempty"`
	ExpiresAt   *time.Time           `bson:"expires_at,omitempty"  json:"expires_at,omitempty"`
	Hash        string               `bson:"hash,omitempty"        json:"hash,omitempty"`
	Hex         string               `bson:"hex,omitempty"         json:"hex,omitempty"`
	Algorithm   string               `bson:"algorithm,omitempty"   json:"algorithm,omitempty"`
	Keys        []CredentialKey      `bson:"keys"                  json:"keys"`
	Properties  []CredentialProperty `bson:"properties"            json:"properties"`
	Validity    CredentialValidity   `bson:"validity"              json:"validity"`
	Tags        []string             `bson:"tags"                  json:"tags"`
	Comments    []CredentialComment  `bson:"comments"              json:"comments"`
	CreatedByID uuid.UUID            `bson:"created_by_id"         json:"created_by_id"`
}

// UpgradeLegacy reads a schema-1 credential (one of the old types, each with
// a username, a password and keys) as the current types, the way the startup
// backfill (repository.BackfillCredentialTypes) rewrites it: a PASSWORD with
// a password and no keys is exactly a PLAINTEXT; anything else becomes OTHER,
// which has the old shape, with its old type kept in LegacyType. Reports
// whether c was a schema-1 credential; a current one is left as it is.
func (c *Credential) UpgradeLegacy() bool {
	if c.SchemaVersion >= CredentialSchemaVersion {
		return false
	}
	c.SchemaVersion = CredentialSchemaVersion
	if c.Type == legacyCredentialTypePassword && len(c.Keys) == 0 && c.Password != "" {
		c.Type = CredentialTypePlaintext
		return true
	}
	if c.Type != CredentialTypeOther && c.Type != "" {
		c.LegacyType = c.Type
	}
	c.Type = CredentialTypeOther
	return true
}
