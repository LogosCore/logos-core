import { describe, expect, it } from "vitest"
import { MAX_SCALE, MIN_SCALE, fitView, panBy, wheelZoomFactor, zoomAt, zoomTo } from "./pan-zoom"

describe("fitView", () => {
  it("shrinks a wide drawing to the viewport and centres it", () => {
    const v = fitView({ width: 3200, height: 200 }, { width: 1632, height: 800 }, 16)
    expect(v.scale).toBeCloseTo(0.5)
    expect(v.x).toBeCloseTo(16)
    expect(v.y).toBeCloseTo((800 - 100) / 2)
  })

  it("never enlarges a small drawing past 100%", () => {
    const v = fitView({ width: 200, height: 100 }, { width: 1000, height: 800 })
    expect(v.scale).toBe(1)
    expect(v).toMatchObject({ x: 400, y: 350 })
  })
})

describe("zoomAt", () => {
  it("keeps the point under the pointer still", () => {
    const before = { scale: 0.5, x: 10, y: 20 }
    const px = 300
    const py = 200
    const contentX = (px - before.x) / before.scale
    const contentY = (py - before.y) / before.scale
    const after = zoomAt(before, 3, px, py)
    expect(after.scale).toBeCloseTo(1.5)
    expect(contentX * after.scale + after.x).toBeCloseTo(px)
    expect(contentY * after.scale + after.y).toBeCloseTo(py)
  })

  it("clamps the scale", () => {
    expect(zoomAt({ scale: 1, x: 0, y: 0 }, 1000, 0, 0).scale).toBe(MAX_SCALE)
    expect(zoomAt({ scale: 1, x: 0, y: 0 }, 0.0001, 0, 0).scale).toBe(MIN_SCALE)
  })

  it("zoomTo lands on an exact scale", () => {
    expect(zoomTo({ scale: 0.37, x: 5, y: 5 }, 1, 100, 100).scale).toBeCloseTo(1)
  })
})

describe("panBy and wheel", () => {
  it("pans by the drag", () => {
    expect(panBy({ scale: 2, x: 1, y: 2 }, 10, -5)).toEqual({ scale: 2, x: 11, y: -3 })
  })

  it("zooms in on wheel up and out on wheel down, about 10% a notch", () => {
    expect(wheelZoomFactor(-100, 0)).toBeGreaterThan(1)
    expect(wheelZoomFactor(100, 0)).toBeLessThan(1)
    expect(wheelZoomFactor(-3, 1)).toBeCloseTo(wheelZoomFactor(-48, 0))
  })
})
