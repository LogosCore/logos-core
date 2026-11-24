package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
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

	wire := args(map[string]any{"channel": "http", "kind": protocol.KindReverseCall,
		"params": map[string]any{"url": "https://a.example"}})
	if err := r.putConnections(context.Background(), s, "", params, wire); err != nil {
		t.Fatal(err)
	}
	c, ok := wire["conn"].(protocol.HelloConnection)
	if !ok || c.Channel != "http" || c.Version != 3 || c.Params[1] != "https://a.example" ||
		c.Params[2] != uint64(30000) || c.Values[1] != "POST" {
		t.Fatalf("the arg: %#v", wire["conn"])
	}
	if numbered := protocol.WireArgs(params, wire); numbered[1] == nil {
		t.Fatal("not in the wire args")
	}

	// A session's connections have no names: one given is refused with the args.
	if _, problems := protocol.TaskArgs(params, map[string]any{"conn": map[string]any{"channel": "http",
		"kind": protocol.KindReverseCall, "name": "front"}}); !strings.Contains(problems["conn"], `has no "name"`) {
		t.Errorf("a name: %v", problems)
	}
	for want, v := range map[string]map[string]any{
		"not one core knows": {"channel": "smoke", "kind": protocol.KindReverseCall},
		"does not carry":     {"channel": "http", "kind": protocol.KindBindCall},
		"params.nope":        {"channel": "http", "kind": protocol.KindReverseCall, "params": map[string]any{"url": "https://a", "nope": 1}},
		"values.method":      {"channel": "http", "kind": protocol.KindReverseCall, "params": map[string]any{"url": "https://a"}, "values": map[string]any{"method": "GET"}},
	} {
		err := r.putConnections(context.Background(), s, "", params, args(v))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

// edit_connection's connection goes as what changed of the session's
// connection at its place (protocol.ConnectionPatch): a value changed
// alone, the connection whole when its kind changes, and an edit that
// changes nothing refused. The session's own connections are kept in their
// CBOR forms (a duration in milliseconds), which count as the same values.
func TestPutConnectionsEditPatch(t *testing.T) {
	r := &minionTaskResolver{
		channels: fakeChannelSchemas{"http": {Version: 3, Kinds: map[string]protocol.ConnectionKind{
			protocol.KindReverseCall: {Fields: []protocol.FieldSpec{{Key: "url", Num: 1, Type: protocol.FieldURL, Required: true},
				{Key: "timeout", Num: 2, Type: protocol.FieldDuration, Default: "30s"}}},
			protocol.KindReversePersistent: {Fields: []protocol.FieldSpec{{Key: "url", Num: 1, Type: protocol.FieldURL, Required: true}}}}}},
		schemas: fakeTaskSchemas{s: &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{Channels: []protocol.BuildChannel{{ModuleName: "http",
			Kinds: []protocol.BuildKind{{Kind: protocol.KindReverseCall, Fields: []protocol.FieldSpec{
				{Key: "path_prefix", Num: 2, Type: protocol.FieldString, Default: "/"}}}}}}}}},
	}
	s := &models.MinionSession{ModuleName: "bob", Connections: []models.MinionSessionConnSpec{{Channel: "http", Kind: protocol.KindReverseCall,
		Version: 3, Params: map[string]any{"url": "https://a.example", "timeout": uint64(30000)}, Values: map[string]any{"2": "/new"}}}}
	params := []protocol.FieldSpec{{Key: "conn", Num: 1, Type: protocol.FieldInt, Format: protocol.FormatConnection},
		{Key: "connection", Num: 2, Type: protocol.FieldStructured, Format: protocol.FormatConnection}}
	edit := func(v map[string]any) (map[string]any, error) {
		wire, problems := protocol.TaskArgs(params, map[string]any{"conn": 1, "connection": v})
		if problems != nil {
			t.Fatal(problems)
		}
		return wire, r.putConnections(context.Background(), s, protocol.ActionEditConnection, params, wire)
	}
	front := func(path, kind string) map[string]any {
		return map[string]any{"channel": "http", "kind": kind,
			"params": map[string]any{"url": "https://a.example", "timeout": "30s"}, "values": map[string]any{"path_prefix": path}}
	}

	wire, err := edit(front("/new/2", protocol.KindReverseCall))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := wire["connection"].(protocol.ConnectionPatch)
	if !ok || p.Whole() || len(p.Values) != 1 || p.Values[2] != "/new/2" || p.Params != nil {
		t.Fatalf("the patch: %#v", wire["connection"])
	}
	if _, err := edit(front("/new", protocol.KindReverseCall)); err == nil || !strings.Contains(err.Error(), "connection 1: nothing changed") {
		t.Fatalf("an edit changing nothing: %v", err)
	}
	persistent := front("/new", protocol.KindReversePersistent)
	persistent["params"] = map[string]any{"url": "https://a.example"}
	delete(persistent, "values")
	if wire, err = edit(persistent); err != nil {
		t.Fatal(err)
	}
	if p, _ := wire["connection"].(protocol.ConnectionPatch); !p.Whole() || p.Kind != protocol.KindReversePersistent {
		t.Fatalf("a kind changed: %#v", wire["connection"])
	}
}

// add_connection's connection goes whole (no patch: nothing to compare it
// with), even one like a connection the session has.
func TestPutConnectionsAdd(t *testing.T) {
	r := &minionTaskResolver{
		channels: fakeChannelSchemas{"http": {Version: 3, Kinds: map[string]protocol.ConnectionKind{
			protocol.KindReverseCall: {Fields: []protocol.FieldSpec{{Key: "url", Num: 1, Type: protocol.FieldURL, Required: true}}}}}},
		schemas: fakeTaskSchemas{s: &models.ModuleBuildSchema{}},
	}
	s := &models.MinionSession{ModuleName: "bob", Connections: []models.MinionSessionConnSpec{{Channel: "http", Kind: protocol.KindReverseCall,
		Params: map[string]any{"url": "https://a.example"}}}}
	params := []protocol.FieldSpec{{Key: "connection", Num: 1, Type: protocol.FieldStructured, Format: protocol.FormatConnection}}
	add := func(url string) (map[string]any, error) {
		wire, problems := protocol.TaskArgs(params, map[string]any{"connection": map[string]any{"channel": "http",
			"kind": protocol.KindReverseCall, "params": map[string]any{"url": url}}})
		if problems != nil {
			t.Fatal(problems)
		}
		return wire, r.putConnections(context.Background(), s, protocol.ActionAddConnection, params, wire)
	}

	wire, err := add("https://b.example")
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := wire["connection"].(protocol.HelloConnection); !ok || c.Params[1] != "https://b.example" || c.Version != 3 {
		t.Fatalf("the arg: %#v", wire["connection"])
	}
	if _, err := add("https://a.example"); err != nil {
		t.Fatalf("one like the session's: %v", err)
	}
}

type oneTarget struct{ t *models.MinionTarget }

func (o oneTarget) FindBySessionID(context.Context, uuid.UUID) (*models.MinionTarget, error) {
	return o.t, nil
}

// remove_connection is refused when it is queued for the connection in use,
// the minion's only one, a place past them, and the one its target names.
func TestCheckRemoval(t *testing.T) {
	s := &models.MinionSession{Conn: 1, Connections: []models.MinionSessionConnSpec{{Channel: "http"}, {Channel: "http"}, {Channel: "tcp"}}}
	r := &minionTaskResolver{targets: oneTarget{t: &models.MinionTarget{Conn: 3, Name: "office"}}}
	params := []protocol.FieldSpec{{Key: "conn", Num: 1, Type: protocol.FieldInt, Format: protocol.FormatConnection}}
	check := func(place int) error {
		return r.checkRemoval(context.Background(), s, protocol.ActionRemoveConnection, params, map[string]any{"conn": int64(place)})
	}
	if err := check(2); err != nil {
		t.Fatalf("another one: %v", err)
	}
	for place, want := range map[int]string{1: "is in use: switch first", 4: "there is no connection 4", 3: "its target office names"} {
		if err := check(place); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("connection %d: %v", place, err)
		}
	}
	s.Connections, s.Conn = s.Connections[:1], 0
	if err := check(1); err == nil || !strings.Contains(err.Error(), "only connection") {
		t.Errorf("the only one: %v", err)
	}
	if err := r.checkRemoval(context.Background(), s, protocol.ActionSwitchConnection, params, map[string]any{"conn": int64(1)}); err != nil {
		t.Errorf("another action: %v", err)
	}
}
