package repository

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"go.mongodb.org/mongo-driver/bson"
)

// A session's task list narrowed: a search reads each task's line as the
// console shows it, important keeps the tasks marked so (the partial
// index), both page like the whole list; a mark keeps who made it first;
// the line follows an arg changed later; tasks from before lines get one.
func TestIntegrationMinionTaskFilters(t *testing.T) {
	db := integrationDB(t)
	ctx := testCtx(t)
	repo := NewMinionTaskRepository(db)
	if err := db.IndexSetupErr(); err != nil {
		t.Fatalf("task indexes: %v", err)
	}
	op, session, alice, bob := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	add := func(i int, sid uuid.UUID, command, rest string, args models.JSONMap) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := repo.Insert(ctx, &models.MinionTask{TaskID: id, OperationID: op, SessionID: sid, Command: command, LineRest: rest,
			Args: args, IssuedBy: alice, State: models.MinionTaskQueued, QueuedAt: t0.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	whoami := add(0, session, "shell", "command", models.JSONMap{"command": "whoami"})
	passwd := add(1, session, "shell", "command", models.JSONMap{"command": "cat /etc/passwd", "timeout": "1m"})
	sleep := add(2, session, "sleep", "", models.JSONMap{"interval": "30s", "jitter": 20})
	dir := add(3, session, "ls", "path", models.JSONMap{"path": "/My Dir"})
	add(4, uuid.New(), "shell", "command", models.JSONMap{"command": "whoami"}) // another session's

	list := func(f MinionTaskFilter, before *MinionTaskCursor, limit int64) []uuid.UUID {
		t.Helper()
		rows, err := repo.ListBySession(ctx, session, f, before, limit)
		if err != nil {
			t.Fatalf("%+v: %v", f, err)
		}
		out := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			out[i] = r.TaskID
		}
		return out
	}
	for _, c := range []struct {
		search string
		want   []uuid.UUID
	}{
		{"whoami", []uuid.UUID{whoami}},
		{"SHELL WHO", []uuid.UUID{whoami}},      // case aside, the line as shown
		{"shell cat /etc", []uuid.UUID{passwd}}, // the rest param bare after the command
		{"--timeout=1m", []uuid.UUID{passwd}},   // an option as shown
		{"--interval=30s", []uuid.UUID{sleep}},
		{"/my dir", []uuid.UUID{dir}},
		{"shell", []uuid.UUID{passwd, whoami}}, // newest first
		{`"etc"`, []uuid.UUID{passwd}},         // quoted: a whole word
		{`"who"`, nil},
		{"[", nil},                                      // literal, not a pattern
		{"  ", []uuid.UUID{dir, sleep, passwd, whoami}}, // blank: no search
	} {
		if got := list(MinionTaskFilter{Search: c.search}, nil, 10); !slices.Equal(got, c.want) {
			t.Errorf("search %q: %v, want %v", c.search, got, c.want)
		}
	}

	// Marked: once, by who did it first; an unknown task is not found.
	at := t0.Add(time.Minute)
	marked, changed, err := repo.SetImportant(ctx, whoami, true, alice, at)
	if err != nil || !changed || marked.ImportantAt == nil || !marked.ImportantAt.Equal(at) || *marked.ImportantBy != alice || marked.Rev != 2 {
		t.Fatalf("mark: %+v changed %v err %v", marked, changed, err)
	}
	again, changed, err := repo.SetImportant(ctx, whoami, true, bob, at.Add(time.Minute))
	if err != nil || changed || *again.ImportantBy != alice || again.Rev != 2 {
		t.Fatalf("marked again: %+v changed %v err %v", again, changed, err)
	}
	if _, _, err := repo.SetImportant(ctx, uuid.New(), true, alice, at); !IsNotFound(err) {
		t.Fatalf("an unknown task: %v", err)
	}
	if _, _, err := repo.SetImportant(ctx, sleep, true, bob, at); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.CountImportant(ctx, session); err != nil || n != 2 {
		t.Fatalf("count %d: %v", n, err)
	}
	if got := list(MinionTaskFilter{Important: true}, nil, 10); !slices.Equal(got, []uuid.UUID{sleep, whoami}) {
		t.Fatalf("important: %v", got)
	}
	if got := list(MinionTaskFilter{Important: true, Search: "who"}, nil, 10); !slices.Equal(got, []uuid.UUID{whoami}) {
		t.Fatalf("important and searched: %v", got)
	}
	// Narrowed lists page as the whole one does.
	first := list(MinionTaskFilter{Important: true}, nil, 1)
	sleepTask, _ := repo.FindByTaskID(ctx, sleep)
	next := list(MinionTaskFilter{Important: true}, &MinionTaskCursor{QueuedAt: sleepTask.QueuedAt, TaskID: sleepTask.TaskID}, 1)
	if !slices.Equal(first, []uuid.UUID{sleep}) || !slices.Equal(next, []uuid.UUID{whoami}) {
		t.Fatalf("pages %v then %v", first, next)
	}

	// Unmarked: once.
	unmarked, changed, err := repo.SetImportant(ctx, sleep, false, alice, at)
	if err != nil || !changed || unmarked.ImportantAt != nil || unmarked.ImportantBy != nil {
		t.Fatalf("unmark: %+v changed %v err %v", unmarked, changed, err)
	}
	if _, changed, _ := repo.SetImportant(ctx, sleep, false, alice, at); changed {
		t.Fatal("unmarked twice")
	}
	if n, _ := repo.CountImportant(ctx, session); n != 1 {
		t.Fatalf("count after unmarking %d", n)
	}

	// An arg changed later (a VPN's routes) shows in the line.
	if err := repo.SetArg(ctx, sleep, "jitter", 5); err != nil {
		t.Fatal(err)
	}
	if got := list(MinionTaskFilter{Search: "--jitter=5"}, nil, 10); !slices.Equal(got, []uuid.UUID{sleep}) {
		t.Fatalf("changed arg: %v", got)
	}

	// A task stored before tasks had lines gets one: every value as an
	// option, what showed bare not known.
	legacy := add(5, session, "shell", "command", models.JSONMap{"command": "id"})
	raw := repo.(*minionTaskRepository).tasks
	if err := raw.UpdateOne(ctx, bson.M{"task_id": legacy}, bson.M{"$unset": bson.M{"search_text": "", "line_rest": ""}}); err != nil {
		t.Fatal(err)
	}
	if got := list(MinionTaskFilter{Search: "id"}, nil, 10); slices.Contains(got, legacy) {
		t.Fatal("a task without a line matched")
	}
	if n, err := repo.BackfillSearchText(ctx); err != nil || n != 1 {
		t.Fatalf("backfill %d: %v", n, err)
	}
	if n, _ := repo.BackfillSearchText(ctx); n != 0 {
		t.Fatalf("backfill again %d", n)
	}
	stored, _ := repo.FindByTaskID(ctx, legacy)
	if stored.SearchText != "shell --command=id" {
		t.Fatalf("backfilled %q", stored.SearchText)
	}
}
