package resolver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/composites"
	"github.com/logoscore/logos-core/core/pkg/eventbus"
	"github.com/logoscore/logos-core/core/pkg/minionfiles"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"github.com/qiniu/qmgo"
)

// A composite command's tasks, queued as an operator's are (docs:
// contracts/task-outsourcing): the session's commands, the composite
// task, its subtasks and its own op, each checked by the same code.

type compositeRig struct {
	r       *minionTaskResolver
	tasks   *compositeTasks
	session *models.MinionSession
	parent  *models.MinionTask
	files   *compositeFiles
}

type compositeTasks struct {
	repository.IMinionTaskRepository
	mu      sync.Mutex
	rows    map[uuid.UUID]*models.MinionTask
	inserts int
	sent    []sentOwnOp
}

type sentOwnOp struct {
	op   int
	wire []byte
	own  models.JSONMap
}

func (f *compositeTasks) Insert(_ context.Context, t *models.MinionTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserts++
	c := *t
	f.rows[t.TaskID] = &c
	return nil
}

func (f *compositeTasks) FindByTaskID(_ context.Context, id uuid.UUID) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok {
		return nil, qmgo.ErrNoSuchDocuments
	}
	c := *t
	return &c, nil
}

func (f *compositeTasks) SendOwnOp(_ context.Context, id uuid.UUID, op int, wire []byte, _ *models.MinionTaskUpload, _ []uuid.UUID,
	own models.JSONMap, _ time.Time) (*models.MinionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentOwnOp{op: op, wire: wire, own: own})
	t := f.rows[id]
	t.State, t.Composite.OwnOp = models.MinionTaskQueued, string(protocol.OwnOpOut)
	c := *t
	return &c, nil
}

type compositeSessions struct {
	repository.IMinionSessionRepository
	s *models.MinionSession
}

func (f compositeSessions) FindBySessionID(context.Context, uuid.UUID) (*models.MinionSession, error) {
	s := *f.s
	return &s, nil
}

type compositeBuilds struct{ b *models.Build }

func (f compositeBuilds) FindByBuildID(context.Context, uuid.UUID) (*models.Build, error) {
	b := *f.b
	return &b, nil
}

type compositeSchemas struct{ sc protocol.BuildSchema }

func (f compositeSchemas) LatestBuildSchema(context.Context, string) (*models.ModuleBuildSchema, error) {
	return &models.ModuleBuildSchema{BuildSchema: f.sc}, nil
}

type compositeFiles struct {
	TaskFiles
	byID map[uuid.UUID]*models.MinionFile
}

func (f *compositeFiles) TaskFile(_ context.Context, op uuid.UUID, id string) (*models.MinionFile, error) {
	fid, err := uuid.Parse(id)
	if m, ok := f.byID[fid]; err == nil && ok && m.Kind == models.MinionFileKindTask && m.OperationID == op {
		return m, nil
	}
	return nil, minionfiles.ErrNoTaskFile
}

func (f *compositeFiles) FileByID(_ context.Context, id uuid.UUID) (*models.MinionFile, error) {
	return f.byID[id], nil
}

type nopBus struct{ eventbus.IEventBus }

func (nopBus) Publish(eventbus.Event) {}

func compositeSchema() protocol.BuildSchema {
	path := protocol.FieldSpec{Key: "path", Num: 1, Type: protocol.FieldString}
	return protocol.BuildSchema{SchemaVersion: protocol.BuildSchemaV3, Version: 1,
		Targets: []protocol.BuildTarget{{ID: "t", Format: "exe", Platforms: []string{"linux/amd64"}}},
		Commands: []protocol.BuildCommand{
			{ID: "ls", Op: 1, Params: []protocol.FieldSpec{path}},
			{ID: "put", Op: 2, Params: []protocol.FieldSpec{path, {Key: "file", Num: 2, Type: protocol.FieldFile, Accept: []string{".txt"}}}},
			{ID: "feed", Op: 3, Params: []protocol.FieldSpec{path, {Key: "src", Num: 2, Type: protocol.FieldStream}}},
			{ID: "archive", Op: 40, Composite: true, Uses: []string{"ls", "put", "feed"}, Params: []protocol.FieldSpec{path}},
			{ID: "bundle", Composite: true, Uses: []string{"ls"}, Params: []protocol.FieldSpec{{Key: "path", Type: protocol.FieldString}}},
		}}
}

func newCompositeRig(t *testing.T, built ...string) *compositeRig {
	t.Helper()
	if built == nil {
		built = []string{"ls", "put", "feed", "archive"}
	}
	seen := time.Now()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), BuildID: uuid.New(), CreatedAt: seen, LastSeenAt: &seen}
	g := &compositeRig{session: s, tasks: &compositeTasks{rows: map[uuid.UUID]*models.MinionTask{}},
		files: &compositeFiles{byID: map[uuid.UUID]*models.MinionFile{}}}
	g.r = &minionTaskResolver{tasks: g.tasks, sessions: compositeSessions{s: s},
		builds:  compositeBuilds{b: &models.Build{BuildID: s.BuildID, ModuleName: "acme", Target: "t", Commands: built}},
		schemas: compositeSchemas{sc: compositeSchema()}, bus: nopBus{}, files: g.files}
	g.parent = &models.MinionTask{TaskID: uuid.New(), OperationID: s.OperationID, SessionID: s.SessionID, Command: "archive",
		Args: models.JSONMap{"path": "/x"}, IssuedBy: uuid.New(), State: models.MinionTaskComposing,
		Composite: &models.MinionComposite{OwnOp: string(protocol.OwnOpNotRun)}}
	g.tasks.rows[g.parent.TaskID] = g.parent
	return g
}

func commandIDs(cs []protocol.BuildCommand) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// A session has its build's commands, and each composite with no op of its
// own once the build has what it uses.
func TestSessionCommandsWithComposites(t *testing.T) {
	g := newCompositeRig(t)
	_, cs, err := g.r.commandsOf(context.Background(), g.session)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(commandIDs(cs), ","); got != "ls,put,feed,archive,bundle,help" {
		t.Fatalf("commands: %s", got)
	}
	g = newCompositeRig(t, "put", "feed")
	_, cs, _ = g.r.commandsOf(context.Background(), g.session)
	if ids := commandIDs(cs); slices.Contains(ids, "bundle") || slices.Contains(ids, "archive") {
		t.Fatalf("without ls: %v", ids)
	}
}

// A composite command's task waits in core; with no op of its own, it has
// none to send.
func TestQueueComposite(t *testing.T) {
	g := newCompositeRig(t)
	ctx := context.Background()
	for _, c := range []struct {
		id    string
		ownOp string
	}{{"archive", string(protocol.OwnOpNotRun)}, {"bundle", ""}} {
		cmd, _ := compositeSchema().Command(c.id)
		values := map[string]any{"path": "/x"}
		wire, problems := protocol.TaskArgs(cmd.Params, values)
		if problems != nil {
			t.Fatal(problems)
		}
		task, err := g.r.queue(ctx, g.session, cmd, values, wire, queueing{by: uuid.New(), files: g.r.taskFile})
		if err != nil {
			t.Fatal(err)
		}
		if task.State != models.MinionTaskComposing || task.Composite == nil || task.Composite.OwnOp != c.ownOp || task.Op != cmd.Op {
			t.Fatalf("%s: %s %+v op %d", c.id, task.State, task.Composite, task.Op)
		}
	}
}

func refused(t *testing.T, err error, why string) {
	t.Helper()
	var r *composites.Refusal
	if !errors.As(err, &r) || !strings.Contains(r.Reason, why) {
		t.Fatalf("got %v, want a refusal with %q", err, why)
	}
}

// A subtask is checked as an operator's task: one of the commands its
// composite uses, its args, no stream; it is the composite's, issued by
// whoever queued it, once.
func TestQueueSubtask(t *testing.T) {
	g := newCompositeRig(t)
	ctx := context.Background()
	_, err := g.r.QueueSubtask(ctx, g.parent, models.MinionSubtask{TaskID: uuid.New(), Command: "bundle"})
	refused(t, err, "not one of the commands archive uses")
	_, err = g.r.QueueSubtask(ctx, g.parent, models.MinionSubtask{TaskID: uuid.New(), Command: "ls", Args: models.JSONMap{"path": 5}})
	refused(t, err, "path")
	_, err = g.r.QueueSubtask(ctx, g.parent, models.MinionSubtask{TaskID: uuid.New(), Command: "feed",
		Args: models.JSONMap{"path": "/x", "src": "a"}})
	refused(t, err, "a stream param takes nothing in a subtask")

	sub := models.MinionSubtask{TaskID: uuid.New(), Command: "ls", Args: models.JSONMap{"path": "/"}, Key: "k", As: "list it"}
	task, err := g.r.QueueSubtask(ctx, g.parent, sub)
	if err != nil {
		t.Fatal(err)
	}
	if task.TaskID != sub.TaskID || task.ParentID == nil || *task.ParentID != g.parent.TaskID || task.SubtaskKey != "k" || task.SubtaskAs != "list it" ||
		task.IssuedBy != g.parent.IssuedBy || task.State != models.MinionTaskQueued || task.Op != 1 || len(task.WireArgs) == 0 || task.Composite != nil {
		t.Fatalf("subtask: %+v", task)
	}
	inserts := g.tasks.inserts
	if again, err := g.r.QueueSubtask(ctx, g.parent, sub); err != nil || again.TaskID != sub.TaskID || g.tasks.inserts != inserts {
		t.Fatalf("queued twice: %v, %d inserts", err, g.tasks.inserts-inserts)
	}
}

// A subtask's file param names a file of the operation's, or one a subtask
// of the same composite got; never another task's.
func TestSubtaskFiles(t *testing.T) {
	g := newCompositeRig(t)
	ctx := context.Background()
	sibling := &models.MinionTask{TaskID: uuid.New(), SessionID: g.session.SessionID, ParentID: &g.parent.TaskID, State: models.MinionTaskDone}
	stranger := &models.MinionTask{TaskID: uuid.New(), SessionID: g.session.SessionID, State: models.MinionTaskDone}
	g.tasks.rows[sibling.TaskID], g.tasks.rows[stranger.TaskID] = sibling, stranger
	got := &models.MinionFile{FileID: uuid.New(), TaskID: sibling.TaskID, OperationID: g.session.OperationID, Name: "/tmp/a.txt", Total: 3,
		SHA256: make([]byte, 32), State: models.MinionFileComplete}
	other := &models.MinionFile{FileID: uuid.New(), TaskID: stranger.TaskID, OperationID: g.session.OperationID, Name: "/tmp/b.txt",
		State: models.MinionFileComplete}
	g.files.byID[got.FileID], g.files.byID[other.FileID] = got, other

	task, err := g.r.QueueSubtask(ctx, g.parent, models.MinionSubtask{TaskID: uuid.New(), Command: "put",
		Args: models.JSONMap{"path": "/y", "file": got.FileID.String()}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Upload == nil || task.Upload.FileID != got.FileID {
		t.Fatalf("upload: %+v", task.Upload)
	}
	_, err = g.r.QueueSubtask(ctx, g.parent, models.MinionSubtask{TaskID: uuid.New(), Command: "put",
		Args: models.JSONMap{"path": "/y", "file": other.FileID.String()}})
	refused(t, err, "no such task file")
}

// The own op's args are checked as an operator's; core keeps the args a
// decision gave beside the operator's, and sends it once.
func TestSendOwnOp(t *testing.T) {
	g := newCompositeRig(t)
	ctx := context.Background()
	_, err := g.r.SendOwnOp(ctx, g.parent, map[string]any{"path": 5})
	refused(t, err, "path")
	if _, err := g.r.SendOwnOp(ctx, g.parent, map[string]any{"path": "/y"}); err != nil {
		t.Fatal(err)
	}
	if len(g.tasks.sent) != 1 || g.tasks.sent[0].op != 40 || g.tasks.sent[0].own["path"] != "/y" || len(g.tasks.sent[0].wire) == 0 {
		t.Fatalf("sent: %+v", g.tasks.sent)
	}
	out, _ := g.tasks.FindByTaskID(ctx, g.parent.TaskID)
	if again, err := g.r.SendOwnOp(ctx, out, nil); err != nil || len(g.tasks.sent) != 1 || again.State != models.MinionTaskQueued {
		t.Fatalf("sent twice: %v %d", err, len(g.tasks.sent))
	}
}
