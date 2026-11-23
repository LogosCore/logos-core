// What the date and time pickers (components/ui/date-picker.tsx) reckon
// with: days as keys (YYYY-MM-DD, a date input's value), a month's page,
// moves by key, bounds, a time's parts, and how far a time is from now. All
// in this browser's time zone; English names, the app's language. No React
// here, so all of it is unit-tested.

/** A day: YYYY-MM-DD, in this browser's time zone. */
export type DayKey = string

/** The first day of a week: Monday, ISO 8601's, as the app writes dates year first. */
export const WEEK_STARTS_ON = 1

export const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
]

const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]

const DAY_KEY = /^(\d{4})-(\d{2})-(\d{2})$/

const pad = (n: number) => String(n).padStart(2, "0")

/** The day a local date is on. */
export function dayKey(d: Date): DayKey {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** A day's local midnight; null for a key that is no day (a 31 November). */
export function dayDate(k: DayKey): Date | null {
  const m = DAY_KEY.exec(k)
  if (!m) return null
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return dayKey(d) === k ? d : null
}

/** A day's year, month (0 to 11) and day of the month. */
function parts(k: DayKey): [number, number, number] {
  const m = DAY_KEY.exec(k)
  if (!m) throw new Error(`not a day: ${k}`)
  return [Number(m[1]), Number(m[2]) - 1, Number(m[3])]
}

/** n days on (back when negative). */
export function addDays(k: DayKey, n: number): DayKey {
  const [y, m, d] = parts(k)
  return dayKey(new Date(y, m, d + n))
}

/** n months on, the day kept where the month has it, else its last: 31 January and one, 28 or 29 February. */
export function addMonths(k: DayKey, n: number): DayKey {
  const [y, m, d] = parts(k)
  const last = new Date(y, m + n + 1, 0).getDate()
  return dayKey(new Date(y, m + n, Math.min(d, last)))
}

/** The day in month m (0 to 11) of year y nearest k's day of the month. */
export function inMonth(k: DayKey, y: number, m: number): DayKey {
  const last = new Date(y, m + 1, 0).getDate()
  return dayKey(new Date(y, m, Math.min(parts(k)[2], last)))
}

export function yearOf(k: DayKey): number {
  return parts(k)[0]
}

export function monthOf(k: DayKey): number {
  return parts(k)[1]
}

export function sameMonth(a: DayKey, b: DayKey): boolean {
  return a.slice(0, 7) === b.slice(0, 7)
}

/** The six weeks a month's page shows, from the week its first day is in: 42 days. */
export function monthGrid(k: DayKey, weekStartsOn = WEEK_STARTS_ON): DayKey[] {
  const [y, m] = parts(k)
  const back = (new Date(y, m, 1).getDay() - weekStartsOn + 7) % 7
  return Array.from({ length: 42 }, (_, i) => dayKey(new Date(y, m, 1 - back + i)))
}

/** The weekdays from weekStartsOn: two letters, and whole. */
export function weekdays(weekStartsOn = WEEK_STARTS_ON): { short: string; long: string }[] {
  return Array.from({ length: 7 }, (_, i) => {
    const long = WEEKDAYS[(weekStartsOn + i) % 7]
    return { short: long.slice(0, 2), long }
  })
}

/** A month's page's title: November 2026. */
export function monthTitle(k: DayKey): string {
  const [y, m] = parts(k)
  return `${MONTHS[m]} ${y}`
}

/** A day as a screen reader says it: Tuesday, 24 November 2026. */
export function dayLabel(k: DayKey): string {
  const [y, m, d] = parts(k)
  return `${WEEKDAYS[new Date(y, m, d).getDay()]}, ${d} ${MONTHS[m]} ${y}`
}

/** Whether day k is within min and max, days both (none: no bound). */
export function dayAllowed(k: DayKey, min?: DayKey | null, max?: DayKey | null): boolean {
  return (!min || k >= min) && (!max || k <= max)
}

/** k, or the bound it is past. */
export function clampDay(k: DayKey, min?: DayKey | null, max?: DayKey | null): DayKey {
  if (min && k < min) return min
  if (max && k > max) return max
  return k
}

/** Whether a whole month (year y, month m) is past a bound: none of its days allowed. */
export function monthAllowed(y: number, m: number, min?: DayKey | null, max?: DayKey | null): boolean {
  const first = dayKey(new Date(y, m, 1))
  const last = dayKey(new Date(y, m + 1, 0))
  return (!min || last >= min) && (!max || first <= max)
}

/** Whether a whole year is past a bound. */
export function yearAllowed(y: number, min?: DayKey | null, max?: DayKey | null): boolean {
  return (!min || `${y}-12-31` >= min) && (!max || `${y}-01-01` <= max)
}

/**
 * The day a key moves the focus to in a month's grid, from k: ← and → a
 * day, ↑ and ↓ a week, PageUp and PageDown a month (a year with Shift),
 * Home and End the week's first and last day. null for another key.
 */
export function moveDay(k: DayKey, key: string, shift = false, weekStartsOn = WEEK_STARTS_ON): DayKey | null {
  switch (key) {
    case "ArrowLeft":
      return addDays(k, -1)
    case "ArrowRight":
      return addDays(k, 1)
    case "ArrowUp":
      return addDays(k, -7)
    case "ArrowDown":
      return addDays(k, 7)
    case "PageUp":
      return addMonths(k, shift ? -12 : -1)
    case "PageDown":
      return addMonths(k, shift ? 12 : 1)
    case "Home":
    case "End": {
      const [y, m, d] = parts(k)
      const back = (new Date(y, m, d).getDay() - weekStartsOn + 7) % 7
      return addDays(k, key === "Home" ? -back : 6 - back)
    }
  }
  return null
}

/**
 * Where a key moves the focus in a grid of count items, columns wide, from
 * i: ← → one, ↑ ↓ a row, Home and End the first and last; kept within it.
 * null for another key.
 */
export function moveInGrid(i: number, key: string, columns: number, count: number): number | null {
  const to = (n: number) => Math.min(count - 1, Math.max(0, n))
  switch (key) {
    case "ArrowLeft":
      return to(i - 1)
    case "ArrowRight":
      return to(i + 1)
    case "ArrowUp":
      return to(i - columns)
    case "ArrowDown":
      return to(i + columns)
    case "Home":
      return 0
    case "End":
      return count - 1
  }
  return null
}

/** The twelve years a page of years shows, with y among them: from the multiple of 12 below it. */
export function yearPage(y: number): number[] {
  const start = y - (((y % 12) + 12) % 12)
  return Array.from({ length: 12 }, (_, i) => start + i)
}

// ---------------------------------------------------------------------------
// Times: a datetime-local input's value, YYYY-MM-DDTHH:MM[:SS]
// ---------------------------------------------------------------------------

/** A time of a day. */
export interface DayTime {
  day: DayKey
  h: number
  m: number
  s: number
}

const DATE_TIME = /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/

/** A datetime-local value's day and time; null for one that is none. */
export function splitDateTime(v: string): DayTime | null {
  const m = DATE_TIME.exec(v)
  if (!m || !dayDate(m[1])) return null
  const [h, mi, s] = [Number(m[2]), Number(m[3]), Number(m[4] ?? 0)]
  return h < 24 && mi < 60 && s < 60 ? { day: m[1], h, m: mi, s } : null
}

/** A day and time as a datetime-local value, seconds only when there are some. */
export function joinDateTime(t: DayTime): string {
  return `${t.day}T${pad(t.h)}:${pad(t.m)}${t.s ? `:${pad(t.s)}` : ""}`
}

/** A day and time's instant, in this browser's time zone. */
export function instantOf(t: DayTime): Date {
  const [y, m, d] = parts(t.day)
  return new Date(y, m, d, t.h, t.m, t.s)
}

/** A local date's day and time, to the second. */
export function dayTimeOf(d: Date): DayTime {
  return { day: dayKey(d), h: d.getHours(), m: d.getMinutes(), s: d.getSeconds() }
}

/**
 * Whether any moment from day at from to day at to (each [h, m, s]) is
 * within min and max: an hour of the hours' column, a minute of the
 * minutes'.
 */
export function spanAllowed(day: DayKey, from: [number, number, number], to: [number, number, number], min?: Date | null, max?: Date | null): boolean {
  const start = instantOf({ day, h: from[0], m: from[1], s: from[2] }).getTime()
  const end = instantOf({ day, h: to[0], m: to[1], s: to[2] }).getTime()
  return (!min || end >= min.getTime()) && (!max || start <= max.getTime())
}

/** t, or the bound it is past (to the second). */
export function clampTime(t: DayTime, min?: Date | null, max?: Date | null): DayTime {
  const at = instantOf(t).getTime()
  if (min && at < min.getTime()) return dayTimeOf(min)
  if (max && at > max.getTime()) return dayTimeOf(max)
  return t
}

/**
 * A bound a picker takes as an instant: a Date, an RFC 3339 time, or a
 * datetime-local value (local); null for none, or one that does not read.
 */
export function boundOf(b: Date | string | null | undefined): Date | null {
  if (b === null || b === undefined || b === "") return null
  if (b instanceof Date) return Number.isNaN(b.getTime()) ? null : b
  const local = splitDateTime(b)
  if (local) return instantOf(local)
  const ms = Date.parse(b)
  return Number.isNaN(ms) ? null : new Date(ms)
}

/** How far a time is from now, as a picker's footer says it: "in 2 h 15 min", "3 days ago", "now". */
export function distanceText(at: Date, now: Date): string {
  const diff = at.getTime() - now.getTime()
  const minutes = Math.round(Math.abs(diff) / 60_000)
  if (minutes === 0) return "now"
  let text: string
  if (minutes < 60) text = `${minutes} min`
  else if (minutes < 24 * 60) {
    const h = Math.floor(minutes / 60)
    const m = minutes % 60
    text = m ? `${h} h ${m} min` : `${h} h`
  } else {
    const days = Math.round(minutes / (24 * 60))
    text = `${days} day${days === 1 ? "" : "s"}`
  }
  return diff > 0 ? `in ${text}` : `${text} ago`
}

/** How far a day is from today: "today", "tomorrow", "in 3 days", "2 days ago". */
export function dayDistanceText(k: DayKey, today: DayKey): string {
  const a = dayDate(k)
  const b = dayDate(today)
  if (!a || !b) return ""
  // Days apart, a clock change aside.
  const n = Math.round((a.getTime() - b.getTime()) / 86_400_000)
  if (n === 0) return "today"
  if (n === 1) return "tomorrow"
  if (n === -1) return "yesterday"
  return n > 0 ? `in ${n} days` : `${-n} days ago`
}

/** This browser's time zone as a picker says it: Europe/Kyiv, UTC+2 (UTC+5:30, UTC). */
export function zoneText(now: Date = new Date()): string {
  const off = -now.getTimezoneOffset()
  const abs = Math.abs(off)
  const utc = off === 0 ? "UTC" : `UTC${off > 0 ? "+" : "-"}${Math.floor(abs / 60)}${abs % 60 ? `:${pad(abs % 60)}` : ""}`
  const name = Intl.DateTimeFormat().resolvedOptions().timeZone
  return name && name !== "UTC" ? `${name}, ${utc}` : utc
}

/** What a calendar's cursor carries (the element the focus is on): a picker's popup focuses it as it opens. */
export const CURSOR_ATTR = "data-calendar-cursor"

/** A wheel's turn this many pixels or more is a mouse's notch: one step, whatever its size. */
const WHEEL_NOTCH = 40
/** A touchpad's small deltas make a step each this many pixels. */
const WHEEL_STEP = 30

/**
 * The steps a wheel event makes (signed: down and on positive), and what
 * is left gathered for the next: a mouse's notch is one step (a line or a
 * page counted in pixels first), a touchpad's small deltas gather until
 * they make one.
 */
export function wheelSteps(deltaY: number, deltaMode: number, gathered: number): { steps: number; gathered: number } {
  const px = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 400 : deltaY
  if (Math.abs(px) >= WHEEL_NOTCH) return { steps: Math.sign(px), gathered: 0 }
  // A turn the other way starts over.
  const total = Math.sign(gathered) === -Math.sign(px) ? px : gathered + px
  // (|| 0: a -0 is none.)
  const steps = Math.trunc(total / WHEEL_STEP) || 0
  return { steps, gathered: total - steps * WHEEL_STEP }
}
