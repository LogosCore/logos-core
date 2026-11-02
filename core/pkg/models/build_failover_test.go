package models

import (
	"testing"

	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/bson"
)

// A build's failover policy is stored by its JSON keys and comes back the
// same; a build without one reads the defaults.
func TestBuildFailoverBSON(t *testing.T) {
	f := BuildFailover{Enabled: true, FailAfter: 10, FailFor: "2m", Wrap: false, CyclePause: "1m", FailbackAfter: "0s"}
	raw, err := bson.Marshal(Build{Failover: &f})
	if err != nil {
		t.Fatal(err)
	}
	var doc bson.M
	_ = bson.Unmarshal(raw, &doc)
	if stored, _ := doc["failover"].(bson.M); stored["fail_after"] == nil || stored["failback_after"] != "0s" {
		t.Fatalf("stored as %v", doc["failover"])
	}
	var back Build
	if err := bson.Unmarshal(raw, &back); err != nil || back.Failover == nil || *back.Failover != f {
		t.Fatalf("read back %+v: %v", back.Failover, err)
	}
	if (&Build{}).FailoverPolicy() != protocol.DefaultBuildFailover() {
		t.Fatal("a build without one")
	}
}
