package wiki

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// BackupReferenceIndexer fills in the attachment references of backups taken
// before backups recorded them.
//
// Until an operation's backups are all indexed, the attachment sweepers
// delete nothing in it (see attachmentKind.held), because an unindexed
// backup might be the only thing still using a blob — and a restore of it
// would bring back an empty frame. So this is what lets the sweepers resume,
// and it runs on its own at boot rather than as a script somebody has to
// remember: forgetting it would not lose data, but it would quietly stop
// reclaiming storage.
//
// Only the sidecar can read a body's bytes, so each backup costs one call to
// it. A sidecar that is not up yet is retried; the indexer stops once nothing
// is left to index, which on an installation created after this change is on
// the first query.
type BackupReferenceIndexer struct {
	backups repository.IWikiDocumentBackupRepository
	refs    AttachmentReferenceReader
	logger  *zap.Logger
	retry   time.Duration
	batch   int64
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
}

// AttachmentReferenceReader derives the attachments a stored body uses.
// Satisfied by *HocuspocusClient.
type AttachmentReferenceReader interface {
	AttachmentReferences(ctx context.Context, contentState []byte, kind models.WikiDocumentKind, text string) (AttachmentReferences, error)
}

func NewBackupReferenceIndexer(
	backups repository.IWikiDocumentBackupRepository,
	refs AttachmentReferenceReader,
	logger *zap.Logger,
) *BackupReferenceIndexer {
	ctx, cancel := context.WithCancel(context.Background())
	return &BackupReferenceIndexer{
		backups: backups,
		refs:    refs,
		logger:  logger,
		retry:   time.Minute,
		// Small: each row carries its whole content_state.
		batch:  50,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

// Start indexes in the background until nothing is left.
func (x *BackupReferenceIndexer) Start() {
	go func() {
		defer close(x.done)
		total := 0
		for {
			n, err := x.indexBatch(x.ctx)
			total += n
			if x.ctx.Err() != nil {
				return
			}
			if err != nil {
				x.logger.Warn("Backup reference indexing paused, retrying",
					zap.Int("indexed_so_far", total), zap.Duration("retry_in", x.retry), zap.Error(err))
				select {
				case <-time.After(x.retry):
					continue
				case <-x.ctx.Done():
					return
				}
			}
			if n == 0 {
				if total > 0 {
					x.logger.Info("Backup reference indexing complete", zap.Int("indexed", total))
				}
				return
			}
		}
	}()
}

// Stop ends indexing and waits for the batch in progress.
func (x *BackupReferenceIndexer) Stop() {
	x.cancel()
	<-x.done
}

// indexBatch indexes up to one batch and reports how many it indexed. An
// error leaves the rest of the batch for the next attempt: the rows are
// still unindexed, so they come back first.
func (x *BackupReferenceIndexer) indexBatch(ctx context.Context) (int, error) {
	rows, err := x.backups.FindUnindexed(ctx, x.batch)
	if err != nil {
		return 0, err
	}
	indexed := 0
	for _, b := range rows {
		refs, err := x.refs.AttachmentReferences(ctx, b.ContentState, b.Kind, b.Content)
		if errors.Is(err, ErrStateUndecodable) {
			// Bytes nothing can read cannot be restored either — the restore
			// routes refuse them the same way — so the state has no pictures
			// to bring back. Its text may still name some; keep those.
			refs, err = x.refs.AttachmentReferences(ctx, nil, b.Kind, b.Content)
		}
		if err != nil {
			return indexed, err
		}
		if err := x.backups.SetReferences(ctx, b.BackupID, parseIDs(refs.ImageReferences), parseIDs(refs.FileReferences)); err != nil {
			return indexed, err
		}
		indexed++
	}
	return indexed, nil
}

// parseIDs keeps the ids that parse. One malformed id must not leave the
// whole backup unindexed — that would hold its operation's sweep forever.
func parseIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}
