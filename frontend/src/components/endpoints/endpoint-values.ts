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
  valuesEqual,
  type Draft,
  type FieldDef,
} from "@/components/modules/management/field-values"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { isRecord, sameJSON } from "@/lib/json-edit"

/** A channel kind as the endpoint UI uses it: its schema's fields, sorted. */
export interface Kind {
  name: string
  /** The kind's own icon; its SVGs are "" for the default. */
  icon: ModuleIconSvgs
  version: number
  fields: FieldDef[]
}

/**
 * Channel kinds by name from the channelKinds query. Secret fields are plain
 * operation data here: edited and shown as text like the rest.
 */
export function kindsByName(
  kinds: readonly {
    moduleName: string
    icon?: string
    iconLight?: string
    schema: { version: number; fields: readonly FieldDef[] }
  }[],
): Map<string, Kind> {
  const out = new Map<string, Kind>()
  for (const k of kinds) {
    out.set(k.moduleName, {
      name: k.moduleName,
      icon: { icon: k.icon ?? "", iconLight: k.iconLight ?? "" },
      version: k.schema.version,
      fields: k.schema.fields
        .map((f) => (f.type === "secret" ? { ...f, type: "string" } : f))
        .sort(compareFields),
    })
  }
  return out
}

/**
 * Slugs: an endpoint's identifier and a build connection's name, the plain
 * text a build, an export and a file name carry. Mirrors the server's rule.
 */
export const SLUG_PATTERN = /^[a-z0-9][a-z0-9._-]{0,62}$/

/** The longest slug SLUG_PATTERN allows. */
const MAX_SLUG_LENGTH = 63

/** The longest endpoint name core takes (channelendpoints.MaxNameLength), in characters: any text. */
export const MAX_ENDPOINT_NAME_LENGTH = 120

/** The longest description core takes (channelendpoints.MaxDescriptionLength), in characters. */
export const MAX_DESCRIPTION_LENGTH = 500

/**
 * The first free slug of slug+tag, slug+tag-2, -3…, the base shortened to
 * stay within the slug length. tag is "" or starts with "-".
 */
export function freeSlug(slug: string, taken: Iterable<string>, tag: string): string {
  const used = new Set(taken)
  for (let n = 1; ; n++) {
    const suffix = n === 1 ? tag : `${tag}-${n}`
    const candidate = slug.slice(0, MAX_SLUG_LENGTH - suffix.length) + suffix
    if (!used.has(candidate)) return candidate
  }
}

/**
 * Any text made a slug, as the server makes one from an endpoint's name
 * (channelendpoints.Slugify): lowercased, each run of other characters one
 * "-", at most 63 long, "endpoint" when nothing is left. "Edge EU (CDN)" is
 * "edge-eu-cdn".
 */
export function slugify(text: string): string {
  let out = ""
  let dash = false
  for (const ch of text.toLowerCase()) {
    if (/[a-z0-9._-]/.test(ch)) {
      if (ch === "-" && (dash || out === "")) continue
      if (out === "" && (ch === "." || ch === "_")) continue
      out += ch
      dash = ch === "-"
    } else if (out !== "" && !dash) {
      out += "-"
      dash = true
    }
  }
  out = out.slice(0, MAX_SLUG_LENGTH).replace(/-+$/, "")
  return out || "endpoint"
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
 * the same channel kind. Its name becomes a valid one (the form then asks the
 * server for a free one like it); settings the kind does not declare are left
 * out, and values equal to the kind's defaults too, so those fields keep
 * following the defaults (an export fills every default in).
 */
export function parseEndpointImport(
  text: string,
  kinds: ReadonlyMap<string, Kind>,
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
  // An export names the endpoint by its slug: the name to start from.
  const name = typeof doc.name === "string" ? doc.name.trim().slice(0, MAX_ENDPOINT_NAME_LENGTH) : ""
  return { ok: true, value: { kind, name: name || "imported", values, notes } }
}

// Generic JSON helpers, kept importable from here for the code that always did.
export { isRecord, sameJSON } from "@/lib/json-edit"

/** An endpoint's name: any text, at most MAX_ENDPOINT_NAME_LENGTH characters, on one line. */
export function endpointNameError(name: string): string | null {
  const n = name.trim()
  if (n === "") return "Required"
  if ([...n].length > MAX_ENDPOINT_NAME_LENGTH) return `At most ${MAX_ENDPOINT_NAME_LENGTH} characters`
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(n)) return "One line: no line breaks or tabs"
  return null
}

/** A slug (an endpoint's, a connection's name): see SLUG_PATTERN. */
export function slugError(slug: string): string | null {
  const s = slug.trim()
  if (s === "") return "Required"
  if (!SLUG_PATTERN.test(s)) {
    return "Lowercase letters, digits, '.', '_' or '-', starting with a letter or digit (at most 63)"
  }
  return null
}

const AS_TEXT = { secretAsText: true }

/**
 * Drafts for an endpoint's stored values (null: a new endpoint), a setting
 * not set filled in with the channel's default: the form shows what the
 * endpoint will use, not empty fields.
 */
export function endpointDrafts(
  fields: readonly FieldDef[],
  values: Record<string, unknown> | null,
): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of fields) {
    out[f.key] = initialDraft(f, values?.[f.key] ?? f.default ?? null, AS_TEXT)
  }
  return out
}

/**
 * The values object to submit (it replaces the stored one), plus per-key
 * errors. Unset fields are left out, and so are fields at their default
 * (the form starts with the defaults filled in): the default applies, and
 * keeps applying should the channel change it.
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
    else if (v !== null && v !== undefined && !valuesEqual(v, f.default)) values[f.key] = v
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

/** File name for a downloaded endpoint, by its slug. */
export function exportFileName(slug: string): string {
  return `${slug}.endpoint.json`
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
