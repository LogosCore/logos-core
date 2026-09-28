import { beforeEach, describe, expect, it, vi } from "vitest"

const memory = new Map<string, string>()
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
})

const { loadScrollPosition, saveScrollPosition, MAX_REMEMBERED } = await import(
  "./wiki-scroll-positions"
)

const at = (documentId: string, block = 3) => ({ documentId, block, offset: 12, scrollTop: 900 })

describe("wiki scroll positions", () => {
  beforeEach(() => memory.clear())

  it("round-trips a position", () => {
    saveScrollPosition(at("a"))
    expect(loadScrollPosition("a")).toEqual(at("a"))
    expect(loadScrollPosition("b")).toBeNull()
  })

  it("keeps one entry per document, the latest", () => {
    saveScrollPosition(at("a", 1))
    saveScrollPosition(at("a", 7))
    expect(loadScrollPosition("a")?.block).toBe(7)
    expect(JSON.parse(memory.get("wiki_scroll_positions")!)).toHaveLength(1)
  })

  // The use case: alternating between a few pages keeps all of them, and only
  // a page not read in the last MAX_REMEMBERED is forgotten.
  it("forgets the least recently read past the cap", () => {
    for (let i = 0; i <= MAX_REMEMBERED; i++) saveScrollPosition(at(`doc-${i}`))
    expect(loadScrollPosition("doc-0")).toBeNull()
    expect(loadScrollPosition("doc-1")).not.toBeNull()

    saveScrollPosition(at("doc-1")) // read again: now the most recent
    saveScrollPosition(at("new"))
    expect(loadScrollPosition("doc-1")).not.toBeNull()
    expect(loadScrollPosition("doc-2")).toBeNull()
  })

  it("treats corrupt storage as empty", () => {
    memory.set("wiki_scroll_positions", "{nope")
    expect(loadScrollPosition("a")).toBeNull()
    memory.set("wiki_scroll_positions", JSON.stringify([{ documentId: "a" }, at("b")]))
    expect(loadScrollPosition("a")).toBeNull()
    expect(loadScrollPosition("b")).toEqual(at("b"))
  })
})
