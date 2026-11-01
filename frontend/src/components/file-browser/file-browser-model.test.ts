import { describe, expect, it } from "vitest"
import { byWhom, inFlight, inFlightAt, listedSays, parentOf, pathsTo, takenIn } from "./file-browser-model"

describe("inFlight", () => {
  const actions: Record<string, string> = { ls: "list_dir", rm: "delete_path", up: "upload_file", ps: "" }
  const all = inFlight(
    [
      { id: "1", command: "ls", state: "SENT", args: { path: "/etc" } },
      { id: "2", command: "rm", state: "DONE", args: { path: "/tmp/x" } },
      { id: "3", command: "up", state: "QUEUED", args: { path: "C:\\Users\\Bob\\a.txt" } },
      { id: "4", command: "ps", state: "SENT", args: {} },
      { id: "5", command: "ls", state: "STARTED", args: { path: 4 } },
    ],
    (c) => actions[c],
  )
  it("keeps the running ones with a path", () => {
    expect(all.map((f) => [f.taskId, f.label])).toEqual([
      ["1", "listing…"],
      ["3", "uploading…"],
    ])
  })
  it("matches Windows paths without case", () => {
    expect(inFlightAt(all, "c:\\users\\bob\\A.TXT")?.taskId).toBe("3")
    expect(inFlightAt(all, "/ETC")).toBeUndefined()
  })
})

describe("paths", () => {
  it("walks to a path from its root", () => {
    expect(pathsTo("/usr/bin")).toEqual(["/", "/usr", "/usr/bin"])
    expect(pathsTo("C:\\Users\\Bob")).toEqual(["C:\\", "C:\\Users", "C:\\Users\\Bob"])
  })
  it("finds a path's folder", () => {
    expect(parentOf("/usr/bin")).toBe("/usr")
    expect(parentOf("/etc")).toBe("/")
    expect(parentOf("/")).toBeNull()
    expect(parentOf("\\\\srv\\share\\docs")).toBe("\\\\srv\\share\\")
  })
  it("says whose a task was", () => {
    expect(byWhom({ number: 46, user: "root" })).toBe("#46 (root)")
    expect(byWhom({ number: 7 })).toBe("#7")
    expect(byWhom(null)).toBe("")
  })
})

describe("listedSays", () => {
  it("says a folder listed, never, or not read and why", () => {
    expect(listedSays({})).toBe("Never listed")
    expect(listedSays({ listedAt: "2026-11-01T10:00:00Z", listedBy: { number: 5, user: "u" } })).toMatch(/^Listed .* by #5 \(u\)$/)
    const denied = listedSays({ listErr: "permission denied", listErrAt: "2026-11-01T10:00:00Z", listErrBy: { number: 6 } })
    expect(denied).toMatch(/^Could not be read .* by #6: permission denied$/)
    expect(listedSays({ listErr: "denied", listedAt: "2026-10-01T10:00:00Z", listedBy: { number: 1 } }).split("\n")).toHaveLength(2)
  })
})

describe("takenIn", () => {
  it("is a folder listed, a file downloaded, a root", () => {
    expect(takenIn({ type: "dir", parent: "/", listedAt: "2026-11-01T10:00:00Z", downloaded: [] })).toBe(true)
    expect(takenIn({ type: "dir", parent: "/", listedAt: null, downloaded: [] })).toBe(false)
    expect(takenIn({ type: "file", parent: "/etc", downloaded: [{}] })).toBe(true)
    expect(takenIn({ type: "file", parent: "/etc", downloaded: [] })).toBe(false)
    expect(takenIn({ type: "link", parent: "/etc", downloaded: [] })).toBe(false)
    expect(takenIn({ type: "dir", parent: null, downloaded: [] })).toBe(true)
  })
})
