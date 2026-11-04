import { describe, expect, it } from "vitest"
import {
  COMPLETED_KEYS,
  LIGHTNING,
  RECALLED_KEYS,
  LINE_CHARGE_FROM,
  alongPoints,
  arcGenerations,
  arcSet,
  arcShape,
  chargeArc,
  chargedKey,
  editOf,
  lineChargeRate,
  strikeSet,
  spanLetters,
  sweepDelay,
  sweptPoints,
  STRIKE_KEYS,
  TYPED_KEYS,
  between,
  bolt,
  brightness,
  burst,
  jagged,
  mayFlash,
  readPromptFx,
  seeded,
  stepSpark,
} from "./prompt-fx-model"

describe("prompt effects", () => {
  it("reads a stored effect: one this version does not know is none", () => {
    expect(readPromptFx("lightning")).toBe("lightning")
    expect(readPromptFx("storm")).toBeNull()
    expect(readPromptFx(null)).toBeNull()
    expect(readPromptFx(undefined)).toBeNull()
  })

  it("draws the same at random from the same seed", () => {
    const a = seeded(7)
    const b = seeded(7)
    const xs = Array.from({ length: 5 }, () => a())
    expect(Array.from({ length: 5 }, () => b())).toEqual(xs)
    expect(xs.every((x) => x >= 0 && x < 1)).toBe(true)
    expect(between(() => 0.5, 10, 20)).toBe(15)
  })
})

describe("an edit", () => {
  it("is a key typed or deleted, text pasted or cut, or a selection replaced", () => {
    expect(editOf("ls", "ls ", 3)).toEqual({ start: 2, removed: 0, inserted: 1 })
    expect(editOf("ls /e", "ls /", 4)).toEqual({ start: 4, removed: 1, inserted: 0 })
    expect(editOf("ab", "abxyz", 5)).toEqual({ start: 2, removed: 0, inserted: 3 })
    expect(editOf("abcd", "ad", 1)).toEqual({ start: 1, removed: 2, inserted: 0 })
    expect(editOf("abcd", "aXYZd", 4)).toEqual({ start: 1, removed: 2, inserted: 3 })
    expect(editOf("same", "same", 2)).toMatchObject({ removed: 0, inserted: 0 })
  })

  it("is placed by the caret in a run of the same letter", () => {
    // An a typed at the start of "aa": the common start says the end.
    expect(editOf("aa", "aaa", 1)).toEqual({ start: 0, removed: 0, inserted: 1 })
    expect(editOf("xaay", "xay", 1)).toEqual({ start: 1, removed: 1, inserted: 0 })
    // A caret that does not fit: the common start.
    expect(editOf("ab", "abc", 0)).toEqual({ start: 2, removed: 0, inserted: 1 })
  })
})

describe("a span of text", () => {
  // Monospace, 7 pixels a letter, from x 10.
  const span = (text: string, left = 0, right = 1000) => ({ text, xs: Array.from({ length: text.length + 1 }, (_, i) => 10 + 7 * i), y: 20, left, right })

  it("lands effects on its letters, not its blanks, at least so far apart", () => {
    expect(spanLetters(span("ab c"), 0)).toEqual([
      { x: 13.5, at: 3.5 / 28 },
      { x: 20.5, at: 10.5 / 28 },
      { x: 34.5, at: 24.5 / 28 },
    ])
    expect(spanLetters(span("abcdef"), 14).map((l) => l.x)).toEqual([13.5, 27.5, 41.5])
    // What the input scrolled out of sight gets none.
    expect(spanLetters(span("abcdef", 20, 40), 0).map((l) => l.x)).toEqual([20.5, 27.5, 34.5])
  })

  it("is swept along, start to end, quickly however long", () => {
    expect(sweepDelay(0, 10, 0.01, 0.3)).toBe(0)
    expect(sweepDelay(1, 10, 0.01, 0.3)).toBeCloseTo(0.1)
    expect(sweepDelay(1, 1000, 0.01, 0.3)).toBe(0.3)
    expect(sweepDelay(0.5, 1000, 0.01, 0.3)).toBe(0.15)
  })
})

describe("a bolt", () => {
  const a = { x: 100, y: 0 }
  const b = { x: 120, y: 300 }

  it("goes from one end to the other, crackling finer each generation", () => {
    const path = jagged(a, b, 5, 0.3, seeded(1))
    expect(path).toHaveLength(33)
    expect(path[0]).toEqual(a)
    expect(path[32]).toEqual(b)
  })

  it("wanders aside no further than its roughness lets it", () => {
    // Each generation moves a middle at most half the last's: all together
    // less than twice the first, roughness times the length.
    const length = Math.hypot(b.x - a.x, b.y - a.y)
    const limit = 2 * 0.3 * length
    for (let seed = 1; seed <= 20; seed++) {
      for (const p of jagged(a, b, 6, 0.3, seeded(seed))) {
        // Its distance from the straight line from a to b.
        const off = Math.abs((b.x - a.x) * (a.y - p.y) - (a.x - p.x) * (b.y - a.y)) / length
        expect(off).toBeLessThan(limit)
      }
    }
  })

  it("forks within its shape's branches, each from a point on its path", () => {
    for (let seed = 1; seed <= 30; seed++) {
      const { main, branches } = bolt(a, b, LIGHTNING.strike.shape, seeded(seed))
      expect(branches.length).toBeGreaterThanOrEqual(LIGHTNING.strike.shape.branches[0])
      expect(branches.length).toBeLessThanOrEqual(LIGHTNING.strike.shape.branches[1])
      for (const branch of branches) expect(main).toContainEqual(branch[0])
    }
  })
})

describe("creases", () => {
  it("stay sharper as they get finer the higher the decay: a longer, more jagged path", () => {
    const pathLength = (points: { x: number; y: number }[]) => points.slice(1).reduce((n, p, i) => n + Math.hypot(p.x - points[i].x, p.y - points[i].y), 0)
    for (let seed = 1; seed <= 10; seed++) {
      const smooth = jagged({ x: 0, y: 0 }, { x: 300, y: 0 }, 6, 0.05, seeded(seed), 0.4)
      const creased = jagged({ x: 0, y: 0 }, { x: 300, y: 0 }, 6, 0.05, seeded(seed), 0.75)
      expect(pathLength(creased)).toBeGreaterThan(pathLength(smooth))
    }
  })
})

describe("a set of arcs across a line", () => {
  it("has two, three along a long line, up to two more as the charge builds", () => {
    expect(arcSet(80, 0, seeded(1))).toHaveLength(2)
    expect(arcSet(400, 0, seeded(1))).toHaveLength(3)
    expect(arcSet(400, 1, seeded(1))).toHaveLength(5)
    expect(arcSet(80, 0.5, seeded(1))).toHaveLength(3)
  })

  it("has its first the brightest and widest, at once; each other its own", () => {
    for (let seed = 1; seed <= 10; seed++) {
      const [first, ...rest] = arcSet(300, 1, seeded(seed))
      expect(first).toMatchObject({ alpha: 1, width: 1.2, dy: 0, after: 0 })
      for (const a of rest) {
        expect(a.alpha).toBeLessThan(1)
        expect(a.width).toBeLessThan(first.width)
        expect(Math.abs(a.dy)).toBeLessThanOrEqual(4)
        expect(a.decay).toBeGreaterThanOrEqual(0.45)
        expect(a.decay).toBeLessThanOrEqual(0.7)
      }
      // Not all alike.
      expect(new Set(rest.map((a) => a.wander)).size).toBe(rest.length)
    }
  })

  it("keeps each arc within its wander, however long the line", () => {
    for (const length of [10, 40, 120, 400, 900]) {
      for (let seed = 1; seed <= 10; seed++) {
        for (const style of arcSet(length, 1, seeded(seed))) {
          const shape = arcShape(style, length)
          for (const p of jagged({ x: 0, y: 20 }, { x: length, y: 20 }, shape.generations, shape.roughness, seeded(seed + 100), shape.decay)) {
            expect(Math.abs(p.y - 20)).toBeLessThan(style.wander + 1e-9)
          }
        }
      }
    }
  })

  it("crackles as finely along a long line, wandering further", () => {
    const style = arcSet(100, 0, seeded(2))[0]
    expect(arcShape(style, 600).generations).toBeGreaterThan(arcShape(style, 40).generations)
    expect(arcSet(800, 0, seeded(3))[0].wander).toBeGreaterThan(arcSet(40, 0, seeded(3))[0].wander)
  })
})

describe("the charge", () => {
  it("makes a typed key's bolt a little more the faster keys come, never more bolts", () => {
    const cold = chargedKey(0)
    expect(cold.sparks.count).toEqual(LIGHTNING.typed.sparks.count)
    expect(cold.rise).toEqual(LIGHTNING.typed.rise)
    const hot = chargedKey(1)
    expect(hot.sparks.count[1]).toBe(Math.round(1.6 * LIGHTNING.typed.sparks.count[1]))
    expect(hot.rise[1]).toBeCloseTo(1.3 * LIGHTNING.typed.rise[1])
    expect(hot.shape.branches[1]).toBeGreaterThan(cold.shape.branches[1])
    expect(hot.shape.decay).toBeGreaterThan(cold.shape.decay ?? 0.5)
  })

  it("strikes a line in two bolts, up to four as it builds, the first the main one", () => {
    expect(strikeSet(0, seeded(1))).toHaveLength(2)
    expect(strikeSet(1, seeded(1))).toHaveLength(4)
    const [main, ...rest] = strikeSet(1, seeded(4))
    expect(main).toMatchObject({ width: 1, alpha: 1, after: 0 })
    for (const b of rest) {
      expect(b.width).toBeLessThan(1)
      expect(b.after).toBeGreaterThan(0)
    }
  })

  it("crackles across the line once it is up a little, more the higher", () => {
    expect(lineChargeRate(LINE_CHARGE_FROM - 0.01)).toBe(0)
    expect(lineChargeRate(LINE_CHARGE_FROM)).toBe(2)
    expect(lineChargeRate(1)).toBe(16)
    expect(lineChargeRate(0.6)).toBeGreaterThan(lineChargeRate(0.3))
  })

  it("crackles over stretches of the text in sight, longer the higher it is", () => {
    // Monospace, 7 pixels a letter, from x 10: 40 letters.
    const span = (left: number, right: number) => ({ text: "x".repeat(40), xs: Array.from({ length: 41 }, (_, i) => 10 + 7 * i), y: 20, left, right })
    for (let seed = 1; seed <= 30; seed++) {
      const arc = chargeArc(span(0, 1000), 1, seeded(seed))!
      expect(arc.from).toBeGreaterThanOrEqual(10)
      expect(arc.to).toBeLessThanOrEqual(290)
      expect(arc.to - arc.from).toBeLessThanOrEqual(60)
      // What the input scrolled away gets none.
      const clipped = chargeArc(span(100, 160), 1, seeded(seed))!
      expect(clipped.from).toBeGreaterThanOrEqual(100)
      expect(clipped.to).toBeLessThanOrEqual(160)
    }
    const longest = (charge: number) => chargeArc(span(0, 1000), charge, () => 0.999)!
    expect(longest(1).to - longest(1).from).toBeGreaterThan(longest(0).to - longest(0).from)
    expect(chargeArc({ text: "", xs: [10], y: 20, left: 0, right: 1000 }, 1, seeded(1))).toBeNull()
  })
})

describe("a completion's arc", () => {
  it("crackles out", () => {
    expect(brightness(COMPLETED_KEYS, 0.38)).toBeCloseTo(0.3)
    expect(brightness(COMPLETED_KEYS, 1)).toBe(0)
  })
})

describe("an arc along a long line", () => {
  it("crackles as finely as a short one's, its stretches about 3 pixels", () => {
    expect(arcGenerations(40)).toBe(4)
    expect(arcGenerations(10)).toBe(3)
    expect(arcGenerations(600)).toBe(7)
    for (const length of [24, 48, 96, 192, 384]) {
      const stretch = length / 2 ** arcGenerations(length)
      expect(stretch).toBeGreaterThan(2)
      expect(stretch).toBeLessThan(4.5)
    }
  })

  it("throws sparks along its way, as many as its length", () => {
    expect(alongPoints({ x: 0, y: 0 }, { x: 100, y: 0 }, 30)).toEqual([
      { x: 30, y: 0 },
      { x: 60, y: 0 },
      { x: 90, y: 0 },
    ])
    expect(alongPoints({ x: 100, y: 5 }, { x: 0, y: 5 }, 50)).toEqual([{ x: 50, y: 5 }])
    expect(alongPoints({ x: 0, y: 0 }, { x: 20, y: 0 }, 30)).toEqual([])
    expect(alongPoints({ x: 0, y: 0 }, { x: 600, y: 0 }, 30)).toHaveLength(19)
  })
})

describe("a recalled line's arc", () => {
  it("is drawn out along the line over its sweep, from its first stretch to all of it", () => {
    expect(sweptPoints(17, 0, 0.35)).toBe(2)
    expect(sweptPoints(17, -0.01, 0.35)).toBe(2)
    expect(sweptPoints(17, 0.175, 0.35)).toBe(9)
    expect(sweptPoints(17, 0.35, 0.35)).toBe(17)
    expect(sweptPoints(17, 0.9, 0.35)).toBe(17)
    expect(sweptPoints(17, 0, 0)).toBe(17)
  })

  it("goes out", () => {
    expect(brightness(RECALLED_KEYS, 0.3)).toBe(1)
    expect(brightness(RECALLED_KEYS, 1)).toBe(0)
  })
})

describe("brightness", () => {
  it("joins its keys, out at the end", () => {
    expect(brightness(TYPED_KEYS, 0)).toBe(1)
    expect(brightness(TYPED_KEYS, 0.2)).toBeCloseTo(0.925)
    expect(brightness(TYPED_KEYS, 1)).toBe(0)
    expect(brightness(TYPED_KEYS, 2)).toBe(0)
  })

  it("flickers a strike: dark between its two strokes", () => {
    expect(brightness(STRIKE_KEYS, 0.2)).toBeLessThan(0.2)
    expect(brightness(STRIKE_KEYS, 0.3)).toBe(1)
  })
})

describe("sparks", () => {
  it("burst upward, as many as asked, and fall", () => {
    const sparks = burst({ x: 0, y: 0 }, LIGHTNING.typed.sparks, seeded(3))
    expect(sparks.length).toBeGreaterThanOrEqual(3)
    expect(sparks.length).toBeLessThanOrEqual(5)
    for (const s of sparks) expect(s.vy).toBeLessThan(0)
    const s = sparks[0]
    const vy = s.vy
    stepSpark(s, 0.1)
    expect(s.vy).toBeGreaterThan(vy)
    expect(s.age).toBeCloseTo(0.1)
  })

  it("burst downward when deleted", () => {
    for (const s of burst({ x: 0, y: 0 }, LIGHTNING.deleted.sparks, seeded(4))) expect(s.vy).toBeGreaterThan(0)
  })
})

describe("flashes", () => {
  it("never come closer than their gap: not three a second", () => {
    expect(mayFlash(1000, null)).toBe(true)
    expect(mayFlash(1300, 1000)).toBe(false)
    expect(mayFlash(1400, 1000)).toBe(true)
  })
})
