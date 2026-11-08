package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

func TestMinionHelp(t *testing.T) {
	factory := []protocol.BuildCommand{
		{ID: "shell", Label: "Run a shell command", Description: "Runs it with sh -c.", Params: []protocol.FieldSpec{
			{Key: "command", Type: protocol.FieldString, Label: "Command", Required: true},
			{Key: "timeout", Type: protocol.FieldDuration, Default: "30s"},
		}},
		{ID: "ls", Label: "List a directory", Params: []protocol.FieldSpec{{Key: "path", Type: protocol.FieldString}}},
		{ID: "exit", Label: "Exit"},
	}
	help := helpCommand(factory)
	commands := append(factory, help)

	if help.ID != protocol.CommandHelp || len(help.Params) != 1 || len(help.Params[0].Options) != 4 {
		t.Fatalf("help command = %+v", help)
	}
	// Its param takes the session's commands, help too, and nothing else.
	if _, problems := protocol.TaskArgs(help.Params, map[string]any{"command": "shell"}); problems != nil {
		t.Fatalf("help shell: %v", problems)
	}
	if _, problems := protocol.TaskArgs(help.Params, map[string]any{"command": "rm"}); problems == nil {
		t.Fatal("help rm passed")
	}
	if _, problems := protocol.TaskArgs(help.Params, map[string]any{}); problems != nil {
		t.Fatalf("help: %v", problems)
	}

	all := helpText(commands, "")
	for _, want := range []string{
		"  shell <command> [timeout=30s]  Run a shell command\n",
		"  ls [path]                      List a directory\n",
		"  exit                           Exit\n",
		"  help [command]                 List the commands, or tell about one\n",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("help lacks %q:\n%s", want, all)
		}
	}

	one := helpText(commands, "shell")
	for _, want := range []string{
		"shell <command> [timeout=30s]\n",
		"Runs it with sh -c.\n",
		"  command  string, required\n",
		"           Command\n",
		"  timeout  duration, default 30s\n",
	} {
		if !strings.Contains(one, want) {
			t.Errorf("help shell lacks %q:\n%s", want, one)
		}
	}
	if got := helpText(commands, "exit"); !strings.Contains(got, "It takes no params.") {
		t.Errorf("help exit:\n%s", got)
	}
}

// A command that works over some kinds only says so in help, and a task of
// it is refused on a session whose minion uses another kind.
func TestCommandKinds(t *testing.T) {
	pty := protocol.BuildCommand{ID: "pty", Label: "Interactive shell", Kinds: []string{protocol.KindReversePersistent, protocol.KindBindPersistent}}
	commands := []protocol.BuildCommand{pty, helpCommand([]protocol.BuildCommand{pty})}
	if all := helpText(commands, ""); !strings.Contains(all, "Interactive shell (over reverse-persistent, bind-persistent)") {
		t.Errorf("help:\n%s", all)
	}
	if one := helpText(commands, "pty"); !strings.Contains(one, "It works over reverse-persistent, bind-persistent connections only") {
		t.Errorf("help pty:\n%s", one)
	}
	ctx := context.Background()
	on := func(kind string) *models.MinionSession {
		return &models.MinionSession{Number: 7, Conn: 1, ConnKind: kind}
	}
	if err := overKind(ctx, on(protocol.KindReverseCall), pty); err == nil ||
		!strings.Contains(err.Error(), "pty works over reverse-persistent or bind-persistent connections; session #7's minion uses connection 1, reverse-call") {
		t.Errorf("over reverse-call: %v", err)
	}
	for _, kind := range []string{protocol.KindReversePersistent, ""} {
		if err := overKind(ctx, on(kind), pty); err != nil {
			t.Errorf("over %q: %v", kind, err)
		}
	}
	if err := overKind(ctx, on(protocol.KindBindCall), protocol.BuildCommand{ID: "ls"}); err != nil {
		t.Errorf("a command for every kind: %v", err)
	}
}

// A param that applies by another's value says so: a tunnel's destination
// is a forward's.
func TestMinionHelpConditions(t *testing.T) {
	tunnel := tunnelCommand(protocol.BuildSchema{Commands: []protocol.BuildCommand{tunnelCmd}}, tunnelCmd, "")
	one := helpText([]protocol.BuildCommand{tunnel}, "tunnel")
	for _, want := range []string{
		"  front      enum, default socks, one of socks, http, forward, vpn\n",
		"  routes     string_list, required, only when front is vpn\n",
		"  host       string, only when front is forward\n",
		"  stop_idle  duration, default 1h\n",
	} {
		if !strings.Contains(one, want) {
			t.Errorf("help tunnel lacks %q:\n%s", want, one)
		}
	}
}

// help shows a command's params as its form does, the order the console
// takes words in: its own, then those under More options, each by order.
func TestMinionHelpFormOrder(t *testing.T) {
	cmd := tunnelCmd
	cmd.Params = []protocol.FieldSpec{
		{Key: "timeout", Num: 2, Type: protocol.FieldDuration, Label: "Connect timeout", Default: "10s", Order: 10, Advanced: true},
		{Key: "max_streams", Num: 3, Type: protocol.FieldInt, Label: "Connections at once", Default: 256, Order: 11, Advanced: true},
	}
	tunnel := tunnelCommand(protocol.BuildSchema{Commands: []protocol.BuildCommand{cmd}}, cmd, "")
	if got, want := commandUsage(tunnel), "tunnel [front=socks] [port=0] [routes] [host] [to] [timeout=10s] [max_streams=256] [stop_idle=1h] [cap=0]"; got != want {
		t.Errorf("usage %q, want %q", got, want)
	}
	feedback := protocol.BuildCommand{ID: "feedback", Params: []protocol.FieldSpec{
		{Key: "message", Type: protocol.FieldString, Label: "Message", Required: true, Order: 2},
		{Key: "level", Type: protocol.FieldEnum, Label: "Level", Default: "warning", Order: 1, Options: []protocol.EnumOption{{Value: "warning"}}},
	}}
	if got := commandUsage(feedback); got != "feedback [level=warning] <message>" {
		t.Errorf("usage %q", got)
	}
	if one := helpText([]protocol.BuildCommand{feedback}, "feedback"); !strings.Contains(one, "  level    enum, default warning, one of warning\n  ") ||
		strings.Index(one, "  level ") > strings.Index(one, "  message ") {
		t.Errorf("help feedback:\n%s", one)
	}
}
