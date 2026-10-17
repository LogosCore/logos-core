package wiki

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// FileSweeper garbage-collects wiki_files rows whose bytes no document, and no
// backup of one, references any more. See attachmentSweeper for how, and why it is safe.
type FileSweeper = attachmentSweeper[models.WikiFile]

func NewFileSweeper(
	docRepo repository.IWikiDocumentRepository,
	backupRepo repository.IWikiDocumentBackupRepository,
	fileRepo repository.IWikiFileRepository,
	store blob.ObjectStore,
	logger *zap.Logger,
	interval time.Duration,
	grace time.Duration,
	dryRun bool,
) *FileSweeper {
	return newAttachmentSweeper(attachmentKind[models.WikiFile]{
		repo:       fileRepo,
		referenced: liveInDocumentsOrBackups(docRepo.FilterReferencedFileIDs, backupRepo.FilterReferencedFileIDs),
		held:       backupRepo.HasUnindexed,
		id:         func(f models.WikiFile) uuid.UUID { return f.FileID },
		operation:  func(f models.WikiFile) uuid.UUID { return f.OperationID },
		objectKey:  func(f models.WikiFile) string { return f.ObjectKey },
		name:       "file",
		title:      "File",
		idKey:      "file_id",
	}, store, logger, interval, grace, dryRun)
}
