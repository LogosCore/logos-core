import { describe, expect, it } from "vitest"
import { readDirWalk } from "./dir-walk"

describe("readDirWalk", () => {
  it("reads where a walk stands", () => {
    expect(readDirWalk({ root: "/home", folders: 40, entries: 900, unread: 1, current: "/home/u" })).toEqual({
      root: "/home",
      folders: 40,
      entries: 900,
      unread: 1,
      skipped: 0,
      current: "/home/u",
    })
  })
  it("is none without a root", () => {
    expect(readDirWalk({ folders: 1 })).toBeNull()
    expect(readDirWalk(null)).toBeNull()
  })
})
