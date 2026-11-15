import { describe, expect, test } from "vitest"
import {
  downloadable,
  formatClock,
  lineAt,
  playable,
  recordingFileName,
  recordingState,
  settingsSummary,
  typedLines,
  type KeyEventLike,
} from "./recording-values"

const key = (keysym: number, name: string, timestamp: number, pressed = true, value?: string): KeyEventLike => ({
  definition: { keysym, name, value },
  pressed,
  timestamp,
})
const char = (c: string, timestamp: number): KeyEventLike[] => [
  key(c.codePointAt(0)!, c, timestamp, true, c),
  key(c.codePointAt(0)!, c, timestamp + 30, false, c),
]
const word = (text: string, from: number): KeyEventLike[] => [...text].flatMap((c, i) => char(c, from + i * 100))

describe("typedLines", () => {
  test("characters as typed, a line ending at Enter or a pause", () => {
    const events = [
      ...word("dir", 0),
      key(0xff0d, "Return", 400, true, "\n"),
      key(0xff0d, "Return", 450, false, "\n"),
      ...word("cd x", 1000),
      // A pause: the next line.
      ...word("ok", 6000),
    ]
    expect(typedLines(events)).toEqual([
      { at: 0, text: "dir⏎" },
      { at: 1000, text: "cd x" },
      { at: 6000, text: "ok" },
    ])
  })

  test("keys that type nothing by name, chords with their modifiers, Shift and AltGr alone not", () => {
    const events = [
      key(0xffe3, "Ctrl", 0),
      key(0x63, "c", 10, true, "c"),
      key(0x63, "c", 20, false, "c"),
      key(0xffe1, "Shift", 30),
      key(0x74, "t", 40, true, "t"),
      key(0xffe1, "Shift", 50, false),
      key(0xffe3, "Ctrl", 60, false),
      key(0xffe1, "Shift", 100),
      key(0x41, "A", 110, true, "A"),
      key(0xffe1, "Shift", 120, false),
      key(0xfe03, "AltGr", 130),
      key(0x40, "@", 140, true, "@"),
      key(0xfe03, "AltGr", 150, false),
      key(0xff08, "Backspace", 200),
      key(0xff09, "Tab", 300),
      key(0x0100044f, "я", 400, true, "я"),
    ]
    expect(typedLines(events)).toEqual([{ at: 10, text: "<Ctrl+C><Ctrl+Shift+T>A@<Backspace><Tab>я" }])
  })

  test("none typed: no line", () => {
    expect(typedLines([key(0xffe1, "Shift", 0), key(0xffe1, "Shift", 10, false)])).toEqual([])
  })
})

test("lineAt is the last line begun by then", () => {
  const lines = [
    { at: 100, text: "a" },
    { at: 500, text: "b" },
    { at: 900, text: "c" },
  ]
  expect([0, 100, 499, 500, 2000].map((p) => lineAt(lines, p))).toEqual([-1, 0, 0, 1, 2])
  expect(lineAt([], 10)).toBe(-1)
})

test("formatClock", () => {
  expect([0, 999, 61_000, 3_599_000, 3_600_000, 36_061_000].map(formatClock)).toEqual([
    "0:00",
    "0:00",
    "1:01",
    "59:59",
    "1:00:00",
    "10:01:01",
  ])
})

describe("a recording's state", () => {
  const r = { state: "SAVED", end: "closed", msg: null, size: 100, lost: 0 }
  test("by how it ended", () => {
    expect(recordingState({ ...r, state: "RECORDING", end: null })).toEqual({ label: "recording", tone: "good", detail: null })
    expect(recordingState(r)).toEqual({ label: "closed", tone: "muted", detail: null })
    expect(recordingState({ ...r, end: "desktop", msg: "Disconnected." })).toEqual({
      label: "ended by the desktop",
      tone: "muted",
      detail: "Disconnected.",
    })
    expect(recordingState({ ...r, end: "core_stopped" })).toEqual({
      label: "core stopped",
      tone: "pending",
      detail: "its last seconds are lost",
    })
    expect(recordingState({ ...r, state: "FAILED", end: "not_opened", msg: "core cannot reach guacd", size: 0 })).toEqual({
      label: "did not open",
      tone: "bad",
      detail: "core cannot reach guacd",
    })
    expect(recordingState({ ...r, size: 0 }).label).toBe("nothing recorded")
    expect(recordingState({ ...r, lost: 2048 }).detail).toBe("2.0 KB lost: core's store did not take them")
  })

  test("plays once it ended with something in it; downloads as soon as it holds something", () => {
    expect(playable(r)).toBe(true)
    expect(playable({ state: "RECORDING", size: 100 })).toBe(false)
    expect(playable({ state: "SAVED", size: 0 })).toBe(false)
    expect(downloadable({ state: "RECORDING", size: 100 })).toBe(true)
    expect(downloadable({ state: "FAILED", size: 0 })).toBe(false)
  })
})

test("recordingFileName is core's", () => {
  expect(recordingFileName({ front: "rdp", taskId: "3f2a9c1b-0000-4000-8000-000000000000", number: 4 })).toBe(
    "desktop-rdp-3f2a9c1b-4.guac",
  )
})

test("settingsSummary says those not at their defaults, as the form does", () => {
  const fields = [
    { key: "quality", label: "Quality", default: "balanced", options: [{ value: "best", label: "Best" }, { value: "balanced", label: "Balanced" }] },
    { key: "view_only", label: "View only", default: false },
    { key: "console", label: "Console", default: false },
    { key: "program", label: "Program", default: "" },
    { key: "font_size", label: "Font size", default: 12 },
  ]
  expect(
    settingsSummary(fields, { quality: "best", view_only: true, console: false, program: "", font_size: 16, extra: "x" }),
  ).toEqual(["Quality: Best", "View only", "Font size: 16", "extra: x"])
  expect(settingsSummary(fields, { quality: "balanced" })).toEqual([])
  expect(settingsSummary(fields, null)).toEqual([])
})
