package taskdata

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// FromJSON is data as JSON (as Value gives it to the web app, and as a
// factory gives core a task's data: a composite's finish, a factory command's
// result) back in core's CBOR: {"$bytes": "<base64>"} a byte string, a whole
// number an integer.
func FromJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return protocol.MarshalCBOR(fromJSON(v))
}

func fromJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		if len(v) == 1 {
			if s, ok := v["$bytes"].(string); ok {
				if b, err := base64.StdEncoding.DecodeString(s); err == nil {
					return b
				}
			}
		}
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = fromJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = fromJSON(e)
		}
		return out
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		if f, err := v.Float64(); err == nil && !math.IsInf(f, 0) {
			return f
		}
		return v.String()
	}
	return v
}
