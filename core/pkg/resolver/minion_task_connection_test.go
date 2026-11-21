package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

type fakeChannelSchemas map[string]*protocol.ConnectionSchema

func (f fakeChannelSchemas) Schema(_ context.Context, module string) (*protocol.ConnectionSchema, error) {
	return f[module], nil
}

// A connection param (structured, format connection) carries the minion's
// numbered form in place of the JSON it was given: params by the channel's
// field numbers, values by the factory's, defaults filled in, the version
// the channel schema's. A channel core does not know, a kind it does not
// carry and a param it does not declare are refused.
func TestPutConnections(t *testing.T) {
	r := &minionTaskResolver{
		channels: fakeChannelSchemas{"http": {Version: 3, Kinds: map[string]protocol.ConnectionKind{protocol.KindReverseCall: {
			Fields: []protocol.FieldSpec{{Key: "url", Num: 1, Type: protocol.FieldURL, Required: true},
				{Key: "timeout", Num: 2, Type: protocol.FieldDuration, Default: "30s"}}}}}},
		schemas: fakeTaskSchemas{s: &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{Channels: []protocol.BuildChannel{{ModuleName: "http",
			Kinds: []protocol.BuildKind{{Kind: protocol.KindReverseCall, Fields: []protocol.FieldSpec{
				{Key: "method", Num: 1, Type: protocol.FieldEnum, Default: "POST",
					Options: []protocol.EnumOption{{Value: "POST"}, {Value: "PUT"}}}}}}}}}}},
	}
	s := &models.MinionSession{ModuleName: "bob"}
	params := []protocol.FieldSpec{{Key: "conn", Num: 1, Type: protocol.FieldStructured, Format: protocol.FormatConnection}}
	args := func(v map[string]any) map[string]any {
		wire, problems := protocol.TaskArgs(params, map[string]any{"conn": v})
		if problems != nil {
			t.Fatal(problems)
		}
		return wire
	}

	wire := args(map[string]any{"channel": "http", "kind": protocol.KindReverseCall, "name": "front",
		"params": map[string]any{"url": "https://a.example"}})
	if err := r.putConnections(context.Background(), s, params, wire); err != nil {
		t.Fatal(err)
	}
	c, ok := wire["conn"].(protocol.HelloConnection)
	if !ok || c.Channel != "http" || c.Name != "front" || c.Version != 3 || c.Params[1] != "https://a.example" ||
		c.Params[2] != uint64(30000) || c.Values[1] != "POST" {
		t.Fatalf("the arg: %#v", wire["conn"])
	}
	if numbered := protocol.WireArgs(params, wire); numbered[1] == nil {
		t.Fatal("not in the wire args")
	}

	for want, v := range map[string]map[string]any{
		"not one core knows": {"channel": "smoke", "kind": protocol.KindReverseCall},
		"does not carry":     {"channel": "http", "kind": protocol.KindBindCall},
		"params.nope":        {"channel": "http", "kind": protocol.KindReverseCall, "params": map[string]any{"url": "https://a", "nope": 1}},
		"values.method":      {"channel": "http", "kind": protocol.KindReverseCall, "params": map[string]any{"url": "https://a"}, "values": map[string]any{"method": "GET"}},
	} {
		err := r.putConnections(context.Background(), s, params, args(v))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}
