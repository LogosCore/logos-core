package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/models"
)

const wikiFileCollection = "wiki_files"

// IWikiFileRepository defines data access for wiki file attachment metadata.
type IWikiFileRepository interface {
	Create(ctx context.Context, file *models.WikiFile) error
	FindByID(ctx context.Context, id uuid.UUID) (models.WikiFile, error)
	FindByDocumentID(ctx context.Context, docID uuid.UUID) ([]models.WikiFile, error)
	// FindCandidatesOlderThan returns active (non-deleted) files whose creation
	// time is before the given cutoff. The sweeper cross-references these
	// against document content to decide which are orphaned.
	FindCandidatesOlderThan(ctx context.Context, cutoff time.Time, limit int64) ([]models.WikiFile, error)
	HardDelete(ctx context.Context, id uuid.UUID) error
}

func NewWikiFileRepository(db database.Database) IWikiFileRepository {
	return newAttachmentRepository[models.WikiFile](db, wikiFileCollection, "file_id")
}
