import { describe, expect, it } from "vitest"
import { atOption, parseScheduleText, SCHEDULE_KEY, splitSchedule, takeSchedule } from "./task-schedule"
import type { CommandDef } from "./command-line"

const now = new Date(2026, 10, 23, 12, 0, 0) // 2026.11.23 12:00 here
const local = (y: number, mo: number, d: number, h: number, mi: number, s = 0) => new Date(y, mo - 1, d, h, mi, s).toISOString()

const ls: CommandDef & { schedulable: boolean } = {
  id: "ls",
  label: "List",
  params: [{ key: "path", type: "string", label: "Path", required: false, default: "." }],
  schedulable: true,
}
const shell: CommandDef & { schedulable: boolean } = {
  id: "shell",
  label: "Shell",
  params: [{ key: "command", type: "string", label: "Command", required: true }],
  schedulable: true,
}
const sleep: CommandDef & { schedulable: boolean } = {
  id: "sleep",
  label: "Sleep",
  params: [
    { key: "interval", type: "duration", label: "Interval", required: true },
    { key: "jitter", type: "int", label: "Jitter", required: false, default: 10 },
  ],
  schedulable: true,
}
const help: CommandDef & { schedulable: boolean } = { id: "help", label: "Help", params: [], schedulable: false }
const remind: CommandDef & { schedulable: boolean } = {
  id: "remind",
  label: "Remind",
  params: [{ key: "at", type: "datetime", label: "At", required: true }],
  schedulable: true,
}
const commands = [ls, shell, sleep, help, remind]

describe("parseScheduleText", () => {
  it("reads the app's form in this browser's time, - / or T as well, and RFC 3339 with its zone", () => {
    expect(parseScheduleText("2026.11.24 10:00", now)).toEqual({ ok: true, at: local(2026, 11, 24, 10, 0) })
    expect(parseScheduleText("2026-11-24T10:00:30", now)).toEqual({ ok: true, at: local(2026, 11, 24, 10, 0, 30) })
    expect(parseScheduleText("2026/11/24 9:05", now)).toEqual({ ok: true, at: local(2026, 11, 24, 9, 5) })
    expect(parseScheduleText("2026-11-24T10:00:00Z", now)).toEqual({ ok: true, at: "2026-11-24T10:00:00.000Z" })
  })

  it("refuses what is no date and time, one that rolls over, and one past", () => {
    for (const text of ["tomorrow", "10:00", "2026.11.24", "2026.13.01 10:00", "2026.11.31 10:00", "2026-11-24T10:00:00"]) {
      // (an RFC 3339 time without its zone is read as the app's local form, with T: valid)
      if (text === "2026-11-24T10:00:00") continue
      const r = parseScheduleText(text, now)
      expect(r.ok, text).toBe(false)
    }
    expect(parseScheduleText("2026.11.23 11:59", now)).toEqual({ ok: false, error: "--at: 2026.11.23 11:59:00 has passed" })
  })
})

describe("takeSchedule", () => {
  it("takes --at= or --at with the next word off the line, anywhere", () => {
    expect(takeSchedule('ls /tmp --at="2026.11.24 10:00"', commands, now)).toEqual({
      line: "ls /tmp",
      at: local(2026, 11, 24, 10, 0),
      text: "2026.11.24 10:00",
      error: null,
    })
    // sleep has no param that takes the rest of the line: --at goes anywhere.
    expect(takeSchedule("sleep --at 2026-11-24T10:00 30s", commands, now)).toMatchObject({ line: "sleep 30s", at: local(2026, 11, 24, 10, 0) })
  })

  it("takes it at the end alone after a param that takes the rest of the line", () => {
    expect(takeSchedule("shell echo --at=x done", commands, now)).toMatchObject({ line: "shell echo --at=x done", at: null, error: null })
    expect(takeSchedule("shell ls -la --at=2026-11-24T10:00", commands, now)).toMatchObject({ line: "shell ls -la", at: local(2026, 11, 24, 10, 0) })
  })

  it("leaves it to a command that takes no time, or has a param called at", () => {
    expect(takeSchedule("help --at=2026-11-24T10:00", commands, now)).toMatchObject({ at: null, error: null, line: "help --at=2026-11-24T10:00" })
    expect(takeSchedule("remind --at=2026-11-24T10:00", commands, now)).toMatchObject({ at: null, error: null })
  })

  it("says why a time is none, the line without it", () => {
    expect(takeSchedule("ls --at=soon", commands, now)).toMatchObject({ line: "ls", at: null, text: "soon", error: expect.stringContaining("a date and time") })
    expect(takeSchedule("ls --at", commands, now).error).toContain("a date and time")
  })
})

describe("the command form's time", () => {
  it("splits from the params' values, and says itself as the line's option", () => {
    expect(splitSchedule({ path: "/tmp", [SCHEDULE_KEY]: "2026-11-24T09:00:00Z" })).toEqual({ args: { path: "/tmp" }, at: "2026-11-24T09:00:00Z" })
    expect(splitSchedule({ path: "/tmp" })).toEqual({ args: { path: "/tmp" }, at: null })
    expect(atOption(local(2026, 11, 24, 10, 0))).toBe('--at="2026.11.24 10:00:00"')
  })
})
