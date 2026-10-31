package repository

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestRevised(t *testing.T) {
	got := revised(bson.M{"$set": bson.M{"name": "x"}, "$inc": bson.M{"n": 2}, "$pull": bson.M{"a": 1}})
	set := got["$set"].(bson.M)
	if set["name"] != "x" || set["updateAt"] == nil {
		t.Fatalf("$set %v", set)
	}
	if _, ok := set["updateAt"].(time.Time); !ok {
		t.Fatalf("updateAt %T", set["updateAt"])
	}
	if inc := got["$inc"].(bson.M); inc["rev"] != 1 || inc["n"] != 2 {
		t.Fatalf("$inc %v", inc)
	}
	if got["$pull"] == nil {
		t.Fatal("other operators dropped")
	}
	// An update without $set or $inc gets them.
	if only := revised(bson.M{"$unset": bson.M{"a": ""}}); only["$inc"].(bson.M)["rev"] != 1 || only["$set"].(bson.M)["updateAt"] == nil {
		t.Fatalf("unset only: %v", only)
	}
}
