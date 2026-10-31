package resolver

import (
	"fmt"
	"strings"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// help is core's own command in a session's console (protocol.CommandHelp):
// what the session's minion takes, or one command's params. Core answers it
// as it is queued; it never goes to the minion.

// helpCommand is help as a command of the session, its param one of the
// session's commands (help among them).
func helpCommand(commands []protocol.BuildCommand) protocol.BuildCommand {
	options := make([]protocol.EnumOption, 0, len(commands)+1)
	for _, c := range commands {
		options = append(options, protocol.EnumOption{Value: c.ID, Label: c.Label})
	}
	const label = "List the commands, or tell about one"
	options = append(options, protocol.EnumOption{Value: protocol.CommandHelp, Label: label})
	return protocol.BuildCommand{
		ID:    protocol.CommandHelp,
		Label: label,
		Description: "Lists the commands this session's minion takes, or one command's params. " +
			"Core answers it: nothing goes to the minion.",
		Params: []protocol.FieldSpec{{
			Key: "command", Type: protocol.FieldEnum, Label: "Command",
			Description: "The command to tell about; all of them when left out.", Options: options,
		}},
	}
}

// helpText is help's answer: the commands with how each is typed, or the
// one named with its params. commands include help.
func helpText(commands []protocol.BuildCommand, topic string) string {
	var b strings.Builder
	if topic != "" {
		for _, c := range commands {
			if c.ID == topic {
				writeCommandHelp(&b, c)
				return b.String()
			}
		}
		fmt.Fprintf(&b, "No command %q.\n", topic)
		return b.String()
	}
	b.WriteString("Commands this session's minion takes:\n\n")
	width := 0
	usages := make([]string, len(commands))
	for i, c := range commands {
		usages[i] = commandUsage(c)
		width = max(width, len(usages[i]))
	}
	for i, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, usages[i], c.Label)
	}
	b.WriteString("\n<param> is required, [param] is not, [param=x] defaults to x.\n")
	b.WriteString("help <command> tells more about one.\n")
	return b.String()
}

func writeCommandHelp(b *strings.Builder, c protocol.BuildCommand) {
	b.WriteString(commandUsage(c) + "\n")
	if c.Label != "" {
		b.WriteString("\n" + c.Label + "\n")
	}
	if c.Description != "" {
		b.WriteString("\n" + c.Description + "\n")
	}
	if len(c.Params) == 0 {
		b.WriteString("\nIt takes no params.\n")
		return
	}
	b.WriteString("\nParams:\n")
	width := 0
	for _, p := range c.Params {
		width = max(width, len(p.Key))
	}
	for _, p := range c.Params {
		var facts []string
		facts = append(facts, string(p.Type))
		if p.Required {
			facts = append(facts, "required")
		} else if p.Default != nil {
			facts = append(facts, fmt.Sprintf("default %v", p.Default))
		}
		if len(p.Options) > 0 {
			values := make([]string, len(p.Options))
			for i, o := range p.Options {
				values[i] = o.Value
			}
			facts = append(facts, "one of "+strings.Join(values, ", "))
		}
		fmt.Fprintf(b, "  %-*s  %s\n", width, p.Key, strings.Join(facts, ", "))
		about := strings.TrimSpace(strings.Join(nonEmpty(p.Label, p.Description), ": "))
		if about != "" {
			fmt.Fprintf(b, "  %-*s  %s\n", width, "", about)
		}
	}
	b.WriteString("\nParams go in order, or as --param=value anywhere.\n")
}

// commandUsage is how a command is typed: shell <command> [timeout=30s].
func commandUsage(c protocol.BuildCommand) string {
	parts := []string{c.ID}
	for _, p := range c.Params {
		switch {
		case p.Required:
			parts = append(parts, "<"+p.Key+">")
		case p.Default != nil:
			parts = append(parts, fmt.Sprintf("[%s=%v]", p.Key, p.Default))
		default:
			parts = append(parts, "["+p.Key+"]")
		}
	}
	return strings.Join(parts, " ")
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
