package resolver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// A task's stored document as the console's JSON tab shows it, for
// debugging: indented JSON in the stored key order, the BSON types a person
// cannot read made readable. A 16-byte binary is a UUID (every id here),
// wire_args the CBOR the minion is sent ({"$cbor": decoded}), any other
// binary {"$binary": base64}; times are RFC 3339 in UTC.

// documentJSON is d as indented JSON.
func documentJSON(d bson.D) (string, error) {
	raw, err := json.Marshal(readable("", d))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ordered is a document whose keys keep their order in JSON.
type ordered []docField

type docField struct {
	key   string
	value any
}

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// readable is a BSON value, under key, as a value encoding/json writes.
func readable(key string, v any) any {
	switch x := v.(type) {
	case bson.D:
		out := make(ordered, len(x))
		for i, e := range x {
			out[i] = docField{e.Key, readable(e.Key, e.Value)}
		}
		return out
	case bson.M:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = readable(k, e)
		}
		return out
	case bson.A:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = readable("", e)
		}
		return out
	case primitive.ObjectID:
		return x.Hex()
	case primitive.DateTime:
		return x.Time().UTC().Format(time.RFC3339Nano)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case primitive.Binary:
		if key == "wire_args" {
			var args any
			if err := protocol.UnmarshalCBOR(x.Data, &args); err == nil {
				return map[string]any{"$cbor": args}
			}
		}
		if len(x.Data) == 16 && (x.Subtype == 0 || x.Subtype == 4) {
			return uuid.UUID(x.Data).String()
		}
		return map[string]any{"$binary": base64.StdEncoding.EncodeToString(x.Data)}
	case primitive.Null, primitive.Undefined:
		return nil
	case primitive.Decimal128, primitive.Regex, primitive.Timestamp:
		return fmt.Sprint(x)
	}
	return v
}
