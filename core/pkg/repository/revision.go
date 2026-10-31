package repository

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// revised is an update (operators, not a pipeline) that also counts the
// row's rev and stamps its updateAt: the rows a client keeps live (hosts,
// hashes, credentials, tasks) carry both, so of two copies of a row the
// client keeps the one with the higher rev, whichever comes last (a
// mutation's answer after the live updates it raced).
func revised(update bson.M) bson.M {
	out := make(bson.M, len(update)+2)
	for k, v := range update {
		out[k] = v
	}
	set := bson.M{"updateAt": time.Now().UTC()}
	if have, ok := update["$set"].(bson.M); ok {
		for k, v := range have {
			set[k] = v
		}
	}
	out["$set"] = set
	inc := bson.M{"rev": 1}
	if have, ok := update["$inc"].(bson.M); ok {
		for k, v := range have {
			inc[k] = v
		}
	}
	out["$inc"] = inc
	return out
}
