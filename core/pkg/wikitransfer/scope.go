package wikitransfer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// CredentialLookup is the read-side dependency exporters use to embed
// credential payloads. Satisfied by repository.ICredentialRepository; tests
// substitute a map-backed fake. FindByID must return the credential with
// its OperationID so the exporter can refuse cross-operation embedding.
type CredentialLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (models.Credential, error)
}

// StateLoader reads one document's Y.js body. Satisfied by
// repository.IWikiDocumentRepository; tests substitute a map-backed fake.
type StateLoader interface {
	FindContentState(ctx context.Context, id uuid.UUID) ([]byte, error)
}

// Scope is the set of documents one export covers, indexed for a
// parent-first walk. Shared by every export format.
//
// The documents here are skeletons: title, placement, icon and the stored
// reference indexes, with `content` and `content_state` projected away. A
// format reads a page's body through ContentState when it reaches it, one
// page at a time, so what an export holds in memory is the shape of the
// tree plus a single body — not every CRDT blob in the wiki at once. That
// is what lets an export of any size run in a fixed amount of memory, and
// it is why nothing here may read doc.ContentState directly.
type Scope struct {
	OperationID   uuid.UUID
	OperationName string
	// Root is the subtree root, or nil for a tree-wide export.
	Root *models.WikiDocument
	// Docs is every live document in scope, root included, without bodies.
	Docs []models.WikiDocument
	// TopLevel are the documents directly under the export root: the
	// subtree root itself, or the operation's root-level documents.
	TopLevel []models.WikiDocument
	// ChildrenByParent lists each document's live children in sort order.
	ChildrenByParent map[uuid.UUID][]models.WikiDocument
	// States is where page bodies are read from. CollectScope sets it to
	// the document repository.
	States StateLoader
}

// ContentState reads one document's Y.js body. A document that has never
// been opened in the editor has none, which is an empty body rather than an
// error.
func (s *Scope) ContentState(ctx context.Context, docID uuid.UUID) ([]byte, error) {
	if s.States == nil {
		return nil, errors.New("scope has no state loader")
	}
	return s.States.FindContentState(ctx, docID)
}

// Label is "tree" or "subtree".
func (s *Scope) Label() string {
	if s.Root != nil {
		return "subtree"
	}
	return "tree"
}

// Title is the export's display title: the root document's title for a
// subtree, the operation name for a tree, "wiki" when both are blank.
func (s *Scope) Title() string {
	if s.Root != nil {
		if t := strings.TrimSpace(s.Root.Title); t != "" {
			return t
		}
	}
	if t := strings.TrimSpace(s.OperationName); t != "" {
		return t
	}
	return "wiki"
}

// CollectScope loads the shape of the tree an export covers, without any
// page bodies — those are read one at a time through Scope.ContentState.
// rootID nil means the whole operation. Trashed documents are excluded; a
// trashed root is an error.
func CollectScope(ctx context.Context, docRepo repository.IWikiDocumentRepository, operationID uuid.UUID, operationName string, rootID *uuid.UUID) (*Scope, error) {
	s := &Scope{
		OperationID:      operationID,
		OperationName:    operationName,
		ChildrenByParent: map[uuid.UUID][]models.WikiDocument{},
		States:           docRepo,
	}

	if rootID != nil {
		root, err := docRepo.FindByID(ctx, *rootID)
		if err != nil {
			return nil, fmt.Errorf("find subtree root: %w", err)
		}
		if root.OperationID != operationID {
			return nil, errors.New("subtree root does not belong to operation")
		}
		if root.DeletedAt != nil {
			return nil, errors.New("subtree root is in trash")
		}
		descendants, err := docRepo.FindSummaryDescendants(ctx, root.OperationID, root.DocumentID)
		if err != nil {
			return nil, fmt.Errorf("find descendants: %w", err)
		}
		// The root arrives whole from FindByID, which is the one lookup that
		// has to read through soft-deletes to tell "trashed" from "absent".
		// Drop its body so no format can read a state off a Scope document
		// for one page out of thousands and appear to work.
		root.ContentState, root.Content = nil, ""
		s.Root = &root
		s.Docs = append(s.Docs, root)
		for _, d := range descendants {
			if d.DeletedAt == nil {
				s.Docs = append(s.Docs, d)
			}
		}
	} else {
		all, err := docRepo.FindSummariesByOperationID(ctx, operationID, false)
		if err != nil {
			return nil, fmt.Errorf("find by operation: %w", err)
		}
		for _, d := range all {
			if d.DeletedAt == nil {
				s.Docs = append(s.Docs, d)
			}
		}
	}

	for _, d := range s.Docs {
		var key uuid.UUID
		if d.ParentDocumentID != nil {
			key = *d.ParentDocumentID
		}
		s.ChildrenByParent[key] = append(s.ChildrenByParent[key], d)
	}
	for k := range s.ChildrenByParent {
		group := s.ChildrenByParent[k]
		sort.SliceStable(group, func(i, j int) bool { return group[i].SortOrder < group[j].SortOrder })
		s.ChildrenByParent[k] = group
	}

	if s.Root != nil {
		s.TopLevel = []models.WikiDocument{*s.Root}
	} else {
		s.TopLevel = s.ChildrenByParent[uuid.UUID{}]
	}
	return s, nil
}

// Walk visits the scope parent-first in sibling order, passing each
// document's depth below the export root and its index among siblings.
func (s *Scope) Walk(fn func(doc models.WikiDocument, depth, siblingIndex int) bool) {
	var visit func(docs []models.WikiDocument, depth int)
	visit = func(docs []models.WikiDocument, depth int) {
		for i, d := range docs {
			if !fn(d, depth, i) {
				continue
			}
			visit(s.ChildrenByParent[d.DocumentID], depth+1)
		}
	}
	visit(s.TopLevel, 0)
}
