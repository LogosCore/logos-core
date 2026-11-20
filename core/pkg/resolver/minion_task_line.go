package resolver

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"

	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// lineNames is what a task's line shows of its connection params in place
// of their places (models.MinionTask.LineNames): the session's build's
// connections by name, as the console shows them, so that the task list's
// search finds "switch --conn=ws". Nil without a connection param, or when
// the build cannot be read (its line then says the place: logged).
func (r *minionTaskResolver) lineNames(ctx context.Context, s *models.MinionSession, params []protocol.FieldSpec, args models.JSONMap) map[string]string {
	var keys []string
	for _, p := range params {
		if _, ok := args[p.Key]; ok && p.Format == protocol.FormatConnection {
			keys = append(keys, p.Key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	b, err := r.builds.FindByBuildID(ctx, s.BuildID)
	if err != nil {
		logger.From(ctx).Warn("a task's connection names: the session's build", zap.Stringer("session", s.SessionID), zap.Error(err))
		return nil
	}
	return models.MinionTaskLineNames(keys, args, b.Connections.Names())
}

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
