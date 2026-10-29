package builds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// --- fixtures ------------------------------------------------------------

var (
	opID  = uuid.MustParse("6b1e2f3a-4c5d-4e6f-8a9b-0c1d2e3f4a5b")
	alice = models.ModuleActor{Type: "user", ID: "u-alice", Name: "alice"}
)

const bobQueue = "logos.factory.rpc.bob-1"

// bobSchema is BOB's build schema.
func bobSchema() protocol.BuildSchema {
	return protocol.BuildSchema{
		SchemaVersion: protocol.BuildSchemaV1, Version: 1,
		Channels: []protocol.BuildChannel{{
			ChannelVersions: protocol.ChannelVersions{ModuleName: "http", Versions: []int{2}},
			Fields: []protocol.FieldSpec{
				{Key: "method", Type: protocol.FieldEnum, Default: "POST", Options: []protocol.EnumOption{{Value: "GET"}, {Value: "POST"}}},
				{Key: "path_prefix", Type: protocol.FieldString, Default: "/", Pattern: `^/`},
			},
		}},
		Fields: []protocol.FieldSpec{
			{Key: "greeting", Type: protocol.FieldString, Default: "hello from BOB"},
			{Key: "kill_date", Type: protocol.FieldDatetime},
			{Key: "punches", Type: protocol.FieldEnumList, Options: []protocol.EnumOption{{Value: "jab"}, {Value: "hook"}}},
			{Key: "badge", Type: protocol.FieldFile, Accept: []string{"image/*"}, MaxSize: 1024},
		},
		Targets: []protocol.BuildTarget{
			{ID: "dummy", Format: "json", Platforms: []string{"any/any"}},
			{ID: "dummy-lite", Format: "json", Platforms: []string{"any/any"}},
		},
		Commands: []protocol.BuildCommand{
			{ID: "sleep", Required: true},
			{ID: "exit", Required: true},
			{ID: "hello", Default: true},
			{ID: "ls", Default: true},
			{ID: "download"},
			{ID: "shell", Targets: []string{"dummy"}},
		},
	}
}

func bobInstance(name string, version int) models.Module {
	sc := bobSchema()
	sc.Version = version
	return models.Module{Type: protocol.ServiceFactory, Name: "bob", Instance: name, Version: "dev",
		RPCQueue: "logos.factory.rpc." + name, Status: models.ModuleStatusRegistered, Build: &models.ModuleBuildSchema{BuildSchema: sc}}
}

// httpSchema is the HTTP channel's connection schema, which BOB's minions
// read.
func httpSchema() *protocol.ConnectionSchema {
	return &protocol.ConnectionSchema{SchemaVersion: "1", Version: 2, Fields: []protocol.FieldSpec{
		{Key: "base_url", Type: protocol.FieldURL, Required: true},
		{Key: "request_timeout", Type: protocol.FieldDuration, Default: "40s"},
	}}
}

type rig struct {
	svc     *Service
	reg     *fakeRegistry
	chs     *fakeChannels
	rpc     *fakeCaller
	store   *fakeStore
	files   *fakeFiles
	builds  *fakeBuilds
	minions *fakeMinions
	clock   time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	sc := bobSchema()
	dns := &protocol.ConnectionSchema{SchemaVersion: "1", Version: 1, Fields: []protocol.FieldSpec{{Key: "zone", Type: protocol.FieldString}}}
	r := &rig{
		reg: &fakeRegistry{schemas: map[string]*models.ModuleBuildSchema{"bob": {BuildSchema: sc}}},
		// BOB's minions do not speak dns.
		chs:     &fakeChannels{schemas: map[string]*protocol.ConnectionSchema{"http": httpSchema(), "dns": dns}},
		rpc:     &fakeCaller{},
		store:   &fakeStore{objects: map[string][]byte{}},
		files:   &fakeFiles{rows: map[uuid.UUID]models.BuildFile{}},
		builds:  &fakeBuilds{rows: map[uuid.UUID]*models.Build{}},
		minions: &fakeMinions{rows: map[uuid.UUID]models.Minion{}},
		clock:   time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC),
	}
	r.reg.setActive(bobInstance("bob-1", 1))
	r.svc = NewService(Repos{Files: r.files, Builds: r.builds, Minions: r.minions},
		r.reg, r.chs, r.rpc, r.store, fakeSigner{}, Config{}, zap.NewNop())
	r.svc.now = func() time.Time { return r.clock }
	t.Cleanup(r.svc.Stop)
	return r
}

func (r *rig) input(mutate func(in *BuildInput)) BuildInput {
	in := BuildInput{
		Description: " A dummy for the EU front's dry run ",
		ModuleName:  "bob", Target: "dummy", Platform: "any/any",
		Values: map[string]any{"punches": []any{"jab"}},
		Connections: []ConnectionInput{{
			ModuleName: "http", Name: "edge", Description: " EU front, first stage ",
			Settings: map[string]any{"base_url": "https://edge.example.com"}, Values: map[string]any{"path_prefix": "/cdn/"},
		}},
		Actor: alice,
	}
	if mutate != nil {
		mutate(&in)
	}
	return in
}

func (r *rig) upload(t *testing.T, name, mediaType, content string) *models.BuildFile {
	t.Helper()
	f, err := r.svc.UploadFile(context.Background(), opID, name, mediaType, strings.NewReader(content), int64(len(content)), alice)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (r *rig) queue(t *testing.T, mutate func(in *BuildInput)) *models.Build {
	t.Helper()
	b, err := r.svc.CreateBuild(context.Background(), opID, r.input(mutate))
	if err != nil {
		t.Fatalf("CreateBuild: %v %v", err, problemsOf(err))
	}
	return b
}

// started queues a build and lets bob-1 take it.
func (r *rig) started(t *testing.T) *models.Build {
	t.Helper()
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	return r.get(t, b.BuildID.String(), models.BuildStateBuilding)
}

func (r *rig) get(t *testing.T, id, wantState string) *models.Build {
	t.Helper()
	b, err := r.svc.Build(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if wantState != "" && b.State != wantState {
		t.Fatalf("state %s, want %s (error %+v)", b.State, wantState, b.Error)
	}
	return b
}

// envelope is a factory's call to core from instance.
func envelope(t *testing.T, instance, op string, payload any) messaging.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return messaging.Envelope{Type: op, Version: protocol.EnvelopeVersionV1,
		Source: protocol.Source{Service: protocol.ServiceFactory, Instance: instance}, Payload: raw}
}

func problemsOf(err error) map[string]string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Problems
	}
	return nil
}

func rpcCode(err error) string {
	var e *messaging.RPCError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// --- creating builds -----------------------------------------------------

func TestCreateBuildQueues(t *testing.T) {
	r := newRig(t)
	badge := r.upload(t, "badge.png", "image/png", "png!")
	b := r.queue(t, func(in *BuildInput) { in.Values["badge"] = badge.FileID.String() })

	if b.State != models.BuildStateQueued || b.SchemaVersion != 1 || b.Deadline != r.clock.Add(30*time.Minute) ||
		b.CreatedBy != alice || b.BuildID.Version() != 4 {
		t.Fatalf("build %+v", b)
	}
	// Defaults filled in, as a factory gets them.
	if b.Values["greeting"] != "hello from BOB" || b.Values["badge"] != badge.FileID.String() {
		t.Fatalf("values %v", b.Values)
	}
	if len(b.FileIDs) != 1 || b.FileIDs[0] != badge.FileID {
		t.Fatalf("file ids %v", b.FileIDs)
	}
	// Each connection's settings as an endpoint export, defaults filled in.
	conn := b.Connections.Items[0]
	if conn.Endpoint.Format != protocol.ChannelEndpointExportV1 || conn.Endpoint.Name != "edge" ||
		conn.Endpoint.Channel != (protocol.ExportChannel{ModuleName: "http", SchemaVersion: 2}) ||
		conn.Endpoint.Params["base_url"] != "https://edge.example.com" || conn.Endpoint.Params["request_timeout"] != "40s" ||
		conn.Values["method"] != "POST" || conn.Values["path_prefix"] != "/cdn/" {
		t.Fatalf("connection %+v", conn)
	}
	// The notes stay core's.
	if len(b.ConnectionDescriptions) != 1 || b.ConnectionDescriptions[0] != "EU front, first stage" {
		t.Fatalf("descriptions %q", b.ConnectionDescriptions)
	}
	if b.Description != "A dummy for the EU front's dry run" {
		t.Fatalf("description %q", b.Description)
	}
	// None picked: the target's default and required ones.
	if got := strings.Join(b.Commands, " "); got != "sleep exit hello ls" {
		t.Fatalf("commands %s", got)
	}
}

func TestCreateBuildCommands(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct {
		target string
		picked []string
		want   string
	}{
		// The required ones are added, and all of them sorted as the
		// factory lists them.
		{"dummy", []string{"shell", "hello"}, "sleep exit hello shell"},
		{"dummy", []string{"exit", "download"}, "sleep exit download"},
		// An empty pick is no commands but the required ones; none named
		// is the defaults.
		{"dummy", []string{}, "sleep exit"},
		{"dummy-lite", nil, "sleep exit hello ls"},
	} {
		b := r.queue(t, func(in *BuildInput) { in.Target, in.Commands = c.target, c.picked })
		if got := strings.Join(b.Commands, " "); got != c.want {
			t.Errorf("%s %v: commands %s, want %s", c.target, c.picked, got, c.want)
		}
	}
}

func TestCreateBuildConnectionDescriptions(t *testing.T) {
	r := newRig(t)
	if b := r.queue(t, func(in *BuildInput) { in.Connections[0].Description = " " }); b.ConnectionDescriptions != nil {
		t.Fatalf("no notes kept as %q", b.ConnectionDescriptions)
	}
	// Characters, not bytes, are counted.
	long := strings.Repeat("é", MaxDescriptionLength)
	if b := r.queue(t, func(in *BuildInput) { in.Connections[0].Description = long }); b.ConnectionDescriptions[0] != long {
		t.Fatal("a note of the longest length was cut")
	}
	if b := r.queue(t, func(in *BuildInput) { in.Description = long }); b.Description != long {
		t.Fatal("a build description of the longest length was cut")
	}
}

func TestCreateBuildConnectionNames(t *testing.T) {
	r := newRig(t)
	b := r.queue(t, func(in *BuildInput) {
		in.Connections[0].Name = " "
		in.Connections = append(in.Connections, ConnectionInput{ModuleName: " http ", Name: " backup ",
			Settings: map[string]any{"base_url": "https://backup.example.com", "request_timeout": ""}})
	})
	var names []string
	for _, c := range b.Connections.Items {
		names = append(names, c.Endpoint.Name+"@"+c.Endpoint.Channel.ModuleName)
	}
	if strings.Join(names, " ") != "http-1@http backup@http" {
		t.Fatalf("connections named %v", names)
	}
	if d := b.ConnectionDescriptions; len(d) != 2 || d[0] == "" || d[1] != "" {
		t.Fatalf("descriptions by position %q", d)
	}
	// An emptied setting takes its default.
	if p := b.Connections.Items[1].Endpoint.Params; p["request_timeout"] != "40s" {
		t.Fatalf("params %v", p)
	}
}

func TestCreateBuildProblems(t *testing.T) {
	r := newRig(t)
	text := r.upload(t, "notes.txt", "text/plain", "hi")
	for _, c := range []struct {
		key    string
		mutate func(in *BuildInput)
	}{
		{"description", func(in *BuildInput) { in.Description = " " }},
		{"description", func(in *BuildInput) { in.Description = strings.Repeat("é", MaxDescriptionLength+1) }},
		{"target", func(in *BuildInput) { in.Target = "windows-exe" }},
		{"platform", func(in *BuildInput) { in.Platform = "" }},
		{"platform", func(in *BuildInput) { in.Platform = "linux/amd64" }},
		{"values.punches", func(in *BuildInput) { in.Values["punches"] = []any{"uppercut"} }},
		{"values.kill_date", func(in *BuildInput) { in.Values["kill_date"] = "tomorrow" }},
		{"values.color", func(in *BuildInput) { in.Values["color"] = "red" }},
		{"values.badge", func(in *BuildInput) { in.Values["badge"] = uuid.NewString() }},
		{"values.badge", func(in *BuildInput) { in.Values["badge"] = text.FileID.String() }}, // not an image
		{"commands[1]", func(in *BuildInput) { in.Commands = []string{"hello", "keylog"} }},
		{"commands[1]", func(in *BuildInput) { in.Commands = []string{"hello", "hello"} }},
		{"commands[0]", func(in *BuildInput) { in.Target, in.Commands = "dummy-lite", []string{"shell"} }}, // the dummy's only
		{"connections", func(in *BuildInput) { in.Connections = nil }},
		{"connections[0]", func(in *BuildInput) { in.Connections[0].ModuleName = "" }},
		{"connections[0]", func(in *BuildInput) { in.Connections[0].ModuleName = "smtp" }}, // no such channel
		{"connections[0]", func(in *BuildInput) { in.Connections[0].ModuleName = "dns" }},  // not BOB's
		{"connections[0].name", func(in *BuildInput) { in.Connections[0].Name = "Edge Node" }},
		{"connections[0].description", func(in *BuildInput) {
			in.Connections[0].Description = strings.Repeat("é", MaxDescriptionLength+1)
		}},
		{"connections[1].name", func(in *BuildInput) { in.Connections = append(in.Connections, in.Connections[0]) }},
		{"connections[1].name", func(in *BuildInput) { // the second one's default name
			in.Connections[0].Name = "http-2"
			in.Connections = append(in.Connections, ConnectionInput{ModuleName: "http", Settings: in.Connections[0].Settings})
		}},
		{"connections[0].settings.base_url", func(in *BuildInput) { delete(in.Connections[0].Settings, "base_url") }},
		{"connections[0].settings.base_url", func(in *BuildInput) { in.Connections[0].Settings["base_url"] = "edge" }},
		{"connections[0].settings.color", func(in *BuildInput) { in.Connections[0].Settings["color"] = "red" }},
		{"connections[0].values.method", func(in *BuildInput) { in.Connections[0].Values["method"] = "PUT" }},
		{"connections[0].values.color", func(in *BuildInput) { in.Connections[0].Values["color"] = "red" }},
	} {
		_, err := r.svc.CreateBuild(context.Background(), opID, r.input(c.mutate))
		if p := problemsOf(err); p[c.key] == "" {
			t.Errorf("want a problem under %q, got %v (%v)", c.key, p, err)
		}
	}
	if len(r.builds.rows) != 0 {
		t.Fatalf("refused builds were stored: %d", len(r.builds.rows))
	}
}

func TestCreateBuildRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(r *rig)
		in    func(in *BuildInput)
		want  string
	}{
		"unknown factory": {in: func(in *BuildInput) { in.ModuleName = "alice" }, want: `no "alice" factory`},
		"no instance":     {setup: func(r *rig) { r.reg.setActive() }, want: "no running"},
		"old instances only": {setup: func(r *rig) {
			r.reg.schemas["bob"].Version = 2
			r.reg.setActive(bobInstance("bob-1", 1))
		}, want: "schema version 2"},
		"crypto": {setup: func(r *rig) { r.reg.schemas["bob"].Crypto = "x25519-xchacha20poly1305" }, want: "payload crypto"},
		// BOB's minions read version 2 of the HTTP channel's settings.
		"channel version": {setup: func(r *rig) { r.chs.schemas["http"].Version = 3 }, want: "does not fit"},
	} {
		r := newRig(t)
		if c.setup != nil {
			c.setup(r)
		}
		_, err := r.svc.CreateBuild(context.Background(), opID, r.input(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// --- offering builds -----------------------------------------------------

func TestOfferAccepted(t *testing.T) {
	r := newRig(t)
	badge := r.upload(t, "badge.png", "image/png", "png!")
	b := r.queue(t, func(in *BuildInput) { in.Values["badge"] = badge.FileID.String() })
	r.svc.Tick(context.Background())

	got := r.get(t, b.BuildID.String(), models.BuildStateBuilding)
	if got.Instance != "bob-1" || got.FactoryVersion != "dev" || got.StartedAt == nil || got.Deadline != r.clock.Add(30*time.Minute) {
		t.Fatalf("build %+v", got)
	}
	calls := r.rpc.recorded()
	if len(calls) != 1 || calls[0].queue != bobQueue || calls[0].op != protocol.TypeFactoryBuild {
		t.Fatalf("calls %+v", calls)
	}
	req := calls[0].payload.(protocol.BuildRequest)
	if p := protocol.ValidateBuildRequest(bobSchema(), req); p != nil {
		t.Fatalf("the request a factory gets does not pass its check: %v", p)
	}
	if got := strings.Join(req.Commands, " "); got != "sleep exit hello ls" {
		t.Fatalf("the factory got commands %s", got)
	}
	// The operators' notes never reach a factory, nor so an artifact.
	if raw, _ := json.Marshal(req); strings.Contains(string(raw), "first stage") || strings.Contains(string(raw), "dry run") {
		t.Fatalf("a description went to the factory: %s", raw)
	}
	key := artifactKey(opID, b.BuildID)
	if req.Artifact.UploadURL != "http://store.test/"+key+"?sig=put" || req.Artifact.MaxSize != 256<<20 ||
		req.Files[badge.FileID.String()].URL != "http://store.test/"+badge.ObjectKey+"?sig=get" ||
		req.Files[badge.FileID.String()].SHA256 != sha("png!") || req.Actor.ID != "u-alice" {
		t.Fatalf("request %+v", req)
	}
}

func TestOfferWithCrypto(t *testing.T) {
	r := newRig(t)
	r.reg.schemas["bob"].Crypto = protocol.CryptoAES256CBCHMACSHA256
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())

	stored := r.get(t, b.BuildID.String(), models.BuildStateBuilding)
	req := r.rpc.recorded()[0].payload.(protocol.BuildRequest)
	if req.Crypto == nil || req.Crypto.Suite != protocol.CryptoAES256CBCHMACSHA256 ||
		stored.Crypto == nil || string(req.Crypto.Material) != string(stored.Crypto.Material) {
		t.Fatalf("request crypto %+v, stored %+v", req.Crypto, stored.Crypto)
	}
	var m protocol.AES256CBCHMACSHA256Material
	if err := json.Unmarshal(req.Crypto.Material, &m); err != nil || len(m.EncKey) != 32 || len(m.MACKey) != 32 {
		t.Fatalf("material %s: %v", req.Crypto.Material, err)
	}
	// The keys never leave core but to the factory.
	if raw, _ := json.Marshal(stored); strings.Contains(string(raw), "enc_key") {
		t.Fatalf("keys in the build's JSON: %s", raw)
	}
}

func TestOfferPassesBusyAndUnreachable(t *testing.T) {
	r := newRig(t)
	r.reg.setActive(bobInstance("bob-1", 1), bobInstance("bob-2", 1), bobInstance("bob-3", 1))
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) {
		switch c.queue {
		case "logos.factory.rpc.bob-1":
			return refuse(c, protocol.CodeBusy, "4 of 4 builds running"), nil
		case "logos.factory.rpc.bob-2":
			return protocol.ReplyEnvelope{}, errors.New("rpc timeout")
		}
		return accept(c), nil
	}
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	if got := r.get(t, b.BuildID.String(), models.BuildStateBuilding); got.Instance != "bob-3" {
		t.Fatalf("built on %s", got.Instance)
	}
}

func TestOfferWhenNoneTakesIt(t *testing.T) {
	r := newRig(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) { return refuse(c, protocol.CodeBusy, "busy"), nil }
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	if got := r.get(t, b.BuildID.String(), models.BuildStateQueued); got.LeaseUntil != nil || got.Instance != "" || got.StartedAt != nil {
		t.Fatalf("not put back in the queue: %+v", got)
	}

	// Later, the instance has room.
	r.rpc.answer = nil
	r.svc.Tick(context.Background())
	r.get(t, b.BuildID.String(), models.BuildStateBuilding)
}

// An offer turned down puts the build back with its queue deadline: busy
// instances cannot keep a build queued past the build timeout.
func TestBusyOffersKeepTheQueueDeadline(t *testing.T) {
	r := newRig(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) { return refuse(c, protocol.CodeBusy, "busy"), nil }
	b := r.queue(t, nil)

	r.clock = r.clock.Add(20 * time.Minute)
	r.svc.Tick(context.Background())
	if got := r.get(t, b.BuildID.String(), models.BuildStateQueued); !got.Deadline.Equal(b.Deadline) || got.ArtifactMaxSize != 0 {
		t.Fatalf("after a busy offer: deadline %s (queued with %s), artifact limit %d", got.Deadline, b.Deadline, got.ArtifactMaxSize)
	}

	r.clock = r.clock.Add(11 * time.Minute)
	r.svc.Tick(context.Background())
	if got := r.get(t, b.BuildID.String(), models.BuildStateFailed); got.Error.Code != models.BuildErrorTimeout {
		t.Fatalf("error %+v", got.Error)
	}
}

// A factory starts, and reports, as soon as it accepts: its first progress
// can reach core before its acceptance does.
func TestReportBeforeAcceptance(t *testing.T) {
	r := newRig(t)
	var progress error
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) {
		req := c.payload.(protocol.BuildRequest)
		p := protocol.BuildProgress{BuildID: req.BuildID, Step: "badge"}
		_, progress = r.svc.HandleProgress(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildProgress, p))
		return accept(c), nil
	}
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	if progress != nil {
		t.Fatalf("progress sent right after accepting: %v", progress)
	}
	// The report came before the plan: both are kept.
	if got := r.get(t, b.BuildID.String(), models.BuildStateBuilding); len(got.StepReports) != 1 || len(got.Plan) != len(bobPlan) {
		t.Fatalf("reports %+v, plan %+v", got.StepReports, got.Plan)
	}
}

func TestOfferWithoutAValidPlan(t *testing.T) {
	r := newRig(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) {
		req := c.payload.(protocol.BuildRequest)
		rep, _ := protocol.NewReply(protocol.Envelope{Type: c.op, Version: protocol.EnvelopeVersionV1}, protocol.Source{Service: "factory"},
			protocol.BuildReply{BuildID: req.BuildID, Accepted: true, Steps: []protocol.BuildStep{{ID: "Bad Id", Label: "x"}}})
		return rep, nil
	}
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	// Built all the same, shown as one step.
	got := r.get(t, b.BuildID.String(), models.BuildStateBuilding)
	if got.Plan != nil {
		t.Fatalf("plan %+v", got.Plan)
	}
	if st := Pipeline(got); len(st) != 3 || st[1].ID != "build" || st[1].State != StageRunning {
		t.Fatalf("pipeline %+v", st)
	}
}

// The acceptance is lost, but the factory built it and reported: the result
// stands, the build is not offered again.
func TestResultBeforeLostAcceptance(t *testing.T) {
	r := newRig(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) {
		req := c.payload.(protocol.BuildRequest)
		res := protocol.BuildResult{BuildID: req.BuildID, Status: protocol.BuildFailed, Error: &protocol.BuildError{Code: "compile_failed", Message: "x"}}
		if _, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res)); err != nil {
			t.Errorf("result: %v", err)
		}
		return protocol.ReplyEnvelope{}, errors.New("rpc timeout")
	}
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	if got := r.get(t, b.BuildID.String(), models.BuildStateFailed); got.Error.Code != "compile_failed" {
		t.Fatalf("error %+v", got.Error)
	}
	if n := len(r.rpc.recorded()); n != 1 {
		t.Fatalf("offered %d times", n)
	}
}

func TestOfferRefused(t *testing.T) {
	r := newRig(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) {
		return refuse(c, protocol.CodeValidationFailed, "invalid build request: values.greeting: too loud"), nil
	}
	b := r.queue(t, nil)
	r.svc.Tick(context.Background())
	got := r.get(t, b.BuildID.String(), models.BuildStateFailed)
	if got.Error.Code != models.BuildErrorRefused || !strings.Contains(got.Error.Message, "too loud") {
		t.Fatalf("error %+v", got.Error)
	}
}

func TestOfferWithoutInstances(t *testing.T) {
	r := newRig(t)
	b := r.queue(t, nil)

	// Restarting: same schema, no instance for now. The build waits.
	r.reg.setActive()
	r.svc.Tick(context.Background())
	r.get(t, b.BuildID.String(), models.BuildStateQueued)

	// The factory came back with a new schema version: the values no longer
	// follow what it declares.
	r.reg.schemas["bob"].Version = 2
	r.reg.setActive(bobInstance("bob-1", 2))
	r.svc.Tick(context.Background())
	if got := r.get(t, b.BuildID.String(), models.BuildStateFailed); got.Error.Code != models.BuildErrorSchemaChanged {
		t.Fatalf("error %+v", got.Error)
	}
}

// --- progress and results ------------------------------------------------

func TestProgress(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	report := func(step, message string) {
		t.Helper()
		p := protocol.BuildProgress{BuildID: b.BuildID.String(), Step: step, Message: message}
		rep, err := r.svc.HandleProgress(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildProgress, p))
		if err != nil || !rep.(protocol.BuildProgressReply).Ack {
			t.Fatalf("progress %s: %v", step, err)
		}
	}
	first := r.clock
	report("render", "1 of 3")
	r.clock = r.clock.Add(time.Second)
	report("render", "2 of 3") // a step's time is its first report's; its message the latest
	report("upload", "")
	got := r.get(t, b.BuildID.String(), "")
	if rr := got.StepReports["render"]; !rr.StartedAt.Equal(first) || rr.Message != "2 of 3" || len(got.StepReports) != 2 {
		t.Fatalf("reports %+v", got.StepReports)
	}
	// The plan's first step was never reported: it is done all the same.
	st := Pipeline(got)
	if st[1].ID != "badge" || st[1].State != StageSucceeded || st[2].State != StageSucceeded || st[3].State != StageRunning {
		t.Fatalf("pipeline %+v", st)
	}

	p := protocol.BuildProgress{BuildID: b.BuildID.String(), Step: "render"}
	if _, err := r.svc.HandleProgress(context.Background(), envelope(t, "bob-2", protocol.TypeFactoryBuildProgress, p)); rpcCode(err) != protocol.CodeNotFound {
		t.Errorf("another instance's progress: %v", err)
	}
	if _, err := r.svc.CancelBuild(context.Background(), b.BuildID.String(), alice); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.HandleProgress(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildProgress, p)); rpcCode(err) != protocol.CodeNotFound {
		t.Errorf("progress of a cancelled build: %v", err)
	}
	bad := protocol.BuildProgress{BuildID: b.BuildID.String(), Step: "Not A Step"}
	if _, err := r.svc.HandleProgress(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildProgress, bad)); rpcCode(err) != protocol.CodeValidationFailed {
		t.Errorf("malformed progress: %v", err)
	}
}

// succeed has bob-1 upload body and report it as the artifact.
func (r *rig) succeed(t *testing.T, b *models.Build, body string, reported string) (any, error) {
	t.Helper()
	r.store.objects[artifactKey(opID, b.BuildID)] = []byte(body)
	res := protocol.BuildResult{BuildID: b.BuildID.String(), Status: protocol.BuildSucceeded,
		Artifact: &protocol.BuildArtifact{Filename: "bob.json", Size: int64(len(reported)), SHA256: sha(reported), MediaType: "application/json"},
		Warnings: []string{"a dummy"}, Log: "built"}
	return r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res))
}

// settle waits for background artifact checks and cancels.
func (r *rig) settle() { r.svc.wg.Wait() }

func TestSucceededBuild(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	rep, err := r.succeed(t, b, `{"minion":1}`, `{"minion":1}`)
	if err != nil || !rep.(protocol.BuildResultReply).Recorded {
		t.Fatalf("result: %v", err)
	}
	r.settle()
	got := r.get(t, b.BuildID.String(), models.BuildStateSucceeded)
	if got.Artifact.Size != 12 || got.Log != "built" || len(got.Warnings) != 1 || got.FinishedAt == nil {
		t.Fatalf("build %+v", got)
	}
	// The minion can do what its build picked.
	minions, _ := r.svc.Minions(context.Background(), opID)
	if len(minions) != 1 || minions[0].BuildID != b.BuildID || strings.Join(minions[0].Commands, " ") != "sleep exit hello ls" {
		t.Fatalf("minions %+v", minions)
	}

	// The factory missed the reply and sends the result again.
	if rep, err := r.succeed(t, b, `{"minion":1}`, `{"minion":1}`); err != nil || !rep.(protocol.BuildResultReply).Recorded {
		t.Fatalf("repeated result: %v", err)
	}
	rc, _, err := r.svc.OpenArtifact(context.Background(), b.BuildID.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()

	if _, err := r.svc.DeleteArtifact(context.Background(), b.BuildID.String()); err != nil || r.store.has(got.Artifact.ObjectKey) {
		t.Fatalf("delete artifact: %v", err)
	}
	if _, _, err := r.svc.OpenArtifact(context.Background(), b.BuildID.String()); err == nil {
		t.Fatal("a deleted artifact opened")
	}
}

func TestArtifactChecks(t *testing.T) {
	for name, c := range map[string]struct {
		upload, report string
		uploaded       bool
	}{
		"mismatch":     {upload: "tampered", report: "original", uploaded: true},
		"larger":       {upload: "original, and more", report: "original", uploaded: true},
		"not uploaded": {report: "original"},
	} {
		r := newRig(t)
		b := r.started(t)
		var err error
		if c.uploaded {
			_, err = r.succeed(t, b, c.upload, c.report)
		} else {
			res := protocol.BuildResult{BuildID: b.BuildID.String(), Status: protocol.BuildSucceeded,
				Artifact: &protocol.BuildArtifact{Filename: "bob.json", Size: int64(len(c.report)), SHA256: sha(c.report)}}
			_, err = r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res))
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r.settle()
		got := r.get(t, b.BuildID.String(), models.BuildStateFailed)
		if got.Error.Code != models.BuildErrorArtifactMismatch || r.store.has(artifactKey(opID, b.BuildID)) {
			t.Fatalf("%s: error %+v", name, got.Error)
		}
		if minions, _ := r.svc.Minions(context.Background(), opID); len(minions) != 0 {
			t.Fatalf("%s: a minion was created", name)
		}
	}
}

// A presigned upload cannot be capped: a result reporting an artifact over the
// max_size its instance was given fails the build, and the upload is deleted.
func TestArtifactOverItsLimit(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	if got := r.get(t, b.BuildID.String(), ""); got.ArtifactMaxSize != 256<<20 {
		t.Fatalf("the limit the instance was given is not kept: %d", got.ArtifactMaxSize)
	}
	r.builds.rows[b.BuildID].ArtifactMaxSize = 4 // the setting when it was offered

	if rep, err := r.succeed(t, b, "too big", "too big"); err != nil || !rep.(protocol.BuildResultReply).Recorded {
		t.Fatalf("result: %v", err)
	}
	res := protocol.BuildResult{BuildID: b.BuildID.String(), Status: protocol.BuildSucceeded,
		Artifact: &protocol.BuildArtifact{Filename: "bob.json", Size: 7, SHA256: sha("too big")}}
	if rep, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res)); err != nil ||
		!rep.(protocol.BuildResultReply).Recorded {
		t.Fatalf("repeated result: %v", err)
	}
	r.settle()
	got := r.get(t, b.BuildID.String(), models.BuildStateFailed)
	if got.Error.Code != models.BuildErrorArtifactTooLarge || got.Artifact != nil || got.Log != "built" ||
		r.store.has(artifactKey(opID, b.BuildID)) {
		t.Fatalf("build %+v, error %+v", got, got.Error)
	}
	if minions, _ := r.svc.Minions(context.Background(), opID); len(minions) != 0 {
		t.Fatal("a minion was created")
	}

	// A build offered before core kept the limit is held to the setting.
	old := r.started(t)
	r.builds.rows[old.BuildID].ArtifactMaxSize = 0
	if _, err := r.succeed(t, old, "small", "small"); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.get(t, old.BuildID.String(), models.BuildStateSucceeded)
}

func TestFailedBuild(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	res := protocol.BuildResult{BuildID: b.BuildID.String(), Status: protocol.BuildFailed,
		Error: &protocol.BuildError{Code: "compile_failed", Message: "go build exited with status 1"}, Log: "undefined: x"}
	for range 2 { // the second is a repeat
		rep, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res))
		if err != nil || !rep.(protocol.BuildResultReply).Recorded {
			t.Fatalf("result: %v", err)
		}
	}
	got := r.get(t, b.BuildID.String(), models.BuildStateFailed)
	if got.Error.Code != "compile_failed" || got.Log != "undefined: x" {
		t.Fatalf("build %+v", got)
	}
}

func TestLateAndForeignResults(t *testing.T) {
	r := newRig(t)
	b := r.started(t)
	res := protocol.BuildResult{BuildID: b.BuildID.String(), Status: protocol.BuildFailed, Error: &protocol.BuildError{Code: "x", Message: "y"}}
	if _, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-2", protocol.TypeFactoryBuildResult, res)); rpcCode(err) != protocol.CodeNotFound {
		t.Errorf("another instance's result: %v", err)
	}
	r.clock = r.clock.Add(31 * time.Minute)
	r.svc.Tick(context.Background())
	r.get(t, b.BuildID.String(), models.BuildStateFailed)
	if _, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, res)); rpcCode(err) != protocol.CodeNotFound {
		t.Errorf("a result after the deadline: %v", err)
	}
	unknown := res
	unknown.BuildID = protocol.NewID()
	if _, err := r.svc.HandleResult(context.Background(), envelope(t, "bob-1", protocol.TypeFactoryBuildResult, unknown)); rpcCode(err) != protocol.CodeNotFound {
		t.Errorf("an unknown build's result: %v", err)
	}
}

// --- cancel, timeouts, lost factories ------------------------------------

func TestCancel(t *testing.T) {
	r := newRig(t)
	queued := r.queue(t, nil)
	if got, err := r.svc.CancelBuild(context.Background(), queued.BuildID.String(), alice); err != nil || got.State != models.BuildStateCancelled || got.CancelledBy.ID != "u-alice" {
		t.Fatalf("cancel a queued build: %v %+v", err, got)
	}

	running := r.started(t)
	if _, err := r.svc.CancelBuild(context.Background(), running.BuildID.String(), alice); err != nil {
		t.Fatal(err)
	}
	r.settle()
	calls := r.rpc.recorded()
	last := calls[len(calls)-1]
	if last.op != protocol.TypeFactoryBuildCancel || last.queue != bobQueue || last.payload.(protocol.BuildCancelRequest).BuildID != running.BuildID.String() {
		t.Fatalf("cancel call %+v", last)
	}
	if _, err := r.svc.CancelBuild(context.Background(), running.BuildID.String(), alice); err == nil {
		t.Fatal("a finished build was cancelled again")
	}
}

// A build of any state can be deleted: its record, its minion and its
// artifact go, a running one is cancelled on its factory first, and the
// operation's files stay.
func TestDeleteBuild(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	f := r.upload(t, "badge.png", "image/png", "png!")
	done := r.started(t)
	if _, err := r.succeed(t, done, "x", "x"); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if len(r.minions.rows) != 1 || !r.store.has(artifactKey(opID, done.BuildID)) {
		t.Fatal("no minion or artifact to delete")
	}
	if err := r.svc.DeleteBuild(ctx, done.BuildID.String(), alice); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.builds.rows[done.BuildID]; ok || len(r.minions.rows) != 0 || r.store.has(artifactKey(opID, done.BuildID)) {
		t.Fatal("a succeeded build left something behind")
	}

	running := r.started(t)
	if err := r.svc.DeleteBuild(ctx, running.BuildID.String(), alice); err != nil {
		t.Fatal(err)
	}
	r.settle()
	calls := r.rpc.recorded()
	if last := calls[len(calls)-1]; last.op != protocol.TypeFactoryBuildCancel || last.payload.(protocol.BuildCancelRequest).BuildID != running.BuildID.String() {
		t.Fatalf("the running build was not cancelled on its factory: %+v", last)
	}
	queued := r.queue(t, nil)
	if err := r.svc.DeleteBuild(ctx, queued.BuildID.String(), alice); err != nil {
		t.Fatal(err)
	}
	if len(r.builds.rows) != 0 {
		t.Fatalf("builds left: %d", len(r.builds.rows))
	}
	if _, err := r.svc.File(ctx, f.FileID.String()); err != nil || !r.store.has(f.ObjectKey) {
		t.Fatal("deleting builds deleted an operation's file")
	}
	if err := r.svc.DeleteBuild(ctx, queued.BuildID.String(), alice); !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("deleting a deleted build: %v", err)
	}
}

func TestTimeoutsAndLostFactories(t *testing.T) {
	r := newRig(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) { return refuse(c, protocol.CodeBusy, "busy"), nil }
	queued := r.queue(t, nil)
	r.rpc.answer = nil
	running := r.started(t)
	r.rpc.answer = func(c call) (protocol.ReplyEnvelope, error) { return refuse(c, protocol.CodeBusy, "busy"), nil }

	r.clock = r.clock.Add(31 * time.Minute)
	r.svc.Tick(context.Background())
	for _, id := range []string{queued.BuildID.String(), running.BuildID.String()} {
		if got := r.get(t, id, models.BuildStateFailed); got.Error.Code != models.BuildErrorTimeout {
			t.Fatalf("%s: %+v", id, got.Error)
		}
	}

	r.rpc.answer = nil
	lost := r.started(t)
	r.reg.setActive() // the instance died
	r.svc.Tick(context.Background())
	if got := r.get(t, lost.BuildID.String(), models.BuildStateFailed); got.Error.Code != models.BuildErrorFactoryLost {
		t.Fatalf("lost factory: %+v", got.Error)
	}
}

// --- files ---------------------------------------------------------------

func TestFiles(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	f := r.upload(t, "badge.png", "image/png", "png!")
	if f.Size != 4 || f.SHA256 != sha("png!") || !r.store.has(f.ObjectKey) {
		t.Fatalf("file %+v", f)
	}
	if _, err := r.svc.UploadFile(ctx, opID, "short.png", "image/png", strings.NewReader("png"), 4, alice); err == nil {
		t.Fatal("an upload shorter than declared was kept")
	}
	if _, err := r.svc.UploadFile(ctx, opID, "big.bin", "", strings.NewReader("x"), 65<<20, alice); err == nil {
		t.Fatal("an upload over the limit was taken")
	}

	// A build refers to it: it stays, through deletes and sweeps.
	r.queue(t, func(in *BuildInput) { in.Values["badge"] = f.FileID.String() })
	if err := r.svc.DeleteFile(ctx, f.FileID.String()); err == nil {
		t.Fatal("a file in use was deleted")
	}
	unused := r.upload(t, "old.png", "image/png", "old!")
	recent := r.upload(t, "new.png", "image/png", "new!")
	r.files.rows[unused.FileID] = withCreated(r.files.rows[unused.FileID], r.clock.Add(-25*time.Hour))
	r.files.rows[f.FileID] = withCreated(r.files.rows[f.FileID], r.clock.Add(-25*time.Hour))
	r.svc.sweepFiles(ctx)
	if _, err := r.svc.File(ctx, unused.FileID.String()); !errors.Is(err, ErrFileNotFound) || r.store.has(unused.ObjectKey) {
		t.Fatal("an old unused upload was kept")
	}
	for _, kept := range []*models.BuildFile{f, recent} {
		if _, err := r.svc.File(ctx, kept.FileID.String()); err != nil {
			t.Fatalf("%s was swept: %v", kept.Name, err)
		}
	}
}

func withCreated(f models.BuildFile, at time.Time) models.BuildFile {
	f.CreatedAt = at
	return f
}

// --- operation cleanup ---------------------------------------------------

func TestDeleteOperationData(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	f := r.upload(t, "badge.png", "image/png", "png!")
	done := r.started(t)
	if _, err := r.succeed(t, done, "x", "x"); err != nil {
		t.Fatal(err)
	}
	r.settle()
	running := r.started(t)
	if err := r.svc.DeleteOperationData(ctx, opID); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if len(r.builds.rows)+len(r.files.rows)+len(r.minions.rows) != 0 || r.store.has(f.ObjectKey) || r.store.has(artifactKey(opID, done.BuildID)) {
		t.Fatal("operation data left behind")
	}
	calls := r.rpc.recorded()
	if last := calls[len(calls)-1]; last.op != protocol.TypeFactoryBuildCancel || last.payload.(protocol.BuildCancelRequest).BuildID != running.BuildID.String() {
		t.Fatalf("the running build was not cancelled on its factory: %+v", last)
	}
}

func TestKinds(t *testing.T) {
	r := newRig(t)
	r.reg.mu.Lock()
	r.reg.icons = map[string]models.ModuleIcon{"bob": {Icon: "<svg bob/>", IconLight: "<svg bob light/>"}}
	r.reg.mu.Unlock()
	kinds, err := r.svc.Kinds(context.Background())
	if err != nil || len(kinds) != 1 {
		t.Fatalf("kinds = %+v (%v)", kinds, err)
	}
	if k := kinds[0]; k.ModuleName != "bob" || k.Instances != 1 || k.Icon != "<svg bob/>" || k.IconLight != "<svg bob light/>" || k.Schema.Version != 1 {
		t.Errorf("kind = %+v", k)
	}
}

// TestBuildByID pins how a build is named: by its build id, nothing else.
func TestBuildByID(t *testing.T) {
	r := newRig(t)
	b := r.queue(t, nil)
	for _, id := range []string{b.BuildID.String(), " " + b.BuildID.String() + " "} {
		if got, err := r.svc.Build(context.Background(), id); err != nil || got.BuildID != b.BuildID {
			t.Fatalf("%q: %v %+v", id, err, got)
		}
	}
	for _, id := range []string{uuid.NewString(), "01M6A3Q9X7J0B5KQZC2V1RS8TN", ""} {
		if _, err := r.svc.Build(context.Background(), id); !errors.Is(err, ErrBuildNotFound) {
			t.Errorf("%q: %v, want not found", id, err)
		}
	}
}
