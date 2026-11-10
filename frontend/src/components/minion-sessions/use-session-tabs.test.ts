import { beforeEach, describe, expect, it, vi } from "vitest"
import { load, openSessionTab, save } from "./use-session-tabs"

const KEY = "logos.sessionTabs.op1"

describe("session tabs storage", () => {
  // No DOM in these tests: a Storage each, in memory.
  beforeEach(() => {
    vi.stubGlobal("localStorage", memoryStorage())
    vi.stubGlobal("sessionStorage", memoryStorage())
  })

  it("keeps this browser tab's tabs, and leaves them for a new one", () => {
    save("op1", { ids: ["a", "b"], active: "b" })
    expect(JSON.parse(sessionStorage.getItem(KEY)!)).toEqual({ ids: ["a", "b"], active: "b" })
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual({ ids: ["a", "b"], active: "b" })
  })

  it("reads this browser tab's own before the last one left", () => {
    localStorage.setItem(KEY, JSON.stringify({ ids: ["other"], active: "other" }))
    sessionStorage.setItem(KEY, JSON.stringify({ ids: ["mine"], active: "mine" }))
    expect(load("op1")).toEqual({ ids: ["mine"], active: "mine" })
  })

  it("starts a new browser tab from the last one left", () => {
    localStorage.setItem(KEY, JSON.stringify({ ids: ["left"], active: "left" }))
    expect(load("op1")).toEqual({ ids: ["left"], active: "left" })
  })

  it("keeps a browser tab empty when its tabs were closed, though another left some", () => {
    save("op1", { ids: [], active: null })
    localStorage.setItem(KEY, JSON.stringify({ ids: ["y"], active: "y" }))
    expect(load("op1")).toEqual({ ids: [], active: null })
  })
})

describe("opening a session's tab from elsewhere", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", memoryStorage())
    vi.stubGlobal("sessionStorage", memoryStorage())
  })

  it("adds it after the open ones, in front, kept for the Sessions page to find", () => {
    // Its own operation: what the page keeps is by operation, for the page's life.
    save("op-interact", { ids: ["a"], active: "a" })
    openSessionTab("op-interact", "b")
    expect(load("op-interact")).toEqual({ ids: ["a", "b"], active: "b" })
  })

  it("brings one already open to the front", () => {
    save("op-again", { ids: ["a", "b"], active: "b" })
    openSessionTab("op-again", "a")
    expect(load("op-again")).toEqual({ ids: ["a", "b"], active: "a" })
  })
})

function memoryStorage(): Storage {
  const m = new Map<string, string>()
  return {
    get length() {
      return m.size
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, String(v)),
  }
}
