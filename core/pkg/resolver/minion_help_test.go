package resolver

import (
	"strings"
	"testing"

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
