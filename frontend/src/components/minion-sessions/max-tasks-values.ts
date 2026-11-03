// How many tasks a minion runs at once (docs: contracts/minion-messages/
// tasks-and-results, Max tasks): the limit as an operator types it, for a
// session (max-tasks.tsx) and a build (the build form).

/** The most a limit may be (protocol.MaxTasksLimit). */
export const MAX_TASKS_LIMIT = 64

/** A limit as typed: a whole number 0 to MAX_TASKS_LIMIT, or why not. */
export function parseMaxTasks(text: string): { ok: true; value: number } | { ok: false; error: string } {
  const t = text.trim()
  if (!/^\d+$/.test(t)) return { ok: false, error: "A whole number" }
  const n = Number(t)
  if (n > MAX_TASKS_LIMIT) return { ok: false, error: `${MAX_TASKS_LIMIT} at most` }
  return { ok: true, value: n }
}

/** A build's limit where the operator sets none (protocol.DefaultMaxTasks). */
export const DEFAULT_MAX_TASKS = 5
