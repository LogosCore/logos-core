import { describe, expect, test } from "vitest"
import { HOT, ROWS, burnFrame, flameHeights, newFire } from "./session-fire-model"

const WIDTH = 900

/** How alike two frames are with the second shifted by shift columns. */
function likeness(a: number[], b: number[], shift: number): number {
  const mean = (xs: number[]) => xs.reduce((s, x) => s + x, 0) / xs.length
  const ma = mean(a)
  const mb = mean(b)
  let sum = 0
  let n = 0
  for (let x = 0; x < a.length; x++) {
    if (x + shift < 0 || x + shift >= b.length) continue
    sum += (a[x] - ma) * (b[x + shift] - mb)
    n++
  }
  return sum / n
}

describe("the fire behind an On Fire row", () => {
  test("burns along the whole row from the start, its heats within the palette", () => {
    const fire = newFire(WIDTH)
    for (let frame = 0; frame < 100; frame++) {
      // The bottom row burns everywhere: a line of fire, not patches.
      expect(flameHeights(fire).every((h) => h > 0)).toBe(true)
      expect(Math.max(...fire.heat)).toBeLessThanOrEqual(HOT)
      burnFrame(fire)
    }
  })

  test("leans neither way: a frame matches the last as well a column left as right", () => {
    // Doom takes the columns left to right, so heat drifting right climbs
    // faster and the fire leans; as first drawn, a wave rolled along it.
    const fire = newFire(WIDTH)
    let last = flameHeights(fire)
    const totals = new Map<number, number>()
    for (let frame = 0; frame < 300; frame++) {
      burnFrame(fire)
      const next = flameHeights(fire)
      for (const shift of [-1, 0, 1]) totals.set(shift, (totals.get(shift) ?? 0) + likeness(last, next, shift))
      last = next
    }
    const [left, still, right] = [-1, 0, 1].map((s) => totals.get(s)!)
    expect(still).toBeGreaterThan(Math.max(left, right))
    expect(Math.abs(left - right) / still).toBeLessThan(0.03)
  })

  test("burns low, its tall flames rare", () => {
    const fire = newFire(WIDTH)
    let low = 0
    let tall = 0
    for (let frame = 0; frame < 200; frame++) {
      burnFrame(fire)
      for (const h of flameHeights(fire)) {
        if (h <= ROWS / 2) low++
        if (h >= ROWS * 0.8) tall++
      }
    }
    const all = 200 * WIDTH
    expect(low / all).toBeGreaterThan(0.6)
    expect(tall / all).toBeGreaterThan(0)
    expect(tall / all).toBeLessThan(0.1)
  })
})
