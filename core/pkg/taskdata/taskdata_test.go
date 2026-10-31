package taskdata

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func data(t *testing.T, v any) []byte {
	t.Helper()
	b, err := protocol.ResultData(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestValue(t *testing.T) {
	got := asJSON(t, Value(map[string]any{
		"bytes": []byte{1, 2, 3},
		"small": uint64(42), "big": uint64(1 << 60), "neg": int64(-(1 << 60)), "negSmall": int64(-7),
		"nan": math.NaN(), "inf": math.Inf(1), "ninf": float32(math.Inf(-1)), "half": 0.5,
		"list": []any{[]byte{0xff}, nil, true, "x"},
	}))
	want := `{"big":"1152921504606846976","bytes":{"$bytes":"AQID"},"half":0.5,"inf":"Infinity",` +
		`"list":[{"$bytes":"/w=="},null,true,"x"],"nan":"NaN","neg":"-1152921504606846976","negSmall":-7,` +
		`"ninf":"-Infinity","small":42}`
	if got != want {
		t.Errorf("mapped:\n%s\nwant\n%s", got, want)
	}
}

func TestJoin(t *testing.T) {
	size, mod := uint64(0), int64(1793438413)
	listing := models.MinionTask{Handler: protocol.HandlerDirListing}
	parts := []models.MinionTaskPart{
		{Seq: 0, DataCBOR: data(t, protocol.DirListing{Path: "/tmp", Sep: "/", Parent: "/", Entries: []protocol.DirEntry{
			{Name: "a", Type: protocol.DirEntryDir}}})},
		{Seq: 1, Out: []byte("no data here")},
		{Seq: 2, DataCBOR: data(t, protocol.DirListing{Entries: []protocol.DirEntry{
			{Name: "b", Type: protocol.DirEntryFile, Size: &size, Modified: &mod, Hidden: true, X: map[string]any{"ino": []byte{9}}}}})},
	}
	got, err := Join(listing, parts)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"entries":[{"name":"a","type":"dir"},{"hidden":true,"modified":1793438413,"name":"b","size":0,"type":"file",` +
		`"x":{"ino":{"$bytes":"CQ=="}}}],"parent":"/","path":"/tmp","sep":"/"}`
	if s := asJSON(t, got); s != want {
		t.Errorf("listing:\n%s\nwant\n%s", s, want)
	}

	// A listing that did not fit is joined as any data, to be shown raw.
	flagged := listing
	flagged.DataProblem = "part 2: ..."
	if got, err := Join(flagged, parts); err != nil || len(got.(map[string]any)["entries"].([]any)) != 2 {
		t.Fatalf("flagged: %v %v", got, err)
	}

	// No handler: arrays appended, the rest the latest; parts kept before
	// CBOR read as they were.
	generic := []models.MinionTaskPart{
		{Seq: 0, Data: models.JSONMap{"rows": []any{"old"}, "n": 1.0}},
		{Seq: 1, DataCBOR: data(t, map[string]any{"rows": []any{"new"}, "n": 2, "raw": []byte{7}})},
	}
	if s := asJSON(t, must(Join(models.MinionTask{}, generic))); s != `{"n":2,"raw":{"$bytes":"Bw=="},"rows":["old","new"]}` {
		t.Errorf("generic: %s", s)
	}
	if got, _ := Join(models.MinionTask{}, []models.MinionTaskPart{{Seq: 0, Out: []byte("x")}}); got != nil {
		t.Errorf("no data: %v", got)
	}
}

func TestCheck(t *testing.T) {
	if Check(protocol.HandlerDirListing, 1, data(t, protocol.DirListing{Path: "/x"})) == nil {
		t.Error("a later part naming the directory passed")
	}
	if Check("", 0, data(t, map[string]any{"anything": 1})) != nil || Check("process_list", 0, data(t, map[string]any{})) != nil {
		t.Error("no handler, or one core does not know, takes any data")
	}
}

func TestSequence(t *testing.T) {
	a, b := data(t, map[string]any{"a": 1}), data(t, map[string]any{"b": []byte{1}})
	seq, err := Sequence([]models.MinionTaskPart{{Seq: 0, DataCBOR: a}, {Seq: 1}, {Seq: 2, DataCBOR: b}})
	if err != nil || !bytes.Equal(seq, append(append([]byte{}, a...), b...)) {
		t.Fatalf("sequence %x, %v", seq, err)
	}
}

func must(v any, err error) any {
	if err != nil {
		panic(err)
	}
	return v
}
