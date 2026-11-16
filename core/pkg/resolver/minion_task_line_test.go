package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"

	"github.com/logoscore/logos-core/core/pkg/models"
)

// The cases of the console's restParam and lineOf (command-line.ts).
func TestLineRest(t *testing.T) {
	text := func(key string, order int) protocol.FieldSpec {
		return protocol.FieldSpec{Key: key, Type: protocol.FieldString, Order: order}
	}
	required := func(f protocol.FieldSpec) protocol.FieldSpec { f.Required = true; return f }
	for _, c := range []struct {
		name   string
		params []protocol.FieldSpec
		want   string
	}{
		{"none", nil, ""},
		// A shell's command line: required text, first.
		{"shell", []protocol.FieldSpec{required(text("command", 1)), {Key: "timeout", Type: protocol.FieldDuration, Order: 2, Default: "30s"}}, "command"},
		// ls's path: none required, the first is text.
		{"ls", []protocol.FieldSpec{{Key: "path", Type: protocol.FieldString, Default: "."}}, "path"},
		// sleep's interval: required, not text.
		{"sleep", []protocol.FieldSpec{required(protocol.FieldSpec{Key: "interval", Type: protocol.FieldDuration, Order: 1}),
			{Key: "jitter", Type: protocol.FieldInt, Order: 2}}, ""},
		// feedback's message takes the rest of a line, but comes second: the
		// line shows it as an option.
		{"feedback", []protocol.FieldSpec{{Key: "level", Type: protocol.FieldEnum, Order: 1}, required(text("message", 2))}, ""},
		// In the form's order, not the schema's: the text param is first by
		// its order.
		{"form order", []protocol.FieldSpec{{Key: "timeout", Type: protocol.FieldDuration, Order: 5}, required(text("command", 1))}, "command"},
		// An advanced param goes last whatever its order.
		{"advanced", []protocol.FieldSpec{{Key: "proxy", Type: protocol.FieldString, Order: 0, Advanced: true}, required(text("url", 1))}, "url"},
		// A param that applies only by a condition never takes the rest.
		{"when", []protocol.FieldSpec{{Key: "host", Type: protocol.FieldString, Order: 1,
			When: &protocol.FieldCondition{Key: "front", Values: []string{"forward"}}}, {Key: "front", Type: protocol.FieldEnum, Order: 2}}, ""},
		// The last required one is the rest: the first is required too, so
		// the rest is not first.
		{"two required", []protocol.FieldSpec{required(text("from", 1)), required(text("to", 2))}, ""},
		// With a default, required does not count.
		{"url", []protocol.FieldSpec{{Key: "url", Type: protocol.FieldURL, Order: 1, Required: true, Default: "https://x"}}, "url"},
	} {
		if got := lineRest(c.params); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

type failingTaskBuilds struct{}

func (failingTaskBuilds) FindByBuildID(context.Context, uuid.UUID) (*models.Build, error) {
	return nil, errors.New("mongo is down")
}

// A task's connection param is shown by its connection's name, read from
// the session's build: what the task list's search finds. A param of
// another format, or none given, reads no build; a build not read leaves
// the place.
func TestLineNames(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), BuildID: uuid.New()}
	conn := protocol.FieldSpec{Key: "conn", Type: protocol.FieldInt, Format: protocol.FormatConnection}
	forParam := protocol.FieldSpec{Key: "for", Type: protocol.FieldDuration}
	b := &models.Build{Connections: models.BuildConnections{Items: []protocol.BuildConnection{
		{Endpoint: protocol.ChannelEndpointExport{Name: "ws"}}, {Endpoint: protocol.ChannelEndpointExport{Name: "https"}}}}}
	r := &minionTaskResolver{builds: fakeTaskBuilds{b: b}}
	if got := r.lineNames(ctx, s, []protocol.FieldSpec{conn, forParam}, models.JSONMap{"conn": 2, "for": "1h"}); len(got) != 1 || got["conn"] != "https" {
		t.Errorf("switch: %v", got)
	}
	none := &minionTaskResolver{builds: failingTaskBuilds{}}
	if got := none.lineNames(ctx, s, []protocol.FieldSpec{forParam}, models.JSONMap{"for": "1h"}); got != nil {
		t.Errorf("no connection param: %v", got)
	}
	if got := none.lineNames(ctx, s, []protocol.FieldSpec{conn}, models.JSONMap{}); got != nil {
		t.Errorf("no conn given: %v", got)
	}
	if got := none.lineNames(ctx, s, []protocol.FieldSpec{conn}, models.JSONMap{"conn": 1}); got != nil {
		t.Errorf("a build not read: %v", got)
	}
}
