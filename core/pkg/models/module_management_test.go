package models

import (
	"reflect"
	"testing"

	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
)

// The manifest and value maps carry `any` values that protocol.ValidateValue
// type-switches on. Through the driver's default codecs a string list would
// come back as primitive.A and an integer as int32, both of which it rejects;
// these pin that they come back as plain JSON types instead.

func TestModuleManifest_BSONRoundTripKeepsJSONTypes(t *testing.T) {
	in := Module{
		Instance: "http-1",
		Manifest: &ModuleManifest{protocol.ManagementManifest{
			SchemaVersion: protocol.ManagementSchemaV1,
			Properties: []protocol.PropertySpec{
				{FieldSpec: protocol.FieldSpec{Key: "hosts", Type: protocol.FieldStringList, Default: []any{"a", "b"}}, Apply: protocol.ApplyLive},
				{FieldSpec: protocol.FieldSpec{Key: "limit", Type: protocol.FieldInt, Default: 5, Min: 1, Max: 10}, Apply: protocol.ApplyRestart},
			},
		}},
	}
	data, err := bson.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Module
	if err := bson.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Manifest == nil || len(out.Manifest.Properties) != 2 {
		t.Fatalf("manifest = %+v", out.Manifest)
	}
	hosts := out.Manifest.Properties[0]
	if _, ok := hosts.Default.([]any); !ok {
		t.Errorf("string_list default type = %T, want []any", hosts.Default)
	}
	if err := protocol.ValidateValue(hosts.FieldSpec, hosts.Default); err != nil {
		t.Errorf("round-tripped default no longer validates: %v", err)
	}
	limit := out.Manifest.Properties[1]
	if limit.Default != float64(5) || limit.Min != float64(1) {
		t.Errorf("int default/min = %#v/%#v, want float64", limit.Default, limit.Min)
	}

	// A row without a manifest stays without one.
	data, _ = bson.Marshal(Module{Instance: "x"})
	var bare Module
	if err := bson.Unmarshal(data, &bare); err != nil {
		t.Fatal(err)
	}
	if bare.Manifest != nil {
		t.Errorf("manifest = %+v, want nil", bare.Manifest)
	}
}

func TestJSONMapAndChanges_BSONRoundTrip(t *testing.T) {
	in := ModuleConfigChange{
		ModuleName: "http",
		Changes: ModuleConfigKeyChanges{
			{Key: "hosts", Old: nil, New: []any{"x"}},
			{Key: "token", Old: RedactedValue, New: RedactedValue},
		},
	}
	cfg := ModuleConfig{Values: JSONMap{"hosts": []any{"x"}, "n": 3, "ok": true}}

	var outChange ModuleConfigChange
	data, err := bson.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := bson.Unmarshal(data, &outChange); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outChange.Changes[0].New, []any{"x"}) || outChange.Changes[0].Old != nil {
		t.Errorf("changes[0] = %#v", outChange.Changes[0])
	}

	var outCfg ModuleConfig
	data, err = bson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := bson.Unmarshal(data, &outCfg); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"hosts": []any{"x"}, "n": float64(3), "ok": true}
	if !reflect.DeepEqual(map[string]any(outCfg.Values), want) {
		t.Errorf("values = %#v, want %#v", outCfg.Values, want)
	}

	// Values written with $set by the repository (plain driver encoding) must
	// read back the same way.
	raw, _ := bson.Marshal(bson.M{"values": bson.M{"hosts": bson.A{"y"}, "n": int32(4)}})
	var fromSet ModuleConfig
	if err := bson.Unmarshal(raw, &fromSet); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromSet.Values["hosts"], []any{"y"}) || fromSet.Values["n"] != float64(4) {
		t.Errorf("values from $set = %#v", fromSet.Values)
	}
}
