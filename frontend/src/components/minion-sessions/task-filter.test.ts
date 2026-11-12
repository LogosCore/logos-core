import { describe, expect, test } from "vitest"
import { emptyListText } from "./task-filter"

describe("an empty task list says why", () => {
  test("by what narrowed it", () => {
    expect(emptyListText({ search: "", important: false })).toBe("No tasks yet.")
    expect(emptyListText({ search: "  ", important: false })).toBe("No tasks yet.")
    expect(emptyListText({ search: " who ", important: false })).toBe("No tasks match “who”.")
    expect(emptyListText({ search: "", important: true })).toBe("No important tasks.")
    expect(emptyListText({ search: "who", important: true })).toBe("No important tasks match “who”.")
  })
})
