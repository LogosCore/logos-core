import {
  connectionDraft,
  effectiveKind,
  evaluateFields,
  paramFields,
  settingFields,
  type ChannelKind,
  type ConnectionDraft,
  type FactoryKind,
  type Target,
} from "./build-values"
import { connectionFailoverErrors, connectionFailoverInput, type ConnectionFailover } from "./failover-values"

// A command's connection param (a structured one of the connection format:
// edit_connection's connection) in the build form's terms: its JSON value
// as a ConnectionDraft, every setting of its channel's kind and every
// parameter of its factory there to fill, and a draft back as the JSON the
// param takes.

/** A connection param's value (protocol FormatConnection), by names. */
export interface ConnectionJSON {
  channel: string
  kind: string
  name?: string
  version?: number
  params?: Record<string, unknown>
  values?: Record<string, unknown>
  failover?: ConnectionFailover
  max_message?: number
}

/** The value a connection param's text holds; null for text that is none (empty, not an object, no channel). */
export function readConnectionJSON(text: string): ConnectionJSON | null {
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof v !== "object" || v === null || Array.isArray(v)) return null
  const o = v as Record<string, unknown>
  if (typeof o.channel !== "string" || o.channel === "") return null
  const rec = (x: unknown) => (typeof x === "object" && x !== null && !Array.isArray(x) ? (x as Record<string, unknown>) : undefined)
  return {
    channel: o.channel,
    kind: typeof o.kind === "string" ? o.kind : "",
    ...(typeof o.name === "string" ? { name: o.name } : {}),
    ...(typeof o.version === "number" ? { version: o.version } : {}),
    ...(rec(o.params) ? { params: rec(o.params) } : {}),
    ...(rec(o.values) ? { values: rec(o.values) } : {}),
    ...(rec(o.failover) ? { failover: rec(o.failover) as ConnectionFailover } : {}),
    ...(typeof o.max_message === "number" ? { max_message: o.max_message } : {}),
  }
}

/** A connection value as the build form edits it: every field there, set or at its default. */
export function draftOfConnection(factory: FactoryKind, ch: ChannelKind | undefined, v: ConnectionJSON, target: Target | undefined): ConnectionDraft {
  return connectionDraft(factory, ch, { moduleName: v.channel, kind: v.kind || null, name: v.name ?? null, settings: v.params, values: v.values, failover: v.failover }, target)
}

/**
 * The draft as the param's value, and its errors by "params.<key>" and
 * "values.<key>" (and "failover.fail_after"): a value at its default left
 * out, which core fills in. kept carries what the form does not edit: the
 * channel's settings version (while the channel stays) and max_message.
 */
export function connectionOfDraft(
  factory: FactoryKind,
  ch: ChannelKind | undefined,
  c: ConnectionDraft,
  target: Target | undefined,
  kept: Pick<ConnectionJSON, "channel" | "version" | "max_message"> | null,
): { value: ConnectionJSON; errors: Record<string, string> } {
  const errors: Record<string, string> = {}
  const kind = effectiveKind(c, target, ch)
  const value: ConnectionJSON = { channel: c.moduleName, kind }
  if (c.name.trim()) value.name = c.name.trim()
  if (kept?.channel === c.moduleName && kept.version !== undefined) value.version = kept.version
  const params = evaluateFields("params.", settingFields(ch, kind), c.settings, errors)
  if (Object.keys(params).length > 0) value.params = params
  const values = evaluateFields("values.", paramFields(factory, c.moduleName, kind), c.drafts, errors)
  if (Object.keys(values).length > 0) value.values = values
  connectionFailoverErrors("", c.failover, errors)
  const failover = connectionFailoverInput(c.failover)
  if (failover) value.failover = failover
  if (kept?.max_message !== undefined) value.max_message = kept.max_message
  // connectionFailoverErrors keys by its prefix: ".failover.fail_after".
  for (const k of Object.keys(errors)) {
    if (k.startsWith(".")) {
      errors[k.slice(1)] = errors[k]
      delete errors[k]
    }
  }
  return { value, errors }
}
