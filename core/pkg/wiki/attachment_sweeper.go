package wiki

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"go.uber.org/zap"
)

// attachmentSweeper runs a periodic GC pass over one kind of wiki attachment
// — images or files — deleting entries whose bytes are no longer referenced
// by any active or trashed document. ImageSweeper and FileSweeper are this
// type, configured by attachmentKind.
//
// Liveness is decided by the document's reference array for the kind
// (image_references, file_references) — the authoritative index the
// Hocuspocus sidecar rewrites on every content persist. (The older approach
// of regex-scanning the document's `content` field was unsound: `content` is
// a plain-text snapshot that never carried attachment URLs, so every aged
// image looked unreferenced and got deleted while still embedded in the live
// document.)
//
// Only attachments older than the grace period are considered, which leaves
// a window for uploads-in-flight (optimistic insert → CRDT sync → Mongo
// snapshot) to land without getting swept.
//
// When dryRun is set the sweeper logs what it WOULD delete but touches
// nothing — the safety gate used to validate the reference index in
// production before arming real deletion.
type attachmentSweeper[T any] struct {
	kind      attachmentKind[T]
	store     blob.ObjectStore
	logger    *zap.Logger
	interval  time.Duration
	grace     time.Duration
	dryRun    bool
	batchSize int64
	ctx       context.Context
	cancel    context.CancelFunc
}

// attachmentKind is what differs between images and files: where their rows
// live, how a row names itself, how documents reference it, and the words the
// logs use for it.
type attachmentKind[T any] struct {
	repo attachmentRows[T]
	// referenced returns the subset of ids some document in opID references.
	referenced func(ctx context.Context, opID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error)
	id         func(T) uuid.UUID
	operation  func(T) uuid.UUID
	objectKey  func(T) string
	// name is the kind in lower case ("image"), title is the capitalised
	// form ("Image"), idKey the log field for the row id ("image_id").
	name, title, idKey string
}

// attachmentRows is the part of an attachment repository the sweeper uses.
type attachmentRows[T any] interface {
	FindCandidatesOlderThan(ctx context.Context, cutoff time.Time, limit int64) ([]T, error)
	HardDelete(ctx context.Context, id uuid.UUID) error
}

func newAttachmentSweeper[T any](
	kind attachmentKind[T],
	store blob.ObjectStore,
	logger *zap.Logger,
	interval time.Duration,
	grace time.Duration,
	dryRun bool,
) *attachmentSweeper[T] {
	ctx, cancel := context.WithCancel(context.Background())
	return &attachmentSweeper[T]{
		kind:      kind,
		store:     store,
		logger:    logger,
		interval:  interval,
		grace:     grace,
		dryRun:    dryRun,
		batchSize: 200,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (s *attachmentSweeper[T]) Start() {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.logger.Info("Wiki "+s.kind.name+" sweeper started",
			zap.Duration("interval", s.interval),
			zap.Duration("grace", s.grace),
			zap.Bool("dry_run", s.dryRun),
		)

		for {
			select {
			case <-ticker.C:
				s.runTick()
			case <-s.ctx.Done():
				s.logger.Info("Wiki " + s.kind.name + " sweeper stopped")
				return
			}
		}
	}()
}

func (s *attachmentSweeper[T]) Stop() {
	s.cancel()
}

func (s *attachmentSweeper[T]) runTick() {
	// Keep the tick bounded so it cannot overlap with the next one.
	tickCtx, cancel := context.WithTimeout(s.ctx, s.interval/2)
	defer cancel()

	cutoff := time.Now().UTC().Add(-s.grace)
	candidates, err := s.kind.repo.FindCandidatesOlderThan(tickCtx, cutoff, s.batchSize)
	if err != nil {
		s.logger.Error(s.kind.title+" sweeper: failed to list candidates", zap.Error(err))
		return
	}
	if len(candidates) == 0 {
		return
	}

	// Group candidates by operation: liveness is checked against the
	// operation's documents, and a reference only counts within its own
	// operation (attachment blobs are operation-scoped).
	byOp := make(map[uuid.UUID][]T, len(candidates))
	for _, a := range candidates {
		opID := s.kind.operation(a)
		byOp[opID] = append(byOp[opID], a)
	}

	deleted := 0
	wouldDelete := 0
	for opID, rows := range byOp {
		ids := make([]uuid.UUID, len(rows))
		for i, a := range rows {
			ids[i] = s.kind.id(a)
		}

		referenced, err := s.kind.referenced(tickCtx, opID, ids)
		if err != nil {
			// Fail safe: on any query error we must NOT delete — skip this
			// operation and retry next tick. Deleting on uncertainty is the
			// exact failure mode that lost data before.
			s.logger.Warn(s.kind.title+" sweeper: reference lookup failed, skipping operation",
				zap.String("operation_id", opID.String()),
				zap.Error(err))
			continue
		}

		for _, a := range rows {
			if _, ok := referenced[s.kind.id(a)]; ok {
				continue
			}
			if s.dryRun {
				wouldDelete++
				s.logger.Info(s.kind.title+" sweeper (dry-run): would delete unreferenced "+s.kind.name,
					zap.String(s.kind.idKey, s.kind.id(a).String()),
					zap.String("operation_id", opID.String()),
					zap.String("key", s.kind.objectKey(a)))
				continue
			}
			if s.hardDelete(tickCtx, a) {
				deleted++
			}
		}
	}

	if s.dryRun {
		if wouldDelete > 0 {
			s.logger.Info("Wiki "+s.kind.name+" sweeper (dry-run) completed", zap.Int("would_delete", wouldDelete))
		}
		return
	}
	if deleted > 0 {
		s.logger.Info("Wiki "+s.kind.name+" sweeper completed", zap.Int("deleted", deleted))
	}
}

// hardDelete removes the blob first, then the metadata row. If blob removal
// fails we keep the metadata so the next sweeper pass can retry — better
// than a dangling object in the bucket.
func (s *attachmentSweeper[T]) hardDelete(ctx context.Context, a T) bool {
	id := s.kind.id(a)
	if err := s.store.Delete(ctx, s.kind.objectKey(a)); err != nil {
		s.logger.Warn(s.kind.title+" sweeper: failed to delete object",
			zap.String(s.kind.idKey, id.String()),
			zap.String("key", s.kind.objectKey(a)),
			zap.Error(err))
		return false
	}
	if err := s.kind.repo.HardDelete(ctx, id); err != nil {
		s.logger.Warn(s.kind.title+" sweeper: failed to delete metadata",
			zap.String(s.kind.idKey, id.String()),
			zap.Error(err))
		return false
	}
	return true
}
