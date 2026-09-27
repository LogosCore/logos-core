import { beforeEach, describe, expect, it, vi } from "vitest"

// The store writes through to localStorage; give it an in-memory one so the
// persistence can be exercised under node. Seeded before the import, since the
// store reads the key once while initialising.
const memory = new Map<string, string>([
  [
    "wiki_footer_sections",
    JSON.stringify({
      "document:tasks": false,
      "drawing:subpages": true,
      "drawing:nonsense": true,
      "document:backlinks": "yes",
    }),
  ],
])
const storage = {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
}
vi.stubGlobal("localStorage", storage)

const { useWikiFooterSectionsStore, isSectionOpen } = await import(
  "./wiki-footer-sections"
)

const stored = () => {
  const raw = memory.get("wiki_footer_sections")
  return raw ? (JSON.parse(raw) as Record<string, boolean>) : undefined
}
const overrides = () => useWikiFooterSectionsStore.getState().overrides

describe("on load", () => {
  it("restores the stored folds and drops unknown keys and non-booleans", () => {
    expect(overrides()).toEqual({
      "document:tasks": false,
      "drawing:subpages": true,
    })
  })
})

describe("defaults", () => {
  beforeEach(() => {
    memory.clear()
    useWikiFooterSectionsStore.setState({ overrides: {} })
  })

  it("opens every section on a prose page", () => {
    for (const id of ["subpages", "backlinks", "tasks"] as const) {
      expect(isSectionOpen({}, "document", id)).toBe(true)
    }
  })

  it("folds every section on a drawing", () => {
    for (const id of ["subpages", "backlinks", "tasks"] as const) {
      expect(isSectionOpen({}, "drawing", id)).toBe(false)
    }
  })
})

describe("setOpen", () => {
  beforeEach(() => {
    memory.clear()
    useWikiFooterSectionsStore.setState({ overrides: {} })
  })

  it("persists a fold as a deviation from the surface's default", () => {
    useWikiFooterSectionsStore.getState().setOpen("document", "backlinks", false)
    expect(overrides()).toEqual({ "document:backlinks": false })
    expect(stored()).toEqual({ "document:backlinks": false })
  })

  it("keeps a drawing's fold off the prose pages", () => {
    useWikiFooterSectionsStore.getState().setOpen("drawing", "tasks", true)
    expect(isSectionOpen(overrides(), "drawing", "tasks")).toBe(true)
    expect(isSectionOpen(overrides(), "document", "tasks")).toBe(true)

    useWikiFooterSectionsStore.getState().setOpen("document", "tasks", false)
    expect(isSectionOpen(overrides(), "drawing", "tasks")).toBe(true)
    expect(isSectionOpen(overrides(), "document", "tasks")).toBe(false)
  })

  it("clears the key once every section is back at its default", () => {
    const { setOpen } = useWikiFooterSectionsStore.getState()
    setOpen("drawing", "subpages", true)
    setOpen("drawing", "subpages", false)
    expect(overrides()).toEqual({})
    expect(stored()).toBeUndefined()
  })

  it("keeps the same state object when nothing changes", () => {
    const before = overrides()
    // Already the drawing default, and already the document default.
    useWikiFooterSectionsStore.getState().setOpen("drawing", "tasks", false)
    useWikiFooterSectionsStore.getState().setOpen("document", "tasks", true)
    expect(overrides()).toBe(before)
  })
})
