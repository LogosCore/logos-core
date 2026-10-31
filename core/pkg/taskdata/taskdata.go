// Package taskdata is what core does with a task's data (docs:
// contracts/task-result-handlers.md): checks each part against the task's
// result handler as it comes, joins the parts when the data is read, and
// converts it to JSON for the web app by one mapping. Parts keep their
// data as the CBOR it came in (models.MinionTaskPart.DataCBOR); this
// package decodes it when it is needed.
package taskdata

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Check checks one part's data against handler, as the part comes. A
// handler this core does not know, or none, takes any data.
func Check(handler string, seq uint64, data []byte) error {
	if handler == protocol.HandlerDirListing {
		_, err := protocol.ReadDirListing(seq, data)
		return err
	}
	return nil
}

// Join is a task's data, its parts (in seq order) joined by its handler's
// rule, as JSON values (Value): nil when no part has data. A task whose
// data did not fit its handler (t.DataProblem) is joined by the generic
// rule, to be shown raw.
func Join(t models.MinionTask, parts []models.MinionTaskPart) (any, error) {
	if t.Handler == protocol.HandlerDirListing && t.DataProblem == "" {
		return joinDirListing(parts)
	}
	return joinGeneric(parts)
}

// joinGeneric joins parts with no declared shape: a key holding an array
// gets each part's elements appended, any other key takes the latest value.
func joinGeneric(parts []models.MinionTaskPart) (any, error) {
	var out map[string]any
	for _, p := range parts {
		data, err := partData(p)
		if err != nil {
			return nil, err
		}
		for k, v := range data {
			if out == nil {
				out = map[string]any{}
			}
			if add, ok := v.([]any); ok {
				if have, ok := out[k].([]any); ok {
					out[k] = append(have, add...)
					continue
				}
			}
			out[k] = v
		}
	}
	if out == nil {
		return nil, nil
	}
	return Value(out), nil
}

// partData is a part's data decoded: from its CBOR, or as a part kept
// before CBOR was had it.
func partData(p models.MinionTaskPart) (map[string]any, error) {
	if len(p.DataCBOR) == 0 {
		if len(p.Data) == 0 {
			return nil, nil
		}
		return map[string]any(p.Data), nil
	}
	var data map[string]any
	if err := protocol.UnmarshalCBOR(p.DataCBOR, &data); err != nil {
		return nil, fmt.Errorf("part %d: %w", p.Seq, err)
	}
	return data, nil
}

// joinDirListing joins a listing: path, sep and parent from the first
// part, the entries of every part appended.
func joinDirListing(parts []models.MinionTaskPart) (any, error) {
	var joined protocol.DirListing
	found := false
	for _, p := range parts {
		if len(p.DataCBOR) == 0 {
			continue
		}
		l, err := protocol.ReadDirListing(p.Seq, p.DataCBOR)
		if err != nil {
			return nil, fmt.Errorf("part %d: %w", p.Seq, err)
		}
		found = true
		if p.Seq == 0 {
			joined.Path, joined.Sep, joined.Parent, joined.X = l.Path, l.Sep, l.Parent, l.X
		}
		joined.Entries = append(joined.Entries, l.Entries...)
	}
	if !found {
		return nil, nil
	}
	return dirListingValue(joined), nil
}

// dirListingValue is a listing as JSON values, by its fields: decoding it
// back from CBOR would hit the decoder's element limit on a big one.
func dirListingValue(l protocol.DirListing) map[string]any {
	entries := make([]any, len(l.Entries))
	for i, e := range l.Entries {
		m := map[string]any{"name": e.Name, "type": e.Type}
		if e.Size != nil {
			m["size"] = Value(*e.Size)
		}
		if e.Modified != nil {
			m["modified"] = Value(*e.Modified)
		}
		for k, v := range map[string]string{"mode": e.Mode, "owner": e.Owner, "group": e.Group, "target": e.Target} {
			if v != "" {
				m[k] = v
			}
		}
		if e.Hidden {
			m["hidden"] = true
		}
		if len(e.X) > 0 {
			m["x"] = Value(e.X)
		}
		entries[i] = m
	}
	out := map[string]any{"path": l.Path, "sep": l.Sep, "entries": entries}
	if l.Parent != "" {
		out["parent"] = l.Parent
	}
	if len(l.X) > 0 {
		out["x"] = Value(l.X)
	}
	return out
}

// maxSafeInt is the largest integer a JSON reader (JavaScript) holds
// exactly: beyond it an integer goes as a string of its digits.
const maxSafeInt = 1<<53 - 1

// Value is a decoded CBOR value as JSON values, by the contract's mapping:
// byte strings as {"$bytes": base64}, integers beyond ±2^53 as strings,
// NaN and the infinities as strings. (Tags never come: the protocol
// refuses them.)
func Value(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = Value(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Value(e)
		}
		return out
	case []byte:
		return map[string]any{"$bytes": base64.StdEncoding.EncodeToString(x)}
	case uint64:
		if x > maxSafeInt {
			return strconv.FormatUint(x, 10)
		}
		return x
	case int64:
		if x > maxSafeInt || x < -maxSafeInt {
			return strconv.FormatInt(x, 10)
		}
		return x
	case float64:
		return float(x)
	case float32:
		return float(float64(x))
	}
	return v
}

func float(f float64) any {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return f
}

// Sequence is the parts' data exactly as the minion sent them, as a CBOR
// sequence (RFC 8742): one item per part that has data, in seq order. A
// part kept before CBOR was is encoded from what was kept.
func Sequence(parts []models.MinionTaskPart) ([]byte, error) {
	var b bytes.Buffer
	for _, p := range parts {
		switch {
		case len(p.DataCBOR) > 0:
			b.Write(p.DataCBOR)
		case len(p.Data) > 0:
			raw, err := protocol.MarshalCBOR(map[string]any(p.Data))
			if err != nil {
				return nil, fmt.Errorf("part %d: %w", p.Seq, err)
			}
			b.Write(raw)
		}
	}
	return b.Bytes(), nil
}
