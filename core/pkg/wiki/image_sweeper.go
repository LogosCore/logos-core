package wiki

import (
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// ImageSweeper garbage-collects wiki_images rows whose bytes no document
// references any more. See attachmentSweeper for how, and why it is safe.
type ImageSweeper = attachmentSweeper[models.WikiImage]

func NewImageSweeper(
	docRepo repository.IWikiDocumentRepository,
	imageRepo repository.IWikiImageRepository,
	store blob.ObjectStore,
	logger *zap.Logger,
	interval time.Duration,
	grace time.Duration,
	dryRun bool,
) *ImageSweeper {
	return newAttachmentSweeper(attachmentKind[models.WikiImage]{
		repo:       imageRepo,
		referenced: docRepo.FilterReferencedImageIDs,
		id:         func(img models.WikiImage) uuid.UUID { return img.ImageID },
		operation:  func(img models.WikiImage) uuid.UUID { return img.OperationID },
		objectKey:  func(img models.WikiImage) string { return img.ObjectKey },
		name:       "image",
		title:      "Image",
		idKey:      "image_id",
	}, store, logger, interval, grace, dryRun)
}
