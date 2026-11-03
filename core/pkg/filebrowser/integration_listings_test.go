package filebrowser

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
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

// A Windows host: one entry for a path however its case is written, spelled
// as the newest listing gives it (a file renamed by case alone), and a path
// typed with / found as with \.
func TestIntegrationWindowsListings(t *testing.T) {
	db := listingsDB(t)
	ctx := context.Background()
	entries := repository.NewFsEntryRepository(db)
	s := NewService(entries, repository.NewFsListingRepository(db), nil, nil, nil, nil, zap.NewNop())
	op := uuid.New()
	session := &models.MinionSession{SessionID: uuid.New(), OperationID: op, Number: 8,
		Hello: models.MinionHello{MachineID: "m-win", OS: "windows", User: `WIN\user`}}
	machine := models.FsMachineKey(*session)
	task := func() *models.MinionTask {
		return &models.MinionTask{TaskID: uuid.New(), OperationID: op, SessionID: session.SessionID}
	}
	t0 := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	list := func(at time.Time, names ...string) {
		t.Helper()
		var es []protocol.DirEntry
		for _, n := range names {
			es = append(es, file(n))
		}
		s.Listings(ctx, task(), session, []protocol.ListingPiece{{ID: 0, Part: 0, Last: true, Path: `C:\Docs`, Sep: `\`, Parent: `C:\`,
			Entries: es}}, at)
	}
	list(t0, "README.txt")
	list(t0.Add(time.Minute), "readme.txt")
	got, _, err := entries.Children(ctx, op, machine, `c:\docs`, repository.FsQuery{ShowDeleted: true, First: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "readme.txt" || got[0].Path != `C:\Docs\readme.txt` || got[0].GoneAt != nil {
		t.Fatalf("after a rename by case: %+v", got)
	}
	// The folder itself, asked for in another case or with /.
	for _, p := range []string{`C:\Docs`, `c:\DOCS`, `C:/Docs`, `c:/docs/`} {
		if f, err := s.Folder(ctx, op, machine, p); err != nil || f == nil || f.Path != `C:\Docs` {
			t.Fatalf("folder %q: %+v %v", p, f, err)
		}
		kids, _, err := s.Children(ctx, op, machine, p, repository.FsQuery{First: 50})
		if err != nil || len(kids) != 1 {
			t.Fatalf("children of %q: %d %v", p, len(kids), err)
		}
	}
	// A listing of the folder in another case keeps the folder's own
	// spelling: its name comes from its parent's listing.
	s.Listings(ctx, task(), session, []protocol.ListingPiece{{ID: 0, Part: 0, Last: true, Path: `c:\docs`, Sep: `\`, Parent: `c:\`,
		Entries: []protocol.DirEntry{file("readme.txt")}}}, t0.Add(2*time.Minute))
	if f, _ := s.Folder(ctx, op, machine, `C:\Docs`); f == nil || f.Path != `C:\Docs` || f.Name != "Docs" {
		t.Fatalf("the folder after a lowercase listing: %+v", f)
	}
}

// volumeTasks are the tasks of the volumes test: each one's parts.
type volumeTasks map[uuid.UUID][]models.MinionTaskPart

func (v volumeTasks) Parts(_ context.Context, id uuid.UUID) ([]models.MinionTaskPart, error) {
	return v[id], nil
}

type oneSession struct{ s *models.MinionSession }

func (o oneSession) FindBySessionID(context.Context, uuid.UUID) (*models.MinionSession, error) {
	return o.s, nil
}
func (o oneSession) ByMachine(context.Context, uuid.UUID, string) ([]models.MinionSession, error) {
	return []models.MinionSession{*o.s}, nil
}

// changedFolders is a bus that keeps the folders file browser events name.
type changedFolders struct {
	eventbus.NopEventBus
	folders []string
}

func (c *changedFolders) Publish(e eventbus.Event) {
	if p, ok := e.Payload.(eventbus.FileBrowserEventPayload); ok {
		c.folders = append(c.folders, p.Folder)
	}
}

// A volumes task: each volume's path a folder holding it, a drive as a root
// (in parts, as a minion with small messages sends them); a later list
// without a drive takes its volume away; an older one changes nothing, nor
// brings back a drive the newer one no longer had.
func TestIntegrationVolumes(t *testing.T) {
	db := listingsDB(t)
	ctx := context.Background()
	entries := repository.NewFsEntryRepository(db)
	tasks := volumeTasks{}
	op := uuid.New()
	session := &models.MinionSession{SessionID: uuid.New(), OperationID: op, Number: 9,
		Hello: models.MinionHello{MachineID: "m-vol", OS: "windows", User: `WIN\user`}}
	bus := &changedFolders{}
	s := NewService(entries, repository.NewFsListingRepository(db), tasks, oneSession{session}, nil, bus, zap.NewNop())
	machine := models.FsMachineKey(*session)
	t0 := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	size, free := uint64(64<<30), uint64(20<<30)
	run := func(at time.Time, parts ...protocol.Volumes) {
		t.Helper()
		id := uuid.New()
		for i, v := range parts {
			b, err := protocol.ResultData(v)
			if err != nil {
				t.Fatal(err)
			}
			tasks[id] = append(tasks[id], models.MinionTaskPart{Seq: uint64(i), DataCBOR: b})
		}
		bus.folders = nil
		s.TaskEnded(ctx, &models.MinionTask{TaskID: id, OperationID: op, SessionID: session.SessionID, State: models.MinionTaskDone,
			DoneAt: &at, Result: protocol.ResultVolumes, Action: protocol.ActionListVolumes})
	}
	roots := func() map[string]*models.FsVolume {
		t.Helper()
		got, err := s.Roots(ctx, op, machine, false)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*models.FsVolume{}
		for _, e := range got {
			out[e.Path] = e.Volume
		}
		return out
	}

	run(t0,
		protocol.Volumes{Sep: `\`, Volumes: []protocol.Volume{{Path: `C:\`, Kind: protocol.VolumeFixed, Label: "System", FS: "NTFS", Size: &size, Free: &free}}},
		protocol.Volumes{Sep: `\`, Volumes: []protocol.Volume{{Path: `D:\`, Kind: protocol.VolumeOptical, NoMedia: true},
			{Path: `C:\Mount\Data`, Kind: protocol.VolumeFixed, FS: "NTFS"}}})
	r := roots()
	if len(r) != 2 || r[`C:\`] == nil || r[`C:\`].FS != "NTFS" || *r[`C:\`].Free != 20<<30 || r[`C:\`].SeenBy.Number != 9 ||
		r[`D:\`] == nil || !r[`D:\`].NoMedia {
		t.Fatalf("roots: %+v", r)
	}
	if f, _ := s.Folder(ctx, op, machine, `C:\Mount\Data`); f == nil || f.Type != "dir" || f.Volume == nil || f.ListedAt != nil {
		t.Fatalf("a volume mounted in a folder: %+v", f)
	}
	if f, _ := s.Folder(ctx, op, machine, `C:\Mount`); f == nil || f.Volume != nil {
		t.Fatalf("the folder on the way to it: %+v", f)
	}
	if !slices.Equal(bus.folders, []string{`C:\`, `D:\`, `C:\Mount\Data`}) {
		t.Fatalf("changed: %v", bus.folders)
	}

	// D: no longer there: its root stays (what was seen of it), without a
	// volume; the change names it.
	run(t0.Add(time.Minute), protocol.Volumes{Sep: `\`, Volumes: []protocol.Volume{{Path: `C:\`, Kind: protocol.VolumeFixed, FS: "NTFS"},
		{Path: `C:\Mount\Data`, Kind: protocol.VolumeFixed}}})
	r = roots()
	if len(r) != 2 || r[`D:\`] != nil || r[`C:\`] == nil || r[`C:\`].Label != "" {
		t.Fatalf("after D: went: %+v", r)
	}
	if !slices.Contains(bus.folders, `D:\`) {
		t.Fatalf("changed: %v", bus.folders)
	}

	// An older list (a task that ended earlier, taken late) changes nothing.
	run(t0.Add(30*time.Second), protocol.Volumes{Sep: `\`, Volumes: []protocol.Volume{{Path: `C:\`, Kind: protocol.VolumeFixed, Label: "Old"},
		{Path: `E:\`, Kind: protocol.VolumeRemovable}}})
	r = roots()
	if _, there := r[`E:\`]; there || r[`C:\`] == nil || r[`C:\`].Label != "" {
		t.Fatalf("after an older list: %+v", r)
	}
	if f, _ := s.Folder(ctx, op, machine, `C:\Mount\Data`); f == nil || f.Volume == nil {
		t.Fatalf("an older list took a newer volume away: %+v", f)
	}
}
