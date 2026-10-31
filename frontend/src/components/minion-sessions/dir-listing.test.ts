import { describe, expect, test } from "vitest"
import { childPath, crumbs, readDirListing, sortEntries } from "./dir-listing"

describe("a dir_listing in the console", () => {
  test("its path in crumbs from the root, on Unix, a Windows drive, a share", () => {
    expect(crumbs("/usr/bin", "/")).toEqual([
      { label: "/", path: "/" },
      { label: "usr", path: "/usr" },
      { label: "bin", path: "/usr/bin" },
    ])
    expect(crumbs("/", "/")).toEqual([{ label: "/", path: "/" }])
    expect(crumbs("C:\\Users\\ann", "\\")).toEqual([
      { label: "C:\\", path: "C:\\" },
      { label: "Users", path: "C:\\Users" },
      { label: "ann", path: "C:\\Users\\ann" },
    ])
    expect(crumbs("\\\\srv\\share\\docs", "\\")).toEqual([
      { label: "\\\\srv\\share\\", path: "\\\\srv\\share\\" },
      { label: "docs", path: "\\\\srv\\share\\docs" },
    ])
    // A Unix name may hold a backslash: only the separator given splits.
    expect(crumbs("/tmp/a\\b", "/").map((c) => c.label)).toEqual(["/", "tmp", "a\\b"])
  })

  test("an entry's path, without doubling the separator at a root", () => {
    expect(childPath({ path: "/usr", sep: "/" }, "bin")).toBe("/usr/bin")
    expect(childPath({ path: "/", sep: "/" }, "etc")).toBe("/etc")
    expect(childPath({ path: "C:\\", sep: "\\" }, "Users")).toBe("C:\\Users")
  })

  test("the data read as a listing, or not one", () => {
    const l = readDirListing({
      path: "/tmp",
      sep: "/",
      parent: "/",
      entries: [{ name: "a", type: "socket-ish", size: 3, x: { ino: 1 } }, { name: "b", type: "dir", hidden: true }],
    })
    expect(l).toMatchObject({ path: "/tmp", parent: "/", entries: [{ name: "a", type: "other", size: 3 }, { name: "b", hidden: true }] })
    expect(readDirListing({ path: "/tmp", sep: ":", entries: [] })).toBeNull()
    expect(readDirListing({ path: "/tmp", sep: "/", entries: [{ type: "file" }] })).toBeNull()
    expect(readDirListing(null)).toBeNull()
  })

  test("folders first, then links, then the rest, each by name", () => {
    const sorted = sortEntries([
      { name: "z.txt", type: "file" },
      { name: "b", type: "dir" },
      { name: "l", type: "link" },
      { name: "a", type: "dir" },
      { name: "a.txt", type: "file" },
    ])
    expect(sorted.map((e) => e.name)).toEqual(["a", "b", "l", "a.txt", "z.txt"])
  })
})
