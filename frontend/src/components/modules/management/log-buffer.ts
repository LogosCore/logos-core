// Client-side buffer for the live log tail. Core relays records and stores
// nothing, so this buffer is the only place they live: capped, deduplicated
// across stream restarts, and gone when the tab closes.

import type { ModuleLogLevel, ModuleLogStreamState } from "@/graphql/gql/graphql"

export const MAX_LINES = 5000

export interface LogLine {
  id: number
  time: string
  ms: number
  level: ModuleLogLevel
  message: string
  attrs: Record<string, unknown> | null
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
  // Time of the newest line, to skip backlog records already shown when the
  // stream restarts (reconnect, level change keeps nothing).
  lastMs: number
  dropped: number
  lost: number
  state: ModuleLogStreamState | null
  reason: string | null
}

export const emptyBuffer: LogBuffer = {
  lines: [],
  nextId: 1,
  lastMs: 0,
  dropped: 0,
  lost: 0,
  state: null,
  reason: null,
}

/** Applies one subscription event; returns a new buffer. */
export function applyEvent(buf: LogBuffer, ev: LogEventLike): LogBuffer {
  let { nextId, lastMs } = buf
  const added: LogLine[] = []
  for (const r of ev.records) {
    const ms = Date.parse(r.time)
    // A restarted stream resends its backlog; keep only what is newer.
    if (ev.backlog && ms <= lastMs) continue
    added.push({
      id: nextId++,
      time: r.time,
      ms,
      level: r.level,
      message: r.message,
      attrs: isRecord(r.attrs) ? r.attrs : null,
    })
    if (ms > lastMs) lastMs = ms
  }
  let lines = buf.lines
  if (added.length > 0) {
    lines = lines.concat(added)
    if (lines.length > MAX_LINES) lines = lines.slice(lines.length - MAX_LINES)
  }
  return {
    lines,
    nextId,
    lastMs,
    dropped: buf.dropped + ev.dropped,
    lost: buf.lost + (ev.lost ? 1 : 0),
    state: ev.state ?? buf.state,
    reason: ev.state ? (ev.reason ?? null) : buf.reason,
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
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

/** Whether a line contains every whitespace-separated term (case-insensitive). */
export function lineMatches(line: LogLine, query: string): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return true
  const text = `${line.level} ${line.message} ${formatAttrs(line.attrs)}`.toLowerCase()
  return terms.every((t) => text.includes(t))
}

/** One line as plain text, for download. */
export function lineText(line: LogLine): string {
  const attrs = formatAttrs(line.attrs)
  return `${line.time} ${line.level.padEnd(5)} ${line.message}${attrs ? " " + attrs : ""}`
}
