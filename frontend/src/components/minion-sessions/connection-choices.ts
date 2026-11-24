// A session's connections as the console and command forms offer them, for
// a param of the "connection" format (switch_connection's conn): typed by
// place, shown by place, channel, kind and address, picked from a list. The
// minion gets the place (from 1). A session's connections have no names.

import type { Draft, FieldDef } from "@/components/modules/management/field-values"

/** The connection format of a param (protocol.FormatConnection). */
export const CONNECTION_FORMAT = "connection"

/**
 * A param that takes a connection's place (an int of the connection format:
 * switch_connection's conn), typed as its place. A structured one
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

/** One of a session's connections. */
export interface ConnectionChoice {
  /** Its place in the minion's connections, from 1: what the minion gets. */
  place: number
  /** The channel it goes through. */
  channel: string
  kind: string
  /** Where it connects, for people: the channel's first setting. */
  address?: string
  /** The one the minion uses now. */
  inUse?: boolean
  /**
   * The connection as a structured connection param takes it (protocol
   * FormatConnection): channel, kind, params and values by key.
   */
  value?: ConnectionValue
}

/** A connection as a structured param of the connection format takes it. */
export interface ConnectionValue {
  channel: string
  kind: string
  params?: Record<string, unknown>
  values?: Record<string, unknown>
}

/** A connection value as the JSON editor shows it; leaves out empty params and values. */
export function connectionJSON(v: ConnectionValue): string {
  const out: Record<string, unknown> = { channel: v.channel, kind: v.kind }
  if (v.params && Object.keys(v.params).length > 0) out.params = v.params
  if (v.values && Object.keys(v.values).length > 0) out.values = v.values
  return JSON.stringify(out, null, 2)
}

/** A connection param's value from what was typed: its place ("2"), one of connections' when they are known. */
export function connectionFromText(
  key: string,
  text: string,
  connections: readonly ConnectionChoice[],
): { ok: true; value: number } | { ok: false; error: string } {
  const t = text.trim()
  const places = connections.map((c) => c.place)
  if (/^\d+$/.test(t)) {
    const place = parseInt(t, 10)
    if (places.length === 0 || places.includes(place)) return { ok: true, value: place }
  }
  return { ok: false, error: `${key}: a connection's number${places.length > 0 ? `, ${places[0]} to ${places[places.length - 1]}` : ""}` }
}

/** How a connection param's value is typed back: its place. */
export function connectionText(value: unknown): string {
  return String(value)
}

/** The choices a partly typed place could be, the one in use last. */
export function matchConnections(partial: string, connections: readonly ConnectionChoice[]): ConnectionChoice[] {
  const p = partial.trim()
  const hits = connections.filter((c) => p === "" || String(c.place).startsWith(p))
  return [...hits.filter((c) => !c.inUse), ...hits.filter((c) => c.inUse)]
}

/** A choice as people know it: "2 · http". */
export function choiceTitle(c: ConnectionChoice): string {
  return `${c.place} · ${c.channel}`
}

/** One line for a choice: "reverse-persistent · ws://edge:8090". */
export function choiceDetail(c: ConnectionChoice): string {
  return [c.kind, c.address].filter(Boolean).join(" · ")
}
