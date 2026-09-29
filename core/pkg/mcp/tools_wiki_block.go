package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wiki"
	"go.uber.org/zap"
)

type setBlockContentArgs struct {
	IdempotencyKey
	DocumentID string             `json:"document_id"       jsonschema:"Page id."`
	Key        string             `json:"key,omitempty"      jsonschema:"Block key (UUID). Use for a single block."`
	Markdown   string             `json:"markdown,omitempty" jsonschema:"Markdown content for the single key."`
	Blocks     []blockContentEntry `json:"blocks,omitempty"  jsonschema:"Batch: several blocks at once. Each has a key and markdown."`
}

type blockContentEntry struct {
	Key      string `json:"key"      jsonschema:"Block key (UUID)."`
	Markdown string `json:"markdown" jsonschema:"Markdown content."`
}

func (a setBlockContentArgs) mergedBlocks() ([]wiki.BlockContentInput, error) {
	seen := make(map[string]bool)
	var out []wiki.BlockContentInput

	if a.Key != "" {
		seen[a.Key] = true
		out = append(out, wiki.BlockContentInput{
			Key:      a.Key,
			Markdown: a.Markdown,
		})
	}
	for _, entry := range a.Blocks {
		key := strings.TrimSpace(entry.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, wiki.BlockContentInput{
			Key:      key,
			Markdown: entry.Markdown,
		})
	}

	if len(out) == 0 {
		return nil, refuse("give key + markdown for one block, or blocks[] for several.")
	}
	if len(out) > 50 {
		return nil, refuse("that is %d blocks in one call; the limit is 50.", len(out))
	}
	return out, nil
}

type blockWriteResultView struct {
	wikiDocView
	Filled   int                    `json:"filled"`
	NotFound int                    `json:"notFound,omitempty"`
	Errors   int                    `json:"errors,omitempty"`
	Results  []blockResultView      `json:"results,omitempty"`
	Watchers int                    `json:"watchers"`
	Notes    []string               `json:"notes,omitempty"`
}

type blockResultView struct {
	Key      string `json:"key"`
	NodeType string `json:"nodeType,omitempty"`
	Status   string `json:"status"`
}

func handleSetBlockContent(ctx context.Context, s *Server, args setBlockContentArgs) (toolResult, error) {
	doc, err := s.loadWikiDocument(ctx, args.DocumentID, models.OperationRoleOperator)
	if err != nil {
		return toolResult{}, err
	}
	if err := requireProse(doc, "set block content on"); err != nil {
		return toolResult{}, err
	}

	blocks, err := args.mergedBlocks()
	if err != nil {
		return toolResult{}, err
	}

	for _, b := range blocks {
		if err := checkCredentialFences(b.Markdown); err != nil {
			return toolResult{}, err
		}
		if err := checkReferenceLinks(b.Markdown); err != nil {
			return toolResult{}, err
		}
	}

	if s.deps.Hocuspocus == nil {
		return toolResult{}, fmt.Errorf("wiki writing is unavailable: the collaboration service is not configured")
	}

	result, err := s.deps.Hocuspocus.SetBlockContent(
		ctx, doc.DocumentID.String(), blocks, viewerID(ctx))
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to set block content",
			zap.String("document_id", doc.DocumentID.String()), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to set block content: %w", err)
	}

	s.forgetMarkdown(ctx, doc.DocumentID.String())

	var filled, notFound, errors int
	var resultViews []blockResultView
	for _, r := range result.Results {
		resultViews = append(resultViews, blockResultView{
			Key:      r.Key,
			NodeType: r.NodeType,
			Status:   r.Status,
		})
		switch r.Status {
		case "ok":
			filled++
		case "not_found":
			notFound++
		default:
			errors++
		}
	}

	if notFound == 0 && errors == 0 {
		resultViews = nil
	}

	payload := blockWriteResultView{
		wikiDocView: toWikiDocView(doc),
		Filled:      filled,
		NotFound:    notFound,
		Errors:      errors,
		Results:     resultViews,
		Watchers:    result.Watchers,
	}

	var notes []string
	if result.Watchers > 0 {
		notes = append(notes, "The operator saw this edit.")
	}
	if notFound > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d key(s) were not found on the page. Read the page to find block keys.", notFound))
	}
	payload.Notes = notes

	summary := fmt.Sprintf("set content of %d block(s) on %s", filled, doc.Title)
	if notFound > 0 {
		summary += fmt.Sprintf(" (%d not found)", notFound)
	}

	return toolResult{
		Payload:     payload,
		OperationID: &doc.OperationID,
		Summary:     summary,
	}, nil
}
