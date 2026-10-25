// Client-side buffer for the live log tail. Core relays records and stores
// nothing, so this buffer is the only place they live: capped, deduplicated
// across stream restarts, and gone when the tab closes.

import type { ModuleLogLevel, ModuleLogStreamState } from "@/graphql/gql/graphql"

/** Lines kept while following the tail. */
export const MAX_LINES = 5000
/**
 * Lines kept at most. While the reader is scrolled up or paused the buffer is
 * not trimmed (trimming shifts every row under them), up to this cap.
 */
export const HARD_MAX_LINES = 2 * MAX_LINES

export interface LogLine {
  id: number // ascending in buffer order
  time: string
  ms: number
  level: ModuleLogLevel
  message: string
  attrs: Record<string, unknown> | null
  // Computed once at ingest: the one-line attrs preview and the lowercase
  // text the filter searches, so renders and filtering stay cheap.
  attrsText: string
  searchText: string
}

export interface LogEventLike {
  backlog: boolean
  dropped: number
  lost: boolean
  state?: ModuleLogStreamState | null
  reason?: string | null
  records: { time: string; level: ModuleLogLevel; message: string; attrs?: unknown }[]
}

export interface LogBuffer {
  lines: LogLine[]
  nextId: number
  // Newest record time, and the records seen at exactly that millisecond:
  // a restarted stream resends its backlog, and only newer records are kept.
  lastMs: number
  lastKeys: string[]
  dropped: number
  lost: number
  state: ModuleLogStreamState | null
  reason: string | null
}

export const emptyBuffer: LogBuffer = {
  lines: [],
  nextId: 1,
  lastMs: 0,
  lastKeys: [],
  dropped: 0,
  lost: 0,
  state: null,
  reason: null,
}

/**
 * Applies one subscription event; returns a new buffer. With trim, the buffer
 * is cut back to MAX_LINES; without (reader scrolled up or paused), only to
 * HARD_MAX_LINES.
 */
export function applyEvent(buf: LogBuffer, ev: LogEventLike, trim = true): LogBuffer {
  let { nextId, lastMs, lastKeys } = buf
  const added: LogLine[] = []
  for (const r of ev.records) {
    const ms = Date.parse(r.time)
    const key = `${r.time}\u0000${r.message}`
    if (ev.backlog && (ms < lastMs || (ms === lastMs && lastKeys.includes(key)))) continue
    if (ms > lastMs) {
      lastMs = ms
      lastKeys = [key]
    } else if (ms === lastMs) {
      lastKeys = [...lastKeys, key]
    }
    const attrs = isRecord(r.attrs) ? r.attrs : null
    const attrsText = formatAttrs(attrs)
    added.push({
      id: nextId++,
      time: r.time,
      ms,
      level: r.level,
      message: r.message,
      attrs,
      attrsText,
      searchText: `${r.level} ${r.message} ${attrsText}`.toLowerCase(),
    })
  }
  let lines = buf.lines
  if (added.length > 0) lines = lines.concat(added)
  const max = trim ? MAX_LINES : HARD_MAX_LINES
  if (lines.length > max) lines = lines.slice(lines.length - max)
  return {
    lines,
    nextId,
    lastMs,
    lastKeys,
    dropped: buf.dropped + ev.dropped,
    lost: buf.lost + (ev.lost ? 1 : 0),
    state: ev.state ?? buf.state,
    reason: ev.state ? (ev.reason ?? null) : buf.reason,
  }
}

/** Clears the lines and the drop counters, keeping stream state and dedupe. */
export function clearLines(buf: LogBuffer): LogBuffer {
  return { ...buf, lines: [], dropped: 0, lost: 0 }
}

export function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

/**
 * Renders attrs on one line as `key=value` pairs in the record's own key
 * order, nested objects as dotted paths; values with spaces are quoted. For
 * the list preview and the filter only — the detail pane shows the structure.
 */
export function formatAttrs(attrs: Record<string, unknown> | null): string {
  if (!attrs) return ""
  const parts: string[] = []
  const walk = (obj: Record<string, unknown>, prefix: string) => {
    for (const [k, v] of Object.entries(obj)) {
      if (isRecord(v)) {
        walk(v, `${prefix}${k}.`)
        continue
      }
      const s = typeof v === "string" ? v : JSON.stringify(v)
      parts.push(`${prefix}${k}=${/[\s"=]/.test(s) ? JSON.stringify(s) : s}`)
    }
  }
  walk(attrs, "")
  return parts.join(" ")
}

/** HH:MM:SS.mmm in local time. */
export function formatTime(ms: number): string {
  const d = new Date(ms)
  const p = (n: number, w = 2) => String(n).padStart(w, "0")
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

/**
 * A predicate for lines containing every whitespace-separated term
 * (case-insensitive); the query is split once, not per line.
 */
export function lineMatcher(query: string): (line: LogLine) => boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return () => true
  return (line) => terms.every((t) => line.searchText.includes(t))
}

/** Index of the first line with id > id (lines are sorted by id). */
export function upperBound(lines: LogLine[], id: number): number {
  let lo = 0
  let hi = lines.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (lines[mid].id <= id) lo = mid + 1
    else hi = mid
  }
  return lo
}

/** The line with id, by binary search. */
export function findLine(lines: LogLine[], id: number): LogLine | undefined {
  const i = upperBound(lines, id) - 1
  return i >= 0 && lines[i].id === id ? lines[i] : undefined
}

/** One line as plain text, for download. */
export function lineText(line: LogLine): string {
  return `${line.time} ${line.level.padEnd(5)} ${line.message}${line.attrsText ? " " + line.attrsText : ""}`
}
