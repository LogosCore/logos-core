package models

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/google/uuid"
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

// An invocation id is stored as a uuid.UUID is (generic binary), and the
// ULID strings of invocations recorded before ids were UUIDs read as the UUID
// of the same bits. The vector is the ULID spec's example, decoded
// independently.
func TestInvocationID_BSON(t *testing.T) {
	type row struct {
		ID InvocationID `bson:"invocation_id"`
	}
	id := NewInvocationID()
	raw, err := bson.Marshal(row{id})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := bson.Marshal(struct {
		ID uuid.UUID `bson:"invocation_id"`
	}{uuid.UUID(id)})
	if !bytes.Equal(raw, want) {
		t.Fatalf("stored as %x, want %x (a uuid.UUID)", raw, want)
	}
	var got row
	if err := bson.Unmarshal(raw, &got); err != nil || got.ID != id || uuid.UUID(id).Version() != 4 {
		t.Fatalf("round trip: %v %s, want %s", err, got.ID, id)
	}

	legacy, _ := bson.Marshal(bson.M{"invocation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	if err := bson.Unmarshal(legacy, &got); err != nil || got.ID.String() != "01563e3a-b5d3-d676-4c61-efb99302bd5b" {
		t.Fatalf("a ULID read as %s: %v", got.ID, err)
	}
	for _, bad := range []any{"not-a-ulid", int32(7)} {
		raw, _ := bson.Marshal(bson.M{"invocation_id": bad})
		if err := bson.Unmarshal(raw, &got); err == nil {
			t.Errorf("%v read as %s", bad, got.ID)
		}
	}
}

func TestInvocationIDFromULID(t *testing.T) {
	for s, want := range map[string]string{
		"01ARZ3NDEKTSV4RRFFQ69G5FAV": "01563e3a-b5d3-d676-4c61-efb99302bd5b",
		"01arz3ndektsv4rrffq69g5fav": "01563e3a-b5d3-d676-4c61-efb99302bd5b", // case does not matter
		"7ZZZZZZZZZZZZZZZZZZZZZZZZZ": "ffffffff-ffff-ffff-ffff-ffffffffffff", // the largest ULID
		"00000000000000000000000000": "00000000-0000-0000-0000-000000000000",
	} {
		if got, err := InvocationIDFromULID(s); err != nil || got.String() != want {
			t.Errorf("%s: %s %v, want %s", s, got, err, want)
		}
	}
	for _, bad := range []string{
		"8ZZZZZZZZZZZZZZZZZZZZZZZZZ",  // 130 bits
		"01ARZ3NDEKTSV4RRFFQ69G5FA",   // 25 characters
		"01ARZ3NDEKTSV4RRFFQ69G5FAVX", // 27
		"01ARZ3NDEKTSV4RRFFQ69G5FAU",  // U is not in the alphabet
		"",
	} {
		if got, err := InvocationIDFromULID(bad); err == nil {
			t.Errorf("%q read as %s", bad, got)
		}
	}
}
