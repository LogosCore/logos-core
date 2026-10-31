// The app's one way to write a time: yyyy.mm.dd hh:mm:ss, 24-hour, local
// time, whatever the browser's locale. Every absolute date or time shown
// goes through here (FormattedDateTimeText for JSX), never toLocaleString.
//
// Tokens: yyyy year, mm month, dd day, hh hour, MM minute, ss second, SSS
// milliseconds.

export const DATE_TIME = "yyyy.mm.dd hh:MM:ss"
export const DATE = "yyyy.mm.dd"
export const TIME = "hh:MM:ss"

function pad(n: number, width = 2): string {
  return n.toString().padStart(width, "0")
}

function toDate(input: string | number | Date): Date {
  return input instanceof Date ? input : new Date(input)
}

/** A time in fmt (DATE_TIME by default). */
export function formatDateTime(input: string | number | Date, fmt: string = DATE_TIME): string {
  const d = toDate(input)
  if (Number.isNaN(d.getTime())) return ""
  return fmt
    .replace("yyyy", d.getFullYear().toString())
    .replace("mm", pad(d.getMonth() + 1))
    .replace("dd", pad(d.getDate()))
    .replace("hh", pad(d.getHours()))
    .replace("MM", pad(d.getMinutes()))
    .replace("ss", pad(d.getSeconds()))
    .replace("SSS", pad(d.getMilliseconds(), 3))
}

/** The day alone: yyyy.mm.dd. */
export function formatDate(input: string | number | Date): string {
  return formatDateTime(input, DATE)
}

/** The time of day alone: hh:mm:ss. */
export function formatTime(input: string | number | Date): string {
  return formatDateTime(input, TIME)
}
