package builds

import (
	"fmt"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// Searching an operation's builds (Builds with a search). A build is found by
// what the Builds page shows of it (factory, target, minion id, description,
// the stage it is on or stopped at, platform, who started it), by its state,
// factory instance, error and artifact name, and by what it was made of: its
// values, and its connections' names, channels, settings, values and the
// operators' notes on them. Only values count, not their keys, so a word
// finds what builds hold rather than every build with that setting.

// searchWords are the words of a search, lowercased; a build matches when
// its text holds every one.
func searchWords(search string) []string {
	return strings.Fields(strings.ToLower(search))
}

func matches(b *models.Build, words []string) bool {
	text := searchText(b)
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// searchText is what b is found by, lowercased.
func searchText(b *models.Build) string {
	parts := []string{b.MinionID.String(), b.Description, b.ModuleName, b.Target, b.Platform, b.State, b.Instance, b.CreatedBy.Name}
	if st := stageAt(Pipeline(b)); st != nil {
		parts = append(parts, st.Label)
	}
	if b.Error != nil {
		parts = append(parts, b.Error.Code, b.Error.Message)
	}
	if b.Artifact != nil {
		parts = append(parts, b.Artifact.Filename)
	}
	parts = appendLeaves(parts, map[string]any(b.Values))
	for _, c := range b.Connections.Items {
		parts = append(parts, c.Endpoint.Name, c.Endpoint.Channel.ModuleName)
		parts = appendLeaves(parts, c.Endpoint.Params)
		parts = appendLeaves(parts, c.Values)
	}
	parts = append(parts, b.ConnectionDescriptions...)
	return strings.ToLower(strings.Join(parts, "\n"))
}

// stageAt is the stage a build is on, or the one it stopped at, as the
// Builds page names it; nil when there is neither.
func stageAt(stages []Stage) *Stage {
	for i := range stages {
		if stages[i].State == StageRunning {
			return &stages[i]
		}
	}
	for i := range stages {
		if stages[i].State == StageFailed || stages[i].State == StageCancelled {
			return &stages[i]
		}
	}
	return nil
}

// appendLeaves appends the strings and numbers v holds, at any depth.
func appendLeaves(out []string, v any) []string {
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case float64, int, int32, int64:
		out = append(out, fmt.Sprint(x))
	case []any:
		for _, e := range x {
			out = appendLeaves(out, e)
		}
	case map[string]any:
		for _, e := range x {
			out = appendLeaves(out, e)
		}
	}
	return out
}
