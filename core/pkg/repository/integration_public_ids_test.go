package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestIntegrationPublicIDs pins how a collection gets public ids after rows
// exist (public_ids.go), on channel endpoints: rows an older core writes
// without one are not refused by the unique index, reading gives them one and
// keeps it, an id another instance gave a row first wins, the startup
// backfill gives the rest one, and two rows can still not share an id.
func TestIntegrationPublicIDs(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewChannelEndpointRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	coll := db.Collection(channelEndpointCollection)
	op := uuid.New()
	legacy := func(name string) primitive.ObjectID {
		t.Helper()
		oid := primitive.NewObjectID()
		if _, err := coll.InsertOne(ctx, bson.M{"_id": oid, "operation_id": op, "name": name, "module_name": "http",
			"schema_version": 1, "values": bson.M{}, "revision": int64(1)}); err != nil {
			t.Fatalf("an older core's insert of %s: %v", name, err)
		}
		return oid
	}
	legacy("old-1")
	legacy("old-2") // a second row without an id: the partial index lets it in
	fresh := &models.ChannelEndpoint{OperationID: op, Name: "new", ModuleName: "http", SchemaVersion: 1, Values: models.JSONMap{}}
	if err := repo.Insert(ctx, fresh); err != nil || fresh.EndpointID == uuid.Nil {
		t.Fatalf("insert: %v, id %s", err, fresh.EndpointID)
	}

	ids := func() map[string]uuid.UUID {
		t.Helper()
		eps, err := repo.List(ctx, op, "")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]uuid.UUID{}
		for _, ep := range eps {
			if ep.EndpointID == uuid.Nil {
				t.Fatalf("%s listed without an id", ep.Name)
			}
			out[ep.Name] = ep.EndpointID
		}
		return out
	}
	first := ids()
	if len(first) != 3 || first["new"] != fresh.EndpointID || first["old-1"] == first["old-2"] {
		t.Fatalf("ids %v", first)
	}
	if again := ids(); again["old-1"] != first["old-1"] || again["old-2"] != first["old-2"] {
		t.Fatalf("ids changed between reads: %v then %v", first, again)
	}
	if ep, err := repo.FindByEndpointID(ctx, first["old-1"]); err != nil || ep.Name != "old-1" {
		t.Fatalf("find by the id it was given: %v %+v", err, ep)
	}

	// Another instance gave the row an id between this one's read and its
	// fill: that id stays.
	raced := legacy("old-3")
	theirs := uuid.New()
	if err := coll.UpdateOne(ctx, bson.M{"_id": raced}, bson.M{"$set": bson.M{"endpoint_id": theirs}}); err != nil {
		t.Fatal(err)
	}
	stale := []models.ChannelEndpoint{{}}
	stale[0].Id = raced
	if err := fillIDs(ctx, coll, "endpoint_id", stale, func(ep *models.ChannelEndpoint) (primitive.ObjectID, *uuid.UUID) {
		return ep.Id, &ep.EndpointID
	}); err != nil || stale[0].EndpointID != theirs {
		t.Fatalf("fill after another instance: %s %v, want %s", stale[0].EndpointID, err, theirs)
	}

	legacy("old-4")
	if n, err := repo.BackfillEndpointIDs(ctx); n != 1 || err != nil {
		t.Fatalf("backfill: %d %v, want the one row without an id", n, err)
	}
	if n, err := repo.BackfillEndpointIDs(ctx); n != 0 || err != nil {
		t.Fatalf("second backfill: %d %v", n, err)
	}
	if n, _ := coll.Count(ctx, bson.M{"endpoint_id": bson.M{"$exists": false}}); n != 0 {
		t.Fatalf("%d rows still without an id", n)
	}

	_, err := coll.InsertOne(ctx, bson.M{"operation_id": op, "name": "copy", "endpoint_id": fresh.EndpointID})
	if !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("a second row with an id in use: %v", err)
	}
}

// TestIntegrationWikiVisitIDs pins a visit's id: a visit recorded by an older
// core gets one when it is next recorded, and keeps it.
func TestIntegrationWikiVisitIDs(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewWikiDocumentVisitRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	user, op, doc := uuid.New(), uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := db.Collection(wikiDocumentVisitCollection).InsertOne(ctx, bson.M{
		"_id": primitive.NewObjectID(), "user_id": user, "operation_id": op, "document_id": doc, "visited_at": at,
	}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.Upsert(ctx, user, op, doc, at.Add(time.Second))
	if err != nil || v.VisitID == uuid.Nil {
		t.Fatalf("revisit: %v, id %s", err, v.VisitID)
	}
	again, err := repo.Upsert(ctx, user, op, doc, at.Add(2*time.Second))
	if err != nil || again.VisitID != v.VisitID {
		t.Fatalf("id %s after another visit, was %s: %v", again.VisitID, v.VisitID, err)
	}
	other, err := repo.Upsert(ctx, user, op, uuid.New(), at)
	if err != nil || other.VisitID == uuid.Nil || other.VisitID == v.VisitID {
		t.Fatalf("a first visit: %v, id %s", err, other.VisitID)
	}
	listed, err := repo.FindByUserOperationActive(ctx, user, op, []uuid.UUID{doc, other.DocumentID}, 0, 10)
	if err != nil || len(listed) != 2 || listed[0].VisitID != v.VisitID {
		t.Fatalf("history %+v: %v", listed, err)
	}
}

// TestIntegrationInvocationIDs pins invocation ids across the switch from
// ULIDs: a ULID row reads as the UUID of its bits, the startup conversion
// stores that UUID for finished invocations only (a running one is finished
// by the core waiting for it, by the id it knows), and a converted row keeps
// its id.
func TestIntegrationInvocationIDs(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewModuleActionInvocationRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("index setup: %v", err)
	}
	coll := db.Collection(moduleActionInvocationCollection)
	at := time.Now().UTC().Truncate(time.Millisecond)
	for i, r := range []struct{ ulid, status string }{
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", models.ModuleActionSucceeded},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAW", models.ModuleActionRunning},
	} {
		if _, err := coll.InsertOne(ctx, bson.M{"invocation_id": r.ulid, "instance": "http-1", "action": "flush",
			"status": r.status, "started_at": at.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	fresh := &models.ModuleActionInvocation{InvocationID: models.NewInvocationID(), Instance: "http-1", Action: "flush",
		Status: models.ModuleActionRunning, StartedAt: at.Add(2 * time.Second)}
	if err := repo.Insert(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, fresh.InvocationID, models.ModuleActionSucceeded, "", "", at.Add(3*time.Second)); err != nil {
		t.Fatalf("finish by a binary id: %v", err)
	}

	list := func() []string {
		t.Helper()
		invs, err := repo.ListByInstance(ctx, "http-1", 0)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(invs))
		for i, inv := range invs {
			out[i] = inv.InvocationID.String()
		}
		return out
	}
	before := list()
	if len(before) != 3 || before[0] != fresh.InvocationID.String() || before[2] != "01563e3a-b5d3-d676-4c61-efb99302bd5b" {
		t.Fatalf("ids %v", before)
	}

	if n, err := repo.ConvertLegacyIDs(ctx); n != 1 || err != nil {
		t.Fatalf("convert: %d %v, want the finished one", n, err)
	}
	binary := func() int64 {
		n, _ := coll.Count(ctx, bson.M{"invocation_id": bson.M{"$type": "binData"}})
		return n
	}
	if n := binary(); n != 2 {
		t.Fatalf("%d binary ids, want 2: the running ULID is left alone", n)
	}
	// The core that started it finishes it by its ULID, then a later boot
	// converts it.
	if err := coll.UpdateOne(ctx, bson.M{"invocation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAW", "status": models.ModuleActionRunning},
		bson.M{"$set": bson.M{"status": models.ModuleActionSucceeded}}); err != nil {
		t.Fatalf("an older core finishing its invocation: %v", err)
	}
	if n, err := repo.ConvertLegacyIDs(ctx); n != 1 || err != nil || binary() != 3 {
		t.Fatalf("second conversion: %d %v", n, err)
	}
	if after := list(); after[0] != before[0] || after[1] != before[1] || after[2] != before[2] {
		t.Fatalf("ids changed by the conversion: %v, were %v", after, before)
	}
}
