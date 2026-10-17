import { beforeEach, describe, expect, it, vi } from "vitest"

const memory = new Map<string, string>()
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
})

const { MAX_REMEMBERED_VIEWS, loadDrawingView, saveDrawingView } = await import(
  "./drawing-view-memory"
)

const view = (documentId: string, zoom = 1) => ({ documentId, scrollX: -120, scrollY: 40.5, zoom })

describe("drawing view memory", () => {
  beforeEach(() => memory.clear())

  it("opens fitted when nothing is remembered", () => {
    expect(loadDrawingView("a")).toBeNull()
  })

  it("remembers each drawing's own view", () => {
    saveDrawingView(view("a", 2))
    saveDrawingView(view("b", 0.5))
    expect(loadDrawingView("a")).toEqual(view("a", 2))
    expect(loadDrawingView("b")).toEqual(view("b", 0.5))
  })

  it("keeps only the most recent drawings", () => {
    for (let i = 0; i <= MAX_REMEMBERED_VIEWS; i++) saveDrawingView(view(`d${i}`))
    expect(loadDrawingView("d0")).toBeNull()
    expect(loadDrawingView(`d${MAX_REMEMBERED_VIEWS}`)).not.toBeNull()
  })

  it("ignores what it did not write", () => {
    memory.set("wiki_drawing_views", JSON.stringify([{ documentId: "a", scrollX: "x" }, { ...view("b"), zoom: 999 }]))
    expect(loadDrawingView("a")).toBeNull()
    expect(loadDrawingView("b")).toBeNull()
    memory.set("wiki_drawing_views", "{not json")
    expect(loadDrawingView("a")).toBeNull()
  })

  it("never stores a view the canvas could not open", () => {
    saveDrawingView({ ...view("a"), scrollX: Number.NaN })
    expect(loadDrawingView("a")).toBeNull()
  })
})
