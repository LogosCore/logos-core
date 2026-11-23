import { describe, expect, it } from "vitest"
import {
  addDays,
  addMonths,
  boundOf,
  clampDay,
  clampTime,
  dayAllowed,
  dayDate,
  dayDistanceText,
  dayKey,
  dayLabel,
  distanceText,
  inMonth,
  joinDateTime,
  monthAllowed,
  monthGrid,
  monthTitle,
  moveDay,
  moveInGrid,
  spanAllowed,
  splitDateTime,
  weekdays,
  wheelSteps,
  yearAllowed,
  yearPage,
} from "./calendar"

describe("days", () => {
  it("reads and writes days as keys, refusing one that is none", () => {
    expect(dayKey(new Date(2026, 10, 24, 23, 59))).toBe("2026-11-24")
    expect(dayDate("2026-11-24")?.getDate()).toBe(24)
    expect(dayDate("2026-11-31")).toBeNull()
    expect(dayDate("24.11.2026")).toBeNull()
  })

  it("adds days and months, a month's end kept within the month", () => {
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01")
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28")
    expect(addMonths("2026-01-31", 1)).toBe("2026-02-28")
    expect(addMonths("2028-01-31", 1)).toBe("2028-02-29")
    expect(addMonths("2026-11-24", -12)).toBe("2025-11-24")
    expect(inMonth("2026-03-31", 2026, 3)).toBe("2026-04-30")
  })

  it("lays a month out in six weeks from Monday", () => {
    const grid = monthGrid("2026-11-24")
    // 1 November 2026 is a Sunday: its week starts on 26 October.
    expect(grid).toHaveLength(42)
    expect(grid[0]).toBe("2026-10-26")
    expect(grid[6]).toBe("2026-11-01")
    expect(grid[41]).toBe("2026-12-06")
    expect(monthGrid("2026-11-24", 0)[0]).toBe("2026-11-01")
  })

  it("names weekdays, months and days in English", () => {
    expect(weekdays().map((w) => w.short)).toEqual(["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"])
    expect(weekdays()[6].long).toBe("Sunday")
    expect(monthTitle("2026-11-24")).toBe("November 2026")
    expect(dayLabel("2026-11-24")).toBe("Tuesday, 24 November 2026")
  })

  it("keeps days, months and years within bounds", () => {
    expect(dayAllowed("2026-11-24", "2026-11-24", null)).toBe(true)
    expect(dayAllowed("2026-11-23", "2026-11-24", null)).toBe(false)
    expect(dayAllowed("2026-12-01", null, "2026-11-30")).toBe(false)
    expect(clampDay("2026-11-01", "2026-11-24", null)).toBe("2026-11-24")
    expect(monthAllowed(2026, 10, "2026-11-24", null)).toBe(true)
    expect(monthAllowed(2026, 9, "2026-11-24", null)).toBe(false)
    expect(yearAllowed(2025, "2026-11-24", null)).toBe(false)
    expect(yearAllowed(2027, null, "2026-11-24")).toBe(false)
  })
})

describe("moves", () => {
  it("moves a day by key: a day, a week, a month or a year, the week's ends", () => {
    expect(moveDay("2026-11-24", "ArrowLeft")).toBe("2026-11-23")
    expect(moveDay("2026-11-24", "ArrowDown")).toBe("2026-12-01")
    expect(moveDay("2026-11-24", "PageUp")).toBe("2026-10-24")
    expect(moveDay("2026-11-24", "PageDown", true)).toBe("2027-11-24")
    // A Tuesday: its week runs from Monday 23 to Sunday 29.
    expect(moveDay("2026-11-24", "Home")).toBe("2026-11-23")
    expect(moveDay("2026-11-24", "End")).toBe("2026-11-29")
    expect(moveDay("2026-11-24", "Enter")).toBeNull()
  })

  it("moves in a grid, kept within it", () => {
    expect(moveInGrid(4, "ArrowUp", 3, 12)).toBe(1)
    expect(moveInGrid(1, "ArrowUp", 3, 12)).toBe(0)
    expect(moveInGrid(11, "ArrowRight", 3, 12)).toBe(11)
    expect(moveInGrid(5, "End", 3, 12)).toBe(11)
    expect(moveInGrid(5, "a", 3, 12)).toBeNull()
  })

  it("pages years by twelve", () => {
    expect(yearPage(2026)).toEqual([2016, 2017, 2018, 2019, 2020, 2021, 2022, 2023, 2024, 2025, 2026, 2027])
    expect(yearPage(2028)[0]).toBe(2028)
  })
})

describe("times", () => {
  it("splits and joins a datetime-local value, seconds only when there are some", () => {
    expect(splitDateTime("2026-11-24T22:30")).toEqual({ day: "2026-11-24", h: 22, m: 30, s: 0 })
    expect(splitDateTime("2026-11-24T22:30:05")).toEqual({ day: "2026-11-24", h: 22, m: 30, s: 5 })
    expect(splitDateTime("2026-11-24T24:00")).toBeNull()
    expect(splitDateTime("soon")).toBeNull()
    expect(joinDateTime({ day: "2026-11-24", h: 9, m: 5, s: 0 })).toBe("2026-11-24T09:05")
    expect(joinDateTime({ day: "2026-11-24", h: 9, m: 5, s: 7 })).toBe("2026-11-24T09:05:07")
  })

  it("keeps hours and minutes within bounds", () => {
    const min = new Date(2026, 10, 24, 14, 30)
    expect(spanAllowed("2026-11-24", [13, 0, 0], [13, 59, 59], min)).toBe(false)
    expect(spanAllowed("2026-11-24", [14, 0, 0], [14, 59, 59], min)).toBe(true)
    expect(spanAllowed("2026-11-24", [14, 29, 0], [14, 29, 59], min)).toBe(false)
    expect(clampTime({ day: "2026-11-24", h: 9, m: 0, s: 0 }, min)).toEqual({ day: "2026-11-24", h: 14, m: 30, s: 0 })
  })

  it("reads a bound as an instant: a Date, RFC 3339, or local", () => {
    expect(boundOf("2026-11-24T10:00:00Z")?.toISOString()).toBe("2026-11-24T10:00:00.000Z")
    expect(boundOf("2026-11-24T10:00")?.getHours()).toBe(10)
    expect(boundOf("")).toBeNull()
    expect(boundOf("soon")).toBeNull()
  })

  it("says how far a time is from now", () => {
    const now = new Date(2026, 10, 24, 12, 0)
    expect(distanceText(new Date(2026, 10, 24, 12, 0, 20), now)).toBe("now")
    expect(distanceText(new Date(2026, 10, 24, 12, 45), now)).toBe("in 45 min")
    expect(distanceText(new Date(2026, 10, 24, 14, 15), now)).toBe("in 2 h 15 min")
    expect(distanceText(new Date(2026, 10, 24, 9, 0), now)).toBe("3 h ago")
    expect(distanceText(new Date(2026, 10, 27, 12, 0), now)).toBe("in 3 days")
    expect(distanceText(new Date(2026, 10, 25, 12, 0), now)).toBe("in 1 day")
    expect(dayDistanceText("2026-11-25", "2026-11-24")).toBe("tomorrow")
    expect(dayDistanceText("2026-11-20", "2026-11-24")).toBe("4 days ago")
    expect(dayDistanceText("2026-11-24", "2026-11-24")).toBe("today")
  })
})

describe("wheelSteps", () => {
  it("makes a mouse's notch one step, whatever its size, lines and pages counted in pixels", () => {
    expect(wheelSteps(100, 0, 0)).toEqual({ steps: 1, gathered: 0 })
    expect(wheelSteps(-53, 0, 12)).toEqual({ steps: -1, gathered: 0 })
    expect(wheelSteps(3, 1, 0)).toEqual({ steps: 1, gathered: 0 })
  })

  it("gathers a touchpad's small deltas into steps, starting over when it turns", () => {
    let g = 0
    let steps = 0
    for (const d of [8, 8, 8, 8, 8]) {
      const r = wheelSteps(d, 0, g)
      g = r.gathered
      steps += r.steps
    }
    expect(steps).toBe(1)
    expect(g).toBe(10)
    expect(wheelSteps(-8, 0, 10)).toEqual({ steps: 0, gathered: -8 })
  })
})
