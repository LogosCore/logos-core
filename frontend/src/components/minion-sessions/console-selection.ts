// Which task a session's console shows. It follows the newest unless the
// operator picks an older one; then it stays on that one, and when the
// newest moves on (a new task, more output, an end) a hint says so and
// takes them back to it. Tasks are oldest first, as the list shows them.

export interface ConsoleTask {
  id: string
  parts: number
  state: string
}

export interface ConsoleSelection {
  /** The task picked; null: following the newest. */
  pinned: string | null
  /** How the newest stood when the operator picked another, to tell when it moves on. */
  seen: string | null
}

export const FOLLOWING: ConsoleSelection = { pinned: null, seen: null }

const mark = (t: ConsoleTask) => `${t.id}:${t.parts}:${t.state}`

/** The task shown: the one picked while it is there, else the newest. */
export function selectedTask<T extends ConsoleTask>(tasks: readonly T[], sel: ConsoleSelection): T | null {
  const picked = sel.pinned !== null ? tasks.find((t) => t.id === sel.pinned) : undefined
  return picked ?? tasks.at(-1) ?? null
}

/** Picks a task: the newest is following it. */
export function selectTask(tasks: readonly ConsoleTask[], id: string): ConsoleSelection {
  const newest = tasks.at(-1)
  if (!newest || newest.id === id) return FOLLOWING
  return { pinned: id, seen: mark(newest) }
}

/** Picks the task before or after the one shown (↑, ↓). */
export function stepTask(tasks: readonly ConsoleTask[], sel: ConsoleSelection, by: -1 | 1): ConsoleSelection {
  const at = tasks.findIndex((t) => t.id === selectedTask(tasks, sel)?.id)
  const next = tasks[Math.min(tasks.length - 1, Math.max(0, at + by))]
  return next ? selectTask(tasks, next.id) : sel
}

/** What happened to the newest since another was picked: nothing, a new task, or more of it. */
export function newestMoved<T extends ConsoleTask>(tasks: readonly T[], sel: ConsoleSelection): { task: T; isNew: boolean } | null {
  const newest = tasks.at(-1)
  if (sel.pinned === null || sel.seen === null || !newest || newest.id === sel.pinned) return null
  if (mark(newest) === sel.seen) return null
  return { task: newest, isNew: !sel.seen.startsWith(`${newest.id}:`) }
}
