// Lays out one log record's attrs for reading. Each module has its own log
// structure, so nothing here knows any key: the record's JSON tree is shown
// as received — its top-level values first, then one section per nested
// object (titled by its path), values verbatim.

import { isRecord, type LogLine } from "./log-buffer"

export interface DetailRow {
  path: string // full path, e.g. "headers.accept"
  label: string // key within its section
  text: string // value as shown
}

export interface DetailSection {
  path: string // "" for the record's top level
  rows: DetailRow[]
}

/** Splits attrs into sections, one per object in the tree, in key order. */
export function detailSections(attrs: Record<string, unknown> | null): DetailSection[] {
  if (!attrs) return []
  const out: DetailSection[] = []
  const walk = (obj: Record<string, unknown>, path: string) => {
    const rows: DetailRow[] = []
    const nested: [string, Record<string, unknown>][] = []
    for (const [key, value] of Object.entries(obj)) {
      const p = path ? `${path}.${key}` : key
      if (isRecord(value)) nested.push([p, value])
      else rows.push({ path: p, label: key, text: formatValue(value) })
    }
    if (rows.length > 0) out.push({ path, rows })
    for (const [p, value] of nested) walk(value, p)
  }
  walk(attrs, "")
  return out
}

/** A value as JSON would show it, strings without quotes. */
export function formatValue(v: unknown): string {
  if (typeof v === "string") return v
  if (v === undefined) return "null"
  return JSON.stringify(v)
}

/** The record as JSON, attrs exactly as the module sent them. */
export function recordJSON(line: LogLine): string {
  return JSON.stringify(
    { time: line.time, level: line.level, message: line.message, attrs: line.attrs ?? {} },
    null,
    2,
  )
}
