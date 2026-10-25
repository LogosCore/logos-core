// Turns one log record's flat attrs into sections a human can read. Records
// arrive with groups flattened to dotted keys ("headers.user-agent"); known
// keys of a request line are sorted into named sections, dotted groups get a
// section each, and anything else lands in "Fields" — so a module with other
// attrs still renders sensibly.

import type { LogLine } from "./log-buffer"

export interface DetailField {
  key: string // full attr key
  label: string // key within its section
  value: unknown
  text: string // value formatted for display
}

export interface DetailSection {
  title: string
  fields: DetailField[]
}

// Known attrs, by section, in display order.
const KNOWN: { title: string; keys: string[] }[] = [
  { title: "Summary", keys: ["outcome", "status", "duration_ms", "core_status", "core_ms", "request_id"] },
  { title: "Error", keys: ["stage", "err", "error"] },
  { title: "Request", keys: ["method", "path", "query", "host", "proto", "content_length", "body_bytes", "response_bytes"] },
  { title: "Client", keys: ["client_ip", "remote_addr"] },
]

const BYTES = /(^|_)(bytes|content_length)$/
const MILLIS = /_ms$/

/** Formats a value for display, with units for known suffixes. */
export function formatValue(key: string, v: unknown): string {
  if (v === null || v === undefined) return "—"
  if (typeof v === "number") {
    if (MILLIS.test(key)) return `${v.toLocaleString(undefined, { maximumFractionDigits: 3 })} ms`
    if (BYTES.test(key)) return humanBytes(v)
    return v.toLocaleString()
  }
  if (typeof v === "string") return v === "" ? "(empty)" : v
  if (typeof v === "boolean") return v ? "true" : "false"
  return JSON.stringify(v)
}

export function humanBytes(n: number): string {
  if (n < 0) return `${n}` // e.g. content_length -1: unknown
  if (n < 1024) return `${n} B`
  const units = ["KiB", "MiB", "GiB"]
  let v = n
  let u = -1
  do {
    v /= 1024
    u++
  } while (v >= 1024 && u < units.length - 1)
  return `${v.toLocaleString(undefined, { maximumFractionDigits: 1 })} ${units[u]} (${n.toLocaleString()} B)`
}

/** Splits attrs into display sections; empty sections are left out. */
export function detailSections(attrs: Record<string, unknown> | null): DetailSection[] {
  if (!attrs) return []
  const used = new Set<string>()
  const field = (key: string, label: string): DetailField => {
    used.add(key)
    return { key, label, value: attrs[key], text: formatValue(key, attrs[key]) }
  }

  const out: DetailSection[] = []
  for (const s of KNOWN) {
    const fields = s.keys.filter((k) => k in attrs).map((k) => field(k, k))
    if (fields.length > 0) out.push({ title: s.title, fields })
  }

  // Dotted groups, alphabetical within each.
  const groups = new Map<string, DetailField[]>()
  for (const key of Object.keys(attrs).sort()) {
    if (used.has(key)) continue
    const dot = key.indexOf(".")
    if (dot <= 0) continue
    const group = key.slice(0, dot)
    const list = groups.get(group) ?? []
    list.push(field(key, key.slice(dot + 1)))
    groups.set(group, list)
  }

  const rest = Object.keys(attrs)
    .filter((k) => !used.has(k))
    .sort()
    .map((k) => field(k, k))
  if (rest.length > 0) out.push({ title: "Fields", fields: rest })

  for (const [group, fields] of groups) {
    out.push({ title: titleCase(group), fields })
  }
  return out
}

function titleCase(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1).replace(/_/g, " ")
}

/** The record as JSON with dotted keys nested back into objects. */
export function recordJSON(line: LogLine): string {
  return JSON.stringify(
    { time: line.time, level: line.level, message: line.message, attrs: unflatten(line.attrs ?? {}) },
    null,
    2,
  )
}

export function unflatten(attrs: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  outer: for (const [key, value] of Object.entries(attrs)) {
    const parts = key.split(".")
    let node = out
    for (const part of parts.slice(0, -1)) {
      node[part] ??= {}
      const next = node[part]
      if (typeof next !== "object" || next === null || Array.isArray(next)) {
        // A scalar already sits on this path ("a" and "a.b" both set): keep
        // the dotted key flat rather than lose a value.
        out[key] = value
        continue outer
      }
      node = next as Record<string, unknown>
    }
    const leaf = parts[parts.length - 1]
    if (typeof node[leaf] === "object" && node[leaf] !== null) out[key] = value
    else node[leaf] = value
  }
  return out
}

/** Tone for an HTTP status: success, client error, server error. */
export function statusTone(v: unknown): "ok" | "warn" | "bad" | null {
  if (typeof v !== "number" || v === 0) return null
  if (v < 400) return "ok"
  if (v < 500) return "warn"
  return "bad"
}
