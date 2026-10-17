package wiki

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// indexerBackups is an unindexed-backup store: FindUnindexed returns what
// SetReferences has not yet indexed, as the real query does.
type indexerBackups struct {
	repository.IWikiDocumentBackupRepository
	rows    []models.WikiDocumentBackup
	indexed map[uuid.UUID][2][]uuid.UUID
}

func (s *indexerBackups) FindUnindexed(_ context.Context, limit int64) ([]models.WikiDocumentBackup, error) {
	var out []models.WikiDocumentBackup
	for _, b := range s.rows {
		if _, done := s.indexed[b.BackupID]; !done && int64(len(out)) < limit {
			out = append(out, b)
		}
	}
	return out, nil
}

func (s *indexerBackups) SetReferences(_ context.Context, id uuid.UUID, images, files []uuid.UUID) error {
	s.indexed[id] = [2][]uuid.UUID{images, files}
	return nil
}

// scriptedRefs answers by backup content, standing in for the sidecar.
type scriptedRefs struct {
	byText map[string]AttachmentReferences
	// undecodable holds contents whose state the sidecar refuses.
	undecodable map[string]bool
	err         error
}

func (s *scriptedRefs) AttachmentReferences(_ context.Context, state []byte, _ models.WikiDocumentKind, text string) (AttachmentReferences, error) {
	if s.err != nil {
		return AttachmentReferences{}, s.err
	}
	if s.undecodable[text] && state != nil {
		return AttachmentReferences{}, ErrStateUndecodable
	}
	return s.byText[text], nil
}

func newIndexer(backups *indexerBackups, refs AttachmentReferenceReader) *BackupReferenceIndexer {
	x := NewBackupReferenceIndexer(backups, refs, zap.NewNop())
	x.batch = 2
	return x
}

func TestBackupReferenceIndexer_IndexesEveryBackup(t *testing.T) {
	img, file := uuid.New(), uuid.New()
	a := models.WikiDocumentBackup{BackupID: uuid.New(), Content: "a", ContentState: []byte{1}}
	b := models.WikiDocumentBackup{BackupID: uuid.New(), Content: "b", ContentState: []byte{1}}
	c := models.WikiDocumentBackup{BackupID: uuid.New(), Content: "c"}
	backups := &indexerBackups{rows: []models.WikiDocumentBackup{a, b, c}, indexed: map[uuid.UUID][2][]uuid.UUID{}}
	refs := &scriptedRefs{byText: map[string]AttachmentReferences{
		"a": {ImageReferences: []string{img.String(), "not-a-uuid"}},
		"b": {FileReferences: []string{file.String()}},
	}}

	x := newIndexer(backups, refs)
	x.Start()
	<-x.done

	if len(backups.indexed) != 3 {
		t.Fatalf("indexed %d of 3 backups", len(backups.indexed))
	}
	if got := backups.indexed[a.BackupID][0]; len(got) != 1 || got[0] != img {
		t.Errorf("a's images = %v, want [%s] (the malformed id dropped, not the backup)", got, img)
	}
	if got := backups.indexed[b.BackupID][1]; len(got) != 1 || got[0] != file {
		t.Errorf("b's files = %v, want [%s]", got, file)
	}
	if got := backups.indexed[c.BackupID]; len(got[0])+len(got[1]) != 0 {
		t.Errorf("c references nothing, got %v", got)
	}
}

// Bytes nothing can decode cannot be restored either, so they hold no
// pictures to keep. Leaving the backup unindexed instead would hold its
// operation's sweep forever.
func TestBackupReferenceIndexer_UndecodableStateFallsBackToText(t *testing.T) {
	img := uuid.New()
	bad := models.WikiDocumentBackup{BackupID: uuid.New(), Content: "bad", ContentState: []byte{0xff}}
	backups := &indexerBackups{rows: []models.WikiDocumentBackup{bad}, indexed: map[uuid.UUID][2][]uuid.UUID{}}
	refs := &scriptedRefs{
		undecodable: map[string]bool{"bad": true},
		byText:      map[string]AttachmentReferences{"bad": {ImageReferences: []string{img.String()}}},
	}

	n, err := newIndexer(backups, refs).indexBatch(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("indexBatch = %d, %v; want 1, nil", n, err)
	}
	if got := backups.indexed[bad.BackupID][0]; len(got) != 1 || got[0] != img {
		t.Errorf("images = %v, want the one its text names", got)
	}
}

// A sidecar that is down is not an answer. Indexing a backup as referencing
// nothing on that error is exactly what would let the sweeper delete.
func TestBackupReferenceIndexer_SidecarErrorIndexesNothing(t *testing.T) {
	backups := &indexerBackups{
		rows:    []models.WikiDocumentBackup{{BackupID: uuid.New(), ContentState: []byte{1}}},
		indexed: map[uuid.UUID][2][]uuid.UUID{},
	}

	_, err := newIndexer(backups, &scriptedRefs{err: errors.New("connection refused")}).indexBatch(context.Background())
	if err == nil {
		t.Fatal("expected the sidecar error to surface")
	}
	if len(backups.indexed) != 0 {
		t.Errorf("a failed lookup indexed %d backups", len(backups.indexed))
	}
}
