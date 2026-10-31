import { describe, expect, test } from "vitest"
import { findMatches, segments } from "./output-find"

describe("finding text in a task's output", () => {
  test("every place it occurs, ignoring case, not overlapping, at most max", () => {
    expect(findMatches("Error: error ERROR", "error")).toEqual([0, 7, 13])
    expect(findMatches("aaaa", "aa")).toEqual([0, 2])
    expect(findMatches("abc", "")).toEqual([])
    expect(findMatches("abc", "x")).toEqual([])
    expect(findMatches("a".repeat(10), "a", 3)).toEqual([0, 1, 2])
  })

  test("the output in pieces around the matches", () => {
    const text = "one two one"
    expect(segments(text, findMatches(text, "one"), 3)).toEqual([
      { text: "one", match: 0 },
      { text: " two " },
      { text: "one", match: 1 },
    ])
    expect(segments("abc", [], 1)).toEqual([{ text: "abc" }])
  })
})
