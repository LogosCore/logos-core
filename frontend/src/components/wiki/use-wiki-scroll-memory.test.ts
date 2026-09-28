import { describe, expect, it, vi } from "vitest"

vi.stubGlobal("localStorage", { getItem: () => null, setItem: () => {}, removeItem: () => {} })

const { measurePosition, applyPosition } = await import("./use-wiki-scroll-memory")

// A minimal scroll container over a column of blocks, enough for the two
// functions: they only read rects, children and scrollTop. Block i spans
// [tops[i], tops[i] + heights[i]) in content coordinates; the container's top
// edge sits at viewport y = 100.
function page(heights: number[], scrollTop: number) {
  const tops: number[] = []
  heights.reduce((y, h) => (tops.push(y), y + h), 0)
  const container = {
    scrollTop,
    getBoundingClientRect: () => ({ top: 100 }),
    contains: () => true,
  }
  const blocks = heights.map((h, i) => ({
    getBoundingClientRect: () => ({
      top: 100 + tops[i] - container.scrollTop,
      bottom: 100 + tops[i] + h - container.scrollTop,
    }),
  }))
  const editorDom = { isConnected: true, children: blocks }
  return {
    container: container as unknown as HTMLElement,
    editorDom: editorDom as unknown as HTMLElement,
    setHeights(next: number[]) {
      heights.splice(0, heights.length, ...next)
      tops.length = 0
      heights.reduce((y, h) => (tops.push(y), y + h), 0)
    },
  }
}

describe("measurePosition", () => {
  it("records the block at the top edge and the distance into it", () => {
    const p = page([100, 100, 100, 100], 250)
    expect(measurePosition(p.container, p.editorDom, "d")).toEqual({
      documentId: "d",
      block: 2,
      offset: 50,
      scrollTop: 250,
    })
  })

  it("records the very top as the top", () => {
    const p = page([100, 100], 3)
    expect(measurePosition(p.container, p.editorDom, "d")?.scrollTop).toBe(0)
  })

  // Between documents the skeleton replaces the editor; the scroll event that
  // swap fires must not overwrite the page's saved position.
  it("records nothing while the editor is not mounted", () => {
    const p = page([100, 100], 150)
    ;(p.editorDom as unknown as { isConnected: boolean }).isConnected = false
    expect(measurePosition(p.container, p.editorDom, "d")).toBeNull()
  })

  it("falls back to scrollTop below the last block", () => {
    const p = page([100, 100], 400)
    expect(measurePosition(p.container, p.editorDom, "d")).toMatchObject({ block: -1, scrollTop: 400 })
  })
})

describe("applyPosition", () => {
  it("returns to the same spot", () => {
    const p = page([100, 100, 100, 100], 0)
    applyPosition(p.container, p.editorDom, { documentId: "d", block: 2, offset: 50, scrollTop: 250 })
    expect(p.container.scrollTop).toBe(250)
  })

  // Why the block is the record and not the pixel offset: an image above the
  // anchor that loads after the restore grows the page, and the same block
  // must still be at the top — which the pixel value alone would miss.
  it("keeps the block in place when content above it grows", () => {
    const p = page([100, 100, 100, 100], 0)
    const saved = { documentId: "d", block: 2, offset: 50, scrollTop: 250 }
    applyPosition(p.container, p.editorDom, saved)
    p.setHeights([400, 100, 100, 100])
    applyPosition(p.container, p.editorDom, saved)
    expect(p.container.scrollTop).toBe(550)
    expect(measurePosition(p.container, p.editorDom, "d")).toMatchObject({ block: 2, offset: 50 })
  })

  it("uses scrollTop when the block no longer exists", () => {
    const p = page([100, 100], 0)
    applyPosition(p.container, p.editorDom, { documentId: "d", block: 9, offset: 5, scrollTop: 120 })
    expect(p.container.scrollTop).toBe(120)
  })
})
