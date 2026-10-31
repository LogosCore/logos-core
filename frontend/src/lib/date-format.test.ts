import { describe, expect, test } from "vitest"
import { formatDate, formatDateTime, formatTime } from "./date-format"
import { dayGroup, formatAbsolute, historyGroup, relativeTime } from "./relative-time"

// Local times, so the test reads the same in any time zone.
const at = new Date(2026, 9, 5, 9, 7, 3, 42)

describe("the app's time format", () => {
  test("yyyy.mm.dd hh:mm:ss, 24-hour, zero-padded", () => {
    expect(formatDateTime(at)).toBe("2026.10.05 09:07:03")
    expect(formatDateTime(new Date(2026, 9, 5, 21, 7, 3))).toBe("2026.10.05 21:07:03")
    expect(formatDate(at)).toBe("2026.10.05")
    expect(formatTime(at)).toBe("09:07:03")
    expect(formatDateTime(at, "yyyy.mm.dd hh:MM:ss.SSS")).toBe("2026.10.05 09:07:03.042")
    expect(formatDateTime(at.toISOString())).toBe("2026.10.05 09:07:03")
    expect(formatDateTime("not a time")).toBe("")
  })

  test("the relative helpers write dates the same way", () => {
    const now = new Date(2026, 9, 31, 12, 0, 0)
    expect(formatAbsolute(at)).toBe("2026.10.05 09:07:03")
    expect(relativeTime(at, now)).toBe("2026.10.05")
    expect(relativeTime(new Date(2026, 9, 30, 21, 5), now)).toBe("yesterday at 21:05")
    expect(dayGroup(at, now)).toEqual({ key: "2026-10-5", label: "2026.10.05, Mon" })
    expect(dayGroup(new Date(2026, 9, 31, 1), now).label).toBe("Today")
    expect(historyGroup(at, now).label).toBe("2026.10.05")
  })
})
