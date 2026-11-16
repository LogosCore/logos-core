package recordings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/guacd"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/recordings/recordingstest"
	"go.uber.org/zap"
)

// clock is a clock the test moves.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type rig struct {
	svc   *Service
	store *recordingstest.Store
	repo  *recordingstest.Repo
	tasks *recordingstest.Tasks
	task  models.MinionTask
	clock *clock
}

func newRig(cfg Config) *rig {
	store, repo := recordingstest.NewStore(), recordingstest.NewRepo()
	task := models.MinionTask{TaskID: uuid.New(), OperationID: uuid.New(), SessionID: uuid.New()}
	tasks := recordingstest.NewTasks(task)
	c := &clock{t: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)}
	svc := NewService(repo, tasks, store, nil, cfg, zap.NewNop())
	svc.now, svc.clock = c.now, c.now
	return &rig{svc: svc, store: store, repo: repo, tasks: tasks, task: task, clock: c}
}

// testConfig keeps parts only as the test asks (End, or a part filled).
func testConfig() Config {
	c := DefaultConfig()
	c.PartEvery, c.RetryWait = time.Hour, time.Millisecond
	return c
}

func (g *rig) begin(t *testing.T, keys bool) *Recorder {
	t.Helper()
	r := g.svc.Begin(context.Background(), Beginning{TaskID: g.task.TaskID, Front: "rdp", UserID: uuid.New(), Username: "ann",
		Settings: map[string]any{"quality": "best"}, Width: 1280, Height: 720, Keys: keys})
	if r == nil {
		t.Fatal("not recorded")
	}
	return r
}

func (g *rig) file(t *testing.T, id uuid.UUID) string {
	t.Helper()
	body, rec, err := g.svc.Open(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	b, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) != rec.Size {
		t.Errorf("read %d bytes, the record says %d", len(b), rec.Size)
	}
	return string(b)
}

func enc(op string, args ...string) string { return guacd.Instruction{Opcode: op, Args: args}.Encode() }

// A desktop's recording: guacd's instructions as they came, the mouse and
// keys between them, each where it came, timed on guacd's clock from its
// last sync; none before guacd's first frame. Its record says how far it
// came, and how it ended.
func TestRecordingKeepsGuacdsInstructionsAndTheOperatorsInput(t *testing.T) {
	g := newRig(testConfig())
	r := g.begin(t, true)
	if kept, made := g.tasks.Counts(); kept != 1 || made != 1 {
		t.Fatalf("counts %d %d", kept, made)
	}
	// Before guacd's first sync: the player reads no input yet.
	r.FromWeb(guacd.Instruction{Opcode: "mouse", Args: []string{"5", "6", "0"}})
	size := enc("size", "0", "1280", "720")
	sync1 := enc("sync", "5000")
	img := enc("img", "1", "14", "0", "image/png", "0", "0")
	// guacd's bytes, cut inside instructions.
	stream := size + sync1 + img
	cut := len(size) + len(sync1) + 3
	r.FromGuacd([]byte(stream[:4]))
	r.FromGuacd([]byte(stream[4:cut]))
	// The key comes while img is half read: it goes after sync, before img.
	g.clock.add(250 * time.Millisecond)
	r.FromWeb(guacd.Instruction{Opcode: "key", Args: []string{"97", "1"}})
	r.FromGuacd([]byte(stream[cut:]))
	g.clock.add(100 * time.Millisecond)
	r.FromWeb(guacd.Instruction{Opcode: "mouse", Args: []string{"10", "20", "1"}})
	r.FromWeb(guacd.Instruction{Opcode: "size", Args: []string{"800", "600"}})
	r.FromWeb(guacd.Instruction{Opcode: "key", Args: []string{"x", "1"}})
	sync2 := enc("sync", "5400")
	errIns := enc("error", "Disconnected by the server", "521")
	// The end of guacd's: an instruction it never ended is not kept.
	r.FromGuacd([]byte(sync2 + errIns + "4.sync,2.5"))
	r.End(models.RecordingEndDesktop)

	want := size + sync1 + enc("key", "97", "1", "5250") + img + enc("mouse", "10", "20", "1", "5350") + sync2 + errIns
	if got := g.file(t, r.ID()); got != want {
		t.Errorf("recorded\n%q\nwant\n%q", got, want)
	}
	rec, _ := g.repo.FindByID(context.Background(), r.ID())
	if rec.State != models.RecordingStateSaved || rec.End != models.RecordingEndDesktop || rec.Msg != "Disconnected by the server" ||
		rec.DurationMs != 400 || rec.Parts != 1 || rec.Size != int64(len(want)) || rec.Lost != 0 || rec.EndedAt == nil || rec.Number != 1 ||
		!rec.Keys || rec.Username != "ann" || rec.Width != 1280 {
		t.Errorf("record %+v", rec)
	}
	// Ended: what comes after is not recorded, and ending it again is
	// nothing.
	r.FromGuacd([]byte(sync1))
	r.End(models.RecordingEndClosed)
	if again, _ := g.repo.FindByID(context.Background(), r.ID()); again.Size != rec.Size || again.End != rec.End {
		t.Errorf("after its end: %+v", again)
	}
	// A nil recorder (a desktop unrecorded) takes it all.
	var none *Recorder
	none.FromGuacd([]byte(sync1))
	none.FromWeb(guacd.Instruction{Opcode: "key", Args: []string{"97", "1"}})
	none.End(models.RecordingEndClosed)
	none.Fail("x")
}

// A view-only desktop's recording holds no input: its server took none.
func TestRecordingViewOnlyHoldsNoInput(t *testing.T) {
	g := newRig(testConfig())
	r := g.begin(t, false)
	sync1 := enc("sync", "100")
	r.FromGuacd([]byte(sync1))
	r.FromWeb(guacd.Instruction{Opcode: "key", Args: []string{"97", "1"}})
	r.FromWeb(guacd.Instruction{Opcode: "mouse", Args: []string{"1", "2", "0"}})
	r.End(models.RecordingEndClosed)
	if got := g.file(t, r.ID()); got != sync1 {
		t.Errorf("recorded %q", got)
	}
}

// A desktop that did not open: its recording says why, and holds nothing.
func TestRecordingOfADesktopThatDidNotOpen(t *testing.T) {
	g := newRig(testConfig())
	r := g.begin(t, true)
	r.Fail("core cannot reach guacd")
	rec, _ := g.repo.FindByID(context.Background(), r.ID())
	if rec.State != models.RecordingStateFailed || rec.End != models.RecordingEndNotOpened || rec.Msg != "core cannot reach guacd" ||
		rec.Parts != 0 {
		t.Errorf("record %+v", rec)
	}
	if _, _, err := g.svc.Open(context.Background(), r.ID()); !errors.Is(err, ErrNothing) {
		t.Errorf("open: %v", err)
	}
	if len(g.store.Keys()) != 0 {
		t.Errorf("parts %v", g.store.Keys())
	}
}

// Parts as they fill: one the store did not take goes first in the next;
// the recording reads as one, in order.
func TestRecordingPartsAndAStoreThatFails(t *testing.T) {
	cfg := testConfig()
	cfg.PartMax = 64
	g := newRig(cfg)
	g.store.Fail(1)
	r := g.begin(t, false)
	var want strings.Builder
	for i := range 6 {
		ins := enc("sync", fmt.Sprint(1000+i)) + enc("blob", "1", strings.Repeat("A", 60))
		want.WriteString(ins)
		r.FromGuacd([]byte(ins))
		// Each fills a part: wait for its put, so they stay apart.
		deadline := time.Now().Add(2 * time.Second)
		for {
			if g.store.Puts() > i || time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	r.End(models.RecordingEndClosed)
	if got := g.file(t, r.ID()); got != want.String() {
		t.Errorf("recorded %d bytes, want %d", len(got), want.Len())
	}
	rec, _ := g.repo.FindByID(context.Background(), r.ID())
	if rec.Parts != 5 || rec.Lost != 0 || rec.DurationMs != 5 {
		t.Errorf("record: %d parts, %d lost, %d ms", rec.Parts, rec.Lost, rec.DurationMs)
	}
}

// Past MaxHeld what comes is lost, a whole instruction at a time: the
// recording still reads, and says what it lacks; so does a last part the
// store never took.
func TestRecordingLosesWholeInstructions(t *testing.T) {
	cfg := testConfig()
	cfg.MaxHeld = 100
	g := newRig(cfg)
	r := g.begin(t, false)
	small := enc("sync", "1")
	big := enc("blob", "1", strings.Repeat("B", 200))
	r.FromGuacd([]byte(small))
	r.FromGuacd([]byte(big[:50]))
	r.FromGuacd([]byte(big[50:]))
	r.FromGuacd([]byte(small))
	r.End(models.RecordingEndClosed)
	if got := g.file(t, r.ID()); got != small+small {
		t.Errorf("recorded %q", got)
	}
	rec, _ := g.repo.FindByID(context.Background(), r.ID())
	if rec.Lost != int64(len(big)) {
		t.Errorf("lost %d, want %d", rec.Lost, len(big))
	}

	g2 := newRig(testConfig())
	g2.store.Fail(3)
	r2 := g2.begin(t, false)
	r2.FromGuacd([]byte(small))
	r2.End(models.RecordingEndClosed)
	rec2, _ := g2.repo.FindByID(context.Background(), r2.ID())
	if rec2.Parts != 0 || rec2.Lost != int64(len(small)) || rec2.State != models.RecordingStateSaved {
		t.Errorf("record %+v", rec2)
	}
}

// A recording whose core stopped: the sweep ends it, its parts kept after
// its core's last word too; one this core records is its own to end.
func TestSweepEndsRecordingsWhoseCoreStopped(t *testing.T) {
	g := newRig(testConfig())
	gone := models.Recording{RecordingID: uuid.New(), TaskID: g.task.TaskID, Number: 1,
		State: models.RecordingStateRecording, Parts: 1, Size: 3, SeenAt: g.clock.now()}
	_ = g.repo.Insert(context.Background(), &gone)
	_ = g.store.Put(context.Background(), partKey(gone.RecordingID, 0), strings.NewReader("abc"), 3, "")
	_ = g.store.Put(context.Background(), partKey(gone.RecordingID, 1), strings.NewReader("defg"), 4, "")
	mine := g.begin(t, false)
	g.clock.add(3 * time.Minute)
	g.svc.Sweep(context.Background())
	rec, _ := g.repo.FindByID(context.Background(), gone.RecordingID)
	if rec.State != models.RecordingStateSaved || rec.End != models.RecordingEndCoreStopped || rec.Parts != 2 || rec.Size != 7 ||
		rec.EndedAt == nil || !rec.EndedAt.Equal(gone.SeenAt) {
		t.Errorf("swept %+v", rec)
	}
	if got := g.file(t, gone.RecordingID); got != "abcdefg" {
		t.Errorf("read %q", got)
	}
	if still, _ := g.repo.FindByID(context.Background(), mine.ID()); still.State != models.RecordingStateRecording {
		t.Errorf("this core's: %s", still.State)
	}
	mine.End(models.RecordingEndClosed)
}

// Delete: one that ended goes, parts and all, and its task counts it
// gone; one recording stays. An operation's go with it, one recording
// ended first. Stop ends this core's, core_stopped.
func TestDeleteAndStop(t *testing.T) {
	g := newRig(testConfig())
	ctx := context.Background()
	a := g.begin(t, false)
	a.FromGuacd([]byte(enc("sync", "1")))
	if _, err := g.svc.Delete(ctx, a.ID()); !errors.Is(err, ErrRecording) {
		t.Fatalf("delete one recording: %v", err)
	}
	a.End(models.RecordingEndClosed)
	if _, err := g.svc.Delete(ctx, a.ID()); err != nil {
		t.Fatal(err)
	}
	if kept, made := g.tasks.Counts(); kept != 0 || made != 1 || len(g.store.Keys()) != 0 {
		t.Errorf("after delete: %d %d %v", kept, made, g.store.Keys())
	}
	if _, err := g.svc.Delete(ctx, a.ID()); err == nil {
		t.Error("deleted twice")
	}

	b := g.begin(t, false)
	b.FromGuacd([]byte(enc("sync", "1")))
	c := g.begin(t, false)
	c.FromGuacd([]byte(enc("sync", "2")))
	c.End(models.RecordingEndClosed)
	if page, _ := g.svc.List(ctx, g.task.TaskID, 0, 10); len(page) != 2 || page[0].Number != 3 || page[1].Number != 2 {
		t.Errorf("list %+v", page)
	}
	if err := g.svc.DeleteByOperation(ctx, g.task.OperationID); err != nil {
		t.Fatal(err)
	}
	if len(g.store.Keys()) != 0 || g.repo.Len() != 0 {
		t.Errorf("after the operation: %v %d", g.store.Keys(), g.repo.Len())
	}
	b.FromGuacd([]byte(enc("sync", "3")))

	// (The operation's desktops are not recorded within goneFor of it.)
	g.clock.add(goneFor + time.Second)
	d := g.begin(t, false)
	d.FromGuacd([]byte(enc("sync", "4")))
	g.svc.Stop()
	rec, _ := g.repo.FindByID(ctx, d.ID())
	if rec.State != models.RecordingStateSaved || rec.End != models.RecordingEndCoreStopped || rec.Parts != 1 {
		t.Errorf("stopped %+v", rec)
	}
	d.End(models.RecordingEndClosed)
	if again, _ := g.repo.FindByID(ctx, d.ID()); again.End != models.RecordingEndCoreStopped {
		t.Errorf("ended again: %s", again.End)
	}
}

// What a recording holds unkept, a part the store did not take with what
// came since, stays within MaxHeld: past it, what comes is lost, not held.
func TestRecordingHoldsNoMoreThanMaxHeld(t *testing.T) {
	cfg := testConfig()
	cfg.MaxHeld, cfg.PartMax = 100, 50
	g := newRig(cfg)
	g.store.Fail(1)
	r := g.begin(t, false)
	first := enc("blob", "1", strings.Repeat("A", 48))
	second := enc("blob", "1", strings.Repeat("B", 48))
	r.FromGuacd([]byte(first))
	// The part it filled is tried, and not taken: held.
	deadline := time.Now().Add(2 * time.Second)
	for g.store.Puts() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	// Held and this one are past MaxHeld: this one is lost.
	r.FromGuacd([]byte(second))
	r.End(models.RecordingEndClosed)
	if got := g.file(t, r.ID()); got != first {
		t.Errorf("recorded %q", got)
	}
	if rec, _ := g.repo.FindByID(context.Background(), r.ID()); rec.Lost != int64(len(second)) {
		t.Errorf("lost %d, want %d", rec.Lost, len(second))
	}
}

// guacd's bytes that stop reading as instructions part way through a
// read: the instructions the read ended before them are recorded, nothing
// after.
func TestRecordingStopsAtUnreadableBytes(t *testing.T) {
	g := newRig(testConfig())
	r := g.begin(t, false)
	a, b := enc("sync", "1"), enc("blob", "1", "AAAA")
	r.FromGuacd([]byte(a + b[:5]))
	r.FromGuacd([]byte(b[5:] + enc("sync", "2") + "x!;" + enc("sync", "3")))
	r.FromGuacd([]byte(enc("sync", "4")))
	r.End(models.RecordingEndClosed)
	if got := g.file(t, r.ID()); got != a+b+enc("sync", "2") {
		t.Errorf("recorded %q", got)
	}
}

// A last part the store does not take is tried within PutWait in all,
// not PutWait each try with the waits between: a core stopping, or an
// operation deleted, waits that long for it at most.
func TestRecordingLastPartWithinPutWait(t *testing.T) {
	cfg := testConfig()
	cfg.PutWait, cfg.RetryWait = 100*time.Millisecond, time.Second
	g := newRig(cfg)
	g.store.Fail(10)
	r := g.begin(t, false)
	r.FromGuacd([]byte(enc("sync", "1")))
	start := time.Now()
	r.End(models.RecordingEndClosed)
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Errorf("ended in %v", took)
	}
	if rec, _ := g.repo.FindByID(context.Background(), r.ID()); rec.State != models.RecordingStateSaved || rec.Parts != 0 {
		t.Errorf("record %+v", rec)
	}
}

// insertGate holds a recording's insert until the test lets it go.
type insertGate struct {
	*recordingstest.Repo
	entered, release chan struct{}
}

func (g *insertGate) Insert(ctx context.Context, r *models.Recording) error {
	close(g.entered)
	<-g.release
	return g.Repo.Insert(ctx, r)
}

// A desktop that begins as its operation's recordings are deleted (its
// task counted before the task went, its record kept after they were
// listed) is not recorded: nothing of it outlives the operation.
func TestRecordingBegunAsItsOperationGoes(t *testing.T) {
	g := newRig(testConfig())
	gate := &insertGate{Repo: g.repo, entered: make(chan struct{}), release: make(chan struct{})}
	g.svc.repo = gate
	got := make(chan *Recorder, 1)
	go func() {
		got <- g.svc.Begin(context.Background(), Beginning{TaskID: g.task.TaskID, Front: "rdp", Keys: true})
	}()
	<-gate.entered
	if err := g.svc.DeleteByOperation(context.Background(), g.task.OperationID); err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	r := <-got
	if r != nil {
		r.FromGuacd([]byte(enc("sync", "1")))
		r.End(models.RecordingEndClosed)
		t.Error("recorded")
	}
	if n := g.repo.Len(); n != 0 || len(g.store.Keys()) != 0 {
		t.Errorf("left %d records, parts %v", n, g.store.Keys())
	}
	if kept, _ := g.tasks.Counts(); kept != 0 {
		t.Errorf("the task counts %d", kept)
	}
	// Past goneFor, its desktops are recorded again (an operation with
	// the id is another).
	g.clock.add(goneFor + time.Second)
	g.svc.repo = g.repo
	r = g.begin(t, false)
	r.End(models.RecordingEndClosed)
}
