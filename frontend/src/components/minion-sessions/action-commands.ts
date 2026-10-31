// Which of a session's commands the console runs for one of core's actions
// (list_dir), when core starts it (docs: contracts/task-results-and-actions.md,
// Running an action): the session's commands with that action, in the
// factory's order (its preference). An item's action in a view offers them
// all, the view's own command first; Browse files runs the one the operator
// picked for the session, if the session still has it, else the first.

export interface ActionCommand {
  id: string
  action?: string | null
}

/** The session's commands with the action, in the factory's order. */
export function actionCommands<C extends ActionCommand>(commands: readonly C[], action: string): C[] {
  return commands.filter((c) => c.action === action)
}

/**
 * The commands an action on a view's item can run: the session's commands
 * with the action, the view's own command first when it has the action
 * (continuing its task), then the rest in the factory's order.
 */
export function commandsFor<C extends ActionCommand>(commands: readonly C[], action: string, own?: string): C[] {
  const all = actionCommands(commands, action)
  const first = all.find((c) => c.id === own)
  return first ? [first, ...all.filter((c) => c !== first)] : all
}

/** The one to run: the picked one while the session has it, else the first. */
export function chooseActionCommand<C extends ActionCommand>(candidates: readonly C[], picked: string | null): C | undefined {
  return candidates.find((c) => c.id === picked) ?? candidates[0]
}

// The operator's pick per session and action, kept in this browser.
const key = (sessionId: string, action: string) => `logos.console.actionCommand.${sessionId}.${action}`

export function readPicked(sessionId: string, action: string): string | null {
  try {
    return localStorage.getItem(key(sessionId, action))
  } catch {
    return null
  }
}

export function savePicked(sessionId: string, action: string, id: string) {
  try {
    localStorage.setItem(key(sessionId, action), id)
  } catch {
    // Kept for the page only.
  }
}
