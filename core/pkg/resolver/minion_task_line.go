package resolver

import (
	"cmp"
	"slices"
	"strings"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

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
