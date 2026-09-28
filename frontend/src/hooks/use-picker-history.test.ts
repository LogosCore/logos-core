import { beforeEach, describe, expect, it, vi } from "vitest"

const memory = new Map<string, string>()
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
})
vi.stubGlobal("window", { addEventListener: () => {} })

const { isScopeChoice, withRecent, readLegacyFrequentIcons } = await import(
  "./use-picker-history"
)

const op = (id: string) => ({ id, name: `Operation ${id}`, description: "" })

describe("isScopeChoice", () => {
  it("counts choosing a different operation", () => {
    expect(
      isScopeChoice(
        { scopedOperation: op("b"), hydrated: true },
        { scopedOperation: op("a"), hydrated: true },
      ),
    ).toBe(true)
    expect(
      isScopeChoice(
        { scopedOperation: op("a"), hydrated: true },
        { scopedOperation: null, hydrated: true },
      ),
    ).toBe(true)
  })

  // Restoring the saved scope on load is not a choice: without this, every
  // page load would move the restored operation to the front.
  it("ignores the restore on hydrate", () => {
    expect(
      isScopeChoice(
        { scopedOperation: op("a"), hydrated: true },
        { scopedOperation: null, hydrated: false },
      ),
    ).toBe(false)
  })

  it("ignores a re-scope of the same operation and unscoping", () => {
    expect(
      isScopeChoice(
        { scopedOperation: { ...op("a"), name: "renamed" }, hydrated: true },
        { scopedOperation: op("a"), hydrated: true },
      ),
    ).toBe(false)
    expect(
      isScopeChoice(
        { scopedOperation: null, hydrated: true },
        { scopedOperation: op("a"), hydrated: true },
      ),
    ).toBe(false)
  })
})

describe("withRecent", () => {
  it("moves to the front, dedupes, caps at 8", () => {
    const list = "abcdefgh".split("").map(op)
    expect(withRecent(list, op("c")).map((o) => o.id).join("")).toBe("cabdefgh")
    expect(withRecent(list, op("z")).map((o) => o.id).join("")).toBe("zabcdefg")
  })
})

describe("readLegacyFrequentIcons", () => {
  beforeEach(() => memory.clear())

  it("shapes the old list for the import and drops junk", () => {
    memory.set(
      "wiki_frequent_icons",
      JSON.stringify([
        { name: "Server", count: 3, lastUsed: Date.UTC(2026, 0, 2) },
        { name: "Bug", count: 0.4, lastUsed: Date.UTC(2026, 0, 1) },
        { name: "Huge", count: 1e12, lastUsed: Date.UTC(2026, 0, 1) },
        { name: "NoTime", count: 1 },
        { name: "BadTime", count: 1, lastUsed: 1e20 },
        "junk",
      ]),
    )
    expect(readLegacyFrequentIcons()).toEqual([
      { name: "Server", count: 3, lastUsedAt: "2026-01-02T00:00:00.000Z" },
      { name: "Bug", count: 1, lastUsedAt: "2026-01-01T00:00:00.000Z" },
      { name: "Huge", count: 1_000_000, lastUsedAt: "2026-01-01T00:00:00.000Z" },
    ])
  })

  it("is empty for a missing or corrupt key", () => {
    expect(readLegacyFrequentIcons()).toEqual([])
    memory.set("wiki_frequent_icons", "{")
    expect(readLegacyFrequentIcons()).toEqual([])
  })
})
