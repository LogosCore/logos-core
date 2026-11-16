import { describe, expect, test } from "vitest"
import { matchTimezones, timezoneNames, timezoneOffset, type Timezone } from "./timezones"

const zones: Timezone[] = [
  { name: "America/New_York", offset: "UTC-5" },
  { name: "America/Newfoundland", offset: "UTC-3:30" },
  { name: "Asia/Kolkata", offset: "UTC+5:30" },
  { name: "Europe/Berlin", offset: "UTC+1" },
  { name: "Europe/Kyiv", offset: "UTC+2" },
  { name: "Pacific/Auckland", offset: "UTC+13" },
  { name: "UTC", offset: "UTC" },
]
const names = (z: Timezone[]) => z.map((t) => t.name)

describe("timezones", () => {
  test("the browser's list, UTC among them, sorted", () => {
    const all = timezoneNames()
    expect(all).toContain("UTC")
    expect(all).toContain("Europe/Berlin")
    expect(all).toEqual([...all].sort())
  })

  test("an offset now, as UTC says it", () => {
    // Midsummer and midwinter: Berlin keeps summer time, Kolkata none.
    expect(timezoneOffset("Europe/Berlin", new Date("2026-07-01T12:00:00Z"))).toBe("UTC+2")
    expect(timezoneOffset("Europe/Berlin", new Date("2026-01-15T12:00:00Z"))).toBe("UTC+1")
    expect(timezoneOffset("Asia/Kolkata", new Date("2026-01-15T12:00:00Z"))).toBe("UTC+5:30")
    expect(timezoneOffset("UTC", new Date())).toBe("UTC")
    expect(timezoneOffset("Mars/Olympus_Mons", new Date())).toBe("")
  })

  test("matched by any part of the name, the city's start first, the browser's own before all", () => {
    expect(names(matchTimezones(zones, "kyiv", ""))).toEqual(["Europe/Kyiv"])
    expect(names(matchTimezones(zones, "new y", ""))).toEqual(["America/New_York"])
    expect(names(matchTimezones(zones, "america/new", ""))).toEqual(["America/New_York", "America/Newfoundland"])
    // "europe" is no city's start: by name. A city's start goes first.
    expect(names(matchTimezones(zones, "Europe", ""))).toEqual(["Europe/Berlin", "Europe/Kyiv"])
    expect(names(matchTimezones(zones, "an", ""))).toEqual(["America/Newfoundland", "Pacific/Auckland"])
    expect(names(matchTimezones(zones, "new", "America/Newfoundland"))).toEqual(["America/Newfoundland", "America/New_York"])
  })

  test("matched by an offset, UTC's or GMT's", () => {
    expect(names(matchTimezones(zones, "utc+2", ""))).toEqual(["Europe/Kyiv"])
    expect(names(matchTimezones(zones, "GMT+5:30", ""))).toEqual(["Asia/Kolkata"])
    expect(names(matchTimezones(zones, "UTC", ""))).toEqual(["UTC"])
  })

  test("nothing typed: all, the browser's own first; at most the limit", () => {
    expect(names(matchTimezones(zones, "", "Europe/Kyiv"))[0]).toBe("Europe/Kyiv")
    expect(matchTimezones(zones, "", "")).toHaveLength(zones.length)
    expect(matchTimezones(zones, "", "", 3)).toHaveLength(3)
    expect(matchTimezones(zones, "nowhere", "")).toEqual([])
  })
})
