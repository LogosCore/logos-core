import { describe, expect, it } from "vitest"
import { HIGHLIGHT_TONES, highlightsKey, MAX_HIGHLIGHTS, splitHighlighted, toggleHighlight } from "./log-highlights"

const pairs = [
  { path: "stage", text: "unpack" },
  { path: "req.method", text: "GET" },
  { path: "err", text: '"too short"' },
  { path: "n", text: "3" },
]

describe("toggleHighlight", () => {
  it("adds a field last, and takes it out again", () => {
    expect(toggleHighlight([], "stage")).toEqual(["stage"])
    expect(toggleHighlight(["stage"], "req.method")).toEqual(["stage", "req.method"])
    expect(toggleHighlight(["stage", "req.method"], "stage")).toEqual(["req.method"])
  })

  it("adds none past one a tone: as many as can be told apart", () => {
    const full = Array.from({ length: MAX_HIGHLIGHTS }, (_, i) => `f${i}`)
    expect(MAX_HIGHLIGHTS).toBe(HIGHLIGHT_TONES.length)
    expect(toggleHighlight(full, "more")).toEqual(full)
    expect(toggleHighlight(full, "f0")).toEqual(full.slice(1))
  })
})

describe("splitHighlighted", () => {
  it("takes the highlighted fields up front, in the highlights' order with their tones, the rest in the record's", () => {
    const { picked, rest } = splitHighlighted(pairs, ["err", "stage"])
    expect(picked).toEqual([
      { path: "err", text: '"too short"', tone: 0 },
      { path: "stage", text: "unpack", tone: 1 },
    ])
    expect(rest.map((p) => p.path)).toEqual(["req.method", "n"])
  })

  it("keeps a field's tone on a line that lacks the one before it", () => {
    // Tones follow the highlights, not what a line has: req.method stays the second's.
    const { picked, rest } = splitHighlighted(pairs, ["gone", "req.method"])
    expect(picked).toEqual([{ path: "req.method", text: "GET", tone: 1 }])
    expect(rest.map((p) => p.path)).toEqual(["stage", "err", "n"])
  })

  it("leaves a line as it is with none highlighted", () => {
    expect(splitHighlighted(pairs, [])).toEqual({ picked: [], rest: pairs })
  })
})

describe("highlightsKey", () => {
  it("is the module's kind: modules of a kind log the same fields", () => {
    expect(highlightsKey({ type: "channel", name: "http" })).toBe("channel:http")
  })
})
