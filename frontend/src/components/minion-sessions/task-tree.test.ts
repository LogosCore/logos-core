import { describe, expect, it } from "vitest"
import { dayHeadings, taskTree, type TreeTask } from "./task-tree"

// a, a composite task, its subtasks a1 and a2 queued after it; b, a plain
// task queued meanwhile, between them; c after them all.
const tasks: TreeTask[] = [{ id: "a" }, { id: "a1", parentId: "a" }, { id: "b" }, { id: "a2", parentId: "a" }, { id: "c" }]

/** The rows as ids, each with its place: "sub", "last", "hook", "stem" as it is each. */
const drawn = (list: readonly TreeTask[]) =>
  taskTree(list).map((r) => [r.task.id, ...(["sub", "last", "hook", "stem"] as const).filter((k) => r[k])].join(" "))

describe("taskTree", () => {
  it("puts a composite task's subtasks right under it, in their order, the last one closing its line", () => {
    expect(drawn(tasks)).toEqual(["a stem", "a1 sub", "a2 sub last", "b", "c"])
  })

  it("draws no stem under a composite task with none of its subtasks listed", () => {
    expect(drawn([{ id: "a" }, { id: "b" }])).toEqual(["a", "b"])
  })

  it("keeps a composite task's subtasks together where the first is when it is not listed, the first hooked", () => {
    expect(drawn(tasks.slice(1))).toEqual(["a1 sub hook", "a2 sub last", "b", "c"])
    expect(drawn([{ id: "b" }, { id: "a2", parentId: "a" }])).toEqual(["b", "a2 sub last hook"])
  })

  it("lists every task once, a subtask of a subtask (composites do not nest) as one of a composite not listed", () => {
    const odd = [{ id: "a" }, { id: "a1", parentId: "a" }, { id: "x", parentId: "a1" }]
    expect(drawn(odd)).toEqual(["a stem", "a1 sub last", "x sub last hook"])
  })
})

describe("dayHeadings", () => {
  // Each task's day: a and its first subtask on Monday, the rest on Tuesday.
  const days: Record<string, string> = { a: "Mon", a1: "Mon", b: "Tue", a2: "Tue", c: "Tue" }
  const dayOf = (t: TreeTask) => ({ key: days[t.id], label: days[t.id] })

  it("heads each day's first task, never between a composite task and its subtasks", () => {
    // a, a1, a2, b, c: a2 is Tuesday's, but under a.
    expect(dayHeadings(taskTree(tasks), dayOf)).toEqual(["Mon", null, null, "Tue", null])
  })

  it("heads a hooked subtask as a task of its day", () => {
    // a1, a2 (a not listed), b, c.
    expect(dayHeadings(taskTree(tasks.slice(1)), dayOf)).toEqual(["Mon", null, "Tue", null])
  })
})
