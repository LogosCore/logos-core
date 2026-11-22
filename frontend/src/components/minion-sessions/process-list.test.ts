import { describe, expect, test } from "vitest"
import { cpuTimeText, folderOf, processNotes, startedSeconds, toggled, treeLines, userText } from "./process-list"

describe("a process's folder", () => {
  test("its path up to its last separator", () => {
    expect(folderOf("/usr/sbin/sshd", "/")).toBe("/usr/sbin")
    expect(folderOf("/init", "/")).toBe("/")
    expect(folderOf(String.raw`C:\Windows\System32\svchost.exe`, "\\")).toBe(String.raw`C:\Windows\System32`)
  })
  test("a drive's or a share's root kept whole", () => {
    expect(folderOf("C:\\app.exe", "\\")).toBe("C:\\")
    expect(folderOf("\\\\nas\\tools\\app.exe", "\\")).toBe("\\\\nas\\tools\\")
    expect(folderOf("\\\\nas\\tools\\bin\\app.exe", "\\")).toBe("\\\\nas\\tools\\bin")
  })
  test("none without a path, or one with no separator", () => {
    expect(folderOf(null, "/")).toBeNull()
    expect(folderOf("", "/")).toBeNull()
    expect(folderOf("sshd", "/")).toBeNull()
  })
})

test("CPU time as ps shows it", () => {
  expect(cpuTimeText(null)).toBe("")
  expect(cpuTimeText(0)).toBe("0:00")
  expect(cpuTimeText(1500)).toBe("0:01")
  expect(cpuTimeText(62_000)).toBe("1:02")
  expect(cpuTimeText(3_723_000)).toBe("1:02:03")
})

test("a start time as kill_process takes it: unix seconds", () => {
  expect(startedSeconds("2026-10-30T08:12:00Z")).toBe(1793347920)
  expect(startedSeconds(null)).toBe(0)
  expect(startedSeconds("not a time")).toBe(0)
})

test("a process's notes: its state past running and sleeping, an architecture not the host's", () => {
  const texts = (p: Parameters<typeof processNotes>[0], host: string | null) => processNotes(p, host).map((n) => n.text)
  expect(texts({ state: "sleeping", arch: "amd64" }, "amd64")).toEqual([])
  expect(texts({ state: "zombie", arch: null }, "amd64")).toEqual(["zombie"])
  expect(texts({ state: "running", arch: "386" }, "amd64")).toEqual(["32-bit"])
  expect(texts({ state: "stopped", arch: "386" }, null)).toEqual(["stopped"])
  expect(texts({ state: "running", arch: "mips" }, "amd64")).toEqual(["mips"])
  // Each says what it means; WOW64 on Windows alone.
  expect(processNotes({ state: "running", arch: "386" }, "amd64", true)[0].title).toBe(
    "A 32-bit program (386) on this x64 host: Windows runs it under WOW64, its layer for 32-bit programs",
  )
  expect(processNotes({ state: "running", arch: "386" }, "amd64")[0].title).toBe("A 32-bit program (386) on this x64 host")
  expect(processNotes({ state: "running", arch: "amd64" }, "arm64")[0].title).toBe("An x64 program (amd64) on this ARM64 host")
  expect(processNotes({ state: "zombie", arch: null }, "amd64")[0].title).toMatch(/parent yet to reap it/)
})

test("who it runs as, with its session, and its integrity where it says more than who", () => {
  expect(userText({ user: String.raw`CORP\ann`, integrity: "medium", session: 1 })).toBe(String.raw`CORP\ann · medium · session 1`)
  expect(userText({ user: String.raw`NT AUTHORITY\SYSTEM`, integrity: "system", session: 0 })).toBe(String.raw`NT AUTHORITY\SYSTEM · system · session 0`)
  expect(userText({ user: "root", integrity: null, session: null })).toBe("root")
  expect(userText({ user: null, integrity: null, session: 0 })).toBe("session 0")
  // Unix: the level is the uid's.
  expect(userText({ user: "root", integrity: "root", session: null })).toBe("root")
  expect(userText({ user: "dbus", integrity: "user", session: null })).toBe("dbus")
  expect(userText({ user: "toor", integrity: "root", session: null })).toBe("toor · root")
  expect(userText({ user: null, integrity: "root", session: null })).toBe("root")
})

test("the lines that draw the tree to a process: its rails, its corner, its stem", () => {
  expect(treeLines({ depth: 0, last: false, guides: [], stem: false })).toEqual([])
  expect(treeLines({ depth: 0, last: false, guides: [], stem: true })).toEqual([{ level: 0, kind: "stem" }])
  expect(treeLines({ depth: 1, last: true, guides: [], stem: false })).toEqual([{ level: 0, kind: "elbow" }])
  expect(treeLines({ depth: 3, last: false, guides: [true, false], stem: true })).toEqual([
    { level: 0, kind: "rail" },
    { level: 2, kind: "tee" },
    { level: 3, kind: "stem" },
  ])
})

test("a pid turned the other way in a set", () => {
  const once = toggled(new Set([1, 2]), 3)
  expect([...once].sort()).toEqual([1, 2, 3])
  expect([...toggled(once, 2)].sort()).toEqual([1, 3])
})
