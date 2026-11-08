package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/pagination"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeTaskTunnels opens the ports asked for: 41000 for any, the one asked
// for otherwise; err refuses.
type fakeTaskTunnels struct {
	opened  []miniontunnels.Opening
	closed  []uuid.UUID
	err     error
	address string
	live    map[uuid.UUID]miniontunnels.Summary
	ended   miniontunnels.Summary
	caps    []int64
}

func (f *fakeTaskTunnels) Open(_ context.Context, o miniontunnels.Opening) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.opened = append(f.opened, o)
	if o.Port != 0 {
		return o.Port, nil
	}
	return 41000, nil
}

func (f *fakeTaskTunnels) Close(_ context.Context, task uuid.UUID) { f.closed = append(f.closed, task) }

func (f *fakeTaskTunnels) Address() string { return f.address }

func (f *fakeTaskTunnels) View(_ context.Context, task uuid.UUID) (miniontunnels.Summary, bool, error) {
	if sum, ok := f.live[task]; ok {
		return sum, true, nil
	}
	return f.ended, false, nil
}

func (f *fakeTaskTunnels) Summary(task uuid.UUID) (miniontunnels.Summary, bool) {
	sum, ok := f.live[task]
	return sum, ok
}

func (f *fakeTaskTunnels) Streams(context.Context, uuid.UUID, miniontunnels.StreamFilter, uint64, int64) ([]models.MinionTunnelStream, error) {
	return nil, nil
}

func (f *fakeTaskTunnels) Port(context.Context, uuid.UUID) (*models.OperationalPort, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeTaskTunnels) Ports(context.Context, uuid.UUID, string, *pagination.Cursor, int64, bool) ([]models.OperationalPort, error) {
	return nil, nil
}

func (f *fakeTaskTunnels) CountPorts(context.Context, uuid.UUID, string) (int64, error) {
	return 0, nil
}

func (f *fakeTaskTunnels) OpenReverse(_ context.Context, o miniontunnels.Opening) error {
	if f.err != nil {
		return f.err
	}
	f.opened = append(f.opened, o)
	return nil
}

func (f *fakeTaskTunnels) Listener(context.Context, uuid.UUID) (*models.MinionListener, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeTaskTunnels) Listeners(context.Context, uuid.UUID, string, *pagination.Cursor, int64, bool) ([]models.MinionListener, error) {
	return nil, nil
}

func (f *fakeTaskTunnels) SetCap(_ context.Context, _ uuid.UUID, limit int64) error {
	f.caps = append(f.caps, limit)
	return f.err
}

func (f *fakeTaskTunnels) CountListeners(context.Context, uuid.UUID, string) (int64, error) {
	return 0, nil
}

// A tunnel task's proxy is live while core listens for it (its port, its
// streams now); once it ended, its records' totals, the port its args say.
// The address is core's, null for the web app's host.
func TestTaskTunnel(t *testing.T) {
	ctx := context.Background()
	live := uuid.New()
	f := &fakeTaskTunnels{live: map[uuid.UUID]miniontunnels.Summary{live: {Port: 41003, Open: 2, Streams: 9, Up: 100, Down: 2000}},
		ended: miniontunnels.Summary{Streams: 4, Up: 7, Down: 8}}
	r := &minionTaskResolver{tunnels: f}
	got, err := r.Tunnel(ctx, &model.MinionTask{ID: live.String()})
	if err != nil || got.Port != 41003 || !got.Listening || got.Open != 2 || got.Streams != 9 || got.Up != 100 || got.Down != 2000 || got.Address != nil {
		t.Fatalf("live: %+v %v", got, err)
	}
	f.address = "ops.example.org"
	got, err = r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Args: map[string]any{"port": float64(41007)}})
	if err != nil || got.Port != 41007 || got.Listening || got.Open != 0 || got.Streams != 4 || got.Up != 7 || got.Down != 8 ||
		got.Address == nil || *got.Address != "ops.example.org" {
		t.Fatalf("ended: %+v %v", got, err)
	}
}

// A stream reads OPENING until the minion's reply, OPEN after it, ENDED
// once it ended; its result by name.
func TestTunnelStreamModel(t *testing.T) {
	at := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	s := models.MinionTunnelStream{Stream: 3, Client: "10.9.0.2:50122", Host: "dc01", Port: 445, OpenedAt: at, Up: 5, Down: 6}
	if m := tunnelStreamModel(&s); m.State != "OPENING" || m.Result != nil || m.Stream != 3 || m.OpenedAt != "2026-11-06T10:00:00Z" {
		t.Errorf("opening: %+v", m)
	}
	open := protocol.TunnelOpen
	s.Result, s.Bound = &open, "10.0.0.7:51234"
	if m := tunnelStreamModel(&s); m.State != "OPEN" || m.Result == nil || *m.Result != "open" || m.Bound == nil || *m.Bound != "10.0.0.7:51234" {
		t.Errorf("open: %+v", m)
	}
	refused := protocol.TunnelRefused
	s.Result, s.Bound, s.EndedAt, s.End = &refused, "", &at, models.TunnelEndNotOpened
	if m := tunnelStreamModel(&s); m.State != "ENDED" || *m.Result != "refused" || m.Bound != nil || m.End == nil || *m.End != "not_opened" {
		t.Errorf("refused: %+v", m)
	}
}

// The tunnel commands a build's factory declares: one that opens TCP and
// UDP, one TCP alone, and a reverse one.
var (
	tunnelCmd    = protocol.BuildCommand{ID: "tunnel", Op: 15, Action: protocol.ActionTunnel, Opens: []string{protocol.OpenTCP, protocol.OpenUDP}}
	tcpTunnelCmd = protocol.BuildCommand{ID: "tunnel", Op: 15, Action: protocol.ActionTunnel}
	reverseCmd   = protocol.BuildCommand{ID: "reverse_tunnel", Op: 17, Action: protocol.ActionReverseTunnel,
		Opens: []string{protocol.OpenTCP, protocol.OpenUDP}}
)

// A tunnel task takes its port before it is queued, for any free one (0)
// or the one asked for, its front its action's first unless it names
// another; its args then carry the port, and its max_streams and timeout
// (the command's defaults when it has none) bound the proxy. A SOCKS
// proxy takes UDP when its command opens udp. No port free, a front the
// action has not, or no tunnels in core, is the operator's to fix.
func TestOpenTunnel(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	task, uid := uuid.New(), uuid.New()
	wire := map[string]any{"port": int64(0)}
	port, front, err := r.openTunnel(ctx, s, uid, task, tunnelCmd, wire)
	if err != nil || port != 41000 || front != protocol.FrontSOCKS || wire["port"] != int64(41000) || wire["front"] != protocol.FrontSOCKS {
		t.Fatalf("any port: %d %s %v, args %v", port, front, err, wire)
	}
	o := f.opened[0]
	if o.Task != task || o.Session != s.SessionID || o.Operation != s.OperationID || o.By != uid || o.Port != 0 ||
		o.MaxStreams != miniontunnels.DefaultMaxStreams || o.Timeout != miniontunnels.DefaultConnectTimeout ||
		o.Purpose != models.OperationalPurposeSOCKS || !o.UDP || o.Cap != 0 {
		t.Errorf("opened %+v", o)
	}
	wire = map[string]any{"port": int64(41007), "max_streams": int64(8), "timeout": int64(3000), protocol.ParamStopIdle: int64(3600000),
		protocol.ParamCap: int64(1 << 20)}
	if port, _, err = r.openTunnel(ctx, s, uid, uuid.New(), tcpTunnelCmd, wire); err != nil || port != 41007 {
		t.Fatalf("the port asked for: %d %v", port, err)
	}
	if o := f.opened[1]; o.Port != 41007 || o.MaxStreams != 8 || o.Timeout != 3*time.Second || o.StopIdle != time.Hour || o.UDP || o.Cap != 1<<20 {
		t.Errorf("opened %+v", o)
	}
	if _, _, err := r.openTunnel(ctx, s, uid, uuid.New(), tunnelCmd, map[string]any{"front": "vpn"}); err == nil || !strings.Contains(err.Error(), "front: one of socks, http, forward") {
		t.Errorf("a front the action has not: %v", err)
	}
	f.err = fmt.Errorf("%w: all 64 are in use", miniontunnels.ErrNoPort)
	if _, _, err := r.openTunnel(ctx, s, uid, uuid.New(), tunnelCmd, map[string]any{}); err == nil || !strings.Contains(err.Error(), "all 64 are in use") {
		t.Errorf("no port free: %v", err)
	}
	f.err = errors.New("mongo down")
	if _, _, err := r.openTunnel(ctx, s, uid, uuid.New(), tunnelCmd, map[string]any{}); err == nil || !strings.Contains(err.Error(), "mongo down") {
		t.Errorf("a store failing: %v", err)
	}
	if _, _, err := (&minionTaskResolver{}).openTunnel(ctx, s, uid, uuid.New(), tunnelCmd, map[string]any{}); err == nil || !strings.Contains(err.Error(), "core opens no tunnels") {
		t.Errorf("no tunnels: %v", err)
	}
}

// A command line that leaves stop_idle out runs by core's default for the
// action (a tunnel 1h, a reverse tunnel never: core adds the param), and
// the task's args then say so, as a form's (which sends every field) do;
// one given stays. withDefault leaves out a param with no default, or none.
func TestTunnelStopIdleDefault(t *testing.T) {
	params := []protocol.FieldSpec{{Key: "timeout", Type: protocol.FieldDuration, Num: 2, Default: "10s"}}
	params = append(params, protocol.TunnelCoreParams(protocol.ActionTunnel)...)
	values := map[string]any{"port": 41000}
	wire, problems := protocol.TaskArgs(params, values)
	if problems != nil {
		t.Fatal(problems)
	}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	if _, _, err := r.openTunnel(context.Background(), s, uuid.New(), uuid.New(), tunnelCmd, wire); err != nil {
		t.Fatal(err)
	}
	if o := f.opened[0]; o.StopIdle != time.Hour || o.Port != 41000 || o.Purpose != models.OperationalPurposeSOCKS {
		t.Errorf("stops after %v, not the default (%+v)", o.StopIdle, o)
	}
	withDefault(values, params, protocol.ParamStopIdle)
	if values[protocol.ParamStopIdle] != "1h" || values["port"] != 41000 {
		t.Errorf("args %v", values)
	}
	reverse := map[string]any{}
	withDefault(reverse, protocol.TunnelCoreParams(protocol.ActionReverseTunnel), protocol.ParamStopIdle)
	if reverse[protocol.ParamStopIdle] != "0s" {
		t.Errorf("a reverse tunnel's args %v", reverse)
	}
	for name, c := range map[string]struct {
		values map[string]any
		params []protocol.FieldSpec
		want   any
	}{
		"given":        {map[string]any{protocol.ParamStopIdle: "5m"}, params, "5m"},
		"given never":  {map[string]any{protocol.ParamStopIdle: "0s"}, params, "0s"},
		"null":         {map[string]any{protocol.ParamStopIdle: nil}, params, "1h"},
		"no default":   {map[string]any{}, []protocol.FieldSpec{{Key: protocol.ParamStopIdle, Type: protocol.FieldDuration}}, nil},
		"not declared": {map[string]any{}, params[:1], nil},
	} {
		withDefault(c.values, c.params, protocol.ParamStopIdle)
		v, ok := c.values[protocol.ParamStopIdle]
		if c.want == nil && ok || c.want != nil && v != c.want {
			t.Errorf("%s: args %v", name, c.values)
		}
	}
}

// A forward front opens its port onto its destination, its host and to,
// checked as a stream's destination is; one that does not read is the
// operator's to fix. The HTTP front opens a port with core's HTTP proxy.
func TestOpenForward(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), Number: 7}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	wire := map[string]any{"front": protocol.FrontForward, "port": int64(0), "host": " 10.0.0.5 ", "to": int64(3389)}
	port, front, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), tunnelCmd, wire)
	if err != nil || port != 41000 || front != protocol.FrontForward {
		t.Fatalf("forward: %d %v", port, err)
	}
	if o := f.opened[0]; o.Purpose != models.OperationalPurposeForward || o.Host != "10.0.0.5" || o.HostPort != 3389 || o.SessionNumber != 7 || o.UDP {
		t.Errorf("opened %+v", o)
	}
	if wire["host"] != "10.0.0.5" {
		t.Errorf("args %v", wire)
	}
	for name, w := range map[string]map[string]any{
		"no host":    {"front": "forward", "to": int64(22)},
		"port 0":     {"front": "forward", "host": "dc01"},
		"port 65536": {"front": "forward", "host": "dc01", "to": int64(65536)},
		"a control":  {"front": "forward", "host": "dc\x0001", "to": int64(22)},
	} {
		if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), tunnelCmd, w); err == nil || !strings.Contains(err.Error(), "destination") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.opened) != 1 {
		t.Errorf("a bad forward opened a port: %+v", f.opened)
	}
	if _, front, _ := r.openTunnel(ctx, s, uuid.New(), uuid.New(), tunnelCmd, map[string]any{"front": protocol.FrontHTTP}); front != protocol.FrontHTTP {
		t.Errorf("http: %s", front)
	}
	if o := f.opened[1]; o.Purpose != models.OperationalPurposeHTTP || o.Host != "" || o.UDP {
		t.Errorf("http: %+v", o)
	}
}

// A tunnel says what is in front, and where a forward goes, from its
// task's args; a proxy's names no destination; its cap is live while it
// runs, its args' once it ended.
func TestTunnelFront(t *testing.T) {
	ctx := context.Background()
	live := uuid.New()
	r := &minionTaskResolver{tunnels: &fakeTaskTunnels{live: map[uuid.UUID]miniontunnels.Summary{live: {Port: 41001, Cap: 4096}}}}
	tunnel := protocol.ActionTunnel
	got, err := r.Tunnel(ctx, &model.MinionTask{ID: live.String(), Action: &tunnel,
		Args: map[string]any{"front": "forward", "port": float64(41001), "host": "fe80::1", "to": float64(22), "cap": float64(1)}})
	if err != nil || got.Front != protocol.FrontForward || got.Reverse || got.To == nil || *got.To != "[fe80::1]:22" || got.Port != 41001 || got.Cap != 4096 {
		t.Fatalf("forward: %+v %v", got, err)
	}
	got, err = r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &tunnel, Args: map[string]any{"port": float64(41000), "cap": float64(9)}})
	if err != nil || got.Front != protocol.FrontSOCKS || got.To != nil || got.Cap != 9 {
		t.Fatalf("socks, ended: %+v %v", got, err)
	}
	got, _ = r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &tunnel, Args: map[string]any{"front": "http", "host": "x", "to": float64(1)}})
	if got.Front != protocol.FrontHTTP || got.To != nil {
		t.Errorf("http: %+v", got)
	}
}

// A reverse tunnel's task takes no operational port: the minion listens
// where its args say (checked); a forward's destination is core's,
// checked as a stream's, and forward is its one front (no reverse SOCKS).
// UDP only through a command that opens it. One that does not read is the
// operator's to fix.
func TestOpenReverse(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New(), Number: 3}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	wire := map[string]any{"bind": " 0.0.0.0 ", "listen": int64(8080), "udp": true, "host": " 10.8.0.2 ", "to": int64(4444), "max_streams": int64(4)}
	port, front, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), reverseCmd, wire)
	if err != nil || port != 0 || front != protocol.FrontForward {
		t.Fatalf("reverse: %d %s %v", port, front, err)
	}
	o := f.opened[0]
	if o.Purpose != models.OperationalPurposeReverse || o.Front != protocol.FrontForward || o.Bind != "0.0.0.0" || o.Listen != 8080 || !o.UDP ||
		o.Host != "10.8.0.2" || o.HostPort != 4444 || o.MaxStreams != 4 || o.SessionNumber != 3 || o.Port != 0 {
		t.Errorf("opened %+v", o)
	}
	if wire["bind"] != "0.0.0.0" || wire["host"] != "10.8.0.2" {
		t.Errorf("args %v", wire)
	}
	tcpOnly := reverseCmd
	tcpOnly.Opens = nil
	for name, c := range map[string]struct {
		cmd  protocol.BuildCommand
		wire map[string]any
		want string
	}{
		"no bind":       {reverseCmd, map[string]any{"listen": int64(1), "host": "h", "to": int64(1)}, "where the minion listens"},
		"listen -1":     {reverseCmd, map[string]any{"bind": "0.0.0.0", "listen": int64(-1), "host": "h", "to": int64(1)}, "where the minion listens"},
		"listen 70000":  {reverseCmd, map[string]any{"bind": "0.0.0.0", "listen": int64(70000), "host": "h", "to": int64(1)}, "where the minion listens"},
		"no host":       {reverseCmd, map[string]any{"bind": "0.0.0.0", "listen": int64(1), "to": int64(1)}, "destination"},
		"to 0":          {reverseCmd, map[string]any{"bind": "0.0.0.0", "listen": int64(1), "host": "h"}, "destination"},
		"socks":         {reverseCmd, map[string]any{"front": "socks", "bind": "0.0.0.0", "listen": int64(1080)}, "front: one of forward"},
		"udp, tcp only": {tcpOnly, map[string]any{"bind": "0.0.0.0", "listen": int64(1), "udp": true, "host": "h", "to": int64(1)}, "TCP alone"},
		"http":          {reverseCmd, map[string]any{"front": "http", "bind": "0.0.0.0", "listen": int64(1)}, "front: one of forward"},
	} {
		if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), c.cmd, c.wire); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.opened) != 1 {
		t.Errorf("a bad reverse tunnel kept: %+v", f.opened)
	}
	f.err = miniontunnels.ErrNoListeners
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), reverseCmd,
		map[string]any{"bind": "0.0.0.0", "listen": int64(1), "host": "h", "to": int64(1)}); err == nil || !strings.Contains(err.Error(), "no reverse port forwards") {
		t.Errorf("no listeners: %v", err)
	}
}

// A reverse tunnel's tunnel has no port: it says where the minion is to
// listen, where it says it does (live), and the destination; it listens
// only once the minion said so.
func TestReverseTunnel(t *testing.T) {
	ctx := context.Background()
	live, quiet := uuid.New(), uuid.New()
	r := &minionTaskResolver{tunnels: &fakeTaskTunnels{address: "core.example", live: map[uuid.UUID]miniontunnels.Summary{
		live: {Open: 1, Streams: 3, Bound: "0.0.0.0:8080"}, quiet: {}}}}
	reverse := protocol.ActionReverseTunnel
	args := map[string]any{"bind": "0.0.0.0", "listen": float64(8080), "udp": true, "host": "10.8.0.2", "to": float64(4444)}
	got, err := r.Tunnel(ctx, &model.MinionTask{ID: live.String(), Action: &reverse, Args: args})
	if err != nil || got.Front != protocol.FrontForward || !got.Reverse || got.Port != 0 || got.Address != nil || got.To == nil ||
		*got.To != "10.8.0.2:4444" || got.Bind == nil || *got.Bind != "0.0.0.0" || got.Listen == nil || *got.Listen != 8080 ||
		got.Bound == nil || *got.Bound != "0.0.0.0:8080" || !got.Listening || got.Open != 1 || got.Streams != 3 || got.UDP == nil || !*got.UDP {
		t.Fatalf("reverse: %+v %v", got, err)
	}
	got, err = r.Tunnel(ctx, &model.MinionTask{ID: quiet.String(), Action: &reverse, Args: args})
	if err != nil || got.Listening || got.Bound != nil {
		t.Fatalf("not listening yet: %+v %v", got, err)
	}
}

type fakeTaskBuilds struct{ b *models.Build }

func (f fakeTaskBuilds) FindByBuildID(context.Context, uuid.UUID) (*models.Build, error) {
	return f.b, nil
}

type fakeTaskSchemas struct{ s *models.ModuleBuildSchema }

func (f fakeTaskSchemas) LatestBuildSchema(context.Context, string) (*models.ModuleBuildSchema, error) {
	return f.s, nil
}

// A session's tunnel commands come with core's own params beside the
// command's (the front, the port, a forward's destination, stop_idle, the
// cap): no num, so they never go to the minion; others come as they are.
func TestCommandsHaveCoreParams(t *testing.T) {
	timeout := protocol.FieldSpec{Key: "timeout", Num: 2, Type: protocol.FieldDuration, Default: "10s"}
	cmd := tunnelCmd
	cmd.Params = []protocol.FieldSpec{timeout}
	ls := protocol.BuildCommand{ID: "ls", Op: 4, Action: protocol.ActionListDir, Params: []protocol.FieldSpec{{Key: "path", Num: 1, Type: protocol.FieldString}}}
	sc := &models.ModuleBuildSchema{BuildSchema: protocol.BuildSchema{Commands: []protocol.BuildCommand{cmd, ls}}}
	r := &minionTaskResolver{builds: fakeTaskBuilds{b: &models.Build{ModuleName: "bob", Commands: []string{"tunnel", "ls"}}},
		schemas: fakeTaskSchemas{s: sc}}
	_, commands, err := r.commandsOf(context.Background(), &models.MinionSession{})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range commands[0].Params {
		keys = append(keys, p.Key)
	}
	if strings.Join(keys, ",") != "timeout,front,port,host,to,stop_idle,cap" || len(commands[1].Params) != 1 || len(sc.Commands[0].Params) != 1 {
		t.Errorf("tunnel %v; ls %d; the schema's own %d", keys, len(commands[1].Params), len(sc.Commands[0].Params))
	}
	wire, problems := protocol.TaskArgs(commands[0].Params, map[string]any{"front": "http"})
	if problems != nil || wire["front"] != "http" || wire["cap"] == nil || len(protocol.WireArgs(commands[0].Params, wire)) != 1 {
		t.Errorf("args %v %v", wire, problems)
	}
}

// A build's tunnel command opens what its target lets it (a Linux build,
// no pipes), and core offers a path, a source address and TLS only where
// it opens them.
func TestCommandsByTarget(t *testing.T) {
	full := protocol.BuildCommand{ID: "tunnel", Op: 15, Action: protocol.ActionTunnel, Opens: []string{protocol.OpenTCP, protocol.OpenUDP,
		protocol.OpenUnix, protocol.OpenPipe, protocol.OpenSource, protocol.OpenTLS}}
	sc := protocol.BuildSchema{Targets: []protocol.BuildTarget{
		{ID: "linux-elf", Opens: []string{protocol.OpenTCP, protocol.OpenUDP, protocol.OpenUnix, protocol.OpenSource, protocol.OpenTLS}},
		{ID: "windows-exe"}}, Commands: []protocol.BuildCommand{full}}
	keys := func(c protocol.BuildCommand) string {
		var out []string
		for _, p := range c.Params {
			out = append(out, p.Key)
		}
		return strings.Join(out, ",")
	}
	linux := tunnelCommand(sc, full, "linux-elf")
	if strings.Join(linux.Opens, ",") != "tcp,udp,unix,source,tls" || keys(linux) != "front,port,host,to,path,source,tls,stop_idle,cap" {
		t.Errorf("linux: opens %v, params %s", linux.Opens, keys(linux))
	}
	if win := tunnelCommand(sc, full, "windows-exe"); !win.CanOpen(protocol.OpenPipe) {
		t.Errorf("windows: opens %v", win.Opens)
	}
	if plain := tunnelCommand(sc, tunnelCmd, "linux-elf"); keys(plain) != "front,port,host,to,stop_idle,cap" {
		t.Errorf("a command that opens tcp and udp: %s", keys(plain))
	}
}

// A forward goes to a unix socket or a pipe where its minion opens them,
// the path's kind its own; a tunnel's TCP streams go from a source address
// where it takes one, and a forward speaks TLS where it does: each checked
// as the task is queued.
func TestOpenPathsAndOptions(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	all := tunnelCmd
	all.Opens = []string{protocol.OpenTCP, protocol.OpenUnix, protocol.OpenPipe, protocol.OpenSource, protocol.OpenTLS}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), all, map[string]any{"front": "forward", "path": " /var/run/docker.sock "}); err != nil {
		t.Fatal(err)
	}
	if o := f.opened[0]; o.Kind != protocol.TunnelKindUnix || o.Path != "/var/run/docker.sock" || o.Host != "" || o.Purpose != models.OperationalPurposeForward {
		t.Errorf("a unix socket: %+v", o)
	}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), all, map[string]any{"front": "forward", "path": `\\dc01\pipe\lsarpc`}); err != nil {
		t.Fatal(err)
	}
	if o := f.opened[1]; o.Kind != protocol.TunnelKindPipe || o.Path != `\\dc01\pipe\lsarpc` {
		t.Errorf("a pipe: %+v", o)
	}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), all, map[string]any{"front": "forward", "host": "intranet", "to": int64(443),
		"tls": true, "source": "10.0.0.7"}); err != nil {
		t.Fatal(err)
	}
	if o := f.opened[2]; !o.TLS || o.Source != "10.0.0.7" || o.Host != "intranet" {
		t.Errorf("tls from a source: %+v", o)
	}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), all, map[string]any{"source": "fe80::1"}); err != nil || f.opened[3].Source != "fe80::1" {
		t.Errorf("a SOCKS proxy from a source: %+v %v", f.opened[3], err)
	}
	tcpOnly := tunnelCmd
	for name, c := range map[string]struct {
		cmd  protocol.BuildCommand
		wire map[string]any
		want string
	}{
		"no pipes":          {tcpOnly, map[string]any{"front": "forward", "path": `\\.\pipe\x`}, "opens no named pipes"},
		"no unix sockets":   {tcpOnly, map[string]any{"front": "forward", "path": "/run/x.sock"}, "opens no unix sockets"},
		"a bad pipe":        {all, map[string]any{"front": "forward", "path": `\\.\nopipe`}, "the forward's path"},
		"a SOCKS path":      {all, map[string]any{"path": "/run/x.sock"}, "a forward's destination"},
		"no source taken":   {tcpOnly, map[string]any{"source": "10.0.0.7"}, "takes no source address"},
		"a source name":     {all, map[string]any{"source": "eth0"}, "an IP"},
		"a path's source":   {all, map[string]any{"front": "forward", "path": "/run/x.sock", "source": "10.0.0.7"}, "not a path's"},
		"no TLS spoken":     {tcpOnly, map[string]any{"front": "forward", "host": "h", "to": int64(443), "tls": true}, "speaks no TLS"},
		"TLS through SOCKS": {all, map[string]any{"tls": true}, "a forward's to a host and a port"},
		"TLS to a path":     {all, map[string]any{"front": "forward", "path": "/run/x.sock", "tls": true}, "a forward's to a host and a port"},
	} {
		if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), c.cmd, c.wire); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.opened) != 4 {
		t.Errorf("a bad one opened: %d", len(f.opened))
	}
	// The tunnel says its path, its source and TLS.
	tunnel := protocol.ActionTunnel
	got, _ := r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &tunnel, Args: map[string]any{"front": "forward",
		"path": "/var/run/docker.sock", "source": "10.0.0.7"}})
	if got.To != nil || got.Path == nil || *got.Path != "/var/run/docker.sock" || got.Source == nil || *got.Source != "10.0.0.7" || got.TLS {
		t.Errorf("a path's tunnel: %+v", got)
	}
	got, _ = r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &tunnel, Args: map[string]any{"front": "forward", "host": "h",
		"to": float64(443), "tls": true}})
	if got.To == nil || *got.To != "h:443" || !got.TLS || got.Path != nil {
		t.Errorf("a TLS forward's tunnel: %+v", got)
	}
}
