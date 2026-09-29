package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/wiki"
	"go.uber.org/zap"
)

type setChecklistAnswerArgs struct {
	IdempotencyKey
	DocumentID string                   `json:"document_id"            jsonschema:"Page id."`
	Key        string                   `json:"key,omitempty"          jsonschema:"Checklist item key (UUID). Use for a single answer."`
	Answer     string                   `json:"answer,omitempty"       jsonschema:"Markdown answer for the single key. Use fenced code blocks for command output."`
	Answers    []checklistAnswerEntry   `json:"answers,omitempty"      jsonschema:"Batch: several answers at once. Each has a key and answer."`
}

type checklistAnswerEntry struct {
	Key    string `json:"key"    jsonschema:"Checklist item key (UUID)."`
	Answer string `json:"answer" jsonschema:"Markdown answer content."`
}

// mergedAnswers resolves the one-or-many input into a single list, same
// pattern as sectionWriteArgs.targets().
func (a setChecklistAnswerArgs) mergedAnswers() ([]wiki.ChecklistAnswerInput, error) {
	seen := make(map[string]bool)
	var out []wiki.ChecklistAnswerInput

	if a.Key != "" {
		seen[a.Key] = true
		out = append(out, wiki.ChecklistAnswerInput{
			Key:      a.Key,
			Markdown: a.Answer,
		})
	}
	for _, entry := range a.Answers {
		key := strings.TrimSpace(entry.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, wiki.ChecklistAnswerInput{
			Key:      key,
			Markdown: entry.Answer,
		})
	}

	if len(out) == 0 {
		return nil, refuse("give key + answer for one item, or answers[] for several.")
	}
	if len(out) > 50 {
		return nil, refuse("that is %d answers in one call; the limit is 50.", len(out))
	}
	return out, nil
}

// checklistWriteResultView is what set_checklist_answer returns.
type checklistWriteResultView struct {
	wikiDocView
	Filled   int                    `json:"filled"`
	NotFound int                    `json:"notFound,omitempty"`
	Errors   int                    `json:"errors,omitempty"`
	Results  []checklistAnswerResultView `json:"results,omitempty"`
	Checklist *checklistStatusView  `json:"checklist,omitempty"`
	Watchers int                    `json:"watchers"`
	Notes    []string               `json:"notes,omitempty"`
}

type checklistAnswerResultView struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

type checklistStatusView struct {
	Total    int `json:"total"`
	Required int `json:"required"`
	Answered int `json:"answered"`
}

func handleSetChecklistAnswer(ctx context.Context, s *Server, args setChecklistAnswerArgs) (toolResult, error) {
	doc, err := s.loadWikiDocument(ctx, args.DocumentID, models.OperationRoleOperator)
	if err != nil {
		return toolResult{}, err
	}
	if err := requireProse(doc, "fill checklist on"); err != nil {
		return toolResult{}, err
	}

	answers, err := args.mergedAnswers()
	if err != nil {
		return toolResult{}, err
	}

	// Validate answer content the same way writeBody does.
	for _, a := range answers {
		if err := checkCredentialFences(a.Markdown); err != nil {
			return toolResult{}, err
		}
		if err := checkReferenceLinks(a.Markdown); err != nil {
			return toolResult{}, err
		}
	}

	if s.deps.Hocuspocus == nil {
		return toolResult{}, fmt.Errorf("wiki writing is unavailable: the collaboration service is not configured")
	}

	result, err := s.deps.Hocuspocus.SetChecklistAnswers(
		ctx, doc.DocumentID.String(), answers, viewerID(ctx))
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to set checklist answers",
			zap.String("document_id", doc.DocumentID.String()), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to set checklist answers: %w", err)
	}

	s.forgetMarkdown(ctx, doc.DocumentID.String())

	var filled, notFound, errors int
	var resultViews []checklistAnswerResultView
	for _, r := range result.Results {
		resultViews = append(resultViews, checklistAnswerResultView{
			Key:    r.Key,
			Status: r.Status,
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

	// Only include per-item results when there are failures to diagnose.
	if notFound == 0 && errors == 0 {
		resultViews = nil
	}

	payload := checklistWriteResultView{
		wikiDocView: toWikiDocView(doc),
		Filled:      filled,
		NotFound:    notFound,
		Errors:      errors,
		Results:     resultViews,
		Watchers:    result.Watchers,
	}
	if result.Checklist.Total > 0 {
		payload.Checklist = &checklistStatusView{
			Total:    result.Checklist.Total,
			Required: result.Checklist.Required,
			Answered: result.Checklist.Answered,
		}
	}

	var notes []string
	if result.Watchers > 0 {
		notes = append(notes, "The operator has this page open and saw the answers appear.")
	}
	if notFound > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d key(s) were not found on the page. Use get_checklist_status to see the valid keys.", notFound))
	}
	payload.Notes = notes

	summary := fmt.Sprintf("filled %d checklist answer(s) on %s", filled, doc.Title)
	if notFound > 0 {
		summary += fmt.Sprintf(" (%d not found)", notFound)
	}

	return toolResult{
		Payload:     payload,
		OperationID: &doc.OperationID,
		Summary:     summary,
	}, nil
}

// getChecklistStatusArgs is the input for get_checklist_status.
type getChecklistStatusArgs struct {
	DocumentID string `json:"document_id" jsonschema:"Page id."`
}

// checklistStatusResultView is what get_checklist_status returns.
type checklistStatusResultView struct {
	wikiDocView
	Items    []checklistItemView  `json:"items"`
	Coverage checklistStatusView  `json:"coverage"`
	Notes    []string             `json:"notes,omitempty"`
}

type checklistItemView struct {
	Key         string `json:"key"`
	Prompt      string `json:"prompt"`
	Required    bool   `json:"required"`
	Answered    bool   `json:"answered"`
	State       string `json:"state,omitempty"`
	AnswerBytes int    `json:"answerBytes"`
}

func handleGetChecklistStatus(ctx context.Context, s *Server, args getChecklistStatusArgs) (toolResult, error) {
	doc, err := s.loadWikiDocument(ctx, args.DocumentID, models.OperationRoleViewer)
	if err != nil {
		return toolResult{}, err
	}
	if err := requireProse(doc, "read checklist status of"); err != nil {
		return toolResult{}, err
	}

	if s.deps.Hocuspocus == nil {
		return toolResult{}, fmt.Errorf("wiki writing is unavailable: the collaboration service is not configured")
	}

	result, err := s.deps.Hocuspocus.GetChecklistStatus(ctx, doc.DocumentID.String())
	if err != nil {
		s.deps.Logger.Warn("mcp: failed to get checklist status",
			zap.String("document_id", doc.DocumentID.String()), zap.Error(err))
		return toolResult{}, fmt.Errorf("failed to read checklist status: %w", err)
	}

	if result.Coverage.Total == 0 {
		return toolResult{}, refuse("%q has no checklist items.", doc.Title)
	}

	items := make([]checklistItemView, 0, len(result.Items))
	for _, item := range result.Items {
		view := checklistItemView{
			Key:         item.Key,
			Prompt:      item.Prompt,
			Required:    item.Required,
			Answered:    item.Answered,
			AnswerBytes: item.AnswerBytes,
		}
		// Only include state when it differs from the derived answered/unanswered.
		if item.State == "not_applicable" || item.State == "flagged" {
			view.State = item.State
		}
		items = append(items, view)
	}

	payload := checklistStatusResultView{
		wikiDocView: toWikiDocView(doc),
		Items:       items,
		Coverage: checklistStatusView{
			Total:    result.Coverage.Total,
			Required: result.Coverage.Required,
			Answered: result.Coverage.Answered,
		},
	}

	return toolResult{
		Payload:     payload,
		OperationID: &doc.OperationID,
		Summary:     fmt.Sprintf("checklist status for %s (%d/%d answered)", doc.Title, result.Coverage.Answered, result.Coverage.Total),
	}, nil
}
