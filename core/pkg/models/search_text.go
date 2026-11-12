package models

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
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
// fields: its build id, its values, and its connections' names, channels,
// settings and values.
func BuildSearchText(b *Build) string {
	parts := jsonLeaves([]string{b.BuildID.String()}, map[string]any(b.Values))
	parts = append(parts, b.Commands...)
	for _, c := range b.Connections.Items {
		parts = append(parts, c.Endpoint.Name, c.Endpoint.Channel.ModuleName)
		parts = jsonLeaves(parts, c.Endpoint.Params)
		parts = jsonLeaves(parts, c.Values)
	}
	return strings.Join(parts, "\n")
}

// MinionTaskSearchText is what a session's task list search reads of a
// task: its line as the console shows it (lineOf, in the frontend's
// minion-sessions/command-line.ts: change the two together), so a search
// for what a row shows finds it, "shell whoami" or "--timeout=30s". The
// command; then its LineRest param's text as typed, when that is text and
// does not start with "--"; then every other value as --key=value, keys in
// order, a list's items and a map's k=v pairs comma-joined, a value with a
// space or a quote in it (or none at all) quoted. Unlike a record's search
// text it holds keys: the line shows them. A connection param holds the
// value it keeps, where the console shows the connection's name.
func MinionTaskSearchText(t *MinionTask) string {
	args := jsonArgs(t.Args)
	parts := []string{t.Command}
	rest := ""
	if s, ok := args[t.LineRest].(string); ok && t.LineRest != "" && strings.TrimSpace(s) != "" &&
		!strings.HasPrefix(strings.TrimLeftFunc(s, unicode.IsSpace), "--") {
		parts = append(parts, s)
		rest = t.LineRest
	}
	for _, k := range slices.Sorted(maps.Keys(args)) {
		if k != rest {
			parts = append(parts, "--"+k+"="+lineQuote(lineShow(args[k])))
		}
	}
	return strings.Join(parts, " ")
}

// jsonArgs are args as the console reads them, through JSON: numbers
// float64, lists []any, maps map[string]any, whatever made them (a stored
// task's come back so; a queued one's are the resolver's).
func jsonArgs(args JSONMap) map[string]any {
	out := map[string]any{}
	if len(args) == 0 {
		return out
	}
	b, err := json.Marshal(map[string]any(args))
	if err != nil || json.Unmarshal(b, &out) != nil {
		return map[string]any(args)
	}
	return out
}

// lineShow is a value as lineOf shows it: a list's items comma-joined, a
// map's k=v pairs, anything else as JavaScript's String has it.
func lineShow(v any) string {
	if m, ok := v.(map[string]any); ok {
		pairs := make([]string, 0, len(m))
		for _, k := range slices.Sorted(maps.Keys(m)) {
			pairs = append(pairs, k+"="+jsString(m[k]))
		}
		return strings.Join(pairs, ",")
	}
	return jsString(v)
}

// jsString is JavaScript's String of a JSON value: an array's items
// comma-joined (null as nothing), an object as "[object Object]".
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		items := make([]string, len(x))
		for i, it := range x {
			if it != nil {
				items[i] = jsString(it)
			}
		}
		return strings.Join(items, ",")
	case map[string]any:
		return "[object Object]"
	}
	return fmt.Sprint(v)
}

// lineQuote quotes a value as lineOf does: as JSON when it holds a space or
// a quote, or is empty.
func lineQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '"' || r == '\'' }) {
		return s
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(s) != nil {
		return s
	}
	return strings.TrimSuffix(b.String(), "\n")
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
