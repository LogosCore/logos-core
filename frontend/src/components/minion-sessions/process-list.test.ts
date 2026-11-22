import { describe, expect, test } from "vitest"
import { cpuTimeText, folderOf, processNotes, startedSeconds, userText } from "./process-list"

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
  expect(processNotes({ state: "sleeping", arch: "amd64" }, "amd64")).toEqual([])
  expect(processNotes({ state: "zombie", arch: null }, "amd64")).toEqual(["zombie"])
  expect(processNotes({ state: "running", arch: "386" }, "amd64")).toEqual(["386"])
  expect(processNotes({ state: "stopped", arch: "386" }, null)).toEqual(["stopped"])
})

test("who it runs as, with its integrity and session", () => {
  expect(userText({ user: String.raw`CORP\ann`, integrity: "medium", session: 1 })).toBe(String.raw`CORP\ann · medium · session 1`)
  expect(userText({ user: "root", integrity: null, session: null })).toBe("root")
  expect(userText({ user: null, integrity: null, session: 0 })).toBe("session 0")
})
