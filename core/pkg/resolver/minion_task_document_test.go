package resolver

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestMinionTaskDocumentJSON(t *testing.T) {
	id := uuid.MustParse("f242ed70-17ef-4858-a17d-a7f4feb193de")
	wire, err := protocol.MarshalCBOR(map[string]any{"command": "id", "timeout": uint64(30000)})
	if err != nil {
		t.Fatal(err)
	}
	oid := primitive.NewObjectID()
	at := time.Date(2026, 10, 31, 9, 20, 13, 504_000_000, time.UTC)
	d := bson.D{
		{Key: "_id", Value: oid},
		{Key: "task_id", Value: primitive.Binary{Subtype: 0, Data: id[:]}},
		{Key: "command", Value: "shell"},
		{Key: "args", Value: bson.D{{Key: "command", Value: "id"}}},
		{Key: "wire_args", Value: primitive.Binary{Data: wire}},
		{Key: "blob", Value: primitive.Binary{Data: []byte{1, 2, 3}}},
		{Key: "queued_at", Value: primitive.NewDateTimeFromTime(at)},
		{Key: "exit", Value: int64(0)},
		{Key: "ok", Value: true},
		{Key: "list", Value: bson.A{int32(1), "x"}},
	}
	got, err := documentJSON(d)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "_id": "` + oid.Hex() + `",
  "task_id": "f242ed70-17ef-4858-a17d-a7f4feb193de",
  "command": "shell",
  "args": {
    "command": "id"
  },
  "wire_args": {
    "$cbor": {
      "command": "id",
      "timeout": 30000
    }
  },
  "blob": {
    "$binary": "AQID"
  },
  "queued_at": "2026-10-31T09:20:13.504Z",
  "exit": 0,
  "ok": true,
  "list": [
    1,
    "x"
  ]
}`
	if strings.TrimSpace(got) != want {
		t.Errorf("document JSON:\n%s\nwant:\n%s", got, want)
	}
}
