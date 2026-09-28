import { beforeEach, describe, expect, it, vi } from "vitest"

// The store writes through to localStorage; give it an in-memory one so the
// pure state transitions can be exercised under node.
const memory = new Map<string, string>()
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
})
vi.stubGlobal("window", { addEventListener: () => {} })

const {
  useScopedOperationStore,
  readLegacyRecentOperationIds,
  clearLegacyRecentOperations,
} = await import("./scoped-operation")

const opA = { id: "op-a", name: "Operation A", description: "" }
const opB = { id: "op-b", name: "Operation B", description: "" }

describe("scopeOperationForWikiDocument", () => {
  beforeEach(() => {
    memory.clear()
    useScopedOperationStore.getState().reset()
    useScopedOperationStore.getState().hydrate("user-1")
  })

  it("switches scope and remembers the document to keep open", () => {
    const store = useScopedOperationStore.getState()
    store.scopeOperation(opB)

    store.scopeOperationForWikiDocument(opA, "doc-1")

    const state = useScopedOperationStore.getState()
    expect(state.scopedOperation).toEqual(opA)
    expect(state.retainedWikiDocumentId).toBe("doc-1")
    expect(JSON.parse(memory.get("scoped_operation_user-1")!)).toEqual(opA)
  })

  it("a plain scope change retains nothing", () => {
    useScopedOperationStore.getState().scopeOperation(opA)
    expect(useScopedOperationStore.getState().retainedWikiDocumentId).toBeNull()
  })

  it("clearRetainedWikiDocument is a one-shot consume and reset drops it too", () => {
    const store = useScopedOperationStore.getState()
    store.scopeOperationForWikiDocument(opA, "doc-1")
    store.clearRetainedWikiDocument()
    expect(useScopedOperationStore.getState().retainedWikiDocumentId).toBeNull()

    store.scopeOperationForWikiDocument(opA, "doc-2")
    store.reset()
    expect(useScopedOperationStore.getState().retainedWikiDocumentId).toBeNull()
  })
})

// The "Recent" list itself moved to the server (hooks/use-picker-history.ts);
// the store only keeps a reader for the old copy, so it can be imported once.
describe("legacy recent operations", () => {
  beforeEach(() => memory.clear())

  it("reads ids newest first, per user, and ignores junk", () => {
    memory.set(
      "recent_operations_user-1",
      JSON.stringify([opB, { name: "no id" }, null, opA]),
    )
    expect(readLegacyRecentOperationIds("user-1")).toEqual(["op-b", "op-a"])
    expect(readLegacyRecentOperationIds("user-2")).toEqual([])
  })

  it("treats a corrupt value as empty, and clear removes it", () => {
    memory.set("recent_operations_user-1", "{not json")
    expect(readLegacyRecentOperationIds("user-1")).toEqual([])
    clearLegacyRecentOperations("user-1")
    expect(memory.has("recent_operations_user-1")).toBe(false)
  })

  it("scoping no longer writes a recents key", () => {
    useScopedOperationStore.getState().reset()
    useScopedOperationStore.getState().hydrate("user-1")
    useScopedOperationStore.getState().scopeOperation(opA)
    expect(memory.has("recent_operations_user-1")).toBe(false)
  })
})
