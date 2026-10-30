import { describe, expect, test } from "vitest"
import { SESSION_MARK_COLORS, SESSION_MARK_STYLES, markRowClass } from "./session-mark-colors"

describe("session mark colors and styles", () => {
  test("36 colors and the styles, under ids core accepts", () => {
    expect(SESSION_MARK_COLORS).toHaveLength(36)
    // core/pkg/models/minion_session_mark.go: SessionMarkStyles.
    expect(SESSION_MARK_STYLES.map((s) => s.id)).toEqual([
      "haxor",
      "rainbow",
      "amber-crt",
      "synthwave",
      "glitch",
      "on-fire",
      "gold",
      "ghost",
    ])
  })

  test("a row's classes: a color's tint, a style's look, nothing for unknown", () => {
    expect(markRowClass("teal-medium")).toContain("bg-(color:--mark-row)")
    expect(markRowClass("rainbow")).toBe("mark-fx mark-fx-rainbow mark-fx-ink")
    expect(markRowClass("ghost")).toBe("mark-fx mark-fx-ghost")
    expect(markRowClass("disco")).toBeUndefined()
    expect(markRowClass(null)).toBeUndefined()
  })
})
