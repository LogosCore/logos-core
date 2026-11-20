import { describe, expect, test } from "vitest"
import { emptyListText } from "./task-filter"

describe("an empty task list says why", () => {
  test("by what narrowed it", () => {
    expect(emptyListText({ search: "", important: false, composite: "" })).toBe("No tasks yet.")
    expect(emptyListText({ search: "  ", important: false, composite: "" })).toBe("No tasks yet.")
    expect(emptyListText({ search: " who ", important: false, composite: "" })).toBe("No tasks match “who”.")
    expect(emptyListText({ search: "", important: true, composite: "" })).toBe("No important tasks.")
    expect(emptyListText({ search: "who", important: true, composite: "" })).toBe("No important tasks match “who”.")
    expect(emptyListText({ search: "", important: false, composite: "p" })).toBe("No such composite task.")
    expect(emptyListText({ search: "who", important: false, composite: "p" })).toBe("Nothing of the composite task and its subtasks matches.")
  })
})
