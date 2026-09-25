package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// FindChangedSinceLastBackup, against a real server. "Changed since its last
// backup" compares two fields of one document, which no index can answer, so
// without a window the scheduler's query read the whole collection every
// tick. The window must select the right documents, oldest change first, and
// must actually be served by an index.
func TestIntegrationBackupWindow(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewWikiDocumentRepository(db)

	raw, err := db.Collection(wikiDocumentCollection).(*database.QmgoCollection).RawCollection()
	if err != nil {
		t.Fatalf("raw collection: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	longAgo, recently := now.Add(-48*time.Hour), now.Add(-10*time.Minute)
	opID := uuid.New()
	seed := func(title string, updated time.Time, backedUp *time.Time) *models.WikiDocument {
		t.Helper()
		doc := &models.WikiDocument{DocumentID: uuid.New(), OperationID: opID, Title: title}
		if err := repo.Create(ctx, doc); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		// Straight through the driver: qmgo stamps updateAt with the current
		// time on every insert, and in production the Hocuspocus sidecar
		// writes it this way too.
		set := bson.M{"updateAt": updated}
		if backedUp != nil {
			set["last_backup_at"] = *backedUp
		}
		if _, err := raw.UpdateOne(ctx, bson.M{"document_id": doc.DocumentID}, bson.M{"$set": set}); err != nil {
			t.Fatalf("stamp %q: %v", title, err)
		}
		return doc
	}
	after := func(t time.Time) *time.Time { t = t.Add(time.Minute); return &t }
	before := func(t time.Time) *time.Time { t = t.Add(-time.Minute); return &t }

	seed("Unchanged since backup", longAgo, after(longAgo))
	neverBackedUp := seed("Never backed up", longAgo, nil)
	editedOld := seed("Edited long ago", longAgo.Add(time.Hour), before(longAgo.Add(time.Hour)))
	editedRecently := seed("Edited recently", recently, before(recently))

	titles := func(since time.Time) []string {
		t.Helper()
		docs, err := repo.FindChangedSinceLastBackup(ctx, since, 10)
		if err != nil {
			t.Fatalf("FindChangedSinceLastBackup: %v", err)
		}
		return titlesOf(docs)
	}

	// No window: every changed document, oldest change first.
	assertSameOrder(t, "full pass", titles(time.Time{}),
		[]string{neverBackedUp.Title, editedOld.Title, editedRecently.Title})
	// A window: only what changed inside it.
	assertSameOrder(t, "windowed pass", titles(now.Add(-time.Hour)),
		[]string{editedRecently.Title})

	// The windowed pass must be an index range, not a collection scan.
	mdb := raw.Database()
	if err := mdb.RunCommand(ctx, bson.D{{Key: "profile", Value: 2}}).Err(); err != nil {
		t.Skipf("cannot enable the profiler on the scratch database (%v); needs dbAdmin", err)
	}
	t.Cleanup(func() { _ = mdb.RunCommand(ctx, bson.D{{Key: "profile", Value: 0}}).Err() })
	titles(now.Add(-time.Hour))

	var entry struct {
		PlanSummary string `bson:"planSummary"`
	}
	err = mdb.Collection("system.profile").FindOne(ctx, bson.M{
		"ns":                      mdb.Name() + "." + wikiDocumentCollection,
		"command.find":            wikiDocumentCollection,
		"command.filter.updateAt": bson.M{"$exists": true},
	}).Decode(&entry)
	if err != nil {
		t.Fatalf("find the profiled query: %v", err)
	}
	if entry.PlanSummary == "COLLSCAN" {
		t.Errorf("the windowed backup query scanned the collection; want the updateAt index")
	}
}
