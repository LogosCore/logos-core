package resolver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"github.com/logoscore/logos-core/core/pkg/repository"
)

// The desktops' recordings as the web app lists them (docs:
// contracts/minion-messages/tunnels.md, Recordings): a desktop task's, a
// page at a time, the latest first; one deleted by an operator. Their
// files: GET /api/v1/desktop-recordings/{id} (controller).

const (
	defaultRecordingPage = 50
	maxRecordingPage     = 200
)

// TaskRecordings reads and deletes the desktops' recordings
// (recordings.Service).
type TaskRecordings interface {
	List(ctx context.Context, taskID uuid.UUID, before, limit int64) ([]models.DesktopRecording, error)
	Recording(ctx context.Context, id uuid.UUID) (*models.DesktopRecording, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.DesktopRecording, error)
}

// DesktopRecordings is a page of a desktop task's recordings, the latest
// first, for a viewer of its session's operation.
func (r *minionTaskResolver) DesktopRecordings(ctx context.Context, taskID string, last *int, before *string) (*model.DesktopRecordingPage, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid task ID")
	}
	t, err := r.tasks.FindByTaskID(ctx, tid)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, moduleGQLError(ctx, errCodeNotFound, "task not found")
		}
		return nil, err
	}
	if _, err := r.session(ctx, t.SessionID.String(), models.OperationRoleViewer); err != nil {
		return nil, err
	}
	page := &model.DesktopRecordingPage{Recordings: []*model.DesktopRecording{}}
	if r.recordings == nil {
		return page, nil
	}
	limit := defaultRecordingPage
	if last != nil {
		limit = min(max(*last, 1), maxRecordingPage)
	}
	var from int64
	if before != nil && *before != "" {
		if from, err = strconv.ParseInt(*before, 10, 64); err != nil || from <= 0 {
			return nil, moduleGQLError(ctx, errCodeBadUserInput, "invalid cursor")
		}
	}
	recs, err := r.recordings.List(ctx, tid, from, int64(limit)+1)
	if err != nil {
		return nil, fmt.Errorf("the task's recordings: %w", err)
	}
	if len(recs) > limit {
		recs = recs[:limit]
		next := strconv.FormatInt(recs[len(recs)-1].Number, 10)
		page.Before = &next
	}
	for i := range recs {
		page.Recordings = append(page.Recordings, recordingModel(&recs[i]))
	}
	return page, nil
}

// DeleteDesktopRecording deletes a recording that ended, its file with it,
// for an operator of its session's operation.
func (r *minionTaskResolver) DeleteDesktopRecording(ctx context.Context, id string) (bool, error) {
	rid, err := uuid.Parse(id)
	if err != nil {
		return false, moduleGQLError(ctx, errCodeBadUserInput, "invalid recording ID")
	}
	if r.recordings == nil {
		return false, moduleGQLError(ctx, errCodeNotFound, "recording not found")
	}
	rec, err := r.recordings.Recording(ctx, rid)
	if err != nil {
		if repository.IsNotFound(err) {
			return false, moduleGQLError(ctx, errCodeNotFound, "recording not found")
		}
		return false, err
	}
	if _, err := r.session(ctx, rec.SessionID.String(), models.OperationRoleOperator); err != nil {
		return false, err
	}
	if _, err := r.recordings.Delete(ctx, rid); err != nil {
		switch {
		case errors.Is(err, recordings.ErrRecording):
			return false, moduleGQLError(ctx, errCodeBadUserInput, err.Error())
		case repository.IsNotFound(err):
			return false, moduleGQLError(ctx, errCodeNotFound, "recording not found")
		}
		return false, fmt.Errorf("delete the recording: %w", err)
	}
	return true, nil
}

// recordingModel is a recording as GraphQL.
func recordingModel(rec *models.DesktopRecording) *model.DesktopRecording {
	m := &model.DesktopRecording{ID: rec.RecordingID.String(), TaskID: rec.TaskID.String(), Number: int(rec.Number), Front: rec.Front,
		UserID: rec.UserID.String(), Username: rec.Username, Width: rec.Width, Height: rec.Height, Keys: rec.Keys, State: rec.State,
		StartedAt: rec.StartedAt.UTC().Format(time.RFC3339Nano), EndedAt: taskTime(rec.EndedAt), DurationMs: int(rec.DurationMs),
		Size: int(rec.Size), Lost: int(rec.Lost), End: strPtr(rec.End), Msg: strPtr(rec.Msg),
		Path: "/desktop-recordings/" + rec.RecordingID.String()}
	if len(rec.Settings) > 0 {
		m.Settings = map[string]any(rec.Settings)
	}
	return m
}
