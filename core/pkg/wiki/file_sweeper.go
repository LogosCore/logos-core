package wiki

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// FileSweeper garbage-collects wiki_files rows whose bytes no document
// references any more. See attachmentSweeper for how, and why it is safe.
type FileSweeper = attachmentSweeper[models.WikiFile]

func NewFileSweeper(
	docRepo repository.IWikiDocumentRepository,
	fileRepo repository.IWikiFileRepository,
	store blob.ObjectStore,
	logger *zap.Logger,
	interval time.Duration,
	grace time.Duration,
	dryRun bool,
) *FileSweeper {
	return newAttachmentSweeper(attachmentKind[models.WikiFile]{
		repo:       fileRepo,
		referenced: docRepo.FilterReferencedFileIDs,
		id:         func(f models.WikiFile) uuid.UUID { return f.FileID },
		operation:  func(f models.WikiFile) uuid.UUID { return f.OperationID },
		objectKey:  func(f models.WikiFile) string { return f.ObjectKey },
		name:       "file",
		title:      "File",
		idKey:      "file_id",
	}, store, logger, interval, grace, dryRun)
}
