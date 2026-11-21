package resolver

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// An entry is unread when it is a warning or an error that came after the
// session's feedback was last marked read; information never is: it is
// counted nowhere, so nothing would ever mark it read.
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
		{models.FeedbackWarning, after, &readAt, true},
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
