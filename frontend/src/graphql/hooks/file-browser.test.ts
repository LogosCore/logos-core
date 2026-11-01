import { describe, expect, it } from "vitest"
import { touches } from "./file-browser"

describe("touches", () => {
  const entries = (path: string | null, foldersOnly = false) => ({ path, foldersOnly })
  it("reads again a changed folder, its parent and the searches", () => {
    const changed = ["/usr/share/doc"]
    expect(touches("entries", entries("/usr/share/doc"), changed)).toBe(true)
    expect(touches("entries", entries("/usr/share", true), changed)).toBe(true)
    expect(touches("search", { query: "x" }, changed)).toBe(true)
  })
  it("leaves the rest of the tree, the roots and the machine alone", () => {
    const changed = ["/usr/share/doc"]
    expect(touches("entries", entries("/usr", true), changed)).toBe(false)
    expect(touches("entries", entries("/etc", true), changed)).toBe(false)
    expect(touches("entries", entries(null, true), changed)).toBe(false)
    expect(touches("machine", undefined, changed)).toBe(false)
  })
  it("reads the roots again when a root or a folder in one changes", () => {
    expect(touches("entries", entries(null), ["/"])).toBe(true)
    expect(touches("entries", entries("/"), ["/home"])).toBe(true)
    expect(touches("entries", entries(null), ["C:\\"])).toBe(true)
  })
  it("compares Windows paths without case", () => {
    expect(touches("entries", entries("c:\\users"), ["C:\\Users\\Bob"])).toBe(true)
  })
})
