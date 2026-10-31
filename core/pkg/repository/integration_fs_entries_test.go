package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

func fsEntry(path, parent, name, typ string, size int64) models.FsEntry {
	e := models.FsEntry{Path: path, PathKey: path, Parent: parent, ParentKey: parent, Name: name, NameKey: strings.ToLower(name), Sep: "/", Type: typ}
	if typ == "file" {
		e.Size = &size
	}
	return e
}

// A machine's snapshot: listings set a folder's children, a newer listing
// marks what it no longer has gone, an older one changes nothing; a delete
// takes a folder and what is under it; a download is recorded; children
// page in order, folders first; a search finds names.
func TestIntegrationFsEntries(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewFsEntryRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	op, m := uuid.New(), "linux-machine-id:abc"
	ref := models.FsTaskRef{TaskID: uuid.New(), SessionID: uuid.New(), Number: 7, User: "root"}
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	tmp := fsEntry("/tmp", "/", "tmp", "dir", 0)
	_ = repo.Ensure(ctx, op, m, []models.FsEntry{fsEntry("/", "", "/", "dir", 0)})
	listing := []models.FsEntry{fsEntry("/tmp/a.txt", "/tmp", "a.txt", "file", 5), fsEntry("/tmp/sub", "/tmp", "sub", "dir", 0),
		fsEntry("/tmp/B.bin", "/tmp", "B.bin", "file", 900)}
	if ok, err := repo.Listing(ctx, op, m, tmp, listing, true, t0, ref); !ok || err != nil {
		t.Fatalf("listing: %v %v", ok, err)
	}
	page, _, _ := repo.Children(ctx, op, m, "/tmp", FsQuery{})
	if len(page) != 3 || page[0].Name != "sub" || page[1].Name != "a.txt" || page[2].Name != "B.bin" {
		t.Fatalf("children by name, folders first: %v", names(page))
	}
	bySize, _, _ := repo.Children(ctx, op, m, "/tmp", FsQuery{Sort: "size", Desc: true})
	if bySize[1].Name != "B.bin" {
		t.Fatalf("by size, desc: %v", names(bySize))
	}
	// Paged one at a time: the same order.
	var all []string
	for after := ""; ; {
		p, next, err := repo.Children(ctx, op, m, "/tmp", FsQuery{First: 1, After: after})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, names(p)...)
		if next == "" {
			break
		}
		after = next
	}
	if strings.Join(all, ",") != "sub,a.txt,B.bin" {
		t.Fatalf("paged: %v", all)
	}
	folder, _ := repo.Get(ctx, op, m, "/tmp")
	if folder.ListedAt == nil || folder.ListedBy.Number != 7 {
		t.Fatalf("the folder's listing: %+v", folder)
	}

	// An older listing: nothing.
	if ok, _ := repo.Listing(ctx, op, m, tmp, listing[:1], true, t0.Add(-time.Minute), ref); ok {
		t.Fatal("an older listing applied")
	}
	// A newer one without B.bin: B.bin gone, not listed.
	t1 := t0.Add(time.Minute)
	if ok, _ := repo.Listing(ctx, op, m, tmp, listing[:2], true, t1, ref); !ok {
		t.Fatal("a newer listing not applied")
	}
	if b, _ := repo.Get(ctx, op, m, "/tmp/B.bin"); b.GoneAt == nil || b.GoneHow != models.FsGoneNotListed {
		t.Fatalf("not listed: %+v", b)
	}
	if shown, _, _ := repo.Children(ctx, op, m, "/tmp", FsQuery{}); len(shown) != 2 {
		t.Fatalf("deleted hidden: %v", names(shown))
	}
	if shown, _, _ := repo.Children(ctx, op, m, "/tmp", FsQuery{ShowDeleted: true}); len(shown) != 3 {
		t.Fatalf("deleted shown: %v", names(shown))
	}

	// sub/x, then sub deleted: both gone.
	sub := fsEntry("/tmp/sub", "/tmp", "sub", "dir", 0)
	_, _ = repo.Listing(ctx, op, m, sub, []models.FsEntry{fsEntry("/tmp/sub/x", "/tmp/sub", "x", "file", 1)}, true, t1, ref)
	if err := repo.Gone(ctx, op, m, sub, models.FsGoneDeleted, t1.Add(time.Second), ref); err != nil {
		t.Fatal(err)
	}
	if x, _ := repo.Get(ctx, op, m, "/tmp/sub/x"); x.GoneAt == nil || x.GoneHow != models.FsGoneDeleted {
		t.Fatalf("under a deleted folder: %+v", x)
	}

	// A download of a.txt: recorded, the entry kept.
	fid := uuid.New()
	if err := repo.Transfer(ctx, op, m, fsEntry("/tmp/a.txt", "/tmp", "a.txt", "file", 0), false,
		models.FsTransfer{TaskID: uuid.New(), FileID: &fid, Size: 5, At: t1.Add(2 * time.Second)}, ref); err != nil {
		t.Fatal(err)
	}
	if a, _ := repo.Get(ctx, op, m, "/tmp/a.txt"); len(a.Downloaded) != 1 || *a.Downloaded[0].FileID != fid {
		t.Fatalf("download: %+v", a)
	}

	// Search by name, deleted ones only when asked.
	if found, _, _ := repo.Search(ctx, op, m, `\.bin`, false, "", 10); len(found) != 0 {
		t.Fatalf("search, deleted hidden: %v", names(found))
	}
	if found, _, _ := repo.Search(ctx, op, m, `\.bin`, true, "", 10); len(found) != 1 {
		t.Fatalf("search, deleted shown: %v", names(found))
	}
	// Another machine sees none of it.
	if other, _, _ := repo.Children(ctx, op, "other", "/tmp", FsQuery{}); len(other) != 0 {
		t.Fatal("another machine's")
	}
}

func names(es []models.FsEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}
