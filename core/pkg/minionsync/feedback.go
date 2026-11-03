package minionsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// Feedback (docs: contracts/minion-messages/sessions, Feedback): a minion's
// warnings and errors, kept with its session (models.MinionFeedback), its
// unread counts on the session. Nothing answers it: the message that
// carried it went, and one sent again adds nothing (one entry per the
// minion's id).

// WithFeedback keeps minions' feedback; without it a feedback item is
// taken and dropped.
func (s *Service) WithFeedback(feedback repository.IMinionFeedbackRepository) *Service {
	s.feedback = feedback
	return s
}

// readFeedback reads and checks a feedback item.
func readFeedback(raw cbor.RawMessage) (protocol.FeedbackItem, error) {
	var f protocol.FeedbackItem
	if err := protocol.UnmarshalCBOR(raw, &f); err != nil {
		return f, errors.New("the feedback does not read")
	}
	return f, f.Validate()
}

// takeFeedback keeps a message's feedback under its session: the entries
// new to it counted unread, the session's oldest beyond MaxSessionFeedback
// let go. It reports whether any was new.
func (s *Service) takeFeedback(ctx context.Context, session *models.MinionSession, items []protocol.FeedbackItem) (bool, error) {
	if s.feedback == nil || len(items) == 0 {
		return false, nil
	}
	now := s.now()
	errs, warns := 0, 0
	// The new ones about each task: counted on it too.
	type tally struct{ errs, warns int }
	byTask := map[uuid.UUID]tally{}
	for _, f := range items {
		e := &models.MinionFeedback{FeedbackID: uuid.New(), OperationID: session.OperationID, SessionID: session.SessionID,
			MinionID: f.ID, Level: protocol.FeedbackLevelName(f.Level), Msg: f.Msg, Code: f.Code, Count: max(int(min(f.Count, 1<<31)), 1),
			ReceivedAt: now}
		if len(f.Task) == 16 {
			id, _ := uuid.FromBytes(f.Task)
			e.Task = &id
		}
		if f.At > 0 {
			at := time.Unix(int64(min(f.At, 1<<62)), 0).UTC()
			e.At = &at
		}
		added, err := s.feedback.Add(ctx, e)
		if err != nil {
			return false, fmt.Errorf("keep feedback: %w", err)
		}
		if !added {
			continue
		}
		var about tally
		if e.Level == models.FeedbackError {
			errs++
			about.errs++
		} else {
			warns++
			about.warns++
		}
		if e.Task != nil {
			n := byTask[*e.Task]
			byTask[*e.Task] = tally{n.errs + about.errs, n.warns + about.warns}
		}
		s.logger.Info("minion feedback", zap.String("session_id", session.SessionID.String()), zap.String("level", e.Level),
			zap.String("code", e.Code), zap.String("msg", e.Msg))
	}
	if errs+warns == 0 {
		return false, nil
	}
	if err := s.sessions.AddFeedback(ctx, session.SessionID, errs, warns, now); err != nil {
		return false, fmt.Errorf("count feedback: %w", err)
	}
	session.FeedbackErrors += errs
	session.FeedbackWarnings += warns
	session.FeedbackUnread += errs + warns
	session.FeedbackAt = &now
	if err := s.feedback.Trim(ctx, session.SessionID, models.MaxSessionFeedback); err != nil {
		s.logger.Warn("trim feedback", zap.String("session_id", session.SessionID.String()), zap.Error(err))
	}
	// Its tasks' counts, which their lists show: a task that is not the
	// session's has none, and one lost leaves the task unmarked, its
	// feedback kept (the minion's id makes a resend add nothing).
	for id, n := range byTask {
		if s.tasks == nil {
			break
		}
		task, err := s.tasks.AddFeedback(ctx, session.SessionID, id, n.errs, n.warns)
		if repository.IsNotFound(err) {
			continue
		}
		if err != nil {
			s.logger.Warn("count a task's feedback", zap.String("task_id", id.String()), zap.Error(err))
			continue
		}
		s.publishTask(task)
	}
	return true, nil
}
