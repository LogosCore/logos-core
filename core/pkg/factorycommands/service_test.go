package factorycommands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/builds"
	"github.com/logoscore/logos-core/core/pkg/credentialrefs"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A factory command's task, from its offer to its end (docs:
// contracts/factory-commands), against fakes of what core keeps.

type rig struct {
	t        *testing.T
	s        *Service
	tasks    *fakeTasks
	mods     *fakeModules
	rpc      *fakeRPC
	files    *fakeFiles
	bus      *fakeBus
	links    *builds.Links
	session  *models.MinionSession
	build    *models.Build
	buildFID uuid.UUID
	clock    time.Time
}

var whois = protocol.FactoryCommand{ID: "whois", Label: "Who holds the address",
	Params: []protocol.FieldSpec{{Key: "address", Type: protocol.FieldString, Label: "Address"}}}

func factoryModule(instance string, fcs ...protocol.FactoryCommand) models.Module {
	return models.Module{Type: protocol.ServiceFactory, Name: "example", Instance: instance, RPCQueue: "logos.factory.rpc." + instance,
		Build: &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{SchemaVersion: protocol.BuildSchemaV3, Version: 1,
			FactoryCommands: fcs}}}
}

func errUnusable(id uuid.UUID) error {
	return &credentialrefs.Unusable{Why: "credential " + id.String() + " is gone"}
}

func newRig(t *testing.T) *rig {
	t.Helper()
	links, err := builds.NewLinks("secret")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 22, 14, 30, 0, 0, time.UTC)
	seen := now.Add(-time.Minute)
	op, buildID, fid := uuid.New(), uuid.New(), uuid.New()
	b := &models.Build{BuildID: buildID, OperationID: op, ModuleName: "example", Description: "Workstations, EU edge",
		Target: "windows-exe", Platform: "windows/amd64", SchemaVersion: 1, FileIDs: []uuid.UUID{fid}, State: models.BuildStateSucceeded,
		ErrorCodes: []models.BuildErrorCode{{Code: 3, ExitCode: 117, Label: "No connection it can use"}}, Instance: "example-1",
		FactoryVersion: "0.4.2", Request: `{"build_id":"` + buildID.String() + `","target":"windows-exe","values":{"debug":false}}`,
		Artifact:  &models.BuildArtifact{Filename: "WinTelemetry.exe", Size: 3, SHA256: strings.Repeat("ab", 32), ObjectKey: "k"},
		CreatedBy: models.ModuleActor{Type: "user", ID: uuid.NewString(), Name: "admin"}, CreatedAt: now.Add(-2 * time.Hour)}
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: op, Number: 12, BuildID: buildID, Target: "windows-exe",
		Platform: "windows/amd64", Description: "Finance workstation", CreatedAt: now.Add(-time.Hour), LastSeenAt: &seen, Staged: true,
		Hello: models.MinionHello{PID: 4120, OS: "windows", Arch: "amd64", Host: "WS-0042", User: `CORP\alice`, Domain: "CORP",
			Integrity: "medium", Addrs: []string{"10.1.2.3", "fe80::1"}, MachineID: "win-machine-guid:4c4c", X: map[string]any{"mode": "service"}},
		Acting:      &models.MinionIdentity{User: `CORP\svc_backup`, How: "impersonation", Since: seen},
		Address:     "10.1.2.3",
		PeerAddress: "203.0.113.7", Peers: []models.MinionSessionPeer{{Address: "203.0.113.7", FirstSeen: s0(now), LastSeen: seen, Count: 5}},
		Sleep: &models.MinionSessionSleep{Interval: 30000, Jitter: 20}, Conn: 1, ConnKind: protocol.KindReverseCall,
		ConnCount: 1, Crypto: protocol.BuildCrypto{Suite: "aes-256-cbc-hmac-sha256", Material: json.RawMessage(`{"enc_key":"AA==","mac_key":"AA=="}`)}}
	g := &rig{t: t, tasks: newFakeTasks(), mods: &fakeModules{}, rpc: &fakeRPC{}, files: newFakeFiles(), bus: &fakeBus{}, links: links,
		session: s, build: b, buildFID: fid, clock: now}
	bf := fakeBuildFiles{
		files: map[uuid.UUID]models.BuildFile{fid: {FileID: fid, OperationID: op, Name: "app.ico", Size: 4,
			SHA256: strings.Repeat("cd", 32), MediaType: "image/x-icon"}},
		bytes: map[uuid.UUID][]byte{fid: []byte("icon")}, artifacts: map[uuid.UUID][]byte{buildID: []byte("MZ!")},
		builds: fakeBuilds{b: map[uuid.UUID]*models.Build{buildID: b}},
	}
	g.s = New(DefaultConfig(), g.tasks, fakeSessions{s: map[uuid.UUID]*models.MinionSession{s.SessionID: s}}, bf.builds, bf, g.mods, nil,
		fakeCreds{c: map[uuid.UUID]protocol.Credential{}}, g.rpc, links, g.files, g.bus, nil)
	g.s.now = func() time.Time { return g.clock }
	return g
}

func s0(now time.Time) time.Time { return now.Add(-50 * time.Minute) }

// queue keeps a task of whois as the resolver queues one: waiting, its offer
// due now.
func (g *rig) queue(args map[string]any) *models.MinionTask {
	g.t.Helper()
	now := g.clock
	t := &models.MinionTask{TaskID: uuid.New(), OperationID: g.session.OperationID, SessionID: g.session.SessionID, Command: "whois",
		Args: models.JSONMap(args), IssuedBy: uuid.New(), State: models.MinionTaskFactory, QueuedAt: now,
		Factory: &models.MinionFactoryRun{Module: "example", Args: models.JSONMap(args), OfferAt: &now}}
	g.tasks.put(t)
	return t
}

// running keeps a task that instance runs.
func (g *rig) running(instance string) *models.MinionTask {
	g.t.Helper()
	t := g.queue(map[string]any{"address": ""})
	at := g.clock
	t.Factory.Instance, t.Factory.OfferedAt, t.Factory.AcceptedAt, t.Factory.ReportAt, t.Factory.OfferAt = instance, &at, &at, &at, nil
	g.tasks.put(t)
	return t
}

// accepting answers every factory.command with an acceptance.
func accepting(_, opType string, payload any) (protocol.ReplyEnvelope, error) {
	if opType != protocol.TypeFactoryCommand {
		return okReply(map[string]any{"cancelled": true}), nil
	}
	req := payload.(protocol.FactoryCommandRequest)
	return okReply(protocol.FactoryCommandReply{TaskID: req.TaskID, Accepted: true}), nil
}

func (g *rig) envelope(instance string, payload any) messaging.Envelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		g.t.Fatal(err)
	}
	return messaging.Envelope{Source: protocol.Source{Service: protocol.ServiceFactory, Instance: instance}, Payload: raw}
}

// checkLink reports whether link is one of core's, signed for path.
func (g *rig) checkLink(link, path string) {
	g.t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Path != path {
		g.t.Fatalf("link %q, want one for %s", link, path)
	}
	if err := g.links.Check(u.Path, u.Query().Get("expires"), u.Query().Get("sig"), g.clock); err != nil {
		g.t.Fatalf("link %q: %v", link, err)
	}
}

func isNotFound(err error) bool {
	var re *messaging.RPCError
	return errors.As(err, &re) && re.Code == protocol.CodeNotFound
}

// An instance takes a task: its call carries the session and its build
// whole, and links signed for the task; the task runs there.
func TestOfferTaken(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = accepting
	task := g.queue(map[string]any{"address": "203.0.113.7"})
	g.s.Tick(context.Background())

	calls := g.rpc.callsOf(protocol.TypeFactoryCommand)
	if len(calls) != 1 || calls[0].queue != "logos.factory.rpc.example-1" {
		t.Fatalf("calls: %+v", calls)
	}
	req := calls[0].payload.(protocol.FactoryCommandRequest)
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	switch {
	case req.Command != "whois" || req.Args["address"] != "203.0.113.7" || req.Actor == nil:
		t.Fatalf("task: %+v", req)
	case req.Session.Number != 12 || req.Session.Hello.Host != "WS-0042" || req.Session.Acting == nil || req.Session.Crypto == nil ||
		req.Session.PeerAddress != "203.0.113.7" || len(req.Session.Peers) != 1 || req.Session.Sleep == nil:
		t.Fatalf("session: %+v", req.Session)
	case string(req.Build.Request) != g.build.Request || req.Build.Description != "Workstations, EU edge" ||
		len(req.Build.ErrorCodes) != 1 || req.Build.ErrorCodes[0].ExitCode != 117 || req.Build.CreatedBy.Name != "admin":
		t.Fatalf("build: %+v", req.Build)
	}
	if r, err := req.Build.ReadRequest(); err != nil || r.Target != "windows-exe" {
		t.Fatalf("request: %+v %v", r, err)
	}
	g.checkLink(req.UploadURL, UploadPath(task.TaskID))
	g.checkLink(req.Build.Artifact.URL, ArtifactPath(task.TaskID))
	g.checkLink(req.Build.Files[g.buildFID.String()].URL, BuildFilePath(task.TaskID, g.buildFID))

	after := g.tasks.row(task.TaskID)
	f := after.Factory
	if after.State != models.MinionTaskFactory || f.Instance != "example-1" || f.AcceptedAt == nil || f.ReportAt == nil ||
		after.StartedAt == nil || f.LeaseBy != "" || f.OfferAt != nil {
		t.Fatalf("taken: %+v %+v", after, f)
	}
	if g.bus.count(task.TaskID) == 0 {
		t.Fatal("nothing published")
	}
}

// A busy instance passes the task on; the instance running the fewest
// tasks is offered it first.
func TestOfferBusyFewestFirst(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois), factoryModule("example-2", whois))
	g.rpc.answer = func(queue, opType string, payload any) (protocol.ReplyEnvelope, error) {
		if queue == "logos.factory.rpc.example-1" {
			return errReply(protocol.CodeBusy, "4 at once"), nil
		}
		return accepting(queue, opType, payload)
	}
	task := g.queue(nil)
	g.s.Tick(context.Background())
	if f := g.tasks.row(task.TaskID).Factory; f.Instance != "example-2" || f.AcceptedAt == nil {
		t.Fatalf("busy, then the next: %+v", f)
	}
	if calls := g.rpc.callsOf(protocol.TypeFactoryCommand); len(calls) != 2 {
		t.Fatalf("calls: %d", len(calls))
	}

	// example-2 runs one now: the next goes to example-1 first.
	g.rpc.answer = accepting
	next := g.queue(nil)
	g.s.Tick(context.Background())
	if f := g.tasks.row(next.TaskID).Factory; f.Instance != "example-1" {
		t.Fatalf("fewest first: %+v", f)
	}
}

// No instance takes it: the task waits, saying why (published once), and is
// offered again, until one registers.
func TestOfferWaits(t *testing.T) {
	g := newRig(t)
	task := g.queue(nil)
	g.s.Tick(context.Background())
	after := g.tasks.row(task.TaskID)
	f := after.Factory
	if f.Instance != "" || f.WaitingSince == nil || !strings.Contains(f.Waiting, `no registered "example" factory instance runs whois`) ||
		f.OfferAt == nil || !f.OfferAt.Equal(g.clock.Add(DefaultConfig().Tick)) || f.LeaseBy != "" {
		t.Fatalf("waits: %+v", f)
	}
	if n := g.bus.count(task.TaskID); n != 1 {
		t.Fatalf("published %d", n)
	}
	// Again, for the same reason: no news.
	g.clock = g.clock.Add(3 * time.Second)
	g.s.Tick(context.Background())
	if n := g.bus.count(task.TaskID); n != 1 {
		t.Fatalf("published %d, the same reason again", n)
	}
	// An instance that declares another factory command takes none.
	g.mods.set(factoryModule("example-1", protocol.FactoryCommand{ID: "decode"}))
	g.clock = g.clock.Add(3 * time.Second)
	g.s.Tick(context.Background())
	if len(g.rpc.callsOf(protocol.TypeFactoryCommand)) != 0 {
		t.Fatal("offered to an instance that does not declare it")
	}
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = accepting
	g.clock = g.clock.Add(3 * time.Second)
	g.s.Tick(context.Background())
	if f := g.tasks.row(task.TaskID).Factory; f.Instance != "example-1" || f.Waiting != "" || f.WaitingSince != nil {
		t.Fatalf("taken at last: %+v", f)
	}
}

// Any other answer is a refusal: the task fails with it.
func TestOfferRefused(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = func(string, string, any) (protocol.ReplyEnvelope, error) {
		return errReply(protocol.CodeValidationFailed, "address: not an address"), nil
	}
	task := g.queue(nil)
	g.s.Tick(context.Background())
	after := g.tasks.row(task.TaskID)
	if after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "refused the task: validation_failed: address: not an address") {
		t.Fatalf("refused: %s %q", after.State, after.Err)
	}
}

// Instances that do not answer pass it on: it waits, offered to none.
func TestOfferUnanswered(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois), factoryModule("example-2", whois))
	task := g.queue(nil) // every call times out
	g.s.Tick(context.Background())
	f := g.tasks.row(task.TaskID).Factory
	if f.Instance != "" || f.Waiting != "example-2 did not answer in time" || len(g.rpc.callsOf(protocol.TypeFactoryCommand)) != 2 {
		t.Fatalf("unanswered: %+v", f)
	}
}

// An instance reports before its acceptance reaches core, and the reply is
// lost: the task runs there all the same.
func TestReportBeforeReply(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	task := g.queue(nil)
	g.rpc.answer = func(_, _ string, payload any) (protocol.ReplyEnvelope, error) {
		req := payload.(protocol.FactoryCommandRequest)
		out := protocol.FactoryCommandOutput{TaskID: req.TaskID, Seq: 1, Out: []byte("looking up\n")}
		if _, err := g.s.HandleOutput(context.Background(), g.envelope("example-1", out)); err != nil {
			t.Error(err)
		}
		return protocol.ReplyEnvelope{}, messaging.ErrRPCTimeout
	}
	g.s.Tick(context.Background())
	after := g.tasks.row(task.TaskID)
	if after.Factory.Instance != "example-1" || after.Factory.AcceptedAt == nil || after.OutSize != int64(len("looking up\n")) {
		t.Fatalf("runs there: %+v %+v", after, after.Factory)
	}
}

// Output reports are kept in order, once each; a seq that skips one leaves
// a part missing; a report of another instance's task is not_found.
func TestOutput(t *testing.T) {
	g := newRig(t)
	task := g.running("example-1")
	report := func(instance string, seq int64, out string) error {
		_, err := g.s.HandleOutput(context.Background(), g.envelope(instance, protocol.FactoryCommandOutput{TaskID: task.TaskID.String(),
			Seq: seq, Out: []byte(out)}))
		return err
	}
	for _, r := range []struct {
		seq int64
		out string
	}{{1, "a"}, {2, "b"}, {2, "b"}, {1, "a"}, {4, "d"}} {
		if err := report("example-1", r.seq, r.out); err != nil {
			t.Fatal(err)
		}
	}
	g.clock = g.clock.Add(time.Minute)
	if err := report("example-1", 5, ""); err != nil { // still runs
		t.Fatal(err)
	}
	after := g.tasks.row(task.TaskID)
	parts := g.tasks.partsOf(task.TaskID)
	if after.Parts != 3 || after.OutSize != 3 || len(parts) != 3 || parts[2].Seq != 3 || after.Factory.Seq != 5 ||
		!after.Factory.ReportAt.Equal(g.clock) {
		t.Fatalf("kept: parts %d, size %d, seq %d, %+v", after.Parts, after.OutSize, after.Factory.Seq, parts)
	}
	if err := report("example-2", 6, "x"); !isNotFound(err) {
		t.Fatalf("another instance's: %v", err)
	}
	if _, err := g.s.HandleOutput(context.Background(), g.envelope("example-1", protocol.FactoryCommandOutput{TaskID: "x", Seq: 1})); err == nil {
		t.Fatal("an invalid report taken")
	}
}

// A result ends the task: its data inline (kept as CBOR, the done part), its
// files the task's own; the same result again is answered the same way.
func TestResult(t *testing.T) {
	g := newRig(t)
	task := g.running("example-1")
	taskFile := uuid.New()
	tk := g.tasks.row(task.TaskID)
	tk.Factory.Files = []uuid.UUID{taskFile}
	g.tasks.put(tk)
	put, err := g.s.PutFile(context.Background(), task.TaskID, "report.txt", strings.NewReader("all clear"), 9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.s.HandleOutput(context.Background(), g.envelope("example-1", protocol.FactoryCommandOutput{TaskID: task.TaskID.String(),
		Seq: 1, Out: []byte("looking up\n")})); err != nil {
		t.Fatal(err)
	}
	res := protocol.FactoryCommandResult{TaskID: task.TaskID.String(), State: protocol.EndDone, OK: true,
		Data: json.RawMessage(`{"address":"203.0.113.7","holder":"Example Net","hops":3}`), Files: []string{put.ID}}
	for range 2 {
		rep, err := g.s.HandleResult(context.Background(), g.envelope("example-1", res))
		if err != nil || !rep.(protocol.FactoryCommandResultReply).Recorded {
			t.Fatalf("result: %+v %v", rep, err)
		}
	}
	after := g.tasks.row(task.TaskID)
	parts := g.tasks.partsOf(task.TaskID)
	switch {
	case after.State != models.MinionTaskDone || after.OK == nil || !*after.OK || after.Factory.ResultAt == nil:
		t.Fatalf("ended: %+v", after)
	case after.DoneSeq == nil || *after.DoneSeq != 1 || len(parts) != 2 || len(parts[1].DataCBOR) == 0 || after.DataProblem != "":
		t.Fatalf("data: done %v, parts %+v, problem %q", after.DoneSeq, parts, after.DataProblem)
	}
	var data map[string]any
	if err := protocol.UnmarshalCBOR(parts[1].DataCBOR, &data); err != nil || data["holder"] != "Example Net" {
		t.Fatalf("data as CBOR: %v %v", data, err)
	}
	f, _ := g.files.FileByID(context.Background(), uuid.MustParse(put.ID))
	if f.Kind != "" || f.TaskID != task.TaskID || f.ForTask != uuid.Nil {
		t.Fatalf("its file, the task's own: %+v", f)
	}
	if len(g.files.released) != 1 || g.files.released[0] != taskFile {
		t.Fatalf("task files released: %v", g.files.released)
	}
	// Another instance's word on it, and a result after it ended.
	if _, err := g.s.HandleResult(context.Background(), g.envelope("example-2", res)); !isNotFound(err) {
		t.Fatalf("another instance's: %v", err)
	}
}

// Data a result names as a file it put is taken, checked against the
// command's result, and the file dropped; a file it did not put fails the
// task; failed carries why.
func TestResultData(t *testing.T) {
	g := newRig(t)
	task := g.running("example-1")
	tk := g.tasks.row(task.TaskID)
	tk.Result = protocol.ResultDirListing
	g.tasks.put(tk)
	put, err := g.s.PutFile(context.Background(), task.TaskID, "data.json", strings.NewReader(`{"entries":"no list"}`), 21)
	if err != nil {
		t.Fatal(err)
	}
	res := protocol.FactoryCommandResult{TaskID: task.TaskID.String(), State: protocol.EndDone, OK: true, DataFile: put.ID}
	if _, err := g.s.HandleResult(context.Background(), g.envelope("example-1", res)); err != nil {
		t.Fatal(err)
	}
	after := g.tasks.row(task.TaskID)
	if after.State != models.MinionTaskDone || after.DataProblem == "" || len(g.files.dropped) != 1 || g.files.dropped[0].String() != put.ID {
		t.Fatalf("data file: %s %q dropped %v", after.State, after.DataProblem, g.files.dropped)
	}

	bad := g.running("example-1")
	res = protocol.FactoryCommandResult{TaskID: bad.TaskID.String(), State: protocol.EndDone, OK: true, DataFile: uuid.NewString()}
	if _, err := g.s.HandleResult(context.Background(), g.envelope("example-1", res)); err != nil {
		t.Fatal(err)
	}
	if after := g.tasks.row(bad.TaskID); after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "data_file: not a file") {
		t.Fatalf("a file not put: %s %q", after.State, after.Err)
	}

	files := g.running("example-1")
	res = protocol.FactoryCommandResult{TaskID: files.TaskID.String(), State: protocol.EndDone, OK: true, Files: []string{uuid.NewString()}}
	if _, err := g.s.HandleResult(context.Background(), g.envelope("example-1", res)); err != nil {
		t.Fatal(err)
	}
	if after := g.tasks.row(files.TaskID); after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "files: ") {
		t.Fatalf("files not put: %s %q", after.State, after.Err)
	}

	failed := g.running("example-1")
	res = protocol.FactoryCommandResult{TaskID: failed.TaskID.String(), State: protocol.EndFailed, Err: "the registry cannot be reached"}
	if _, err := g.s.HandleResult(context.Background(), g.envelope("example-1", res)); err != nil {
		t.Fatal(err)
	}
	if after := g.tasks.row(failed.TaskID); after.State != models.MinionTaskFailed || after.Err != "the registry cannot be reached" ||
		after.Factory.ResultAt == nil {
		t.Fatalf("failed: %s %q", after.State, after.Err)
	}

	notOK := g.running("example-1")
	exit := int64(2)
	res = protocol.FactoryCommandResult{TaskID: notOK.TaskID.String(), State: protocol.EndDone, Err: "no such address", Exit: &exit}
	if _, err := g.s.HandleResult(context.Background(), g.envelope("example-1", res)); err != nil {
		t.Fatal(err)
	}
	if after := g.tasks.row(notOK.TaskID); after.State != models.MinionTaskFailed || after.Exit == nil || *after.Exit != 2 {
		t.Fatalf("done, not ok: %s %v", after.State, after.Exit)
	}
}

// A stop ends the task cancelled at once, and its instance hears it; one
// waiting for an instance has none to tell.
func TestStop(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = accepting
	task := g.running("example-1")
	after, err := g.s.StopTask(context.Background(), g.tasks.row(task.TaskID))
	if err != nil || after.State != models.MinionTaskCancelled {
		t.Fatalf("stopped: %+v %v", after, err)
	}
	waiting := g.queue(nil)
	if after, err := g.s.StopTask(context.Background(), g.tasks.row(waiting.TaskID)); err != nil || after.State != models.MinionTaskCancelled {
		t.Fatalf("stopped while it waited: %+v %v", after, err)
	}
	g.s.Stop() // waits for the cancels on their way
	cancels := g.rpc.callsOf(protocol.TypeFactoryCommandCancel)
	if len(cancels) != 1 || cancels[0].queue != "logos.factory.rpc.example-1" ||
		cancels[0].payload.(protocol.FactoryCommandCancelRequest).TaskID != task.TaskID.String() {
		t.Fatalf("cancels: %+v", cancels)
	}
	if _, err := g.s.HandleOutput(context.Background(), g.envelope("example-1", protocol.FactoryCommandOutput{TaskID: task.TaskID.String(),
		Seq: 1, Out: []byte("x")})); !isNotFound(err) {
		t.Fatalf("output after the stop: %v", err)
	}
	if _, err := g.s.StopTask(context.Background(), g.tasks.row(task.TaskID)); !errors.Is(err, ErrNotInFactory) {
		t.Fatalf("a stop of an ended task: %v", err)
	}
}

// Core gives up on a task whose instance is gone, or silent too long (and
// tells it to stop); one offered by a core that stopped is offered again.
func TestExpire(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = accepting
	gone := g.running("example-9")
	silent := g.running("example-1")
	tk := g.tasks.row(silent.TaskID)
	long := g.clock.Add(-protocol.FactoryCommandNoReport - time.Second)
	tk.Factory.ReportAt = &long
	g.tasks.put(tk)
	offered := g.queue(nil)
	tk = g.tasks.row(offered.TaskID)
	ago := g.clock.Add(-time.Minute)
	tk.Factory.Instance, tk.Factory.OfferedAt, tk.Factory.OfferAt = "example-1", &ago, nil
	g.tasks.put(tk)
	fresh := g.running("example-1")

	g.s.expire(context.Background())
	if after := g.tasks.row(gone.TaskID); after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "example-9 stopped") {
		t.Fatalf("instance gone: %s %q", after.State, after.Err)
	}
	if after := g.tasks.row(silent.TaskID); after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "no report for 10m0s") {
		t.Fatalf("silent: %s %q", after.State, after.Err)
	}
	if after := g.tasks.row(offered.TaskID); after.Factory.Instance != "" || after.State != models.MinionTaskFactory {
		t.Fatalf("offered, unanswered: %+v", after.Factory)
	}
	if after := g.tasks.row(fresh.TaskID); after.State != models.MinionTaskFactory {
		t.Fatalf("one that reports: %s", after.State)
	}
	g.s.Stop()
	if cancels := g.rpc.callsOf(protocol.TypeFactoryCommandCancel); len(cancels) != 1 ||
		cancels[0].payload.(protocol.FactoryCommandCancelRequest).TaskID != silent.TaskID.String() {
		t.Fatalf("cancels: %+v", cancels)
	}
}

// A task whose session's build is gone, or keeps no request, can never run;
// one whose credential is gone either.
func TestOfferCannotRun(t *testing.T) {
	g := newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = accepting
	g.build.Request = ""
	task := g.queue(nil)
	g.s.Tick(context.Background())
	if after := g.tasks.row(task.TaskID); after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "no request kept") {
		t.Fatalf("no request: %s %q", after.State, after.Err)
	}

	g = newRig(t)
	g.mods.set(factoryModule("example-1", whois))
	g.rpc.answer = accepting
	task = g.queue(nil)
	tk := g.tasks.row(task.TaskID)
	tk.CredentialIDs = []uuid.UUID{uuid.New()}
	g.tasks.put(tk)
	g.s.Tick(context.Background())
	if after := g.tasks.row(task.TaskID); after.State != models.MinionTaskFailed || !strings.Contains(after.Err, "is gone") {
		t.Fatalf("credential gone: %s %q", after.State, after.Err)
	}
	if len(g.rpc.callsOf(protocol.TypeFactoryCommand)) != 0 {
		t.Fatal("offered all the same")
	}
}

// The links a call carries reach the task's files and its build's, while it
// runs.
func TestTransfers(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	task := g.running("example-1")
	argFile := &models.MinionFile{FileID: uuid.New(), Kind: models.MinionFileKindTask, OperationID: g.session.OperationID,
		Name: "edge.pcap", Total: 4, State: models.MinionFileComplete}
	other := &models.MinionFile{FileID: uuid.New(), Kind: models.MinionFileKindTask, OperationID: g.session.OperationID,
		Name: "other", Total: 1, State: models.MinionFileComplete}
	g.files.add(argFile, []byte("pcap"))
	g.files.add(other, []byte("x"))
	tk := g.tasks.row(task.TaskID)
	tk.Factory.Files = []uuid.UUID{argFile.FileID}
	g.tasks.put(tk)

	read := func(r io.ReadCloser, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		b, _ := io.ReadAll(r)
		return string(b)
	}
	r, _, err := g.s.File(ctx, task.TaskID, argFile.FileID)
	if got := read(r, err); got != "pcap" {
		t.Fatalf("its arg's file: %q", got)
	}
	if _, _, err := g.s.File(ctx, task.TaskID, other.FileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a file it does not name: %v", err)
	}
	put, err := g.s.PutFile(ctx, task.TaskID, "notes.txt", bytes.NewReader([]byte("noted")), 5)
	if err != nil || put.Size != 5 {
		t.Fatalf("put: %+v %v", put, err)
	}
	r, _, err = g.s.File(ctx, task.TaskID, uuid.MustParse(put.ID))
	if got := read(r, err); got != "noted" {
		t.Fatalf("a file it put: %q", got)
	}
	r, _, err = g.s.BuildFile(ctx, task.TaskID, g.buildFID)
	if got := read(r, err); got != "icon" {
		t.Fatalf("its build's file: %q", got)
	}
	if _, _, err := g.s.BuildFile(ctx, task.TaskID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not its build's: %v", err)
	}
	r, a, err := g.s.Artifact(ctx, task.TaskID)
	if got := read(r, err); got != "MZ!" || a.Filename != "WinTelemetry.exe" {
		t.Fatalf("its build's artifact: %q %+v", got, a)
	}
	if _, err := g.s.PutFile(ctx, task.TaskID, "a/b", strings.NewReader(""), 0); !errors.Is(err, ErrNoFileName) {
		t.Fatalf("a path for a name: %v", err)
	}
	if _, err := g.s.PutFile(ctx, task.TaskID, "big", strings.NewReader(""), DefaultConfig().MaxFileSize+1); err == nil {
		t.Fatal("a file over the limit taken")
	}

	waiting := g.queue(nil)
	if _, _, err := g.s.Artifact(ctx, waiting.TaskID); !errors.Is(err, ErrTaskEnded) {
		t.Fatalf("a task no instance took: %v", err)
	}
	if _, err := g.s.StopTask(ctx, g.tasks.row(task.TaskID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.s.File(ctx, task.TaskID, argFile.FileID); !errors.Is(err, ErrTaskEnded) {
		t.Fatalf("after its end: %v", err)
	}
	if _, _, err := g.s.File(ctx, uuid.New(), argFile.FileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no such task: %v", err)
	}
	if ok, err := g.s.IsFactoryTask(ctx, task.TaskID); err != nil || !ok {
		t.Fatalf("a factory task: %v %v", ok, err)
	}
	g.s.Stop()
}
