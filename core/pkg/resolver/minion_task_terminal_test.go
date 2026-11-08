package resolver

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/miniontunnels"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// A tunnel command whose minion runs processes and accepts.
var shellTunnelCmd = protocol.BuildCommand{ID: "tunnel", Op: 15, Action: protocol.ActionTunnel,
	Opens: []string{protocol.OpenTCP, protocol.OpenProcess, protocol.OpenAccept}}

// The terminal front and its program, args and size are offered only where
// the minion runs processes.
func TestTerminalCommand(t *testing.T) {
	sc := protocol.BuildSchema{Commands: []protocol.BuildCommand{shellTunnelCmd}}
	keys := func(c protocol.BuildCommand) (string, []string) {
		var out, fronts []string
		for _, p := range c.Params {
			out = append(out, p.Key)
			if p.Key == protocol.ParamFront {
				for _, o := range p.Options {
					fronts = append(fronts, o.Value)
				}
			}
		}
		return strings.Join(out, ","), fronts
	}
	params, fronts := keys(tunnelCommand(sc, shellTunnelCmd, ""))
	for _, k := range []string{protocol.ParamProgram, protocol.ParamArgs, protocol.ParamRows, protocol.ParamCols} {
		if !strings.Contains(","+params+",", ","+k+",") {
			t.Errorf("a shell's command lacks %s: %s", k, params)
		}
	}
	if !slices.Contains(fronts, protocol.FrontTerminal) {
		t.Errorf("a shell's fronts %v", fronts)
	}
	params, fronts = keys(tunnelCommand(sc, tunnelCmd, ""))
	if strings.Contains(params, protocol.ParamProgram) || strings.Contains(params, protocol.ParamRows) || slices.Contains(fronts, protocol.FrontTerminal) {
		t.Errorf("a command with no processes: %s %v", params, fronts)
	}
}

// A terminal's task runs its program (none: the host's shell) with its
// args split as a shell's words, 24×80 on the raw port unless it says;
// what does not read is the operator's to fix. A SOCKS proxy takes BIND
// where the minion accepts.
func TestOpenTerminal(t *testing.T) {
	ctx := context.Background()
	s := &models.MinionSession{SessionID: uuid.New(), OperationID: uuid.New()}
	f := &fakeTaskTunnels{}
	r := &minionTaskResolver{tunnels: f}
	wire := map[string]any{"front": "terminal", "program": " /bin/bash ", "args": ` -l -c 'echo "a b"' `}
	if _, front, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), shellTunnelCmd, wire); err != nil || front != protocol.FrontTerminal {
		t.Fatalf("terminal: %s %v", front, err)
	}
	o := f.opened[0]
	if o.Purpose != models.OperationalPurposeTerminal || o.Program != "/bin/bash" || strings.Join(o.Args, "|") != `-l|-c|echo "a b"` ||
		o.Rows != 24 || o.Cols != 80 || o.Accept {
		t.Errorf("opened %+v", o)
	}
	if wire["program"] != "/bin/bash" || wire["args"] != `-l -c 'echo "a b"'` || wire["rows"] != int64(24) || wire["cols"] != int64(80) {
		t.Errorf("args %v", wire)
	}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), shellTunnelCmd, map[string]any{"front": "terminal", "rows": int64(50),
		"cols": int64(200)}); err != nil {
		t.Fatal(err)
	}
	if o := f.opened[1]; o.Program != "" || len(o.Args) != 0 || o.Rows != 50 || o.Cols != 200 {
		t.Errorf("a shell: %+v", o)
	}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), shellTunnelCmd, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if o := f.opened[2]; o.Purpose != models.OperationalPurposeSOCKS || !o.Accept {
		t.Errorf("a SOCKS proxy that binds: %+v", o)
	}
	if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), tunnelCmd, map[string]any{}); err != nil || f.opened[3].Accept {
		t.Errorf("a SOCKS proxy that does not: %+v %v", f.opened[3], err)
	}
	all := shellTunnelCmd
	all.Opens = append(slices.Clone(all.Opens), protocol.OpenSource)
	for name, c := range map[string]struct {
		cmd  protocol.BuildCommand
		wire map[string]any
		want string
	}{
		"no processes":  {tunnelCmd, map[string]any{"front": "terminal"}, "runs no processes"},
		"an open quote": {shellTunnelCmd, map[string]any{"front": "terminal", "args": `-c 'echo`}, "quote"},
		"no rows":       {shellTunnelCmd, map[string]any{"front": "terminal", "rows": int64(0)}, "the terminal"},
		"too many cols": {shellTunnelCmd, map[string]any{"front": "terminal", "cols": int64(5000)}, "the terminal"},
	} {
		if _, _, err := r.openTunnel(ctx, s, uuid.New(), uuid.New(), c.cmd, c.wire); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.opened) != 4 {
		t.Errorf("a bad one opened: %d", len(f.opened))
	}
	// A terminal's params with another front, and a source with a terminal,
	// are refused as the task's args are read, before anything opens: each
	// applies by the front (FieldSpec.When).
	params := tunnelCommand(protocol.BuildSchema{Commands: []protocol.BuildCommand{all}}, all, "").Params
	for name, c := range map[string]struct {
		values    map[string]any
		key, want string
	}{
		"a SOCKS program": {map[string]any{"program": "sh"}, "program", "only when front is terminal"},
		"forward args":    {map[string]any{"front": "forward", "host": "h", "to": int64(1), "args": "-x"}, "args", "only when front is terminal"},
		"a SOCKS size":    {map[string]any{"rows": int64(30)}, "rows", "only when front is terminal"},
		"a source":        {map[string]any{"front": "terminal", "source": "10.0.0.7"}, "source", "only when front is socks, http or forward"},
	} {
		if _, problems := protocol.TaskArgs(params, c.values); problems[c.key] != c.want || len(problems) != 1 {
			t.Errorf("%s: %v", name, problems)
		}
	}
	// A SOCKS proxy's args take no terminal's defaults.
	if args, problems := protocol.TaskArgs(params, map[string]any{}); problems != nil || args[protocol.ParamRows] != nil || args[protocol.ParamCols] != nil {
		t.Errorf("a SOCKS proxy's args: %v %v", args, problems)
	}
}

// A terminal's tunnel says its program, args and size; a SOCKS proxy that
// binds says so while it runs. A process's stream says its program,
// whether it had a terminal and its exit; an accept's who came. A
// terminal's port says its program.
func TestTerminalModels(t *testing.T) {
	ctx := context.Background()
	live := uuid.New()
	f := &fakeTaskTunnels{live: map[uuid.UUID]miniontunnels.Summary{live: {Port: 41002, Accept: true}}}
	r := &minionTaskResolver{tunnels: f}
	tunnel := protocol.ActionTunnel
	got, err := r.Tunnel(ctx, &model.MinionTask{ID: uuid.New().String(), Action: &tunnel, Args: map[string]any{"front": "terminal",
		"program": "/bin/bash", "args": "-l", "rows": float64(24), "cols": float64(80)}})
	if err != nil || got.Front != protocol.FrontTerminal || got.Program == nil || *got.Program != "/bin/bash" || got.Args == nil || *got.Args != "-l" ||
		got.Rows == nil || *got.Rows != 24 || got.Cols == nil || *got.Cols != 80 || got.SocksBind {
		t.Errorf("a terminal's: %+v %v", got, err)
	}
	if got, _ := r.Tunnel(ctx, &model.MinionTask{ID: live.String(), Action: &tunnel}); !got.SocksBind || got.Program != nil || got.Rows != nil {
		t.Errorf("a SOCKS proxy's: %+v", got)
	}
	yes, code := true, int64(130)
	m := tunnelStreamModel(&models.MinionTunnelStream{Stream: 1, Kind: protocol.TunnelKindProcess, Terminal: &yes, Exit: &code})
	if m.Kind == nil || *m.Kind != "process" || m.Program != nil || m.Terminal == nil || !*m.Terminal || m.Exit == nil || *m.Exit != 130 {
		t.Errorf("a shell's stream: %+v", m)
	}
	m = tunnelStreamModel(&models.MinionTunnelStream{Stream: 2, Kind: protocol.TunnelKindAccept, Came: "10.0.0.9:40001"})
	if m.Kind == nil || *m.Kind != "accept" || m.Came == nil || *m.Came != "10.0.0.9:40001" || m.Terminal != nil || m.Exit != nil {
		t.Errorf("an accept's stream: %+v", m)
	}
	if m = tunnelStreamModel(&models.MinionTunnelStream{Stream: 3, Host: "dc01", Port: 445}); m.Kind != nil {
		t.Errorf("a TCP stream: %+v", m)
	}
	p := r.portModel(&models.OperationalPort{TaskID: uuid.New(), Purpose: models.OperationalPurposeTerminal, Program: "cmd.exe"})
	if p.Front != "terminal" || p.Program == nil || *p.Program != "cmd.exe" {
		t.Errorf("a terminal's port: %+v", p)
	}
}
