package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
)

const wikiImageCollection = "wiki_images"

// IWikiImageRepository defines data access for wiki image metadata.
type IWikiImageRepository interface {
	Create(ctx context.Context, img *models.WikiImage) error
	FindByID(ctx context.Context, id uuid.UUID) (models.WikiImage, error)
	FindByDocumentID(ctx context.Context, docID uuid.UUID) ([]models.WikiImage, error)
	// FindCandidatesOlderThan returns active (non-deleted) images whose creation
	// time is before the given cutoff. The sweeper cross-references these
	// against document content to decide which are orphaned.
	FindCandidatesOlderThan(ctx context.Context, cutoff time.Time, limit int64) ([]models.WikiImage, error)
	HardDelete(ctx context.Context, id uuid.UUID) error
}

func NewWikiImageRepository(db database.Database) IWikiImageRepository {
	return newAttachmentRepository[models.WikiImage](db, wikiImageCollection, "image_id")
}
