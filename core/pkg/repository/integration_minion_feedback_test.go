package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A session's feedback: one entry per minion id, newest first in pages,
// by level and by a word of its message or code; a task's; the newest kept
// past a cap; gone with the operation. The session counts it unread until
// marked read.
func TestIntegrationMinionFeedback(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionFeedbackRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("feedback indexes: %v", err)
	}
	op, session, task := uuid.New(), uuid.New(), uuid.New()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	add := func(i int, level, msg, code string, onTask bool) bool {
		t.Helper()
		e := &models.MinionFeedback{FeedbackID: uuid.New(), OperationID: op, SessionID: session, MinionID: []byte{byte(i), 0, 0, 0, 0, 0, 0, 1},
			Level: level, Msg: msg, Code: code, Count: 1, ReceivedAt: t0.Add(time.Duration(i) * time.Second)}
		if onTask {
			e.Task = &task
		}
		added, err := repo.Add(ctx, e)
		if err != nil {
			t.Fatal(err)
		}
		return added
	}
	for i := range 5 {
		level := models.FeedbackWarning
		if i%2 == 1 {
			level = models.FeedbackError
		}
		if !add(i, level, "check-ins failed", "checkin_failed", i == 3) {
			t.Fatalf("entry %d not added", i)
		}
	}
	if add(2, models.FeedbackError, "again", "x", false) {
		t.Fatal("the same minion id twice")
	}
	if !add(5, models.FeedbackError, "The DISK is full", "disk_full", false) {
		t.Fatal("entry 5")
	}

	page, next, err := repo.List(ctx, session, FeedbackQuery{First: 4})
	if err != nil || len(page) != 4 || next == "" || page[0].Msg != "The DISK is full" {
		t.Fatalf("first page: %d %q %v", len(page), next, err)
	}
	rest, next, err := repo.List(ctx, session, FeedbackQuery{First: 4, After: next})
	if err != nil || len(rest) != 2 || next != "" || rest[1].MinionID[0] != 0 {
		t.Fatalf("second page: %d %q %v", len(rest), next, err)
	}
	errs, _, _ := repo.List(ctx, session, FeedbackQuery{Level: models.FeedbackError})
	disk, _, _ := repo.List(ctx, session, FeedbackQuery{Search: "disk"})
	byCode, _, _ := repo.List(ctx, session, FeedbackQuery{Search: "checkin_"})
	if len(errs) != 3 || len(disk) != 1 || len(byCode) != 5 {
		t.Fatalf("errors %d, disk %d, by code %d", len(errs), len(disk), len(byCode))
	}
	if _, _, err := repo.List(ctx, session, FeedbackQuery{After: "nonsense"}); err == nil {
		t.Fatal("a bad cursor passed")
	}
	if onTask, err := repo.ForTask(ctx, task, 20); err != nil || len(onTask) != 1 {
		t.Fatalf("the task's: %d %v", len(onTask), err)
	}

	if err := repo.Trim(ctx, session, 3); err != nil {
		t.Fatal(err)
	}
	kept, _, _ := repo.List(ctx, session, FeedbackQuery{})
	if len(kept) != 3 || kept[2].MinionID[0] != 3 {
		t.Fatalf("after the trim: %d", len(kept))
	}
	if err := repo.DeleteByOperationID(ctx, op); err != nil {
		t.Fatal(err)
	}
	if left, _, _ := repo.List(ctx, session, FeedbackQuery{}); len(left) != 0 {
		t.Fatalf("%d left with the operation gone", len(left))
	}

	// The session's counts.
	sessions := NewMinionSessionRepository(db)
	sid := uuid.New()
	if err := sessions.Insert(ctx, &models.MinionSession{SessionID: sid, OperationID: op, CreatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := sessions.AddFeedback(ctx, sid, 1, 2, t0); err != nil {
		t.Fatal(err)
	}
	if err := sessions.AddFeedback(ctx, sid, 1, 0, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s, err := sessions.FindBySessionID(ctx, sid)
	if err != nil || s.FeedbackErrors != 2 || s.FeedbackWarnings != 2 || s.FeedbackUnread != 4 || !s.FeedbackAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("counts: %+v %v", s, err)
	}
	if err := sessions.MarkFeedbackRead(ctx, sid, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s, _ = sessions.FindBySessionID(ctx, sid); s.FeedbackUnread != 0 || s.FeedbackErrors != 0 || s.FeedbackReadAt == nil {
		t.Fatalf("after read: %+v", s)
	}
}
