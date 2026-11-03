import { describe, expect, it } from "vitest"
import { feedbackLevel, feedbackSummary, firstLine, toggled } from "./feedback-values"

describe("feedback", () => {
  it("reads a level, a later one as a warning", () => {
    expect(feedbackLevel("error")).toBe("error")
    expect(feedbackLevel("warning")).toBe("warning")
    expect(feedbackLevel("critical")).toBe("warning")
  })
  it("says the unread counts", () => {
    expect(feedbackSummary(3, 1)).toBe("3 errors, 1 warning")
    expect(feedbackSummary(1, 0)).toBe("1 error")
    expect(feedbackSummary(0, 2)).toBe("2 warnings")
    expect(feedbackSummary(0, 0)).toBe("")
  })
})

describe("a task's feedback", () => {
  it("gives a one-line message whole", () => {
    expect(firstLine("disk full")).toEqual({ line: "disk full", more: 0 })
  })
  it("counts the lines after the first, not a trailing newline", () => {
    expect(firstLine("upload failed\nretry 1\nretry 2\n")).toEqual({ line: "upload failed", more: 2 })
  })
  it("opens an entry, and shuts it again", () => {
    const one = toggled(new Set(), "a")
    expect([...one]).toEqual(["a"])
    expect([...toggled(one, "a")]).toEqual([])
  })
})
