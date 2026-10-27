package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo/field"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
)

// Module management storage: desired property values per instance, the audit
// trail of changes to them, and action invocations. The manifest itself lives
// on the registry row (Module.Manifest). Wire types are in
// logos-golang-protocol/protocol/management.go.

// Action invocation states.
const (
	ModuleActionRunning   = "RUNNING"
	ModuleActionSucceeded = "SUCCEEDED"
	ModuleActionFailed    = "FAILED"
	ModuleActionTimedOut  = "TIMED_OUT"
)

// RedactedValue replaces secret values in the audit trail and stored params.
const RedactedValue = "***"

// ModuleActor identifies who made a management change. Mirrors protocol.Actor.
type ModuleActor struct {
	Type string `bson:"type" json:"type"` // user | agent | system
	ID   string `bson:"id" json:"id"`
	Name string `bson:"name,omitempty" json:"name,omitempty"`
}

// Protocol converts to the wire actor.
func (a ModuleActor) Protocol() *protocol.Actor {
	return &protocol.Actor{Type: a.Type, ID: a.ID, Name: a.Name}
}

// ModuleConfig is the desired property values of one instance: one document
// per instance. Config is per instance only — there is no shared module-level
// config. Revision grows with every write and never goes back.
//
// TODO(module-mgmt): secret property values are stored here in clear text for
// the prototype. Encrypt them at rest (envelope encryption keyed off a core
// secret) before this leaves prototype status. They are never returned over
// GraphQL — see resolver/module_management_resolver.go.
type ModuleConfig struct {
	field.DefaultField `bson:",inline"`

	Instance   string      `bson:"instance" json:"instance"`
	ModuleName string      `bson:"module_name" json:"module_name"`
	Values     JSONMap     `bson:"values" json:"values"`
	Revision   int64       `bson:"revision" json:"revision"`
	UpdatedBy  ModuleActor `bson:"updated_by" json:"updated_by"`
	UpdatedAt  time.Time   `bson:"updated_at" json:"updated_at"`
}

// ModuleConfigKeyChange is one key's old/new value in an audit entry. Secret
// values are RedactedValue (or nil when unset).
type ModuleConfigKeyChange struct {
	Key string `bson:"key" json:"key"`
	Old any    `bson:"old" json:"old"`
	New any    `bson:"new" json:"new"`
}

// ModuleConfigChange is an audit entry: one successful updateModuleConfig of
// one instance's config.
type ModuleConfigChange struct {
	field.DefaultField `bson:",inline"`

	ChangeID   uuid.UUID `bson:"change_id" json:"change_id"`
	Instance   string    `bson:"instance" json:"instance"`
	ModuleName string    `bson:"module_name" json:"module_name"`
	Revision   int64     `bson:"revision" json:"revision"`
	// Changes is stored as JSON-shaped BSON (see JSONMap) so old/new values
	// read back as plain JSON types.
	Changes ModuleConfigKeyChanges `bson:"changes" json:"changes"`
	Actor   ModuleActor            `bson:"actor" json:"actor"`
	At      time.Time              `bson:"at" json:"at"`
}

// ModuleConfigKeyChanges is a list of key changes with JSON-faithful BSON
// encoding (wrapped in a document, since BSON's top level must be one).
type ModuleConfigKeyChanges []ModuleConfigKeyChange

func (c ModuleConfigKeyChanges) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if c == nil {
		c = ModuleConfigKeyChanges{}
	}
	return jsonToBSONValue([]ModuleConfigKeyChange(c))
}

func (c *ModuleConfigKeyChanges) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	var out []ModuleConfigKeyChange
	if err := bsonValueToJSON(t, data, &out); err != nil {
		return err
	}
	*c = out
	return nil
}

// ModuleActionInvocation is one run of a declared action.
type ModuleActionInvocation struct {
	field.DefaultField `bson:",inline"`

	InvocationID InvocationID `bson:"invocation_id" json:"invocation_id"`
	Instance     string       `bson:"instance" json:"instance"`
	ModuleName   string       `bson:"module_name" json:"module_name"`
	Action       string       `bson:"action" json:"action"`
	// Params as sent, with secret params replaced by RedactedValue.
	Params JSONMap `bson:"params,omitempty" json:"params,omitempty"`
	Status string  `bson:"status" json:"status"`
	// ResultJSON is the module's result as raw JSON. Kept as a string: it is
	// module-defined and arbitrary, and a document could carry "$"-prefixed keys
	// BSON would misread.
	ResultJSON string      `bson:"result,omitempty" json:"-"`
	Error      string      `bson:"error,omitempty" json:"error,omitempty"`
	Actor      ModuleActor `bson:"actor" json:"actor"`
	StartedAt  time.Time   `bson:"started_at" json:"started_at"`
	FinishedAt *time.Time  `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
}

// Result decodes ResultJSON; nil when empty or undecodable.
func (m *ModuleActionInvocation) Result() any {
	if m.ResultJSON == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(m.ResultJSON), &v); err != nil {
		return nil
	}
	return v
}

// ModuleManifest is the stored management manifest. It is encoded through
// JSON so that defaults/bounds (typed `any`) round-trip as plain JSON values —
// float64, string, bool, []any — exactly as the module sent them, instead of
// coming back as BSON-specific types (int32, primitive.A, primitive.D) that
// protocol.ValidateValue would reject.
type ModuleManifest struct {
	protocol.ManagementManifest
}

func (m ModuleManifest) MarshalBSON() ([]byte, error) {
	return jsonToBSONDoc(m.ManagementManifest)
}

func (m *ModuleManifest) UnmarshalBSON(data []byte) error {
	return bsonDocToJSON(data, &m.ManagementManifest)
}

// ModuleConnectionSchema is the stored minion connection schema, JSON-encoded
// for the same reason as ModuleManifest.
type ModuleConnectionSchema struct {
	protocol.ConnectionSchema
}

func (m ModuleConnectionSchema) MarshalBSON() ([]byte, error) {
	return jsonToBSONDoc(m.ConnectionSchema)
}

func (m *ModuleConnectionSchema) UnmarshalBSON(data []byte) error {
	return bsonDocToJSON(data, &m.ConnectionSchema)
}

// ModuleBuildSchema is the stored build schema of a factory, JSON-encoded
// like the manifest so it round-trips exactly.
type ModuleBuildSchema struct {
	protocol.BuildSchema
}

func (m ModuleBuildSchema) MarshalBSON() ([]byte, error) {
	return jsonToBSONDoc(m.BuildSchema)
}

func (m *ModuleBuildSchema) UnmarshalBSON(data []byte) error {
	return bsonDocToJSON(data, &m.BuildSchema)
}

// JSONMap is a string-keyed map of JSON values with the same JSON-faithful BSON
// encoding as ModuleManifest. Used for property values and action params.
type JSONMap map[string]any

func (m JSONMap) MarshalBSON() ([]byte, error) {
	if m == nil {
		return jsonToBSONDoc(map[string]any{})
	}
	return jsonToBSONDoc(map[string]any(m))
}

func (m *JSONMap) UnmarshalBSON(data []byte) error {
	out := map[string]any{}
	if err := bsonDocToJSON(data, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

// jsonToBSONDoc converts a JSON-marshallable object to a BSON document via
// relaxed Extended JSON. Integers become int32/int64, other numbers doubles.
func jsonToBSONDoc(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode as json: %w", err)
	}
	var raw bson.Raw
	if err := bson.UnmarshalExtJSON(data, false, &raw); err != nil {
		return nil, fmt.Errorf("json to bson: %w", err)
	}
	return raw, nil
}

// bsonDocToJSON is the inverse of jsonToBSONDoc: BSON → relaxed Extended JSON
// → out. Numbers decode as float64, arrays as []any, documents as maps.
func bsonDocToJSON(data []byte, out any) error {
	j, err := bson.MarshalExtJSON(bson.Raw(data), false, false)
	if err != nil {
		return fmt.Errorf("bson to json: %w", err)
	}
	if err := json.NewDecoder(bytes.NewReader(j)).Decode(out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

// jsonToBSONValue encodes any JSON value (including arrays) as a BSON value by
// wrapping it in a one-key document and extracting the element.
func jsonToBSONValue(v any) (bsontype.Type, []byte, error) {
	doc, err := jsonToBSONDoc(map[string]any{"v": v})
	if err != nil {
		return 0, nil, err
	}
	rv, err := bson.Raw(doc).LookupErr("v")
	if err != nil {
		return 0, nil, err
	}
	return rv.Type, rv.Value, nil
}

// bsonValueToJSON is the inverse of jsonToBSONValue.
func bsonValueToJSON(t bsontype.Type, data []byte, out any) error {
	if t == bson.TypeNull || t == bson.TypeUndefined {
		return nil
	}
	doc, err := bson.Marshal(bson.D{{Key: "v", Value: bson.RawValue{Type: t, Value: data}}})
	if err != nil {
		return err
	}
	var wrapper struct {
		V json.RawMessage `json:"v"`
	}
	if err := bsonDocToJSON(doc, &wrapper); err != nil {
		return err
	}
	return json.Unmarshal(wrapper.V, out)
}

// InvocationID is an action invocation's id: a UUIDv4, stored as binary like
// every UUID. Invocations recorded before ids were UUIDs hold a ULID string,
// which reads as the UUID of the same 128 bits: the value the startup
// conversion stores in its place, so a row keeps its id across it.
type InvocationID uuid.UUID

// NewInvocationID returns a fresh invocation id.
func NewInvocationID() InvocationID { return InvocationID(uuid.New()) }

func (id InvocationID) String() string { return uuid.UUID(id).String() }

func (id InvocationID) MarshalText() ([]byte, error) { return uuid.UUID(id).MarshalText() }

func (id *InvocationID) UnmarshalText(text []byte) error {
	return (*uuid.UUID)(id).UnmarshalText(text)
}

// MarshalBSONValue stores the id as a uuid.UUID is stored: generic binary.
func (id InvocationID) MarshalBSONValue() (bsontype.Type, []byte, error) {
	return bson.TypeBinary, bsoncore.AppendBinary(nil, bson.TypeBinaryGeneric, id[:]), nil
}

// UnmarshalBSONValue reads a stored id: binary, or a legacy ULID string.
func (id *InvocationID) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	switch t {
	case bson.TypeBinary:
		_, b, _, ok := bsoncore.ReadBinary(data)
		if !ok || len(b) != len(id) {
			return fmt.Errorf("invocation_id: binary of %d bytes, want %d", len(b), len(id))
		}
		copy(id[:], b)
		return nil
	case bson.TypeString:
		s, _, ok := bsoncore.ReadString(data)
		if !ok {
			return fmt.Errorf("invocation_id: malformed string")
		}
		legacy, err := InvocationIDFromULID(s)
		if err != nil {
			return fmt.Errorf("invocation_id: %w", err)
		}
		*id = legacy
		return nil
	}
	return fmt.Errorf("invocation_id: BSON %s, want binary", t)
}

// crockford is the ULID alphabet, Crockford's base32.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// InvocationIDFromULID is the id of an invocation recorded with a ULID: the
// 128 bits the ULID encodes.
func InvocationIDFromULID(s string) (InvocationID, error) {
	var id InvocationID
	s = strings.ToUpper(s)
	// 26 characters of 5 bits are 130 bits: the first character carries only
	// 3 of the 128.
	if len(s) != 26 || s[0] > '7' {
		return id, fmt.Errorf("%q is not a ULID", s)
	}
	v := new(big.Int)
	for i := range len(s) {
		d := strings.IndexByte(crockford, s[i])
		if d < 0 {
			return id, fmt.Errorf("%q is not a ULID", s)
		}
		v.Lsh(v, 5).Or(v, big.NewInt(int64(d)))
	}
	v.FillBytes(id[:])
	return id, nil
}
