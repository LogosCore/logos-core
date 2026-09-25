import { beforeEach, describe, expect, it, vi } from "vitest"

// The store writes through to localStorage; give it an in-memory one so the
// state transitions can be exercised under node. The legacy key is seeded
// before the import, since the store drops it when it loads.
const memory = new Map<string, string>([
  ["wiki_expanded_nodes", JSON.stringify(["from-any-operation"])],
])
const storage = {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
}
vi.stubGlobal("localStorage", storage)

const { settleRestoredIds, useWikiStore } = await import("./wiki")

const stored = (operationId: string) => {
  const raw = memory.get(`wiki_expanded_nodes:${operationId}`)
  return raw ? (JSON.parse(raw) as string[]) : undefined
}

describe("on load", () => {
  it("drops the set every operation used to share", () => {
    expect(memory.has("wiki_expanded_nodes")).toBe(false)
  })
})

describe("expanded tree nodes", () => {
  beforeEach(() => {
    memory.clear()
    useWikiStore.setState({
      expandedOperationId: null,
      expandedNodes: new Set(),
      unconfirmedExpandedIds: new Set(),
      expandedRestoredAt: 0,
    })
  })

  it("keeps each operation's set apart", () => {
    const store = useWikiStore.getState()
    store.showOperationTree("op-a")
    store.expandNode("a1")
    store.showOperationTree("op-b")
    expect([...useWikiStore.getState().expandedNodes]).toEqual([])
    store.expandNode("b1")

    store.showOperationTree("op-a")
    expect([...useWikiStore.getState().expandedNodes]).toEqual(["a1"])
    expect(stored("op-a")).toEqual(["a1"])
    expect(stored("op-b")).toEqual(["b1"])
  })

  it("showing the same operation again keeps the session's changes", () => {
    const store = useWikiStore.getState()
    store.showOperationTree("op-a")
    store.expandNode("a1")
    const unconfirmed = useWikiStore.getState().unconfirmedExpandedIds

    store.showOperationTree("op-a")
    expect(useWikiStore.getState().expandedNodes.has("a1")).toBe(true)
    expect(useWikiStore.getState().unconfirmedExpandedIds).toBe(unconfirmed)
  })

  it("an operation whose last node collapses leaves nothing in storage", () => {
    const store = useWikiStore.getState()
    store.showOperationTree("op-a")
    store.toggleNode("a1")
    store.toggleNode("a1")
    expect(memory.has("wiki_expanded_nodes:op-a")).toBe(false)
  })

  it("restored ids wait for a check; ids expanded now do not", () => {
    memory.set("wiki_expanded_nodes:op-a", JSON.stringify(["kept", "gone"]))
    const store = useWikiStore.getState()
    store.showOperationTree("op-a")
    store.expandNode("new")

    const state = useWikiStore.getState()
    expect([...state.unconfirmedExpandedIds].sort()).toEqual(["gone", "kept"])
    expect(state.expandedRestoredAt).toBeGreaterThan(0)
  })

  it("settling drops the empty branches from the set and from storage", () => {
    memory.set("wiki_expanded_nodes:op-a", JSON.stringify(["kept", "gone"]))
    const store = useWikiStore.getState()
    store.showOperationTree("op-a")

    store.settleExpanded(["kept"], ["gone"])
    const state = useWikiStore.getState()
    expect([...state.expandedNodes]).toEqual(["kept"])
    expect(state.unconfirmedExpandedIds.size).toBe(0)
    expect(stored("op-a")).toEqual(["kept"])
  })

  it("confirming alone leaves the expanded set untouched", () => {
    memory.set("wiki_expanded_nodes:op-a", JSON.stringify(["kept"]))
    useWikiStore.getState().showOperationTree("op-a")
    const before = useWikiStore.getState().expandedNodes

    useWikiStore.getState().settleExpanded(["kept"], [])
    expect(useWikiStore.getState().expandedNodes).toBe(before)
  })

  it("a storage that refuses writes does not break toggling", () => {
    const setItem = storage.setItem
    storage.setItem = () => {
      throw new Error("QuotaExceededError")
    }
    try {
      const store = useWikiStore.getState()
      store.showOperationTree("op-a")
      store.toggleNode("a1")
      expect(useWikiStore.getState().expandedNodes.has("a1")).toBe(true)
    } finally {
      storage.setItem = setItem
    }
  })
})

describe("settleRestoredIds", () => {
  const restoredAt = 1_000
  const unconfirmed = new Set(["folder", "emptied", "unanswered", "cached"])

  it("settles only unconfirmed ids with an answer fetched since the restore", () => {
    const result = settleRestoredIds(
      [
        { id: "folder", childCount: 3, fetchedAt: 1_500 },
        { id: "emptied", childCount: 0, fetchedAt: 1_000 },
        // No answer to go on, whatever the timestamp says.
        { id: "unanswered", childCount: null, fetchedAt: 1_200 },
        { id: "cached", childCount: 0, fetchedAt: 999 },
        { id: "session", childCount: 0, fetchedAt: 2_000 },
      ],
      unconfirmed,
      restoredAt,
    )
    expect(result).toEqual({ confirmed: ["folder"], dropped: ["emptied"] })
  })
})
