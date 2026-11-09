import { describe, expect, it } from "vitest"
import { viaNote, viaShort } from "./via-values"

describe("viaShort", () => {
  it("says nothing for a session a channel brings", () => {
    expect(viaShort([])).toBe("")
  })
  it("names the nearest delegate, and counts the ones behind it", () => {
    expect(viaShort([{ number: 3 }])).toBe("via #3")
    expect(viaShort([{ number: 3 }, { number: 2 }, { number: 7 }])).toBe("via #3 +2")
  })
})

describe("viaNote", () => {
  it("says nothing for a session a channel brings", () => {
    expect(viaNote([], "2026-11-09T10:41:02Z")).toBe("")
  })
  it("names every delegate, nearest first", () => {
    expect(viaNote([{ number: 3 }])).toBe("Through session #3's delegate task")
    expect(viaNote([{ number: 3 }, { number: 2 }, { number: 7 }])).toBe("Through session #3's delegate task, then #2's, then #7's")
  })
  it("says since when", () => {
    expect(viaNote([{ number: 3 }], "2026-11-09T10:41:02Z")).toMatch(/^Through session #3's delegate task, since 2026\.11\.09 \d\d:41:02$/)
  })
})
