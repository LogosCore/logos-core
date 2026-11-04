import { describe, expect, it } from "vitest"
import {
  BED_FROM,
  BED_TOP,
  CELL,
  COOL_MIN,
  FIELD,
  FIRE_TOP,
  HEAT_BUMP,
  INFERNO,
  bedLevel,
  burning,
  cooled,
  douse,
  emberLook,
  embers,
  fed,
  feed,
  fireColors,
  fuseAlong,
  fuseDuration,
  keyFlame,
  kindle,
  letterLook,
  newBlaze,
  pointAlong,
  recallDuration,
  rise,
  sourceLevel,
  stepEmber,
  throwSpacing,
  throwsUpTo,
} from "./prompt-fx-inferno-model"
import { seeded } from "./prompt-fx-model"
import { FIRE_DARK } from "./session-fire-model"

describe("heat", () => {
  it("builds as keys come fast, a little as they come slow, and cools in a couple of seconds", () => {
    let fast = 0
    for (let i = 0; i < 15; i++) fast = fed(fast, 0.1, HEAT_BUMP)
    expect(fast).toBeGreaterThan(0.75)
    let slow = 0
    for (let i = 0; i < 15; i++) slow = fed(slow, 0.5, HEAT_BUMP)
    expect(slow).toBeLessThan(0.3)
    expect(cooled(1, 2)).toBeLessThan(0.2)
    expect(fed(0.99, 0, 0.5)).toBe(1)
  })

  it("lights the bed once it is warm, at the top when it is hottest", () => {
    expect(bedLevel(BED_FROM - 0.01)).toBe(0)
    expect(bedLevel(BED_FROM)).toBeGreaterThan(0)
    expect(bedLevel(1)).toBe(BED_TOP)
  })

  it("feeds the bed as hot as the heat, flickering; nothing when cool", () => {
    const random = seeded(5)
    for (let i = 0; i < 50; i++) {
      const bed = sourceLevel(1, random)
      expect(bed).toBeLessThanOrEqual(BED_TOP)
      expect(bed).toBeGreaterThanOrEqual(BED_TOP - 3)
      expect(sourceLevel(0, random)).toBe(0)
    }
  })

  it("makes a typed key's flame taller, longer and wider as it builds", () => {
    const cold = keyFlame(0)
    const hot = keyFlame(1)
    expect(hot.level).toBeGreaterThan(cold.level)
    expect(hot.level).toBeLessThanOrEqual(FIRE_TOP)
    expect(hot.seconds).toBeGreaterThan(cold.seconds)
    expect(hot.half).toBeGreaterThan(cold.half)
  })
})

describe("the fire", () => {
  it("is cells about the On Fire mark's, with room over the line for its tallest puff", () => {
    expect(CELL).toBeLessThanOrEqual(2)
    // The line sits about 10 rows over the bottom.
    expect(FIELD / CELL).toBeGreaterThanOrEqual(Math.ceil(FIRE_TOP / COOL_MIN) + 10)
  })

  it("puffs and is gone: never higher than its heat over COOL_MIN rows, gone soon after its source", () => {
    for (let seed = 1; seed <= 10; seed++) {
      const b = newBlaze(30, 40)
      const random = seeded(seed)
      for (let i = 0; i < 6; i++) {
        kindle(b, 13, 16, 35, FIRE_TOP)
        rise(b, random)
        // Nothing over the tallest a puff can be.
        expect(b.heat.slice(0, (35 - Math.ceil(FIRE_TOP / COOL_MIN)) * 30).every((h) => h === 0)).toBe(true)
      }
      for (let i = 0; i < Math.ceil(FIRE_TOP / COOL_MIN) + 1; i++) rise(b, random)
      expect(burning(b)).toBe(false)
    }
  })

  it("climbs from its source, a row a step but where it drifts onto a column not yet taken", () => {
    const b = newBlaze(12, 8)
    feed(b, () => FIRE_TOP)
    rise(b, seeded(1))
    expect(b.heat.slice(7 * 12, 8 * 12).some((h) => h > 0)).toBe(true)
    expect(b.heat.slice(0, 4 * 12).every((h) => h === 0)).toBe(true)
  })

  it("is never hotter than its top, and dies out once its source is cut", () => {
    const b = newBlaze(12, 8)
    const random = seeded(2)
    for (let i = 0; i < 20; i++) {
      feed(b, () => FIRE_TOP + 10)
      rise(b, random)
    }
    expect(Math.max(...b.heat)).toBeLessThanOrEqual(FIRE_TOP)
    expect(burning(b)).toBe(true)
    for (let i = 0; i < 10; i++) {
      feed(b, () => 0)
      rise(b, random)
    }
    expect(burning(b)).toBe(false)
  })

  it("burns up from where it is kindled, never down", () => {
    const b = newBlaze(20, 30)
    const random = seeded(3)
    for (let i = 0; i < 12; i++) {
      kindle(b, 8, 11, 20, FIRE_TOP)
      rise(b, random)
    }
    // Above the kindled row: fire. Below it: none.
    expect(b.heat.slice(5 * 20, 20 * 20).some((h) => h > 0)).toBe(true)
    expect(b.heat.slice(21 * 20, 30 * 20).every((h) => h === 0)).toBe(true)
  })

  it("is kindled at least as hot as asked, inside it, not cooled where it is hotter", () => {
    const b = newBlaze(10, 10)
    kindle(b, -5, 2, 4, 20)
    expect([...b.heat.slice(40, 43)]).toEqual([20, 20, 20])
    expect(b.heat[43]).toBe(0)
    kindle(b, 0, 0, 4, 10)
    expect(b.heat[40]).toBe(20)
    kindle(b, 9, 50, 99, 99)
    expect(b.heat[9 * 10 + 9]).toBe(FIRE_TOP)
  })

  it("is put out by half when doused", () => {
    const b = newBlaze(4, 4)
    kindle(b, 0, 3, 2, 20)
    douse(b, 0.5)
    expect([...b.heat.slice(8, 12)]).toEqual([10, 10, 10, 10])
  })

  it("is colored see-through when cold, the ramp's last at its top", () => {
    const colors = fireColors(FIRE_DARK, FIRE_TOP)
    expect(colors).toHaveLength((FIRE_TOP + 1) * 4)
    expect(colors[3]).toBe(0)
    expect([...colors.slice(FIRE_TOP * 4)]).toEqual([...FIRE_DARK[FIRE_DARK.length - 1]])
  })
})

describe("embers", () => {
  const step = (kind: "spark" | "ash" | "smoke") => {
    const [e] = embers({ x: 0, y: 0 }, { kind, count: [1, 1], speed: [20, 20], spread: 0, life: [1, 1], size: [4, 4], temp: [1, 1] }, seeded(3))
    const random = seeded(4)
    for (let i = 0; i < 10; i++) stepEmber(e, 0.05, random)
    return e
  }

  it("rise as sparks, fall as ash", () => {
    expect(step("spark").y).toBeLessThan(-10)
    expect(step("ash").y).toBeGreaterThan(0)
  })

  it("look their age: a spark burns down and cools, smoke swells", () => {
    const spark = step("spark")
    expect(emberLook(spark).alpha).toBeLessThan(1)
    expect(emberLook(spark).temp).toBeLessThan(1)
    expect(emberLook(step("smoke")).size).toBeGreaterThan(4)
  })
})

describe("a line burning", () => {
  it("has its letters as typed, then glowing hot, burning out, gone: all at once", () => {
    const spec = INFERNO.burn
    expect(letterLook(-0.1, spec)).toEqual({ original: 1, hot: 0, temp: 0 })
    expect(letterLook(spec.catches / 2, spec)).toEqual({ original: 0.5, hot: 0.5, temp: 0.5 })
    const out = letterLook(spec.catches + spec.burnsOut / 2, spec)
    expect(out.original).toBe(0)
    expect(out.hot).toBeCloseTo(0.5)
    expect(letterLook(spec.catches + spec.burnsOut + 0.001, spec)).toEqual({ original: 0, hot: 0, temp: 0 })
  })

  it("flashes white-hot off the whole line, hotter than a key's puff, and quick: no longer than its letters take", () => {
    expect(INFERNO.burn.level).toBe(FIRE_TOP)
    expect(INFERNO.burn.level).toBeGreaterThan(keyFlame(1).level)
    expect(INFERNO.burn.seconds).toBeLessThanOrEqual(INFERNO.burn.catches + INFERNO.burn.burnsOut)
    expect(INFERNO.burn.catches + INFERNO.burn.burnsOut).toBeLessThan(0.25)
  })
})

describe("a fuse", () => {
  it("burns from one end to the other in its time", () => {
    const f = { from: { x: 100, y: 5 }, to: { x: 0, y: 5 }, duration: 0.2, age: 0 }
    expect(fuseAlong(f)).toBe(0)
    expect(fuseAlong({ ...f, age: 0.1 })).toBe(50)
    expect(fuseAlong({ ...f, age: 0.5 })).toBe(100)
    expect(pointAlong(f, 25)).toEqual({ x: 75, y: 5 })
    expect(fuseDuration(10)).toBe(0.08)
    expect(fuseDuration(5000)).toBe(0.3)
    expect(recallDuration(40)).toBeCloseTo(0.11)
    expect(recallDuration(600)).toBeCloseTo(0.25)
    expect(recallDuration(5000)).toBe(0.3)
  })

  it("throws its embers by the pixel: a long line as many as its length, a short word no fewer", () => {
    const sparks = INFERNO.recalled.older[0]
    const count = (length: number) => throwsUpTo(0, length, throwSpacing(sparks, length)).at.length
    const thickness = (length: number) => count(length) / length
    expect(thickness(600)).toBeCloseTo(thickness(60), 1)
    expect(count(600)).toBeGreaterThan(10 * count(40))
    expect(count(15)).toBeGreaterThanOrEqual(sparks.least)
    expect(throwSpacing(sparks, 0)).toBe(0)
  })

  it("throws each one once, however its frames fall", () => {
    let next = 0
    const at: number[] = []
    for (const upTo of [3, 3, 7.5, 10]) {
      const due = throwsUpTo(next, upTo, 2.5)
      at.push(...due.at)
      next = due.next
    }
    expect(at).toEqual([0, 2.5, 5, 7.5, 10])
    expect(next).toBe(12.5)
    expect(throwsUpTo(0, 10, 0)).toEqual({ at: [], next: 0 })
  })
})
