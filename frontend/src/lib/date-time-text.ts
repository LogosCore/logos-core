// A date and time two ways: as the app writes them (yyyy.mm.dd hh:mm, 24
// hours, seconds when there are some; lib/date-format.ts's DATE_TIME) and as
// a datetime-local input's value (YYYY-MM-DDTHH:MM[:SS]), both in this
// browser's time zone. DateTimeInput shows the one and carries the other.

/** A datetime-local value's parts. */
const VALUE = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/

/** What may be typed: the app's form, - or / for the dots, T for the space, one-digit parts. */
const TYPED = /^(\d{4})[./-](\d{1,2})[./-](\d{1,2})(?:\s+|T)(\d{1,2}):(\d{1,2})(?::(\d{1,2}))?$/

const pad = (n: number) => String(n).padStart(2, "0")

/** A datetime-local value as the app writes it: 2026.11.24 10:30; any other text as it is. */
export function dateTimeText(value: string): string {
  const m = VALUE.exec(value)
  if (!m) return value
  return `${m[1]}.${m[2]}.${m[3]} ${m[4]}:${m[5]}${m[6] && m[6] !== "00" ? `:${m[6]}` : ""}`
}

/**
 * Text typed as a datetime-local value (2026-11-24T10:30, seconds when
 * typed and not zero): "" for none, null for text that is no date and time
 * (a 31 November, a 24:00 too).
 */
export function dateTimeValue(text: string): string | null {
  const t = text.trim()
  if (t === "") return ""
  const m = TYPED.exec(t)
  if (!m) return null
  const [y, mo, d, h, mi, s] = [m[1], m[2], m[3], m[4], m[5], m[6] ?? "0"].map(Number)
  const day = new Date(y, mo - 1, d)
  if (day.getFullYear() !== y || day.getMonth() !== mo - 1 || day.getDate() !== d || h > 23 || mi > 59 || s > 59) return null
  return `${y}-${pad(mo)}-${pad(d)}T${pad(h)}:${pad(mi)}${s ? `:${pad(s)}` : ""}`
}

// Dates alone: a date input's value (YYYY-MM-DD), and the app's yyyy.mm.dd.

const DAY_VALUE = /^(\d{4})-(\d{2})-(\d{2})$/
const DAY_TYPED = /^(\d{4})[./-](\d{1,2})[./-](\d{1,2})$/

/** A date input's value as the app writes it: 2026.11.24; any other text as it is. */
export function dateText(value: string): string {
  const m = DAY_VALUE.exec(value)
  return m ? `${m[1]}.${m[2]}.${m[3]}` : value
}

/** Text typed as a date input's value (2026-11-24): "" for none, null for text that is no date. */
export function dateValue(text: string): string | null {
  const t = text.trim()
  if (t === "") return ""
  const m = DAY_TYPED.exec(t)
  if (!m) return null
  const [y, mo, d] = [m[1], m[2], m[3]].map(Number)
  const day = new Date(y, mo - 1, d)
  if (day.getFullYear() !== y || day.getMonth() !== mo - 1 || day.getDate() !== d) return null
  return `${y}-${pad(mo)}-${pad(d)}`
}
