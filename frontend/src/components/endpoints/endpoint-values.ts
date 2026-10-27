// Pure value handling for the channel endpoint editor: drafts for a
// connection schema's fields, and the values object the server expects. Field
// rules are the same as for properties (field-values.ts); what differs is that
// the whole set is submitted at once rather than as a diff, and that secret
// fields are plain operation data, edited as text like the rest.

import { GraphQLRequestError } from "@/lib/graphql-client"
import {
  compareFields,
  emptyDraft,
  formatValue,
  initialDraft,
  parseDraft,
  validateValue,
  type Draft,
  type FieldDef,
} from "@/components/modules/management/field-values"

/** A channel kind as the endpoint UI uses it: its schema's fields, sorted. */
export interface Kind {
  name: string
  /** The kind's own SVG icon; "" for the default. */
  icon: string
  version: number
  fields: FieldDef[]
}

/**
 * Channel kinds by name from the channelKinds query. Secret fields are plain
 * operation data here: edited and shown as text like the rest.
 */
export function kindsByName(
  kinds: readonly { moduleName: string; icon?: string; schema: { version: number; fields: readonly FieldDef[] } }[],
): Map<string, Kind> {
  const out = new Map<string, Kind>()
  for (const k of kinds) {
    out.set(k.moduleName, {
      name: k.moduleName,
      icon: k.icon ?? "",
      version: k.schema.version,
      fields: k.schema.fields
        .map((f) => (f.type === "secret" ? { ...f, type: "string" } : f))
        .sort(compareFields),
    })
  }
  return out
}

/** Endpoint names: unique slugs. Mirrors the server's rule. */
export const ENDPOINT_NAME_PATTERN = /^[a-z0-9][a-z0-9._-]{0,62}$/

/** The longest endpoint name ENDPOINT_NAME_PATTERN allows. */
const MAX_NAME_LENGTH = 63

/**
 * The first free name of name+tag, name+tag-2, -3…, the base shortened to
 * stay within the name length. tag is "" or starts with "-".
 */
function freeName(name: string, taken: Iterable<string>, tag: string): string {
  const used = new Set(taken)
  for (let n = 1; ; n++) {
    const suffix = n === 1 ? tag : `${tag}-${n}`
    const candidate = name.slice(0, MAX_NAME_LENGTH - suffix.length) + suffix
    if (!used.has(candidate)) return candidate
  }
}

/**
 * A free name for a duplicate of the endpoint called name: name-dup, or
 * name-dup-2, -3… when that is taken.
 */
export function duplicateName(name: string, taken: Iterable<string>): string {
  return freeName(name, taken, "-dup")
}

/** The export format of an endpoint (Copy / Download as JSON), as imports take it. */
export const ENDPOINT_EXPORT_FORMAT = "logos.channel-endpoint.v1"

/** The router state with which an import opens the New endpoint form. */
export interface EndpointImportState {
  endpointImport: { name: string; values: Record<string, unknown>; notes: string[] }
}

/** The import a location's state carries, if any. */
export function endpointImportFrom(state: unknown): EndpointImportState["endpointImport"] | null {
  if (!isRecord(state) || !isRecord(state.endpointImport)) return null
  const { name, values, notes } = state.endpointImport
  if (typeof name !== "string" || !isRecord(values) || !Array.isArray(notes)) return null
  return { name, values, notes: notes.filter((n): n is string => typeof n === "string") }
}

/** What an endpoint export becomes: a new endpoint's form, filled in. */
export interface EndpointImport {
  kind: Kind
  name: string
  values: Record<string, unknown>
  /** What the form should say about it: settings left out, another version. */
  notes: string[]
}

/**
 * Reads an endpoint export (Copy / Download as JSON) for a new endpoint of
 * the same channel kind. Its name becomes a valid, free one; settings the kind
 * does not declare are left out, and values equal to the kind's defaults too,
 * so those fields keep following the defaults (an export fills every default
 * in).
 */
export function parseEndpointImport(
  text: string,
  kinds: ReadonlyMap<string, Kind>,
  taken: Iterable<string>,
): { ok: true; value: EndpointImport } | { ok: false; error: string } {
  let doc: unknown
  try {
    doc = JSON.parse(text)
  } catch (err) {
    return { ok: false, error: `Not JSON: ${err instanceof Error ? err.message : String(err)}` }
  }
  if (!isRecord(doc) || doc.format !== ENDPOINT_EXPORT_FORMAT) {
    return { ok: false, error: `Not an endpoint export: its "format" must be "${ENDPOINT_EXPORT_FORMAT}".` }
  }
  const channel = isRecord(doc.channel) ? doc.channel : {}
  const moduleName = typeof channel.module_name === "string" ? channel.module_name : ""
  if (moduleName === "") return { ok: false, error: 'The export names no channel ("channel.module_name").' }
  const kind = kinds.get(moduleName)
  if (!kind) return { ok: false, error: `No "${moduleName}" channel declares connection settings here.` }
  if (doc.params !== undefined && !isRecord(doc.params)) {
    return { ok: false, error: 'Its "params" must be an object of settings.' }
  }

  const notes: string[] = []
  if (typeof channel.schema_version === "number" && channel.schema_version !== kind.version) {
    notes.push(
      `Exported for ${moduleName} schema v${channel.schema_version}; this channel is at v${kind.version}. Check the values.`,
    )
  }
  const fields = new Map(kind.fields.map((f) => [f.key, f]))
  const values: Record<string, unknown> = {}
  const unknown: string[] = []
  for (const [key, value] of Object.entries(doc.params ?? {})) {
    const field = fields.get(key)
    if (!field) unknown.push(key)
    else if (value !== null && !sameJSON(value, field.default)) values[key] = value
  }
  if (unknown.length > 0) {
    notes.push(`Left out, not settings of ${moduleName} v${kind.version}: ${unknown.sort().join(", ")}.`)
  }
  const name = typeof doc.name === "string" ? slug(doc.name) : ""
  return { ok: true, value: { kind, name: freeName(name || "imported", taken, ""), values, notes } }
}

/** name as an endpoint name: lowercase, other characters as "-". */
function slug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^[^a-z0-9]+/, "")
    .slice(0, MAX_NAME_LENGTH)
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

/** Whether two JSON values are equal (key order aside). */
function sameJSON(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((v, i) => sameJSON(v, b[i]))
  if (isRecord(a) && isRecord(b)) {
    const ka = Object.keys(a)
    return ka.length === Object.keys(b).length && ka.every((k) => sameJSON(a[k], b[k]))
  }
  return false
}

export function nameError(name: string): string | null {
  const n = name.trim()
  if (n === "") return "Required"
  if (!ENDPOINT_NAME_PATTERN.test(n)) {
    return "Lowercase letters, digits, '.', '_' or '-', starting with a letter or digit (at most 63)"
  }
  return null
}

const AS_TEXT = { secretAsText: true }

/** Drafts for an endpoint's stored values (null: a new endpoint). */
export function endpointDrafts(
  fields: readonly FieldDef[],
  values: Record<string, unknown> | null,
): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of fields) {
    out[f.key] = initialDraft(f, values?.[f.key] ?? null, AS_TEXT)
  }
  return out
}

/**
 * The values object to submit (it replaces the stored one), plus per-key
 * errors. Unset fields are left out: their default applies.
 */
export function evaluateEndpoint(
  fields: readonly FieldDef[],
  drafts: Record<string, Draft>,
): { values: Record<string, unknown>; errors: Record<string, string> } {
  const values: Record<string, unknown> = {}
  const errors: Record<string, string> = {}
  for (const f of fields) {
    const parsed = parseDraft(f, drafts[f.key] ?? emptyDraft(f, AS_TEXT))
    if (!parsed.ok) {
      errors[f.key] = parsed.error
      continue
    }
    const v = parsed.value
    const error = validateValue(f, v, { fallback: f.default })
    if (error) errors[f.key] = error
    else if (v !== null && v !== undefined) values[f.key] = v
  }
  return { values, errors }
}

/**
 * Field problems from a BAD_USER_INPUT error (extensions.problems), by key;
 * problems that are not one field's are under "".
 */
export function problemsFromError(err: unknown): Record<string, string> {
  const out: Record<string, string> = {}
  if (!(err instanceof GraphQLRequestError)) return out
  for (const e of err.errors) {
    const problems = e.extensions?.problems
    if (!Array.isArray(problems)) continue
    for (const p of problems) {
      if (p && typeof p === "object" && typeof p.message === "string") {
        out[typeof p.key === "string" ? p.key : ""] = p.message
      }
    }
  }
  return out
}

/** File name for a downloaded endpoint. */
export function exportFileName(name: string): string {
  return `${name}.endpoint.json`
}

/** One set value of an endpoint, as the list shows it. */
export interface SettingRow {
  key: string
  label: string
  text: string
}

/**
 * An endpoint's set values in its kind's field order, formatted for display.
 * Without a kind (the channel no longer declares one), its raw keys.
 */
export function settingRows(kind: Kind | null, values: Record<string, unknown>): SettingRow[] {
  if (!kind) {
    return Object.entries(values).map(([key, v]) => ({ key, label: key, text: formatValue("string", v) }))
  }
  return kind.fields
    .filter((f) => values[f.key] !== undefined && values[f.key] !== null)
    .map((f) => ({ key: f.key, label: f.label, text: formatValue(f.type, values[f.key], f.unit) }))
}

/** Endpoints grouped by channel kind, kinds in name order. */
export function groupByKind<T extends { moduleName: string }>(items: readonly T[]): [string, T[]][] {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const list = groups.get(item.moduleName)
    if (list) list.push(item)
    else groups.set(item.moduleName, [item])
  }
  return [...groups].sort(([a], [b]) => a.localeCompare(b))
}
