import { describe, expect, test } from "vitest"
import { FOLLOWING, newestMoved, selectTask, selectedTask, stepTask, type ConsoleTask } from "./console-selection"

const task = (id: string, parts = 0, state = "DONE"): ConsoleTask => ({ id, parts, state })
const tasks = [task("a"), task("b"), task("c", 1, "RUNNING")]

describe("which task the console shows", () => {
  test("it follows the newest until another is picked", () => {
    expect(selectedTask(tasks, FOLLOWING)?.id).toBe("c")
    expect(selectedTask([...tasks, task("d")], FOLLOWING)?.id).toBe("d")
    expect(selectedTask([], FOLLOWING)).toBeNull()
    const sel = selectTask(tasks, "a")
    expect(selectedTask([...tasks, task("d")], sel)?.id).toBe("a")
    // Picking the newest is following it again.
    expect(selectTask(tasks, "c")).toEqual(FOLLOWING)
  })

  test("a picked task that is gone gives way to the newest", () => {
    expect(selectedTask(tasks, { pinned: "x", seen: null })?.id).toBe("c")
  })

  test("↑ and ↓ step through the list, and stop at its ends", () => {
    expect(selectedTask(tasks, stepTask(tasks, FOLLOWING, -1))?.id).toBe("b")
    expect(stepTask(tasks, FOLLOWING, 1)).toEqual(FOLLOWING)
    const first = selectTask(tasks, "a")
    expect(selectedTask(tasks, stepTask(tasks, first, -1))?.id).toBe("a")
    expect(stepTask(tasks, selectTask(tasks, "b"), 1)).toEqual(FOLLOWING)
  })

  test("while another is picked, the newest moving on is told: more of it, or a new one", () => {
    const sel = selectTask(tasks, "a")
    expect(newestMoved(tasks, sel)).toBeNull()
    expect(newestMoved([task("a"), task("b"), task("c", 2, "RUNNING")], sel)).toMatchObject({ task: { id: "c" }, isNew: false })
    expect(newestMoved([task("a"), task("b"), task("c", 1, "DONE")], sel)).toMatchObject({ isNew: false })
    expect(newestMoved([...tasks, task("d")], sel)).toMatchObject({ task: { id: "d" }, isNew: true })
    expect(newestMoved([...tasks, task("d")], FOLLOWING)).toBeNull()
  })
})
