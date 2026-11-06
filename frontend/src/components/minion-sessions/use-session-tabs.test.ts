import { beforeEach, describe, expect, it, vi } from "vitest"
import { load, save } from "./use-session-tabs"

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
