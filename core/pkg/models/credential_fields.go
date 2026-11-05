package models

import (
	"fmt"
	"strings"
	"time"
)

// CredentialField names one of a credential's typed fields by the name it is
// stored under, which is also what the agent tools call it.
type CredentialField string

const (
	CredentialFieldUsername    CredentialField = "username"
	CredentialFieldRealm       CredentialField = "realm"
	CredentialFieldPassword    CredentialField = "password"
	CredentialFieldKey         CredentialField = "key"
	CredentialFieldPassphrase  CredentialField = "passphrase"
	CredentialFieldCertificate CredentialField = "certificate"
	CredentialFieldPrivateKey  CredentialField = "private_key"
	CredentialFieldTicket      CredentialField = "ticket"
	CredentialFieldService     CredentialField = "service"
	CredentialFieldExpiresAt   CredentialField = "expires_at"
	CredentialFieldHash        CredentialField = "hash"
	CredentialFieldHex         CredentialField = "hex"
	CredentialFieldAlgorithm   CredentialField = "algorithm"
	CredentialFieldKeys        CredentialField = "keys"
)

// CredentialFields is every typed field, in the order they are listed when
// one type's are not.
var CredentialFields = []CredentialField{
	CredentialFieldUsername, CredentialFieldRealm, CredentialFieldPassword,
	CredentialFieldKey, CredentialFieldPassphrase, CredentialFieldCertificate,
	CredentialFieldPrivateKey, CredentialFieldTicket, CredentialFieldService,
	CredentialFieldExpiresAt, CredentialFieldHash, CredentialFieldHex,
	CredentialFieldAlgorithm, CredentialFieldKeys,
}

// CredentialFieldKind says how a field is typed in and shown.
type CredentialFieldKind string

const (
	// CredentialFieldKindText is one line of plain text: a username, a realm.
	CredentialFieldKindText CredentialFieldKind = "TEXT"
	// CredentialFieldKindSecret is one line of secret material: a password,
	// a hash. Shown in monospace to copy, not masked: these are records of
	// the operation, like every other value here.
	CredentialFieldKindSecret CredentialFieldKind = "SECRET"
	// CredentialFieldKindBlob is secret material of many lines: a key, a
	// certificate, a ticket.
	CredentialFieldKindBlob CredentialFieldKind = "BLOB"
	// CredentialFieldKindDateTime is a moment: when a ticket expires.
	CredentialFieldKindDateTime CredentialFieldKind = "DATETIME"
	// CredentialFieldKindKeyList is OTHER's list of named keys.
	CredentialFieldKindKeyList CredentialFieldKind = "KEY_LIST"
)

// CredentialFieldSpec is one field of a credential type.
type CredentialFieldSpec struct {
	Field    CredentialField
	Label    string
	Kind     CredentialFieldKind
	Required bool
	// Hint is what a form shows in the field while it is empty.
	Hint string
}

// CredentialTypeSpec is a credential type and the fields it takes, in the
// order forms and write-outs list them.
type CredentialTypeSpec struct {
	Type        CredentialType
	Label       string
	Description string
	Fields      []CredentialFieldSpec
}

// Takes reports whether the type has field f.
func (s CredentialTypeSpec) Takes(f CredentialField) bool {
	for _, field := range s.Fields {
		if field.Field == f {
			return true
		}
	}
	return false
}

// FieldNames lists the type's fields for a message: "username, key,
// passphrase".
func (s CredentialTypeSpec) FieldNames() string {
	names := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		names[i] = string(f.Field)
	}
	return strings.Join(names, ", ")
}

func usernameField() CredentialFieldSpec {
	return CredentialFieldSpec{Field: CredentialFieldUsername, Label: "Username", Kind: CredentialFieldKindText}
}

func realmField() CredentialFieldSpec {
	return CredentialFieldSpec{Field: CredentialFieldRealm, Label: "Realm/Domain", Kind: CredentialFieldKindText, Hint: "e.g. CORP"}
}

// credentialTypeSpecs is the one table of which fields each type takes: the
// API and the agent tools check against it, the UI builds its forms from it
// (the credentialTypes query), and the exports write fields out by it.
var credentialTypeSpecs = []CredentialTypeSpec{
	{
		Type: CredentialTypePlaintext, Label: "Plaintext",
		Description: "A password in the clear.",
		Fields: []CredentialFieldSpec{
			usernameField(), realmField(),
			{Field: CredentialFieldPassword, Label: "Password", Kind: CredentialFieldKindSecret, Required: true},
		},
	},
	{
		Type: CredentialTypeKey, Label: "Key",
		Description: "A private key, such as an SSH key.",
		Fields: []CredentialFieldSpec{
			usernameField(),
			{Field: CredentialFieldKey, Label: "Key", Kind: CredentialFieldKindBlob, Required: true, Hint: "-----BEGIN OPENSSH PRIVATE KEY-----"},
			{Field: CredentialFieldPassphrase, Label: "Passphrase", Kind: CredentialFieldKindSecret},
		},
	},
	{
		Type: CredentialTypeCertificate, Label: "Certificate",
		Description: "A client certificate, with its private key or its PFX password.",
		Fields: []CredentialFieldSpec{
			usernameField(), realmField(),
			{Field: CredentialFieldCertificate, Label: "Certificate", Kind: CredentialFieldKindBlob, Required: true, Hint: "PEM, or a PFX as base64"},
			{Field: CredentialFieldPrivateKey, Label: "Private key", Kind: CredentialFieldKindBlob, Hint: "PEM"},
			{Field: CredentialFieldPassword, Label: "Password", Kind: CredentialFieldKindSecret, Hint: "The PFX's"},
		},
	},
	{
		Type: CredentialTypeTicket, Label: "Ticket",
		Description: "A Kerberos ticket, as kirbi or ccache.",
		Fields: []CredentialFieldSpec{
			usernameField(), realmField(),
			{Field: CredentialFieldTicket, Label: "Ticket", Kind: CredentialFieldKindBlob, Required: true, Hint: "kirbi or ccache, as base64"},
			{Field: CredentialFieldService, Label: "Service", Kind: CredentialFieldKindText, Hint: "e.g. cifs/dc01.corp.local"},
			{Field: CredentialFieldExpiresAt, Label: "Expires at", Kind: CredentialFieldKindDateTime},
		},
	},
	{
		Type: CredentialTypeHash, Label: "Hash",
		Description: "A hash that opens something by itself, to pass rather than crack.",
		Fields: []CredentialFieldSpec{
			usernameField(), realmField(),
			{Field: CredentialFieldHash, Label: "Hash", Kind: CredentialFieldKindSecret, Required: true},
			{Field: CredentialFieldAlgorithm, Label: "Algorithm", Kind: CredentialFieldKindText, Hint: "e.g. NTLM"},
		},
	},
	{
		Type: CredentialTypeHex, Label: "Hex",
		Description: "Raw key material in hex, such as a Kerberos AES key.",
		Fields: []CredentialFieldSpec{
			usernameField(), realmField(),
			{Field: CredentialFieldHex, Label: "Value", Kind: CredentialFieldKindSecret, Required: true, Hint: "Hex digits"},
			{Field: CredentialFieldAlgorithm, Label: "Algorithm", Kind: CredentialFieldKindText, Hint: "e.g. aes256-cts-hmac-sha1-96"},
		},
	},
	{
		Type: CredentialTypeOther, Label: "Other",
		Description: "Anything else: a password and any number of named keys.",
		Fields: []CredentialFieldSpec{
			usernameField(),
			{Field: CredentialFieldPassword, Label: "Password", Kind: CredentialFieldKindSecret},
			{Field: CredentialFieldKeys, Label: "Keys", Kind: CredentialFieldKindKeyList},
		},
	},
}

// CredentialTypeSpecs is every type with its fields, in the order they are
// offered.
func CredentialTypeSpecs() []CredentialTypeSpec { return credentialTypeSpecs }

// CredentialTypeSpecFor is type t's fields; false for a type that is not one.
func CredentialTypeSpecFor(t CredentialType) (CredentialTypeSpec, bool) {
	for _, s := range credentialTypeSpecs {
		if s.Type == t {
			return s, true
		}
	}
	return CredentialTypeSpec{}, false
}

// CredentialTypeNames lists the types for a message: "PLAINTEXT, KEY, ...".
func CredentialTypeNames() string {
	names := make([]string, len(credentialTypeSpecs))
	for i, s := range credentialTypeSpecs {
		names[i] = string(s.Type)
	}
	return strings.Join(names, ", ")
}

// FieldString is the value of one of c's one-value fields; "" for
// expires_at and keys, which are not strings.
func (c *Credential) FieldString(f CredentialField) string {
	switch f {
	case CredentialFieldUsername:
		return c.Username
	case CredentialFieldRealm:
		return c.Realm
	case CredentialFieldPassword:
		return c.Password
	case CredentialFieldKey:
		return c.Key
	case CredentialFieldPassphrase:
		return c.Passphrase
	case CredentialFieldCertificate:
		return c.Certificate
	case CredentialFieldPrivateKey:
		return c.PrivateKey
	case CredentialFieldTicket:
		return c.Ticket
	case CredentialFieldService:
		return c.Service
	case CredentialFieldHash:
		return c.Hash
	case CredentialFieldHex:
		return c.Hex
	case CredentialFieldAlgorithm:
		return c.Algorithm
	}
	return ""
}

// HasField reports whether c holds a value in f.
func (c *Credential) HasField(f CredentialField) bool {
	switch f {
	case CredentialFieldExpiresAt:
		return c.ExpiresAt != nil
	case CredentialFieldKeys:
		return len(c.Keys) > 0
	}
	return c.FieldString(f) != ""
}

// ClearField empties f, and returns the value that stores it empty.
func (c *Credential) ClearField(f CredentialField) any {
	switch f {
	case CredentialFieldUsername:
		c.Username = ""
	case CredentialFieldRealm:
		c.Realm = ""
	case CredentialFieldPassword:
		c.Password = ""
	case CredentialFieldKey:
		c.Key = ""
	case CredentialFieldPassphrase:
		c.Passphrase = ""
	case CredentialFieldCertificate:
		c.Certificate = ""
	case CredentialFieldPrivateKey:
		c.PrivateKey = ""
	case CredentialFieldTicket:
		c.Ticket = ""
	case CredentialFieldService:
		c.Service = ""
	case CredentialFieldExpiresAt:
		c.ExpiresAt = nil
		return nil
	case CredentialFieldHash:
		c.Hash = ""
	case CredentialFieldHex:
		c.Hex = ""
	case CredentialFieldAlgorithm:
		c.Algorithm = ""
	case CredentialFieldKeys:
		c.Keys = []CredentialKey{}
		return []CredentialKey{}
	}
	return ""
}

// FieldsOutside lists the fields c holds a value in that spec does not take.
func (c *Credential) FieldsOutside(spec CredentialTypeSpec) []CredentialField {
	var out []CredentialField
	for _, f := range CredentialFields {
		if c.HasField(f) && !spec.Takes(f) {
			out = append(out, f)
		}
	}
	return out
}

// CheckFields reports whether c fits its type: a type that is one, a value
// in each field the type requires, none in a field it does not take, and hex
// digits in hex.
func (c *Credential) CheckFields() error {
	spec, ok := CredentialTypeSpecFor(c.Type)
	if !ok {
		return fmt.Errorf("type %q is not one of %s", c.Type, CredentialTypeNames())
	}
	if extra := c.FieldsOutside(spec); len(extra) > 0 {
		return FieldsNotTakenError(spec, extra)
	}
	for _, f := range spec.Fields {
		if f.Required && !c.HasField(f.Field) {
			return NeedsFieldError(spec, f.Field)
		}
	}
	if c.Hex != "" {
		if _, err := NormalizeCredentialHex(c.Hex); err != nil {
			return err
		}
	}
	return nil
}

// FieldsNotTakenError is the refusal for values in fields a type has not.
func FieldsNotTakenError(spec CredentialTypeSpec, fields []CredentialField) error {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = string(f)
	}
	return fmt.Errorf("%s %s credential has no %s (it takes %s)",
		article(spec.Type), spec.Type, strings.Join(names, ", "), spec.FieldNames())
}

// NeedsFieldError is the refusal for a required field left empty.
func NeedsFieldError(spec CredentialTypeSpec, f CredentialField) error {
	return fmt.Errorf("%s %s credential needs a %s", article(spec.Type), spec.Type, f)
}

// article is "an" before OTHER, "a" before the rest.
func article(t CredentialType) string {
	if t != "" && strings.ContainsRune("AEIOU", rune(t[0])) {
		return "an"
	}
	return "a"
}

// NormalizeCredentialHex is a hex value as stored: whitespace and colons
// (a pasted dump, aa:bb:cc) and a leading 0x taken out. Refused unless what
// is left is hex digits making whole bytes.
func NormalizeCredentialHex(v string) (string, error) {
	compact := strings.Map(func(r rune) rune {
		if r == ':' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
	if len(compact) > 1 && (compact[:2] == "0x" || compact[:2] == "0X") {
		compact = compact[2:]
	}
	if compact == "" {
		return "", nil
	}
	for _, r := range compact {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return "", fmt.Errorf("hex %q is not hex digits", clipForMessage(v))
		}
	}
	if len(compact)%2 != 0 {
		return "", fmt.Errorf("hex %q has an odd number of digits: whole bytes take two each", clipForMessage(v))
	}
	return compact, nil
}

// clipForMessage keeps a refused value short enough to quote.
func clipForMessage(v string) string {
	const most = 40
	if r := []rune(v); len(r) > most {
		return string(r[:most]) + "…"
	}
	return v
}

// CredentialFieldValue is one of a credential's fields holding a value, as an
// export writes it out.
type CredentialFieldValue struct {
	Spec  CredentialFieldSpec
	Value string
}

// FilledFields lists the fields of c's type that hold a value, in the type's
// order, expires_at in RFC 3339. Keys are not among them: they are a list,
// c.Keys, written out one by one.
func (c *Credential) FilledFields() []CredentialFieldValue {
	spec, ok := CredentialTypeSpecFor(c.Type)
	if !ok {
		// Not a type (a record from outside, never upgraded): written out as
		// the old shape, which OTHER has, rather than as nothing.
		spec, _ = CredentialTypeSpecFor(CredentialTypeOther)
	}
	var out []CredentialFieldValue
	for _, f := range spec.Fields {
		var value string
		switch f.Kind {
		case CredentialFieldKindKeyList:
			continue
		case CredentialFieldKindDateTime:
			if c.ExpiresAt == nil {
				continue
			}
			value = c.ExpiresAt.UTC().Format(time.RFC3339)
		default:
			value = c.FieldString(f.Field)
		}
		if value != "" {
			out = append(out, CredentialFieldValue{Spec: f, Value: value})
		}
	}
	return out
}
