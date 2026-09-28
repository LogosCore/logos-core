import { beforeEach, describe, expect, it, vi } from "vitest"

const memory = new Map<string, string>()
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
})

const { readGridPreference, writeGridPreference } = await import("./drawing-grid-preference")

describe("drawing grid preference", () => {
  beforeEach(() => memory.clear())

  it("is on until the operator turns it off", () => {
    expect(readGridPreference()).toBe(true)
  })

  it("remembers either choice", () => {
    writeGridPreference(false)
    expect(readGridPreference()).toBe(false)
    writeGridPreference(true)
    expect(readGridPreference()).toBe(true)
  })

  it("defaults to on when storage is unavailable", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("denied")
      },
    })
    expect(readGridPreference()).toBe(true)
    expect(() => writeGridPreference(false)).not.toThrow()
  })
})
