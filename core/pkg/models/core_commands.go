package models

import (
	"slices"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

// CommandRehello is core's own command in a session's console: it asks the
// minion for its hello again (protocol.RehelloItem), its host and process
// as they are now. Core keeps it as a task, running until a hello comes
// (MinionSession.RehelloAt); it never goes to the minion as a task. The
// protocol does not reserve the name: a build schema's command of that id
// is shadowed by core's.
const CommandRehello = "rehello"

// CoreCommands are core's own commands: the protocol's
// (protocol.CoreCommands), and rehello.
func CoreCommands() []string {
	return append(protocol.CoreCommands(), CommandRehello)
}

// IsCoreCommand reports whether id is one of core's own commands.
func IsCoreCommand(id string) bool {
	return slices.Contains(CoreCommands(), id)
}
