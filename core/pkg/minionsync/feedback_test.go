package minionsync

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeFeedback keeps entries, one per session and minion id.
type fakeFeedback struct {
	repository.IMinionFeedbackRepository
	rows    []models.MinionFeedback
	trimmed int
}

func (f *fakeFeedback) Add(_ context.Context, e *models.MinionFeedback) (bool, error) {
	for _, r := range f.rows {
		if r.SessionID == e.SessionID && bytes.Equal(r.MinionID, e.MinionID) {
			return false, nil
		}
	}
	f.rows = append(f.rows, *e)
	return true, nil
}

func (f *fakeFeedback) Trim(context.Context, uuid.UUID, int) error { f.trimmed++; return nil }

func (f *fakeSessions) AddFeedback(_ context.Context, id uuid.UUID, errs, warns int, at time.Time) error {
	for i := range f.rows {
		if f.rows[i].SessionID == id {
			r := &f.rows[i]
			r.FeedbackErrors, r.FeedbackWarnings, r.FeedbackUnread = r.FeedbackErrors+errs, r.FeedbackWarnings+warns, r.FeedbackUnread+errs+warns
			r.FeedbackAt = &at
		}
	}
	return nil
}

// A minion's feedback is kept with its session and counted unread; the
// message sent again adds nothing; one that does not read is refused; the
// task it names is kept, and counted on that task when it is the
// session's.
func TestFeedback(t *testing.T) {
	r := newRig(t)
	feedback := &fakeFeedback{}
	r.svc.WithFeedback(feedback)
	sid, row := helloWith(t, r, nil)
	task := uuid.New()
	tasks := &fakeTasks{rows: []models.MinionTask{{TaskID: task, SessionID: sid}}}
	r.svc.WithTasks(tasks)
	warn := protocol.FeedbackItem{T: protocol.ItemFeedback, ID: bytes.Repeat([]byte{1}, 8), Level: protocol.FeedbackWarning,
		Msg: "3 check-ins failed", Code: "checkin_failed", At: 1_800_000_000, Count: 3}
	fail := protocol.FeedbackItem{T: protocol.ItemFeedback, ID: bytes.Repeat([]byte{2}, 8), Level: protocol.FeedbackError,
		Msg: "the task panicked", Code: "task_panic", Task: task[:]}
	checkIn := checkInOf(t, r, sid)
	id := protocol.NewMessageID()
	send := func(items ...any) {
		t.Helper()
		if _, err := r.sendUnder(t, sid, r.keys, id, items...); err != nil {
			t.Fatal(err)
		}
	}
	send(warn, fail)
	if len(feedback.rows) != 2 || row.FeedbackErrors != 1 || row.FeedbackWarnings != 1 || row.FeedbackUnread != 2 || row.FeedbackAt == nil {
		t.Fatalf("kept %d; session %d errors, %d warnings, %d unread", len(feedback.rows), row.FeedbackErrors, row.FeedbackWarnings, row.FeedbackUnread)
	}
	if e := feedback.rows[1]; e.Level != models.FeedbackError || e.Task == nil || *e.Task != task || e.Code != "task_panic" || e.Count != 1 {
		t.Fatalf("the error: %+v", e)
	}
	if e := feedback.rows[0]; e.At == nil || e.At.Unix() != 1_800_000_000 || e.Count != 3 {
		t.Fatalf("the warning: %+v", e)
	}
	if tt := tasks.find(task); tt.FeedbackErrors != 1 || tt.FeedbackWarnings != 0 {
		t.Fatalf("the task counts %d errors, %d warnings", tt.FeedbackErrors, tt.FeedbackWarnings)
	}
	// The same message again (its reply lost): nothing new.
	id = protocol.NewMessageID()
	send(warn, fail)
	if len(feedback.rows) != 2 || row.FeedbackUnread != 2 || tasks.find(task).FeedbackErrors != 1 {
		t.Fatalf("sent again: %d kept, %d unread, task %d errors", len(feedback.rows), row.FeedbackUnread, tasks.find(task).FeedbackErrors)
	}
	// About a task not the session's: kept, counted on no task.
	other := fail
	other.ID = bytes.Repeat([]byte{3}, 8)
	stranger := uuid.New()
	other.Task = stranger[:]
	id = protocol.NewMessageID()
	send(other)
	if len(feedback.rows) != 3 || row.FeedbackErrors != 2 {
		t.Fatalf("about a stranger: %d kept, session %d errors", len(feedback.rows), row.FeedbackErrors)
	}
	bad := warn
	bad.ID = []byte{1}
	errs := 0
	for _, it := range checkIn(bad) {
		if protocol.ItemType(it) == protocol.ItemErr {
			errs++
		}
	}
	if errs != 1 {
		t.Fatalf("%d errs for feedback that does not read", errs)
	}
}
