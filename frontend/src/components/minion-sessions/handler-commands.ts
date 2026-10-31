// Which command core's console runs for a result handler when it starts one
// itself, not continuing a task (docs: contracts/task-result-handlers.md,
// Running a handler's command): of the session's commands with the handler,
// the one the operator picked for this session if still there, else the
// first in the factory's order (its preference). Continuing a task (a
// folder opened in a listing) runs that task's command instead.

export interface HandlerCommand {
  id: string
  handler?: string | null
}

/** The session's commands with handler, in the factory's order. */
export function handlerCommands<C extends HandlerCommand>(commands: readonly C[], handler: string): C[] {
  return commands.filter((c) => c.handler === handler)
}

/** The one to run: the picked one while the session has it, else the first. */
export function chooseHandlerCommand<C extends HandlerCommand>(candidates: readonly C[], picked: string | null): C | undefined {
  return candidates.find((c) => c.id === picked) ?? candidates[0]
}

// The operator's pick per session and handler, kept in this browser.
const key = (sessionId: string, handler: string) => `logos.console.handlerCommand.${sessionId}.${handler}`

export function readPicked(sessionId: string, handler: string): string | null {
  try {
    return localStorage.getItem(key(sessionId, handler))
  } catch {
    return null
  }
}

export function savePicked(sessionId: string, handler: string, id: string) {
  try {
    localStorage.setItem(key(sessionId, handler), id)
  } catch {
    // Kept for the page only.
  }
}
