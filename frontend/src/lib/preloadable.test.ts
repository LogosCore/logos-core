import { describe, expect, it, vi } from "vitest"
import { preloadable } from "./preloadable"

describe("preloadable", () => {
  it("fetches once, then has the module at hand", async () => {
    const importer = vi.fn(async () => ({ name: "m" }))
    const code = preloadable(importer)
    expect(code.current()).toBeNull()
    const [a, b] = await Promise.all([code.load(), code.load()])
    expect(a).toBe(b)
    expect(code.current()).toBe(a)
    await code.load()
    expect(importer).toHaveBeenCalledTimes(1)
  })

  it("fetches again after a failure", async () => {
    const importer = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ name: "m" })
    const code = preloadable(importer)
    await expect(code.load()).rejects.toThrow("offline")
    expect(code.current()).toBeNull()
    await expect(code.load()).resolves.toEqual({ name: "m" })
    expect(code.current()).toEqual({ name: "m" })
    expect(importer).toHaveBeenCalledTimes(2)
  })
})
