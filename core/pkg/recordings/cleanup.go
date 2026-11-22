package recordings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.uber.org/zap"
)

// Retention cleanups (docs: Storage, Retention cleanups), an admin's or
// the retention policy's (pkg/storage, which runs them in batches): they
// take the recordings that ended before a time, but those of tasks marked
// important. A recording taken loses its files (its parts, or its video
// and keys) and keeps its record, marked cleaned up (CleanedAt), which its
// task still lists; it no longer plays. Nothing is kept elsewhere first.
//
// A recording is marked first, then its files are deleted: the mark keeps
// the converter off it (ClaimToConvert passes it over, and a conversion
// that outlived its lease keeps no video: SaveKept), and one the encoder
// has under a lease running is not marked. Files the store did not delete
// take the mark off again, for a cleanup to come.

// ErrCleaned: a retention cleanup deleted the recording's files.
var ErrCleaned = errors.New("a retention cleanup deleted its files: it no longer plays")

// ToClean are the recordings a cleanup of those saved before before takes
// now: of one operation (nil: of every one), the oldest first, at most
// limit.
func (s *Service) ToClean(ctx context.Context, before time.Time, operationID *uuid.UUID, limit int64) ([]models.Recording, error) {
	return s.repo.ToClean(ctx, before, s.now(), operationID, limit)
}

// ToCleanByOperation sums them, every operation's, by operation.
func (s *Service) ToCleanByOperation(ctx context.Context, before time.Time) ([]models.RecordingsToClean, error) {
	return s.repo.ToCleanByOperation(ctx, before, s.now())
}

// CleanUp takes recs (ToClean's) for cleanup: each marked cleaned up, then
// its files deleted; their tasks' lists are told. One a cleanup no longer
// takes is passed over (another took it, it was deleted, or the encoder has
// it). At the store's first failure it stops, that recording unmarked, and
// says it: what it took before stays taken. ctx ending stops it before its
// next recording (ctx's error): the one under way is taken whole. It
// answers how many it took, and their bytes.
func (s *Service) CleanUp(ctx context.Context, recs []models.Recording, cleanup uuid.UUID) (int64, int64, error) {
	var taken, bytes int64
	// A recording begun is taken whole, whatever ctx does meanwhile.
	work := context.WithoutCancel(ctx)
	tasks := map[uuid.UUID]bool{}
	defer func() {
		for id := range tasks {
			if t, err := s.tasks.AddRecording(work, id, 0, 0); err == nil {
				s.publish(t)
			}
		}
	}()
	for _, rec := range recs {
		if err := ctx.Err(); err != nil {
			return taken, bytes, err
		}
		// As it is now: its video may have come since the batch was read.
		marked, err := s.repo.MarkCleaned(work, rec.RecordingID, cleanup, s.now())
		if err != nil {
			return taken, bytes, fmt.Errorf("mark a recording cleaned up: %w", err)
		}
		if marked == nil {
			continue
		}
		if err := s.dropAll(work, *marked); err != nil {
			if uerr := s.repo.UnmarkCleaned(work, marked.RecordingID); uerr != nil {
				s.logger.Warn("A recording a cleanup did not take is still marked cleaned up",
					zap.String("recording_id", marked.RecordingID.String()), zap.Error(uerr))
			}
			return taken, bytes, fmt.Errorf("delete a recording's files: %w", err)
		}
		tasks[marked.TaskID] = true
		taken, bytes = taken+1, bytes+marked.StoredBytes()
	}
	return taken, bytes, nil
}
