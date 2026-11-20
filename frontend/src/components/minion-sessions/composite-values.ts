import type { MinionTaskFieldsFragment as Task } from "@/graphql/gql/graphql"

// A composite command's task (docs: contracts/task-outsourcing): its
// factory's code runs the session's other commands as its subtasks, before
// and after the command's own op on the minion. While the code decides, the
// task waits in core (COMPOSING); its own op, when out, is QUEUED, SENT and
// RUNNING as any task. What the console says of one.

export type Composite = NonNullable<Task["composite"]>

/** The state a composite task waits in core in. */
export const COMPOSING = "COMPOSING"

/** A task's states once it ended (task-actions' FINISHED, which uses this file). */
const ENDED = new Set(["DONE", "FAILED", "CANCELLED"])

/** Where a composite task stands, in words: what it waits for; null for one that ended, or another task. */
export function compositeStands(t: Pick<Task, "state" | "composite">): string | null {
  const c = t.composite
  if (!c || ENDED.has(t.state)) return null
  switch (c.phase) {
    case "finishing":
      return "finishing: what is under way stops first"
    case "stopping":
      return "stopping: what is under way stops, then its factory may clean up"
    case "cleanup":
      return "cleaning up after the stop"
    case "closed":
      return "its session closed: its factory is called no more"
  }
  return t.state === COMPOSING ? "its factory decides" : "its own op is out"
}

/** A composite task's phase in a word or two, for its state's line while its own op is out (COMPOSING's says it whole); null for none. */
export function phaseWord(t: Pick<Task, "state" | "composite">): string | null {
  const phase = t.composite?.phase
  if (!phase || t.state === COMPOSING || ENDED.has(t.state)) return null
  return phase === "cleanup" ? "cleaning up" : phase === "closed" ? "its session closed" : phase
}

/** How a composite task's own op stands, in words; null for a command with no op of its own. */
export function ownOpText(c: Pick<Composite, "ownOp" | "ownEnd">): string | null {
  switch (c.ownOp) {
    case "not_run":
      return "not run"
    case "out":
      return "out on the minion"
    case "ended": {
      const e = c.ownEnd
      if (!e) return "ended"
      switch (e.state) {
        case "done":
          return e.ok ? (e.exit != null ? `done, exit ${e.exit}` : "done") : e.exit != null ? `ended not ok, exit ${e.exit}` : "ended not ok"
        case "refused":
          return `refused: ${e.err ?? "it failed core's checks"}`
        case "failed":
          return `failed: ${e.err ?? "core ended it"}`
        case "cancelled":
          return "cancelled"
      }
      return e.state
    }
  }
  return null
}

/** Whether a stop now ends a composite task at once (core's StopTask): an operator stopped it already, its cleanup runs, or its session closed. */
export function stopsAtOnce(t: Pick<Task, "composite">): boolean {
  const phase = t.composite?.phase
  return phase === "stopping" || phase === "cleanup" || phase === "closed"
}

/** A subtask's place, in words: its line as its factory named it, a cleanup one said so. */
export function subtaskText(t: Pick<Task, "parentId" | "subtaskAs" | "cleanup">): string | null {
  if (!t.parentId) return null
  const as = t.subtaskAs?.trim()
  if (t.cleanup) return as ? `cleanup: ${as}` : "cleanup"
  return as || null
}
