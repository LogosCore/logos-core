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

// noBackups is a backup repository with every backup indexed and none
// referencing anything: the sweepers behave exactly as before backups
// counted.
type noBackups struct {
	repository.IWikiDocumentBackupRepository
}

func (noBackups) FilterReferencedImageIDs(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	return map[uuid.UUID]struct{}{}, nil
}

func (noBackups) FilterReferencedFileIDs(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	return map[uuid.UUID]struct{}{}, nil
}

func (noBackups) HasUnindexed(context.Context, uuid.UUID) (bool, error) { return false, nil }

// stubBackups references a fixed set of image ids, and can report an
// operation as not yet indexed or fail either lookup.
type stubBackups struct {
	noBackups
	images     map[uuid.UUID]struct{}
	unindexed  bool
	heldErr    error
	lookupErr  error
	lookupHits int
}

func (s *stubBackups) FilterReferencedImageIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	s.lookupHits++
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	out := map[uuid.UUID]struct{}{}
	for _, id := range ids {
		if _, ok := s.images[id]; ok {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

func (s *stubBackups) HasUnindexed(context.Context, uuid.UUID) (bool, error) {
	return s.unindexed, s.heldErr
}

func runImageSweep(t *testing.T, backups repository.IWikiDocumentBackupRepository, candidates ...models.WikiImage) *stubImageRepo {
	t.Helper()
	docRepo := &stubImageDocRepo{referenced: map[uuid.UUID]map[uuid.UUID]struct{}{}}
	imgRepo := &stubImageRepo{candidates: candidates}
	s := NewImageSweeper(docRepo, backups, imgRepo, &stubStore{}, zap.NewNop(), time.Hour, time.Hour, false)
	s.runTick()
	return imgRepo
}

// The fix: an image no live page uses any more but a backup still does is
// what that backup's restore would put back, so it must be kept.
func TestImageSweeper_KeepsAnImageOnlyABackupReferences(t *testing.T) {
	opID := uuid.New()
	inBackup := newImageCandidate(opID)
	orphan := newImageCandidate(opID)

	imgRepo := runImageSweep(t, &stubBackups{images: map[uuid.UUID]struct{}{inBackup.ImageID: {}}}, inBackup, orphan)

	if len(imgRepo.deleted) != 1 || imgRepo.deleted[0] != orphan.ImageID {
		t.Fatalf("expected only the image nothing references deleted, got %v", imgRepo.deleted)
	}
}

// A backup written before backups recorded references might be the only
// thing using any blob in its operation, so nothing there may go until it
// is indexed.
func TestImageSweeper_HoldsAnOperationWithUnindexedBackups(t *testing.T) {
	opID := uuid.New()
	imgRepo := runImageSweep(t, &stubBackups{unindexed: true}, newImageCandidate(opID))

	if len(imgRepo.deleted) != 0 {
		t.Fatalf("an operation with unindexed backups must delete nothing, got %v", imgRepo.deleted)
	}
}

func TestImageSweeper_FailSafeWhenTheBackupChecksFail(t *testing.T) {
	for name, backups := range map[string]*stubBackups{
		"index check":      {heldErr: errors.New("mongo down")},
		"reference lookup": {lookupErr: errors.New("mongo down")},
	} {
		t.Run(name, func(t *testing.T) {
			imgRepo := runImageSweep(t, backups, newImageCandidate(uuid.New()))
			if len(imgRepo.deleted) != 0 {
				t.Fatalf("a failed %s must skip deletion, got %v", name, imgRepo.deleted)
			}
		})
	}
}
