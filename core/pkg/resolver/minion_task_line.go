package resolver

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/logoscore/logos-golang-protocol/protocol"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// lineJSON are the structured params a task's args hold
// (models.MinionTask.LineJSON): its line shows them as JSON.
func lineJSON(params []protocol.FieldSpec, args models.JSONMap) []string {
	var keys []string
	for _, p := range params {
		if _, ok := args[p.Key]; ok && p.Type == protocol.FieldStructured {
			keys = append(keys, p.Key)
		}
	}
	return keys
}

// lineRest is the param a task's line shows bare, after its command
// (models.MinionTask.LineRest), as the console's lineOf picks it
// (frontend minion-sessions/command-line.ts: change them together). In the
// order the command's form shows its params (compareFormOrder: advanced
// last, then by order and label), the param that takes the rest of a typed
// line (restParam: free text, the last required param or, with none
// required, the first; never one that applies only by a condition), when
// it is also the first. Empty for none: every value goes as an option.
func lineRest(params []protocol.FieldSpec) string {
	if len(params) == 0 {
		return ""
	}
	sorted := slices.Clone(params)
	slices.SortStableFunc(sorted, func(a, b protocol.FieldSpec) int {
		return cmp.Or(cmp.Compare(advancedRank(a), advancedRank(b)), cmp.Compare(a.Order, b.Order),
			strings.Compare(strings.ToLower(orKey(a.Label, a.Key)), strings.ToLower(orKey(b.Label, b.Key))))
	})
	rest := sorted[0]
	for _, p := range sorted {
		if p.Required && p.Default == nil && p.When == nil {
			rest = p // the last required one
		}
	}
	if rest.Key != sorted[0].Key || rest.When != nil {
		return ""
	}
	switch rest.Type {
	case protocol.FieldString, protocol.FieldURL, protocol.FieldSecret:
		return rest.Key
	}
	return ""
}

func advancedRank(p protocol.FieldSpec) int {
	if p.Advanced {
		return 1
	}
	return 0
}

// connectionText says the connection a task's connection param names, as
// the session has it now (models.MinionTask.ConnectionText): its place,
// channel and kind, then what tells it from others (connectionSummary).
// Empty without such a param, or for a place the session has no connection
// at.
func (r *minionTaskResolver) connectionText(ctx context.Context, s *models.MinionSession, params []protocol.FieldSpec,
	args models.JSONMap) string {
	place := connPlaceArg(params, args)
	if place < 1 {
		return ""
	}
	cur, ok := r.currentConnection(ctx, s, place)
	if !ok {
		return ""
	}
	channel, _ := cur["channel"].(string)
	kind, _ := cur["kind"].(string)
	parts := []string{strconv.Itoa(place), channel, kind}
	if r.channels != nil {
		if schema, err := r.channels.Schema(ctx, channel); err == nil && schema != nil {
			values, _ := cur["params"].(map[string]any)
			parts = append(parts, connectionSummary(schema.Kinds[kind], values)...)
		}
	}
	return strings.Join(parts, " · ")
}

// connectionSummary is what tells a connection of kind k from others, as
// the web app shows it (settingsSummary in frontend builds/build-values.ts:
// change them together): the values of the fields its Summary names, in
// its order, those set; without one, the first two set, the fields by order
// and then label.
func connectionSummary(k protocol.ConnectionKind, values map[string]any) []string {
	set := func(f protocol.FieldSpec) bool { v, ok := values[f.Key]; return ok && v != nil }
	var shown []protocol.FieldSpec
	if len(k.Summary) > 0 {
		for _, key := range k.Summary {
			if i := slices.IndexFunc(k.Fields, func(f protocol.FieldSpec) bool { return f.Key == key }); i >= 0 && set(k.Fields[i]) {
				shown = append(shown, k.Fields[i])
			}
		}
	} else {
		fields := slices.Clone(k.Fields)
		slices.SortStableFunc(fields, func(a, b protocol.FieldSpec) int {
			return cmp.Or(cmp.Compare(a.Order, b.Order), strings.Compare(orKey(a.Label, a.Key), orKey(b.Label, b.Key)))
		})
		for _, f := range fields {
			if set(f) && len(shown) < 2 {
				shown = append(shown, f)
			}
		}
	}
	out := make([]string, len(shown))
	for i, f := range shown {
		out[i] = summaryValue(values[f.Key])
	}
	return out
}

// summaryValue is a setting's value in a summary: text as it is, a list's
// items joined, anything else as JSON.
func summaryValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool, int, int64, float64, uint64:
		return fmt.Sprint(x)
	case []any:
		items := make([]string, len(x))
		for i, it := range x {
			items[i] = summaryValue(it)
		}
		return strings.Join(items, ", ")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
