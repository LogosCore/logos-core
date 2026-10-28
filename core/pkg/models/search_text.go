package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Search text is what a list search reads of a record's dynamic parts: maps
// keyed by a schema's fields, such as an endpoint's values or a build's
// connections, which a Mongo $regex on a field path cannot reach. It holds
// the values only, not their keys, so a search finds what records hold
// rather than every record with that setting. Repositories store it with
// the record (search_text) and match it like the record's other searched
// fields.

// EndpointSearchText is an endpoint's values, as its list search reads them.
func EndpointSearchText(e *ChannelEndpoint) string {
	return strings.Join(jsonLeaves(nil, map[string]any(e.Values)), "\n")
}

// BuildSearchText is what a build's list search reads beyond the build's own
// fields: its minion id, its values, and its connections' names, channels,
// settings and values.
func BuildSearchText(b *Build) string {
	parts := jsonLeaves([]string{b.MinionID.String()}, map[string]any(b.Values))
	parts = append(parts, b.Commands...)
	for _, c := range b.Connections.Items {
		parts = append(parts, c.Endpoint.Name, c.Endpoint.Channel.ModuleName)
		parts = jsonLeaves(parts, c.Endpoint.Params)
		parts = jsonLeaves(parts, c.Values)
	}
	return strings.Join(parts, "\n")
}

// jsonLeaves appends the strings and numbers v holds, at any depth.
func jsonLeaves(out []string, v any) []string {
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case float64, float32, int, int32, int64, json.Number:
		out = append(out, fmt.Sprint(x))
	case []string:
		out = append(out, x...)
	case []any:
		for _, e := range x {
			out = jsonLeaves(out, e)
		}
	case map[string]any:
		for _, e := range x {
			out = jsonLeaves(out, e)
		}
	}
	return out
}
