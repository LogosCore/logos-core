import { describe, expect, it } from "vitest"
import { dateTimeText, dateTimeValue } from "./date-time-text"

describe("dateTimeText", () => {
  it("writes a datetime-local value the app's way, 24 hours, seconds only when there are some", () => {
    expect(dateTimeText("2026-11-24T00:08")).toBe("2026.11.24 00:08")
    expect(dateTimeText("2026-11-24T22:30:05")).toBe("2026.11.24 22:30:05")
    expect(dateTimeText("2026-11-24T22:30:00")).toBe("2026.11.24 22:30")
  })

  it("leaves any other text as it is", () => {
    expect(dateTimeText("")).toBe("")
    expect(dateTimeText("tomorrow")).toBe("tomorrow")
  })
})

describe("dateTimeValue", () => {
  it("reads the app's form, and - / T or one-digit parts too", () => {
    expect(dateTimeValue("2026.11.24 22:30")).toBe("2026-11-24T22:30")
    expect(dateTimeValue(" 2026-11-24T22:30:05 ")).toBe("2026-11-24T22:30:05")
    expect(dateTimeValue("2026/1/4 9:05")).toBe("2026-01-04T09:05")
    expect(dateTimeValue("2026.11.24 22:30:00")).toBe("2026-11-24T22:30")
  })

  it("is empty for nothing, and none for what is no date and time", () => {
    expect(dateTimeValue("  ")).toBe("")
    for (const text of ["2026.11.24", "22:30", "2026.11.31 10:00", "2026.13.01 10:00", "2026.11.24 24:00", "2026.11.24 10:60", "soon"]) {
      expect(dateTimeValue(text), text).toBeNull()
    }
  })
})

describe("dates alone", () => {
  it("writes and reads a date the app's way", async () => {
    const { dateText, dateValue } = await import("./date-time-text")
    expect(dateText("2026-11-24")).toBe("2026.11.24")
    expect(dateText("soon")).toBe("soon")
    expect(dateValue("2026.11.24")).toBe("2026-11-24")
    expect(dateValue("2026/1/4")).toBe("2026-01-04")
    expect(dateValue(" ")).toBe("")
    expect(dateValue("2026.11.31")).toBeNull()
    expect(dateValue("2026.11.24 10:00")).toBeNull()
  })
})
