package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// Backup reference indexing, against a real server. The sweepers' safety
// rests on two answers only a server gives: that a backup written before the
// field existed — where it is absent, not false — counts as unindexed, and
// that the liveness lookup on backups is scoped to its operation like the one
// on documents.
func TestIntegrationBackupReferences(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewWikiDocumentBackupRepository(db)
	// The new indexes must build: a failed build is fatal at boot.
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("backup indexes: %v", err)
	}

	raw, err := db.Collection(wikiDocumentBackupCollection).(*database.QmgoCollection).RawCollection()
	if err != nil {
		t.Fatalf("raw collection: %v", err)
	}

	opID, otherOp := uuid.New(), uuid.New()
	kept, gone, elsewhere := uuid.New(), uuid.New(), uuid.New()

	// Written the way a pre-indexing build wrote it: no references_indexed.
	legacyID := uuid.New()
	if _, err := raw.InsertOne(ctx, bson.M{"backup_id": legacyID, "operation_id": opID, "document_id": uuid.New()}); err != nil {
		t.Fatalf("insert legacy backup: %v", err)
	}
	// Written by this build, never indexed: the field is false.
	unindexed := &models.WikiDocumentBackup{BackupID: uuid.New(), OperationID: opID, DocumentID: uuid.New()}
	// Written by this build, indexed at creation.
	indexed := &models.WikiDocumentBackup{BackupID: uuid.New(), OperationID: opID, DocumentID: uuid.New()}
	indexed.IndexReferences([]uuid.UUID{kept}, nil)
	// Another operation's backup naming an id from this one must not keep it.
	foreign := &models.WikiDocumentBackup{BackupID: uuid.New(), OperationID: otherOp, DocumentID: uuid.New()}
	foreign.IndexReferences([]uuid.UUID{elsewhere}, nil)
	for _, b := range []*models.WikiDocumentBackup{unindexed, indexed, foreign} {
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("create backup: %v", err)
		}
	}

	held, err := repo.HasUnindexed(ctx, opID)
	if err != nil || !held {
		t.Fatalf("HasUnindexed(op) = %v, %v; want true for a backup with the field absent or false", held, err)
	}
	if held, _ := repo.HasUnindexed(ctx, otherOp); held {
		t.Error("HasUnindexed(otherOp) = true; its only backup is indexed")
	}

	pending, err := repo.FindUnindexed(ctx, 10)
	if err != nil {
		t.Fatalf("FindUnindexed: %v", err)
	}
	ids := map[uuid.UUID]bool{}
	for _, b := range pending {
		ids[b.BackupID] = true
	}
	if len(pending) != 2 || !ids[legacyID] || !ids[unindexed.BackupID] {
		t.Fatalf("FindUnindexed = %v, want the legacy and the unindexed backup", ids)
	}

	for _, id := range []uuid.UUID{legacyID, unindexed.BackupID} {
		if err := repo.SetReferences(ctx, id, nil, nil); err != nil {
			t.Fatalf("SetReferences: %v", err)
		}
	}
	if held, _ := repo.HasUnindexed(ctx, opID); held {
		t.Error("HasUnindexed(op) still true after indexing every backup")
	}

	live, err := repo.FilterReferencedImageIDs(ctx, opID, []uuid.UUID{kept, gone, elsewhere})
	if err != nil {
		t.Fatalf("FilterReferencedImageIDs: %v", err)
	}
	if _, ok := live[kept]; !ok || len(live) != 1 {
		t.Errorf("live = %v, want only %s (scoped to the operation)", live, kept)
	}
}
