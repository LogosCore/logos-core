import type { FieldDef } from "@/components/modules/management/field-values"
import { formatDateTime } from "@/lib/date-format"
import { dateTimeValue } from "@/lib/date-time-text"
import { commandOf, restParam, words, type CommandDef } from "./command-line"

// A task's time (docs: contracts/minion-messages/tasks-and-results,
// Scheduled tasks): the command form's Schedule field, and a command line's
// --at=, for a command whose task takes one (the session's command's
// schedulable: not core's own, not a tunnel). Core keeps the task SCHEDULED
// until then. A time is a date and time, in this browser's time zone as the
// app shows times (2026.11.24 10:00:00), or RFC 3339 with its zone.

/** The command form's Schedule field's key: none a param can have (keys are [a-z][a-z0-9_]*). */
export const SCHEDULE_KEY = "@at"

/** The command form's Schedule field, under More options, after the params. */
export const SCHEDULE_FIELD: FieldDef = {
  key: SCHEDULE_KEY,
  type: "datetime",
  label: "Schedule",
  description: "When it may go to the minion, in this browser's time: core keeps it until then. Empty: at once.",
  order: Number.MAX_SAFE_INTEGER,
  required: false,
  advanced: true,
  platforms: [],
}

/** A command a line can schedule: one that takes a time, with no param of its own called at. */
type Schedulable = CommandDef & { schedulable?: boolean }

/** The command form's values split: the params' args, and the task's time (null: none). */
export function splitSchedule(values: Record<string, unknown>): { args: Record<string, unknown>; at: string | null } {
  const { [SCHEDULE_KEY]: at, ...args } = values
  return { args, at: typeof at === "string" && at !== "" ? at : null }
}

/** A task's time as a line's option: --at="2026.11.24 10:00:00". */
export function atOption(at: string): string {
  return `--at="${formatDateTime(at)}"`
}

// An RFC 3339 time with its zone (Z, +02:00).
const ZONED = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$/i

/**
 * A time as typed (--at=): the RFC 3339 time core takes, or why it is not
 * one. A date and time after now: the app's form in this browser's time
 * zone (2026.11.24 10:00, seconds if wanted; - or / for the dots, T for the
 * space), or RFC 3339 with its zone.
 */
export function parseScheduleText(text: string, now: Date = new Date()): { ok: true; at: string } | { ok: false; error: string } {
  const t = text.trim()
  let at: Date | null = null
  // The app's form in local time, as the form's field reads it
  // (a 31 November is none); else RFC 3339 with its zone.
  const local = dateTimeValue(t)
  if (local) at = new Date(local)
  else if (ZONED.test(t) && !Number.isNaN(Date.parse(t))) at = new Date(t)
  if (!at) return { ok: false, error: `--at: a date and time, as 2026.11.24 10:00 (this browser's time), not "${t}"` }
  if (at.getTime() <= now.getTime()) return { ok: false, error: `--at: ${formatDateTime(at)} has passed` }
  return { ok: true, at: at.toISOString() }
}

/**
 * A line's time, taken off it: --at=… (or --at …) on a command whose task
 * takes one and that has no param called at. With a param that takes the
 * rest of the line (a shell's command line), only at its end, as the
 * line's other options; elsewhere, anywhere. line is the line without it,
 * at the time core takes (null: none, or error says why the text is not one).
 */
export function takeSchedule(
  line: string,
  commands: readonly Schedulable[],
  now: Date = new Date(),
): { line: string; at: string | null; text: string | null; error: string | null } {
  const none = { line, at: null, text: null, error: null }
  const command = commandOf(line, commands) as Schedulable | undefined
  if (!command?.schedulable || command.params.some((p) => p.key === "at")) return none
  const ws = words(line)
  for (let i = 1; i < ws.length; i++) {
    const m = /^--at(?:=(.*))?$/.exec(ws[i].text)
    if (!m) continue
    const last = m[1] !== undefined ? i : i + 1
    if (last >= ws.length) return { line, at: null, text: "", error: "--at: a date and time, as 2026.11.24 10:00" }
    if (restParam(command) && last !== ws.length - 1) continue
    const text = m[1] ?? ws[i + 1].text
    const rest = `${line.slice(0, ws[i].start).trimEnd()} ${line.slice(ws[last].end).trimStart()}`.trim()
    const parsed = parseScheduleText(text, now)
    return parsed.ok ? { line: rest, at: parsed.at, text, error: null } : { line: rest, at: null, text, error: parsed.error }
  }
  return none
}
