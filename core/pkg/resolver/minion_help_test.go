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
