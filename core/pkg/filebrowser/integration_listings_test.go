package filebrowser

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// listingsDB is a scratch database for one test, dropped after it; the test
// is skipped without INTEGRATION_MONGO_URI (make test-integration).
func listingsDB(t *testing.T) database.Database {
	t.Helper()
	uri := os.Getenv("INTEGRATION_MONGO_URI")
	if uri == "" {
		t.Skip("INTEGRATION_MONGO_URI not set; skipping the file browser's integration test")
	}
	name := fmt.Sprintf("itest_fb_%d", time.Now().UnixNano())
	db, err := database.Connect(context.Background(), uri, name)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri)); err == nil {
			_ = client.Database(name).Drop(ctx)
			_ = client.Disconnect(ctx)
		}
		_ = db.Close(ctx)
	})
	return db
}

func file(name string) protocol.DirEntry {
	size := uint64(len(name))
	return protocol.DirEntry{Name: name, Type: protocol.DirEntryFile, Size: &size}
}

// TestIntegrationListings: a listing's pieces go into the snapshot as they
// come, in any order, at the time of its first; what a whole one no longer
// has goes only once every piece is in; a partial one marks nothing gone; a
// folder not read says why until a listing of it.
func TestIntegrationListings(t *testing.T) {
	db := listingsDB(t)
	ctx := context.Background()
	entries := repository.NewFsEntryRepository(db)
	s := NewService(entries, repository.NewFsListingRepository(db), nil, nil, nil, nil, zap.NewNop())

	op := uuid.New()
	session := &models.MinionSession{SessionID: uuid.New(), OperationID: op, Number: 7, Hello: models.MinionHello{MachineID: "m1", User: "root"}}
	machine := models.FsMachineKey(*session)
	task := func() *models.MinionTask {
		return &models.MinionTask{TaskID: uuid.New(), OperationID: op, SessionID: session.SessionID}
	}
	names := func(path string, deleted bool) map[string]string {
		got, _, err := entries.Children(ctx, op, machine, path, repository.FsQuery{ShowDeleted: true, First: 500})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range got {
			if e.GoneAt == nil {
				out[e.Name] = "here"
			} else if deleted {
				out[e.Name] = e.GoneHow
			}
		}
		return out
	}
	t0 := time.Now().Add(-time.Hour).Truncate(time.Millisecond)

	// A whole listing in one piece.
	first := task()
	s.Listings(ctx, first, session, []protocol.ListingPiece{{ID: 0, Part: 0, Last: true, Path: "/data", Sep: "/", Parent: "/",
		Entries: []protocol.DirEntry{file("old.txt"), file("a.txt")}}}, t0)
	if got := names("/data", true); len(got) != 2 || got["old.txt"] != "here" {
		t.Fatalf("after the first listing: %v", got)
	}

	// A later listing in three pieces, its second first: it waits for the
	// first, which names the folder; nothing is gone until all are in.
	later := task()
	t1 := t0.Add(time.Minute)
	s.Listings(ctx, later, session, []protocol.ListingPiece{{ID: 0, Part: 1, Entries: []protocol.DirEntry{file("b.txt")}}}, t1)
	if got := names("/data", true); len(got) != 2 {
		t.Fatalf("a piece before its first changed the folder: %v", got)
	}
	s.Listings(ctx, later, session, []protocol.ListingPiece{{ID: 0, Part: 0, Path: "/data", Sep: "/", Parent: "/",
		Entries: []protocol.DirEntry{file("a.txt")}}}, t1.Add(time.Second))
	if got := names("/data", true); got["b.txt"] != "here" || got["old.txt"] != "here" {
		t.Fatalf("before its last piece: %v", got)
	}
	s.Listings(ctx, later, session, []protocol.ListingPiece{{ID: 0, Part: 2, Last: true, Entries: []protocol.DirEntry{file("c.txt")}}}, t1.Add(2*time.Second))
	got := names("/data", true)
	if got["a.txt"] != "here" || got["b.txt"] != "here" || got["c.txt"] != "here" || got["old.txt"] != models.FsGoneNotListed {
		t.Fatalf("after the whole listing: %v", got)
	}
	// The listing's time is when core took its first piece.
	folder, err := s.Folder(ctx, op, machine, "/data")
	if err != nil || folder.ListedAt == nil || !folder.ListedAt.Equal(t1) || folder.ListedBy.TaskID != later.TaskID {
		t.Fatalf("the folder: %+v %v", folder, err)
	}

	// A partial listing adds what it has and marks nothing gone.
	s.Listings(ctx, task(), session, []protocol.ListingPiece{{ID: 0, Part: 0, Last: true, Partial: true, Path: "/data", Sep: "/", Parent: "/",
		Entries: []protocol.DirEntry{file("d.txt")}}}, t1.Add(time.Minute))
	if got := names("/data", true); got["d.txt"] != "here" || got["a.txt"] != "here" {
		t.Fatalf("after a partial listing: %v", got)
	}

	// Several listings in one part, one of a folder not read: it says why,
	// until a listing of it.
	walk := task()
	s.Listings(ctx, walk, session, []protocol.ListingPiece{
		{ID: 0, Part: 0, Last: true, Path: "/etc", Sep: "/", Parent: "/", Entries: []protocol.DirEntry{file("hosts")}},
		{ID: 1, Part: 0, Last: true, Path: "/root", Sep: "/", Parent: "/", Err: "permission denied"},
	}, t1.Add(2*time.Minute))
	root, err := s.Folder(ctx, op, machine, "/root")
	if err != nil || root == nil || root.ListErr != "permission denied" || root.ListedAt != nil {
		t.Fatalf("the folder not read: %+v %v", root, err)
	}
	if got := names("/etc", false); got["hosts"] != "here" {
		t.Fatalf("/etc: %v", got)
	}
	s.Listings(ctx, task(), session, []protocol.ListingPiece{{ID: 0, Part: 0, Last: true, Path: "/root", Sep: "/", Parent: "/",
		Entries: []protocol.DirEntry{file(".bashrc")}}}, t1.Add(3*time.Minute))
	root, _ = s.Folder(ctx, op, machine, "/root")
	if root.ListErr != "" || root.ListedAt == nil {
		t.Fatalf("a listing did not clear the failure: %+v", root)
	}
}
