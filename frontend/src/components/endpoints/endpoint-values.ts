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
 * A free name for a duplicate of the endpoint called name: name-dup, or
 * name-dup-2, -3… when that is taken, the base shortened to stay within the
 * name length.
 */
export function duplicateName(name: string, taken: Iterable<string>): string {
  const used = new Set(taken)
  for (let n = 1; ; n++) {
    const suffix = n === 1 ? "-dup" : `-dup-${n}`
    const candidate = name.slice(0, MAX_NAME_LENGTH - suffix.length) + suffix
    if (!used.has(candidate)) return candidate
  }
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
