package resolver

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// Factory commands (docs: contracts/factory-commands): listed beside a
// session's build's commands, told apart by help, and queued to wait in core
// for their factory, each checked as an operator's task is.

type fakeFactoryRunner struct {
	queued  []uuid.UUID
	stopped []uuid.UUID
}

func (f *fakeFactoryRunner) Queued(t *models.MinionTask) { f.queued = append(f.queued, t.TaskID) }

func (f *fakeFactoryRunner) StopTask(_ context.Context, t *models.MinionTask) (*models.MinionTask, error) {
	f.stopped = append(f.stopped, t.TaskID)
	c := *t
	c.State = models.MinionTaskCancelled
	return &c, nil
}

type factoryOps struct {
	repository.IOperationRepository
	op models.Operation
}

func (f factoryOps) FindByID(context.Context, uuid.UUID) (models.Operation, error) { return f.op, nil }

var factoryWhois = protocol.FactoryCommand{ID: "whois", Label: "Who holds the address", Params: []protocol.FieldSpec{
	{Key: "address", Type: protocol.FieldString, Label: "Address", Required: true},
	{Key: "depth", Type: protocol.FieldInt, Label: "Depth", Default: float64(2)},
	{Key: "capture", Type: protocol.FieldFile, Label: "Capture", Accept: []string{".pcap"}},
	{Key: "login", Type: protocol.FieldCredential, Label: "Login"},
}}

func factorySchema() protocol.BuildSchema {
	sc := compositeSchema()
	// rehello is core's own, which the protocol does not reserve: core's
	// stays the session's.
	sc.FactoryCommands = []protocol.FactoryCommand{factoryWhois, {ID: "rehello", Label: "Not core's"}}
	return sc
}

func newFactoryRig(t *testing.T) (*compositeRig, *fakeFactoryRunner) {
	t.Helper()
	g := newCompositeRig(t)
	g.r.schemas = compositeSchemas{sc: factorySchema()}
	runner := &fakeFactoryRunner{}
	g.r.factory = runner
	g.r.operationRepo = factoryOps{op: models.Operation{OperationID: g.session.OperationID}}
	return g, runner
}

func factoryAdminCtx() context.Context {
	return gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{UserID: uuid.NewString(), Username: "admin", Roles: []string{"admin"}})
}

// A session has the factory's own commands after its build's, but core's
// own; without what runs them, none.
func TestSessionCommandsWithFactoryCommands(t *testing.T) {
	g, _ := newFactoryRig(t)
	ctx := context.Background()
	sc, cs, err := g.r.commandsOf(ctx, g.session)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(commandIDs(cs), ","); got != "ls,put,feed,archive,bundle,whois,help" {
		t.Fatalf("commands: %s", got)
	}
	if ids := factoryIDs(sc, cs); !slices.Equal(ids, []string{"whois"}) {
		t.Fatalf("factory commands: %v", ids)
	}
	help := cs[len(cs)-1]
	if !slices.ContainsFunc(help.Params[0].Options, func(o protocol.EnumOption) bool { return o.Value == "whois" }) {
		t.Fatal("help does not tell about whois")
	}
	g.r.factory = nil
	if _, cs, _ = g.r.commandsOf(ctx, g.session); slices.Contains(commandIDs(cs), "whois") {
		t.Fatal("listed with nothing to run it")
	}
}

// A factory command's task waits in core with its args as the call carries
// them (defaults filled in), the task file and the credential they name.
func TestQueueFactoryCommand(t *testing.T) {
	g, runner := newFactoryRig(t)
	fileID, credID := uuid.New(), uuid.NewString()
	g.files.byID[fileID] = &models.MinionFile{FileID: fileID, Kind: models.MinionFileKindTask, OperationID: g.session.OperationID,
		Name: "edge.pcap", Total: 10}
	g.r.creds = fakeTaskCreds{rows: map[string]protocol.Credential{credID: {ID: credID, Type: protocol.CredentialTypePlaintext}}}
	m, err := g.r.QueueMinionTask(factoryAdminCtx(), g.session.SessionID.String(), "whois",
		map[string]any{"address": "203.0.113.7", "capture": fileID.String(), "login": credID})
	if err != nil {
		t.Fatal(err)
	}
	if m.State != models.MinionTaskFactory || m.Factory == nil || m.Factory.Module != "acme" || m.Factory.Instance != nil {
		t.Fatalf("queued: %+v %+v", m, m.Factory)
	}
	task := g.tasks.rows[uuid.MustParse(m.ID)]
	f := task.Factory
	switch {
	case task.Op != 0 || task.Args["depth"] != nil || f.Args["depth"] != float64(2) || f.Args["address"] != "203.0.113.7":
		t.Fatalf("args: %+v, the call's %+v", task.Args, f.Args)
	case !slices.Equal(f.Files, []uuid.UUID{fileID}) || len(task.CredentialIDs) != 1 || task.CredentialIDs[0].String() != credID:
		t.Fatalf("files %v, credentials %v", f.Files, task.CredentialIDs)
	case f.OfferAt == nil || task.Upload != nil || len(task.WireArgs) > 0:
		t.Fatalf("nothing for a minion: %+v", task)
	case !slices.Equal(runner.queued, []uuid.UUID{task.TaskID}):
		t.Fatalf("runner told: %v", runner.queued)
	}

	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, "address: value is required"},
		{map[string]any{"address": "x", "port": float64(1)}, "port: not a declared field"},
		{map[string]any{"address": "x", "capture": uuid.NewString()}, "capture: no such task file"},
		{map[string]any{"address": "x", "login": uuid.NewString()}, "is gone"},
	} {
		if _, err := g.r.QueueMinionTask(factoryAdminCtx(), g.session.SessionID.String(), "whois", c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%v: err %v, want %q", c.args, err, c.want)
		}
	}
	g.files.byID[fileID].Name = "edge.txt"
	if _, err := g.r.QueueMinionTask(factoryAdminCtx(), g.session.SessionID.String(), "whois",
		map[string]any{"address": "x", "capture": fileID.String()}); err == nil || !strings.Contains(err.Error(), "capture: edge.txt") {
		t.Fatalf("a file the param does not take: %v", err)
	}
}

// An operator's stop of a factory command's task goes to what runs it.
func TestCancelFactoryCommand(t *testing.T) {
	g, runner := newFactoryRig(t)
	m, err := g.r.QueueMinionTask(factoryAdminCtx(), g.session.SessionID.String(), "whois", map[string]any{"address": "x"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := g.r.CancelMinionTask(factoryAdminCtx(), m.ID)
	if err != nil || after.State != models.MinionTaskCancelled || len(runner.stopped) != 1 {
		t.Fatalf("stopped: %+v %v %v", after, err, runner.stopped)
	}
}

// help marks a factory command, and says its factory runs it.
func TestHelpFactoryCommand(t *testing.T) {
	ls := protocol.BuildCommand{ID: "ls", Op: 1, Label: "List a directory"}
	whois := factoryBuildCommand(factoryWhois)
	commands := []protocol.BuildCommand{ls, whois, helpCommand([]protocol.BuildCommand{ls, whois})}
	all := helpText(commands, "", "whois")
	if !strings.Contains(all, "Who holds the address (its factory runs it)\n") || strings.Contains(all, "List a directory (its factory") {
		t.Fatalf("listing:\n%s", all)
	}
	if one := helpText(commands, "whois", "whois"); !strings.Contains(one, "Its factory runs it, not the minion") {
		t.Fatalf("one:\n%s", one)
	}
	if one := helpText(commands, "ls", "whois"); strings.Contains(one, "Its factory runs it") {
		t.Fatalf("a minion's command:\n%s", one)
	}
}

// Factory commands are told from commands by the schema, and show marked,
// with no targets.
func TestFactoryCommandModels(t *testing.T) {
	sc := factorySchema()
	for id, want := range map[string]bool{"whois": true, "ls": false, "rehello": false, "help": false, "nope": false} {
		if _, ok := factoryCommandOf(&sc, id); ok != want {
			t.Fatalf("%s: factory %v", id, ok)
		}
	}
	if _, ok := factoryCommandOf(nil, "whois"); ok {
		t.Fatal("no schema, a factory command")
	}
	m := toBuildSchemaModel(&sc)
	if len(m.FactoryCommands) != 2 || !m.FactoryCommands[0].Factory || len(m.FactoryCommands[0].Targets) != 0 || m.FactoryCommands[0].Op != 0 {
		t.Fatalf("schema's factory commands: %+v", m.FactoryCommands)
	}
	if slices.ContainsFunc(m.Commands, func(c *model.BuildCommand) bool { return c.Factory }) {
		t.Fatal("a command marked factory")
	}
}

// resultFiles are tasks' files in memory, as FilesOfTask lists them, and the
// tasks they were asked for.
type resultFiles struct {
	TaskFiles
	files []models.MinionFile
	asked []uuid.UUID
}

func (f *resultFiles) FilesOfTask(_ context.Context, task uuid.UUID) ([]models.MinionFile, error) {
	f.asked = append(f.asked, task)
	var out []models.MinionFile
	for _, x := range f.files {
		if x.TaskID == task {
			out = append(out, x)
		}
	}
	return out, nil
}

// A factory command's task's files are those its result named, as core
// lists them; another task's are not read (each row of a list would cost a
// query), and are none.
func TestFactoryCommandFiles(t *testing.T) {
	task := uuid.New()
	files := &resultFiles{files: []models.MinionFile{
		{FileID: uuid.New(), TaskID: task, SessionID: uuid.New(), Name: "report.csv", Total: 12, Received: 12,
			State: models.MinionFileComplete, ContentType: "text/csv", SHA256: []byte{1, 2}},
		{FileID: uuid.New(), TaskID: task, Name: "summary.txt", Total: 3, Received: 3, State: models.MinionFileComplete},
		{FileID: uuid.New(), TaskID: uuid.New(), Name: "another task's"},
	}}
	r := &minionTaskResolver{files: files}
	ctx := context.Background()
	factory := &model.MinionTaskFactory{Module: "example"}

	got, err := r.Files(ctx, &model.MinionTask{ID: task.String(), Factory: factory})
	if err != nil || len(got) != 2 || got[0].ID != files.files[0].FileID.String() || got[0].Name != "report.csv" || got[0].Size != 12 ||
		got[0].Sha256 == nil || *got[0].Sha256 != "0102" || got[0].TaskID == nil || *got[0].TaskID != task.String() ||
		got[0].ContentType == nil || *got[0].ContentType != "text/csv" || got[1].Name != "summary.txt" {
		t.Fatalf("a factory command's task's files: %+v %v", got, err)
	}
	files.asked = nil
	if got, err := r.Files(ctx, &model.MinionTask{ID: task.String()}); err != nil || got == nil || len(got) != 0 || len(files.asked) != 0 {
		t.Fatalf("another task's: %+v %v, read for %v", got, err, files.asked)
	}
	if got, err := (&minionTaskResolver{}).Files(ctx, &model.MinionTask{ID: task.String(), Factory: factory}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no files service: %+v %v", got, err)
	}
	if _, err := r.Files(ctx, &model.MinionTask{ID: "x", Factory: factory}); err == nil {
		t.Fatal("a bad task id read")
	}
}
