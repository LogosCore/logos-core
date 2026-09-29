package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Filter helpers ---

// matchesWikiFilter returns true when a document passes all active metadata
// filters. A zero-value filter (empty tags, status, pageType) matches everything.
func matchesWikiFilter(doc *models.WikiDocument, tags []string, status, pageType string) bool {
	if len(tags) > 0 {
		docTags := make(map[string]bool, len(doc.Tags))
		for _, t := range doc.Tags {
			docTags[t] = true
		}
		for _, t := range tags {
			if !docTags[t] {
				return false
			}
		}
	}
	if status != "" {
		docStatus := string(doc.Status.Or())
		if !strings.EqualFold(docStatus, status) {
			return false
		}
	}
	if pageType != "" {
		if !strings.EqualFold(doc.PageType, pageType) {
			return false
		}
	}
	return true
}

// filterWikiDocs returns only the documents that match all active filters.
func filterWikiDocs(docs []models.WikiDocument, tags []string, status, pageType string) []models.WikiDocument {
	if len(tags) == 0 && status == "" && pageType == "" {
		return docs
	}
	out := make([]models.WikiDocument, 0, len(docs))
	for _, d := range docs {
		if matchesWikiFilter(&d, tags, status, pageType) {
			out = append(out, d)
		}
	}
	return out
}

// --- Args ---

type setWikiTagsArgs struct {
	IdempotencyKey
	DocumentID  string   `json:"document_id,omitempty"  jsonschema:"Page id."`
	DocumentIDs []string `json:"document_ids,omitempty" jsonschema:"Several page ids."`
	Tags        []string `json:"tags"                   jsonschema:"Replace the page's tags with this list. Empty list clears."`
}

type listWikiTagsArgs struct {
	OperationID string `json:"operation_id,omitempty" jsonschema:"Operation id; omit for the operator's current one."`
}

type listWikiPageTypesArgs struct {
	OperationID string `json:"operation_id,omitempty" jsonschema:"Operation id; omit for the operator's current one."`
}

// --- Registration ---

func registerWikiMetadataTools(s *Server) {
	register(s, &mcp.Tool{
		Name:        "set_wiki_tags",
		Description: "Replace a page's tags. Empty list clears. Applies to one or many pages.",
	}, writeTool, handleSetWikiTags)

	register(s, &mcp.Tool{
		Name:        "list_wiki_tags",
		Description: "Tags in use across the operation with page counts. Check before inventing new tags.",
	}, readTool, handleListWikiTags)

	register(s, &mcp.Tool{
		Name:        "list_wiki_page_types",
		Description: "Page types in use across the operation with page counts. Check before inventing new types.",
	}, readTool, handleListWikiPageTypes)
}

// --- Handlers ---

func handleSetWikiTags(ctx context.Context, s *Server, args setWikiTagsArgs) (toolResult, error) {
	ids := mergeDocumentIDs(args.DocumentID, args.DocumentIDs)
	if len(ids) == 0 {
		return toolResult{}, refuse("give document_id for one page, or document_ids for several.")
	}
	if len(ids) > 25 {
		return toolResult{}, refuse("that is %d pages; the limit is 25.", len(ids))
	}

	type tagResult struct {
		ID    string `json:"id"`
		Title string `json:"title,omitempty"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}

	var results []tagResult
	var applied int
	var opID *uuid.UUID

	for _, id := range ids {
		doc, err := s.loadWikiDocument(ctx, id, models.OperationRoleOperator)
		if err != nil {
			results = append(results, tagResult{ID: id, Error: err.Error()})
			continue
		}

		input := model.UpdateWikiDocumentInput{Tags: args.Tags}
		if _, err := s.deps.WikiDocs.UpdateWikiDocument(ctx, id, input); err != nil {
			results = append(results, tagResult{ID: id, Title: doc.Title, Error: err.Error()})
			continue
		}

		results = append(results, tagResult{ID: id, Title: doc.Title, OK: true})
		applied++
		if opID == nil {
			opID = &doc.OperationID
		}
	}

	failed := len(ids) - applied
	payload := struct {
		Applied int         `json:"applied"`
		Failed  int         `json:"failed,omitempty"`
		Results []tagResult `json:"results,omitempty"`
	}{Applied: applied, Failed: failed}
	if failed > 0 {
		payload.Results = results
	}

	return toolResult{
		Payload:     payload,
		OperationID: opID,
		Summary:     fmt.Sprintf("set tags on %d page(s)", applied),
	}, nil
}

func handleListWikiTags(ctx context.Context, s *Server, args listWikiTagsArgs) (toolResult, error) {
	opID, err := s.scopedOperation(ctx, args.OperationID, models.OperationRoleViewer)
	if err != nil {
		return toolResult{}, err
	}

	if s.deps.WikiDocRepo == nil {
		return toolResult{}, fmt.Errorf("wiki repository not available")
	}

	rows, err := s.deps.WikiDocRepo.AggregateTagCounts(ctx, opID)
	if err != nil {
		return toolResult{}, fmt.Errorf("failed to list tags: %w", err)
	}

	type tagRow struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	views := make([]tagRow, len(rows))
	for i, r := range rows {
		views[i] = tagRow{Tag: r.Tag, Count: r.Count}
	}

	return toolResult{
		Payload:     views,
		OperationID: &opID,
		Summary:     fmt.Sprintf("listed %d tags", len(views)),
	}, nil
}

func handleListWikiPageTypes(ctx context.Context, s *Server, args listWikiPageTypesArgs) (toolResult, error) {
	opID, err := s.scopedOperation(ctx, args.OperationID, models.OperationRoleViewer)
	if err != nil {
		return toolResult{}, err
	}

	if s.deps.WikiDocRepo == nil {
		return toolResult{}, fmt.Errorf("wiki repository not available")
	}

	rows, err := s.deps.WikiDocRepo.AggregatePageTypeCounts(ctx, opID)
	if err != nil {
		return toolResult{}, fmt.Errorf("failed to list page types: %w", err)
	}

	type typeRow struct {
		PageType string `json:"pageType"`
		Count    int    `json:"count"`
	}
	views := make([]typeRow, len(rows))
	for i, r := range rows {
		views[i] = typeRow{PageType: r.PageType, Count: r.Count}
	}

	return toolResult{
		Payload:     views,
		OperationID: &opID,
		Summary:     fmt.Sprintf("listed %d page types", len(views)),
	}, nil
}

// mergeDocumentIDs deduplicates a single id and a list of ids.
func mergeDocumentIDs(single string, list []string) []string {
	seen := make(map[string]bool, len(list)+1)
	var out []string
	for _, id := range append([]string{single}, list...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
