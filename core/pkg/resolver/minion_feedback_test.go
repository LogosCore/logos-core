package resolver

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// An entry is unread when it is an error that came after the session's
// feedback was last marked read; warnings and information never are: they
// are counted nowhere, so they would never be marked read.
func TestFeedbackUnread(t *testing.T) {
	readAt := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	entry := func(level string, received time.Time) models.MinionFeedback {
		return models.MinionFeedback{FeedbackID: uuid.New(), Level: level, Msg: "m", Count: 1, ReceivedAt: received}
	}
	before, after := readAt.Add(-time.Minute), readAt.Add(time.Minute)
	for _, c := range []struct {
		level    string
		received time.Time
		readAt   *time.Time
		want     bool
	}{
		{models.FeedbackError, after, &readAt, true},
		{models.FeedbackWarning, after, &readAt, false},
		{models.FeedbackWarning, after, nil, false},
		{models.FeedbackError, before, &readAt, false},
		{models.FeedbackError, after, nil, true},
		{models.FeedbackInfo, after, &readAt, false},
		{models.FeedbackInfo, after, nil, false},
	} {
		if got := toFeedbackModel(entry(c.level, c.received), c.readAt).Unread; got != c.want {
			t.Errorf("%s received %v, read at %v: unread %v, want %v", c.level, c.received, c.readAt, got, c.want)
		}
	}
}
