package resolver

import (
	"context"
	"testing"

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

// A task's connection param is said by the connection it names when it is
// queued: its place, channel and kind, then the channel's summary fields
// set, in its order, or without one the first two set by order.
func TestConnectionText(t *testing.T) {
	fields := []protocol.FieldSpec{
		{Key: "base_url", Num: 1, Type: protocol.FieldURL, Label: "Base URL", Order: 1},
		{Key: "host_header", Num: 3, Type: protocol.FieldString, Label: "Host header", Order: 3},
		{Key: "mode", Num: 14, Type: protocol.FieldEnum, Label: "Mode", Order: 0},
	}
	r := &minionTaskResolver{channels: fakeChannelSchemas{"http": {Version: 4, Kinds: map[string]protocol.ConnectionKind{
		protocol.KindReverseCall:       {Fields: fields},
		protocol.KindReversePersistent: {Fields: fields, Summary: []string{"base_url", "mode"}},
	}}}}
	s := &models.MinionSession{ModuleName: "bob", Connections: []models.MinionSessionConnSpec{
		{Channel: "http", Kind: protocol.KindReverseCall, Params: map[string]any{"host_header": "cdn", "base_url": "https://a.example", "mode": "url-based"}},
		{Channel: "http", Kind: protocol.KindReversePersistent, Params: map[string]any{"host_header": "cdn", "mode": "factory-based"}},
	}}
	params := []protocol.FieldSpec{{Key: "conn", Num: 1, Type: protocol.FieldInt, Format: protocol.FormatConnection}}
	for place, want := range map[int]string{
		1: "1 · http · reverse-call · url-based · https://a.example", // the first two set, by order
		2: "2 · http · reverse-persistent · factory-based",           // the summary's, base_url not set
		3: "",                                                        // no connection there
	} {
		if got := r.connectionText(context.Background(), s, params, models.JSONMap{"conn": int64(place)}); got != want {
			t.Errorf("connection %d: %q, want %q", place, got, want)
		}
	}
	if got := r.connectionText(context.Background(), s, []protocol.FieldSpec{{Key: "for", Type: protocol.FieldDuration}},
		models.JSONMap{"for": "1h"}); got != "" {
		t.Errorf("no connection param: %q", got)
	}
	if got := summaryValue([]any{"a", int64(2)}); got != "a, 2" {
		t.Errorf("a list: %q", got)
	}
}
