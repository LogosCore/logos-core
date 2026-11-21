// A session's connections as the console and command forms offer them, for
// a param of the "connection" format (switch_connection's conn): typed by
// name or by place, shown by name, picked from a list. The minion gets the
// place (from 1); operators never need to know the build's order.

import type { Draft, FieldDef } from "@/components/modules/management/field-values"

/** The connection format of a param (protocol.FormatConnection). */
export const CONNECTION_FORMAT = "connection"

/**
 * A param that takes a connection's place (an int of the connection format:
 * switch_connection's conn), typed by name or place. A structured one
 * (edit_connection's connection) takes the connection whole, as JSON.
 */
export function isConnectionPlace(p: { type: string; format?: string | null }): boolean {
  return p.format === CONNECTION_FORMAT && p.type === "int"
}

/**
 * Drafts with connection c in fields of the connection format: its place in
 * the int ones, its JSON in the structured ones; a field in keep (typed
 * already) stays as it is.
 */
export function withConnection(
  fields: readonly FieldDef[],
  drafts: Record<string, Draft>,
  c: ConnectionChoice,
  keep: ReadonlySet<string> = new Set(),
): Record<string, Draft> {
  const out = { ...drafts }
  for (const f of fields) {
    if (f.format !== CONNECTION_FORMAT || keep.has(f.key)) continue
    if (f.type === "int") out[f.key] = { kind: "text", text: String(c.place) }
    if (f.type === "structured" && c.value) out[f.key] = { kind: "json", text: connectionJSON(c.value) }
  }
  return out
}

/** One of a session's build's connections. */
export interface ConnectionChoice {
  /** Its place in the build's connections, from 1: what the minion gets. */
  place: number
  name: string
  kind: string
  /** Where it connects, for people: the channel's first setting. */
  address?: string
  /** The one the minion uses now. */
  inUse?: boolean
  /**
   * The connection as a structured connection param takes it (protocol
   * FormatConnection): channel, kind, name, params and values by key.
   */
  value?: ConnectionValue
}

/** A connection as a structured param of the connection format takes it. */
export interface ConnectionValue {
  channel: string
  kind: string
  name: string
  params?: Record<string, unknown>
  values?: Record<string, unknown>
}

/** A connection value as the JSON editor shows it; leaves out empty params and values. */
export function connectionJSON(v: ConnectionValue): string {
  const out: Record<string, unknown> = { channel: v.channel, kind: v.kind, name: v.name }
  if (v.params && Object.keys(v.params).length > 0) out.params = v.params
  if (v.values && Object.keys(v.values).length > 0) out.values = v.values
  return JSON.stringify(out, null, 2)
}

/**
 * A connection param's value from what was typed: a place ("2") or a name
 * ("ws", case aside). A name two connections share needs the place.
 */
export function connectionFromText(
  key: string,
  text: string,
  connections: readonly ConnectionChoice[],
): { ok: true; value: number } | { ok: false; error: string } {
  const t = text.trim()
  if (/^\d+$/.test(t)) return { ok: true, value: parseInt(t, 10) }
  const named = connections.filter((c) => c.name.toLowerCase() === t.toLowerCase())
  if (named.length === 1) return { ok: true, value: named[0].place }
  if (named.length > 1) {
    return { ok: false, error: `${key}: ${named.length} connections are called ${t}: use its number (${named.map((c) => c.place).join(", ")})` }
  }
  const names = connections.map((c) => c.name).join(", ")
  return { ok: false, error: `${key}: no connection ${t}${names ? `: ${names}` : ""}` }
}

/** How a connection param's value is typed back: its name when that names it alone, else its place. */
export function connectionText(value: unknown, connections: readonly ConnectionChoice[]): string {
  const place = typeof value === "number" ? value : Number(value)
  const c = connections.find((x) => x.place === place)
  if (c && connections.filter((x) => x.name.toLowerCase() === c.name.toLowerCase()).length === 1) return c.name
  return String(value)
}

/** The choices a partly typed value could be: by name or place, the one in use last. */
export function matchConnections(partial: string, connections: readonly ConnectionChoice[]): ConnectionChoice[] {
  const p = partial.trim().toLowerCase()
  const hits = connections.filter((c) => p === "" || c.name.toLowerCase().startsWith(p) || String(c.place).startsWith(p))
  return [...hits.filter((c) => !c.inUse), ...hits.filter((c) => c.inUse)]
}

/** One line for a choice: "ws · reverse-persistent · ws://edge:8090". */
export function choiceDetail(c: ConnectionChoice): string {
  return [c.kind, c.address].filter(Boolean).join(" · ")
}
