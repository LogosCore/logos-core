import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import { dirname, join } from "node:path"
import { beforeEach, describe, expect, it, vi } from "vitest"

const memory = new Map<string, string>()
let full = false
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => {
    if (full) throw new DOMException("quota", "QuotaExceededError")
    memory.set(k, v)
  },
  removeItem: (k: string) => void memory.delete(k),
})

const { drawingLibraryAdapter } = await import("./drawing-library")
const { BUILTIN_SHAPES, SHAPE_CATEGORIES, matchesShape } = await import("./drawing-builtin-library")

const item = { id: "lib1", status: "unpublished", elements: [], created: 1 }

describe("the operator's library", () => {
  beforeEach(() => {
    memory.clear()
    full = false
  })

  it("starts empty", () => {
    expect(drawingLibraryAdapter.load({ source: "load" })).toBeNull()
  })

  it("gives back what was saved", async () => {
    await drawingLibraryAdapter.save({ libraryItems: [item] as never })
    expect(drawingLibraryAdapter.load({ source: "load" })).toEqual({ libraryItems: [item] })
  })

  it("treats a damaged entry as no library rather than failing the canvas", () => {
    memory.set("wiki_drawing_library", "{not json")
    expect(drawingLibraryAdapter.load({ source: "load" })).toBeNull()
    memory.set("wiki_drawing_library", JSON.stringify({ libraryItems: "nope" }))
    expect(drawingLibraryAdapter.load({ source: "load" })).toBeNull()
  })

  // Excalidraw shows the operator a failed save; swallowing it would leave a
  // library that looks kept and is gone on reload.
  it("reports a save that did not fit", () => {
    full = true
    expect(() => drawingLibraryAdapter.save({ libraryItems: [item] as never })).toThrow()
  })
})

describe("the built-in shapes", () => {
  it("have unique keys", () => {
    const keys = BUILTIN_SHAPES.map((s) => s.key)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it("all belong to a category, and every category has some", () => {
    const ids = new Set<string>(SHAPE_CATEGORIES.map((c) => c.id))
    for (const shape of BUILTIN_SHAPES) expect(ids.has(shape.category), shape.name).toBe(true)
    for (const category of SHAPE_CATEGORIES) {
      expect(BUILTIN_SHAPES.some((s) => s.category === category.id), category.label).toBe(true)
    }
  })

  // The panel lays a category out by its first tile's size.
  it("are one tile size within a category", () => {
    for (const category of SHAPE_CATEGORIES) {
      const sizes = new Set(BUILTIN_SHAPES.filter((s) => s.category === category.id).map((s) => s.tile))
      expect(sizes.size, category.label).toBe(1)
    }
  })

  it("build in both styles, and styled ones differ between them", () => {
    for (const shape of BUILTIN_SHAPES) {
      const sketch = shape.build("sketch")
      const clean = shape.build("clean")
      expect(sketch.length, shape.name).toBeGreaterThan(0)
      if (shape.styled) expect(JSON.stringify(sketch), shape.name).not.toBe(JSON.stringify(clean))
    }
  })

  // Otherwise inserting one drops loose parts on the canvas: a card's icon
  // strokes, a legend's chips.
  it("made of several parts are one group", () => {
    for (const shape of BUILTIN_SHAPES) {
      const elements = shape.build("sketch")
      if (elements.length < 2) continue
      const groups = elements.map((el) => (el as { groupIds?: string[] }).groupIds?.join())
      expect(new Set(groups).size, shape.name).toBe(1)
      expect(groups[0], shape.name).toBeTruthy()
    }
  })
})

describe("searching shapes", () => {
  const names = (query: string) => BUILTIN_SHAPES.filter((s) => matchesShape(s, query)).map((s) => s.name)

  it("matches the abbreviations operators use", () => {
    expect(names("dc")).toContain("Domain controller")
    expect(names("pwned")).toContain("Compromised")
    expect(names("c2")).toContain("C2 server")
  })

  it("needs every word to match", () => {
    expect(names("lateral movement")).toEqual(["Lateral movement"])
  })

  it("matches the category name", () => {
    expect(names("severity").length).toBeGreaterThanOrEqual(5)
  })

  it("an empty search matches everything", () => {
    expect(names("  ")).toHaveLength(BUILTIN_SHAPES.length)
  })
})

describe("the generated icons", () => {
  // The icons are lucide's, converted at generation time. A lucide bump that
  // nobody regenerated for leaves the library drawing the old version.
  it("come from the installed lucide-react", () => {
    const require = createRequire(import.meta.url)
    const pkg = join(dirname(require.resolve("lucide-react")), "../../package.json")
    const { version } = JSON.parse(readFileSync(pkg, "utf8")) as { version: string }
    const generated = readFileSync(new URL("./drawing-icons.generated.ts", import.meta.url), "utf8")
    expect(generated).toContain(`from lucide-react ${version}\n`)
  })
})
