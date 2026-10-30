import { describe, expect, test } from "vitest"
import { BASE_MAX, FLOOR, ROWS, burnFrame, newFire } from "./session-fire-model"

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
  test("burns along the whole row, within the canvas", () => {
    const fire = newFire()
    for (let frame = 0; frame < 200; frame++) {
      const heights = burnFrame(fire, WIDTH)
      expect(heights).toHaveLength(WIDTH)
      expect(Math.min(...heights)).toBeGreaterThanOrEqual(FLOOR)
      expect(Math.max(...heights)).toBeLessThanOrEqual(ROWS)
    }
  })

  test("burns in place: each frame matches the last best unshifted", () => {
    // A wave rolling along the row made the fire flow from left to right;
    // then a frame matched the last best a couple of columns over.
    const fire = newFire()
    let last = burnFrame(fire, WIDTH)
    const totals = new Map<number, number>()
    for (let frame = 0; frame < 100; frame++) {
      const next = burnFrame(fire, WIDTH)
      for (let shift = -4; shift <= 4; shift++) totals.set(shift, (totals.get(shift) ?? 0) + likeness(last, next, shift))
      last = next
    }
    const [best] = [...totals].sort((a, b) => b[1] - a[1])[0]
    expect(best).toBe(0)
  })

  test("tall flames are rare, but there are some", () => {
    const fire = newFire()
    let tall = 0
    for (let frame = 0; frame < 300; frame++) tall += burnFrame(fire, WIDTH).filter((h) => h > BASE_MAX + 1).length
    const share = tall / (300 * WIDTH)
    expect(share).toBeGreaterThan(0)
    expect(share).toBeLessThan(0.05)
  })
})
