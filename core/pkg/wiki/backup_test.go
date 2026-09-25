package wiki

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// backupDocs serves one scripted batch per tick and records the window each
// tick asked for.
type backupDocs struct {
	repository.IWikiDocumentRepository
	batches [][]models.WikiDocument
	asked   []time.Time
}

func (f *backupDocs) FindChangedSinceLastBackup(_ context.Context, since time.Time, _ int64) ([]models.WikiDocument, error) {
	f.asked = append(f.asked, since)
	if len(f.batches) == 0 {
		return nil, nil
	}
	batch := f.batches[0]
	f.batches = f.batches[1:]
	return batch, nil
}

func (f *backupDocs) Update(context.Context, *models.WikiDocument, map[string]interface{}) error {
	return nil
}

type backupStore struct {
	repository.IWikiDocumentBackupRepository
	failFor uuid.UUID
}

func (f *backupStore) Create(_ context.Context, b *models.WikiDocumentBackup) error {
	if b.DocumentID == f.failFor {
		return errors.New("object store unavailable")
	}
	return nil
}

func docs(n int) []models.WikiDocument {
	out := make([]models.WikiDocument, n)
	for i := range out {
		out[i] = models.WikiDocument{DocumentID: uuid.New()}
	}
	return out
}

// TestBackupScheduler_ScansOnlySinceItsLastCompletePass pins the window the
// scheduler asks for. Its query compares two fields per document, which no
// index answers, so an unbounded search reads the whole collection — every
// tick, changed or not. After one pass that leaves nothing behind, each tick
// must only look at documents updated since, and keep looking further back
// whenever a pass could not finish.
func TestBackupScheduler_ScansOnlySinceItsLastCompletePass(t *testing.T) {
	failing := docs(1)
	repo := &backupDocs{batches: [][]models.WikiDocument{
		docs(1), // first pass: drains everything
		docs(2), // full batch: may have left documents behind
		failing, // a backup fails: must be retried
		docs(1), // drains again
		nil,     // nothing changed
	}}
	s := NewBackupScheduler(repo, &backupStore{failFor: failing[0].DocumentID}, zap.NewNop(), time.Hour)
	s.batchSize = 2

	before := time.Now().UTC()
	for range 5 {
		s.runBackupTick()
	}

	if !repo.asked[0].IsZero() {
		t.Fatalf("first pass searched from %v, want everything", repo.asked[0])
	}
	window := repo.asked[1]
	if window.IsZero() || window.After(before) || before.Sub(window) > backupScanOverlap+time.Minute {
		t.Fatalf("after a complete pass the search starts at %v, want just before %v", window, before)
	}
	// Neither the full batch nor the failed backup may move the window on.
	for i := 2; i <= 3; i++ {
		if !repo.asked[i].Equal(window) {
			t.Errorf("tick %d searched from %v, want the unchanged %v", i+1, repo.asked[i], window)
		}
	}
	if !repo.asked[4].After(window) {
		t.Errorf("after the next complete pass the search starts at %v, want later than %v", repo.asked[4], window)
	}
}
