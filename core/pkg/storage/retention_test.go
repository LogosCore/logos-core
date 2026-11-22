package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings"
	"github.com/logoscore/logos-core/core/pkg/recordings/recordingstest"
	"go.uber.org/zap"
)

const day = 24 * time.Hour

// fakePolicy keeps the retention policy in memory, counting its reads.
type fakePolicy struct {
	mu   sync.Mutex
	p    models.StorageRetention
	gets int
}

func (f *fakePolicy) Get(context.Context) (models.StorageRetention, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	return f.p, nil
}

func (f *fakePolicy) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets
}

func (f *fakePolicy) Set(_ context.Context, p models.StorageRetention) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.p = p
	return nil
}

// testClock is the wall clock until the test sets it; safe to move while
// the service's goroutines read it.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.t.IsZero() {
		return time.Now()
	}
	return c.t
}

func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// cleanupRig is the service with cleanups: the recordings service over the
// in-memory bucket and records.
type cleanupRig struct {
	svc     *Service
	actions *fakeActions
	policy  *fakePolicy
	repo    *recordingstest.Repo
	store   *recordingstest.Store
	clock   *testClock
}

func newCleanupRig(t *testing.T, ops fakeOps) *cleanupRig {
	t.Helper()
	store, repo := recordingstest.NewStore(), recordingstest.NewRepo()
	recs := recordings.NewService(repo, recordingstest.NewTasks(models.MinionTask{TaskID: uuid.New()}), store, nil,
		recordings.DefaultConfig(), zap.NewNop())
	svc, actions := newService(t, "", fakeUsage{}, ops)
	policy := &fakePolicy{p: models.StorageRetention{OlderThanDays: models.DefaultRetentionDays}}
	svc.WithRetention(policy, recs)
	clock := &testClock{}
	svc.now = clock.now
	return &cleanupRig{svc: svc, actions: actions, policy: policy, repo: repo, store: store, clock: clock}
}

// add keeps a recording of op's task that ended ago, of size bytes in one
// part.
func (g *cleanupRig) add(t *testing.T, op, task uuid.UUID, ago time.Duration, size int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ended := time.Now().UTC().Add(-ago)
	part := strings.Repeat("x", size)
	if err := g.store.Put(context.Background(), partOf(id), strings.NewReader(part), int64(size), ""); err != nil {
		t.Fatal(err)
	}
	if err := g.repo.Insert(context.Background(), &models.Recording{RecordingID: id, OperationID: op, TaskID: task, Front: "rdp",
		State: models.RecordingStateSaved, Parts: 1, Size: int64(size), StartedAt: ended.Add(-time.Minute), EndedAt: &ended}); err != nil {
		t.Fatal(err)
	}
	return id
}

func partOf(id uuid.UUID) string { return fmt.Sprintf("parts/%s/%06d", id, 0) }

// kept reports whether a recording keeps its file, and its mark.
func (g *cleanupRig) kept(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	rec, err := g.repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	_, headErr := g.store.Head(context.Background(), partOf(id))
	if (headErr == nil) != (rec.CleanedAt == nil) {
		t.Fatalf("recording %s: file kept %v, cleaned up at %v", id, headErr == nil, rec.CleanedAt)
	}
	return headErr == nil
}

// ended waits for a cleanup's action to end, as it is then.
func (g *cleanupRig) ended(t *testing.T, id uuid.UUID) models.StorageAction {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a := g.actions.get(id); a.Status != models.StorageActionRunning {
			return a
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the cleanup %s did not end", id)
	return models.StorageAction{}
}

// A preview is what a cleanup would take now: the recordings that ended
// the age ago, but those of tasks marked important; in all, and by
// operation, the most bytes first, a page at a time. An operation gone has
// no name.
func TestCleanupPreview(t *testing.T) {
	red, blue, gone := uuid.New(), uuid.New(), uuid.New()
	g := newCleanupRig(t, fakeOps{{OperationID: red, Name: "Red"}, {OperationID: blue, Name: "Blue"}})
	starred := uuid.New()
	g.add(t, red, uuid.New(), 40*day, 100)
	g.add(t, red, uuid.New(), 31*day, 50)
	g.add(t, red, uuid.New(), 10*day, 999)
	g.add(t, red, starred, 90*day, 999)
	g.repo.Important(starred, true)
	g.add(t, blue, uuid.New(), 60*day, 400)
	g.add(t, gone, uuid.New(), 60*day, 20)

	p, err := g.svc.CleanupPreview(context.Background(), 30, "", 2)
	if err != nil || p.OlderThanDays != 30 || p.Recordings != 4 || p.Bytes != 570 || p.Total != 3 || !p.More || len(p.Operations) != 2 {
		t.Fatalf("preview %+v %v", p, err)
	}
	if o := p.Operations[0]; o.OperationID != blue || o.OperationName != "Blue" || o.Recordings != 1 || o.Bytes != 400 {
		t.Fatalf("the largest first: %+v", o)
	}
	if o := p.Operations[1]; o.OperationID != red || o.Recordings != 2 || o.Bytes != 150 {
		t.Fatalf("then %+v", o)
	}
	if !p.Before.Before(time.Now().Add(-30*day+time.Minute)) || p.Before.Before(time.Now().Add(-30*day-time.Minute)) {
		t.Fatalf("before %v", p.Before)
	}
	rest, err := g.svc.CleanupPreview(context.Background(), 30, CleanupCursor(p.Operations[1]), 2)
	if err != nil || rest.More || len(rest.Operations) != 1 || rest.Operations[0].OperationID != gone || rest.Operations[0].OperationName != "" {
		t.Fatalf("the page after %+v %v", rest, err)
	}
	if p, _ := g.svc.CleanupPreview(context.Background(), 45, "", 10); p.Recordings != 2 || p.Bytes != 420 {
		t.Fatalf("at 45 days %+v", p)
	}
	for _, days := range []int{0, -1, MaxRetentionDays + 1} {
		if _, err := g.svc.CleanupPreview(context.Background(), days, "", 10); !errors.Is(err, ErrBadAge) {
			t.Errorf("%d days: %v", days, err)
		}
	}
	if _, err := g.svc.CleanupPreview(context.Background(), 30, "not a cursor", 10); !errors.Is(err, ErrBadCursor) {
		t.Errorf("a bad cursor: %v", err)
	}
	plain, _ := newService(t, "", fakeUsage{}, nil)
	if _, err := plain.CleanupPreview(context.Background(), 30, "", 10); !errors.Is(err, ErrNoRetention) {
		t.Errorf("without cleanups: %v", err)
	}
}

// A cleanup runs in the background, a batch at a time: each recording it
// takes loses its file and keeps its record, marked; those of tasks marked
// important, those that ended since its age, and those of another
// operation for one that covers one, are kept. The action keeps who ran
// it, its age, what it covered, and what it took. One runs at a time.
func TestCleanUp(t *testing.T) {
	red, blue := uuid.New(), uuid.New()
	g := newCleanupRig(t, fakeOps{{OperationID: red, Name: "Red"}, {OperationID: blue, Name: "Blue"}})
	g.svc.cfg.CleanupBatch = 2
	starred := uuid.New()
	g.repo.Important(starred, true)
	old := []uuid.UUID{g.add(t, red, uuid.New(), 40*day, 10), g.add(t, red, uuid.New(), 35*day, 20), g.add(t, red, uuid.New(), 33*day, 30)}
	blueOld := g.add(t, blue, uuid.New(), 50*day, 40)
	recent := g.add(t, red, uuid.New(), 2*day, 50)
	important := g.add(t, red, starred, 400*day, 60)
	alice := models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}

	gone := uuid.New()
	if _, err := g.svc.CleanUp(context.Background(), alice, 30, &gone); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("an operation gone: %v", err)
	}
	a, err := g.svc.CleanUp(context.Background(), alice, 30, &red)
	if err != nil || a.Status != models.StorageActionRunning || a.Action != models.StorageActionCleanup || a.Bucket != "desktop-recordings" ||
		a.OlderThanDays != 30 || a.Before == nil || *a.OperationID != red || a.OperationName != "Red" {
		t.Fatalf("started %+v %v", a, err)
	}
	done := g.ended(t, a.ActionID)
	if done.Status != models.StorageActionSucceeded || done.Recordings != 3 || done.Bytes != 60 || done.Actor.Name != "alice" {
		t.Fatalf("ended %+v", done)
	}
	for _, id := range old {
		if g.kept(t, id) {
			t.Errorf("an old recording of the operation kept its file")
		}
		if rec, _ := g.repo.FindByID(context.Background(), id); *rec.CleanedBy != a.ActionID {
			t.Errorf("cleaned by %v, want %v", rec.CleanedBy, a.ActionID)
		}
	}
	for name, id := range map[string]uuid.UUID{"another operation's": blueOld, "a recent one": recent, "an important task's": important} {
		if !g.kept(t, id) {
			t.Errorf("%s was taken", name)
		}
	}

	// Every operation's, once the one before ended.
	every, err := g.svc.CleanUp(context.Background(), alice, 30, nil)
	if err != nil || every.OperationID != nil {
		t.Fatalf("every operation's: %+v %v", every, err)
	}
	if done := g.ended(t, every.ActionID); done.Recordings != 1 || done.Bytes != 40 || g.kept(t, blueOld) {
		t.Fatalf("every operation's ended %+v", done)
	}

	// One at a time.
	running := &models.StorageAction{ActionID: uuid.New(), Action: models.StorageActionCleanup, Bucket: "desktop-recordings",
		Status: models.StorageActionRunning}
	if err := g.actions.Insert(context.Background(), running); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.CleanUp(context.Background(), alice, 30, nil); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second while one runs: %v", err)
	}
	if _, err := g.svc.CleanUp(context.Background(), alice, 0, nil); !errors.Is(err, ErrBadAge) {
		t.Fatalf("0 days: %v", err)
	}
}

// The store failing stops a cleanup: the action says why, and what it took
// before; the recording it was taking keeps its record unmarked, for the
// next cleanup.
func TestCleanUpStoreFails(t *testing.T) {
	op := uuid.New()
	g := newCleanupRig(t, fakeOps{{OperationID: op, Name: "Red"}})
	first := g.add(t, op, uuid.New(), 40*day, 10)
	second := g.add(t, op, uuid.New(), 35*day, 20)
	a, err := g.svc.CleanUp(context.Background(), models.RetentionActor, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done := g.ended(t, a.ActionID); done.Status != models.StorageActionSucceeded || done.Recordings != 2 {
		t.Fatalf("with the store up %+v", done)
	}

	third := g.add(t, op, uuid.New(), 33*day, 30)
	g.store.FailDeletes(1)
	a, err = g.svc.CleanUp(context.Background(), models.RetentionActor, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := g.ended(t, a.ActionID)
	if done.Status != models.StorageActionFailed || !strings.Contains(done.Err, "delete a recording's files") || done.Recordings != 0 {
		t.Fatalf("with the store down %+v", done)
	}
	if !g.kept(t, third) {
		t.Fatalf("a recording whose part the store did not delete is marked cleaned up")
	}
	if g.kept(t, first) || g.kept(t, second) {
		t.Fatal("what the first cleanup took came back")
	}
}

// The retention policy: off, at 30 days, until an admin sets it. Turned
// on, its first cleanup starts at once, of every operation, by the policy;
// the next is due a day after it began, or an hour after one that failed
// ended; off, none runs.
func TestRetentionPolicy(t *testing.T) {
	op := uuid.New()
	g := newCleanupRig(t, fakeOps{{OperationID: op, Name: "Red"}})
	g.svc.cfg.RetentionCheckEvery = time.Hour
	old := g.add(t, op, uuid.New(), 50*day, 10)
	g.add(t, op, uuid.New(), 20*day, 10)

	r, err := g.svc.Retention(context.Background())
	if err != nil || r.Policy.Enabled || r.Policy.OlderThanDays != 30 || r.LastRun != nil || r.NextRun != nil {
		t.Fatalf("before anyone set it %+v %v", r, err)
	}
	// Started, the loop looks at the policy (off), then waits an hour: only
	// turning it on has it look again.
	read := g.policy.reads()
	g.svc.Start()
	for deadline := time.Now().Add(5 * time.Second); g.policy.reads() == read; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("started, the loop did not look at the policy")
		}
	}
	alice := models.ModuleActor{Type: "user", ID: "u1", Name: "alice"}
	if _, err := g.svc.SetRetention(context.Background(), alice, true, 0); !errors.Is(err, ErrBadAge) {
		t.Fatalf("0 days: %v", err)
	}
	r, err = g.svc.SetRetention(context.Background(), alice, true, 40)
	if err != nil || !r.Policy.Enabled || r.Policy.OlderThanDays != 40 || r.Policy.UpdatedBy.Name != "alice" || r.Policy.UpdatedAt == nil {
		t.Fatalf("set %+v %v", r, err)
	}
	var run *models.StorageAction
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if last, _ := g.actions.Last(context.Background(), models.StorageActionCleanup, models.RetentionActor); last != nil {
			run = last
			break
		}
	}
	if run == nil {
		t.Fatal("turned on, the policy ran no cleanup")
	}
	done := g.ended(t, run.ActionID)
	if done.OlderThanDays != 40 || done.OperationID != nil || done.Recordings != 1 || g.kept(t, old) {
		t.Fatalf("the policy's cleanup %+v", done)
	}
	r, _ = g.svc.Retention(context.Background())
	if r.LastRun == nil || r.LastRun.ActionID != run.ActionID || r.NextRun == nil || !r.NextRun.Equal(done.StartedAt.Add(day)) {
		t.Fatalf("after its first run %+v", r)
	}

	// Due a day after it began.
	g.svc.runPolicy(context.Background())
	if n := g.cleanups(); n != 1 {
		t.Fatalf("ran again within the day: %d cleanups", n)
	}
	start := done.StartedAt
	g.clock.set(start.Add(day + time.Minute))
	g.svc.runPolicy(context.Background())
	if n := g.cleanups(); n != 2 {
		t.Fatalf("a day later: %d cleanups", n)
	}
	g.ended(t, g.lastCleanup(t).ActionID)

	// An hour after one that failed.
	g.actions.mu.Lock()
	for i := range g.actions.list {
		a := &g.actions.list[i]
		if a.Actor == models.RetentionActor && a.StartedAt.After(start) {
			finished := start.Add(day + 2*time.Minute)
			a.Status, a.FinishedAt = models.StorageActionFailed, &finished
		}
	}
	g.actions.mu.Unlock()
	failedAt := start.Add(day + 2*time.Minute)
	g.clock.set(failedAt.Add(30 * time.Minute))
	g.svc.runPolicy(context.Background())
	if n := g.cleanups(); n != 2 {
		t.Fatalf("half an hour after a failure: %d cleanups", n)
	}
	g.clock.set(failedAt.Add(61 * time.Minute))
	g.svc.runPolicy(context.Background())
	if n := g.cleanups(); n != 3 {
		t.Fatalf("an hour after a failure: %d cleanups", n)
	}
	g.ended(t, g.lastCleanup(t).ActionID)

	// Off, none runs.
	if _, err := g.svc.SetRetention(context.Background(), alice, false, 40); err != nil {
		t.Fatal(err)
	}
	g.clock.set(failedAt.Add(10 * day))
	g.svc.runPolicy(context.Background())
	if r, _ := g.svc.Retention(context.Background()); g.cleanups() != 3 || r.NextRun != nil {
		t.Fatalf("off: %d cleanups, next %v", g.cleanups(), r.NextRun)
	}
}

// cleanups counts the cleanups kept.
func (g *cleanupRig) cleanups() int {
	g.actions.mu.Lock()
	defer g.actions.mu.Unlock()
	n := 0
	for _, a := range g.actions.list {
		if a.Action == models.StorageActionCleanup {
			n++
		}
	}
	return n
}

func (g *cleanupRig) lastCleanup(t *testing.T) models.StorageAction {
	t.Helper()
	a, err := g.actions.Last(context.Background(), models.StorageActionCleanup, models.RetentionActor)
	if err != nil || a == nil {
		t.Fatalf("no cleanup: %v", err)
	}
	return *a
}
