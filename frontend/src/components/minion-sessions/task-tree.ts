// A session's tasks as its console's list shows them: each composite task
// with its subtasks right under it, a tree's lines joining them as a
// process list's tree is drawn (process-list-view.tsx). Composites do not
// nest (docs: contracts/task-outsourcing), so a tree is a composite task
// and its subtasks.

/** What a task is to the tree. */
export interface TreeTask {
  id: string
  parentId?: string | null
}

/** A task's place in the tree: what its lines draw. */
export interface TaskTreePlace {
  /** A subtask: under its composite task, its corner in that task's column. */
  sub: boolean
  /** A subtask, the last of its composite's listed: an elbow (└), else a tee (├). */
  last: boolean
  /**
   * A subtask, the first of its composite's listed when the list does not
   * hold that task (a search, the important ones alone, a page not loaded):
   * its corner joins nothing above.
   */
  hook: boolean
  /** A composite task with subtasks listed under it: a stem down to them. */
  stem: boolean
}

/** A task as the list shows it, with its place in the tree. */
export interface TaskTreeRow<T extends TreeTask = TreeTask> extends TaskTreePlace {
  task: T
}

/**
 * Tasks as the list shows them: in their order, but for subtasks, each
 * composite task's right under it, in theirs; those of a composite task
 * the list does not hold together, where the first of them is.
 */
export function taskTree<T extends TreeTask>(tasks: readonly T[]): TaskTreeRow<T>[] {
  // A subtask's composite task is one of these; a subtask never is.
  const tops = new Set(tasks.filter((t) => !t.parentId).map((t) => t.id))
  const subs = new Map<string, T[]>()
  for (const t of tasks) {
    if (!t.parentId) continue
    const list = subs.get(t.parentId)
    if (list) list.push(t)
    else subs.set(t.parentId, [t])
  }
  const rows: TaskTreeRow<T>[] = []
  const under = (parent: string, hooked: boolean) => {
    const list = subs.get(parent) ?? []
    list.forEach((task, i) => rows.push({ task, sub: true, last: i === list.length - 1, hook: hooked && i === 0, stem: false }))
  }
  // Composite tasks not held, whose subtasks are in already.
  const placed = new Set<string>()
  for (const t of tasks) {
    if (!t.parentId) {
      rows.push({ task: t, sub: false, last: false, hook: false, stem: subs.has(t.id) })
      under(t.id, false)
    } else if (!tops.has(t.parentId) && !placed.has(t.parentId)) {
      placed.add(t.parentId)
      under(t.parentId, true)
    }
  }
  return rows
}

/**
 * The day headings over a list's rows (taskTree), by each row: a day's
 * label over the first task of the day, else null. None comes between a
 * composite task and its subtasks, whatever their day: it would cut the
 * tree's lines.
 */
export function dayHeadings<T extends TreeTask>(
  rows: readonly TaskTreeRow<T>[],
  dayOf: (t: T) => { key: string; label: string },
): (string | null)[] {
  let last: string | null = null
  return rows.map((r) => {
    if (r.sub && !r.hook) return null
    const day = dayOf(r.task)
    if (day.key === last) return null
    last = day.key
    return day.label
  })
}
