package resolver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// Minion feedback over GraphQL (minion_sessions.graphql): a session's
// warnings and errors from its minion, a page at a time; marked read for
// everyone of the operation at once; a task's own on the task.

func (r *minionSessionResolver) MinionFeedback(ctx context.Context, sessionID string, level, search, after *string, first *int) (*model.MinionFeedbackPage, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid session ID")
	}
	s, err := r.sessions.FindBySessionID(ctx, sid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "session not found")
		}
		return nil, err
	}
	if _, err := r.viewer(ctx, s.OperationID.String()); err != nil {
		return nil, err
	}
	q := repository.FeedbackQuery{Level: deref(level), Search: deref(search), After: deref(after)}
	if q.Level != "" && q.Level != models.FeedbackWarning && q.Level != models.FeedbackError {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "level is warning or error")
	}
	if first != nil {
		q.First = int64(*first)
	}
	out := &model.MinionFeedbackPage{Entries: []*model.MinionFeedback{}}
	if r.feedback == nil {
		return out, nil
	}
	entries, next, err := r.feedback.List(ctx, s.SessionID, q)
	if err != nil {
		if strings.Contains(err.Error(), "bad cursor") {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		}
		return nil, err
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, toFeedbackModel(e, s.FeedbackReadAt))
	}
	out.Next = strPtr(next)
	return out, nil
}

func (r *minionSessionResolver) MarkMinionFeedbackRead(ctx context.Context, id string) (*model.MinionSession, error) {
	s, err := r.operatorSession(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := r.sessions.MarkFeedbackRead(ctx, s.SessionID, now); err != nil {
		return nil, err
	}
	s.FeedbackErrors, s.FeedbackWarnings, s.FeedbackUnread, s.FeedbackReadAt = 0, 0, 0, &now
	r.bus.Publish(eventbus.NewMinionSessionUpdatedEvent(eventbus.MinionSessionEventPayload{
		SessionID: s.SessionID.String(), OperationID: s.OperationID.String(),
	}))
	return r.withMark(ctx, s), nil
}

// Feedback is MinionTask.feedback: what its minion said of it, newest first.
func (r *minionTaskResolver) Feedback(ctx context.Context, t *model.MinionTask) ([]*model.MinionFeedback, error) {
	out := []*model.MinionFeedback{}
	if r.feedback == nil {
		return out, nil
	}
	id, err := uuid.Parse(t.ID)
	if err != nil {
		return nil, fmt.Errorf("task id %q: %w", t.ID, err)
	}
	entries, err := r.feedback.ForTask(ctx, id, 20)
	if err != nil || len(entries) == 0 {
		return out, err
	}
	var readAt *time.Time
	if sid, err := uuid.Parse(t.SessionID); err == nil {
		if s, err := r.sessions.FindBySessionID(ctx, sid); err == nil {
			readAt = s.FeedbackReadAt
		}
	}
	for _, e := range entries {
		out = append(out, toFeedbackModel(e, readAt))
	}
	return out, nil
}

// toFeedbackModel is an entry as GraphQL has it: unread when it came after
// readAt (its session's feedback last marked read).
func toFeedbackModel(e models.MinionFeedback, readAt *time.Time) *model.MinionFeedback {
	m := &model.MinionFeedback{ID: e.FeedbackID.String(), Level: e.Level, Msg: e.Msg, Code: strPtr(e.Code), Count: max(e.Count, 1),
		ReceivedAt: e.ReceivedAt.UTC().Format(time.RFC3339Nano), Unread: readAt == nil || e.ReceivedAt.After(*readAt)}
	if e.Task != nil {
		id := e.Task.String()
		m.TaskID = &id
	}
	if e.At != nil {
		at := e.At.UTC().Format(time.RFC3339)
		m.At = &at
	}
	return m
}
