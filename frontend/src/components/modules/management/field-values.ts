// Pure value handling for module-management fields: turning a stored JSON value
// into what a control edits (a "draft"), parsing a draft back into the JSON the
// server expects, client-side validation that mirrors the server's rules, and
// the display formatting shared by the status list, the diff and the activity
// feed. No React here, so all of it is unit-tested directly.
//
// Wire encoding per field type (see module_management.graphql):
//   string / url / secret / enum → string
//   int / bytes                  → integer number
//   number                       → number
//   bool                         → boolean
//   duration                     → Go duration string ("30s", "1m30s")
//   string_list                  → string[]
//   string_map                   → { [key: string]: string }
//   datetime                     → RFC 3339 timestamp ("2027-01-31T00:00:00Z")
//   enum_list                    → string[] of option values
//   file                         → a build file's id (build schemas only)
// Bounds (min / max) are numbers, duration strings for duration, RFC 3339
// strings for datetime.

import type {
  ModuleActionInvocationFieldsFragment,
  ModuleConfigChangesQuery,
  ModuleFieldDefFieldsFragment,
} from "@/graphql/gql/graphql"

export type FieldDef = ModuleFieldDefFieldsFragment

export type FieldType =
  | "string"
  | "url"
  | "secret"
  | "enum"
  | "int"
  | "number"
  | "bytes"
  | "bool"
  | "duration"
  | "string_list"
  | "string_map"
  | "datetime"
  | "enum_list"
  | "file"

// ---------------------------------------------------------------------------
// Bytes
// ---------------------------------------------------------------------------

export const BYTE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB"] as const
export type ByteUnit = (typeof BYTE_UNITS)[number]

export function byteUnitFactor(unit: ByteUnit): number {
  return 1024 ** BYTE_UNITS.indexOf(unit)
}

/** Largest unit that represents `n` exactly, so editing never loses bytes. */
export function splitBytes(n: number): { text: string; unit: ByteUnit } {
  if (n === 0 || !Number.isInteger(n)) return { text: String(n), unit: "B" }
  for (let i = BYTE_UNITS.length - 1; i > 0; i--) {
    const unit = BYTE_UNITS[i]
    const f = byteUnitFactor(unit)
    if (n % f === 0) return { text: String(n / f), unit }
  }
  return { text: String(n), unit: "B" }
}

/** "1536" → "1.5 KiB". Binary units, one decimal, trailing ".0" dropped. */
export function humanizeBytes(n: number): string {
  if (!Number.isFinite(n)) return String(n)
  const sign = n < 0 ? "-" : ""
  let abs = Math.abs(n)
  let i = 0
  while (abs >= 1024 && i < BYTE_UNITS.length - 1) {
    abs /= 1024
    i++
  }
  const text = i === 0 ? String(abs) : abs.toFixed(1).replace(/\.0$/, "")
  return `${sign}${text} ${BYTE_UNITS[i]}`
}

// ---------------------------------------------------------------------------
// Durations (Go time.ParseDuration syntax)
// ---------------------------------------------------------------------------

const DURATION_UNIT_NS: Record<string, number> = {
  ns: 1,
  us: 1e3,
  "µs": 1e3,
  "μs": 1e3,
  ms: 1e6,
  s: 1e9,
  m: 60e9,
  h: 3600e9,
}

const DURATION_PART = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/y

/** Nanoseconds for a Go duration string, or null if it does not parse. */
export function parseGoDuration(input: string): number | null {
  let s = input.trim()
  if (s === "") return null
  let sign = 1
  if (s[0] === "-" || s[0] === "+") {
    if (s[0] === "-") sign = -1
    s = s.slice(1)
  }
  if (s === "0") return 0
  if (s === "") return null
  let total = 0
  DURATION_PART.lastIndex = 0
  while (DURATION_PART.lastIndex < s.length) {
    const m = DURATION_PART.exec(s)
    if (!m) return null
    total += parseFloat(m[1]) * DURATION_UNIT_NS[m[2]]
  }
  return sign * total
}

export const DURATION_HINT = 'Go duration, e.g. "500ms", "30s", "1m30s", "2h"'

// ---------------------------------------------------------------------------
// Datetimes: RFC 3339 on the wire, local time in the control
// ---------------------------------------------------------------------------

const pad = (n: number) => String(n).padStart(2, "0")

/**
 * What a datetime-local input shows for an RFC 3339 value: the same instant
 * in local time, seconds only when there are some. "" when it does not parse.
 */
export function rfc3339ToLocalInput(value: string): string {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ""
  const base = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
  return d.getSeconds() ? `${base}:${pad(d.getSeconds())}` : base
}

/** The RFC 3339 (UTC) value of a datetime-local input's text, or null. */
export function localInputToRFC3339(text: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?$/.test(text)) return null
  const d = new Date(text) // no offset: local time
  if (Number.isNaN(d.getTime())) return null
  return d.toISOString().replace(/\.\d{3}Z$/, "Z")
}

/** RFC 3339 → milliseconds, or null. Go accepts any offset; so does this. */
function rfc3339Millis(value: unknown): number | null {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/i.test(value)) {
    return null
  }
  const ms = Date.parse(value)
  return Number.isNaN(ms) ? null : ms
}

// ---------------------------------------------------------------------------
// Drafts — what a control holds while the user edits
// ---------------------------------------------------------------------------

/**
 * `text` covers every type typed into an input (and enum's selected value);
 * an empty text means "not set" (the default applies). `bool` is null when not
 * set. Secrets never carry their stored value: `keep` leaves it alone,
 * `replace` sends `text`, `clear` removes it.
 */
export type Draft =
  | { kind: "text"; text: string }
  | { kind: "bool"; value: boolean | null }
  | { kind: "bytes"; text: string; unit: ByteUnit }
  | { kind: "secret"; mode: "keep" | "replace" | "clear"; text: string }
  | { kind: "map"; entries: MapEntry[] }
  // enum_list: the checked options; none means not set.
  | { kind: "set"; values: string[] }
  // file: a build file's id; null when not set.
  | { kind: "file"; id: string | null }

/** One row of a string_map control. */
export interface MapEntry {
  key: string
  value: string
}

export interface DraftOptions {
  /** Edit secrets as plain write-only text (action params) instead of keep/replace. */
  secretAsText?: boolean
}

export function emptyDraft(field: FieldDef, opts: DraftOptions = {}): Draft {
  return initialDraft(field, null, opts)
}

/** Draft for a stored JSON value (null = nothing stored). */
export function initialDraft(
  field: FieldDef,
  value: unknown,
  opts: DraftOptions = {},
): Draft {
  const type = field.type as FieldType
  if (type === "secret" && !opts.secretAsText) {
    return { kind: "secret", mode: "keep", text: "" }
  }
  if (type === "bool") {
    return { kind: "bool", value: typeof value === "boolean" ? value : null }
  }
  if (type === "string_map") {
    return {
      kind: "map",
      entries: isPlainObject(value)
        ? Object.entries(value).map(([key, v]) => ({ key, value: typeof v === "string" ? v : String(v) }))
        : [],
    }
  }
  if (type === "bytes") {
    if (typeof value === "number") return { kind: "bytes", ...splitBytes(value) }
    return { kind: "bytes", text: "", unit: defaultByteUnit(field) }
  }
  if (type === "enum_list") {
    return { kind: "set", values: Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : [] }
  }
  if (type === "file") {
    return { kind: "file", id: typeof value === "string" && value !== "" ? value : null }
  }
  if (type === "datetime") {
    return { kind: "text", text: typeof value === "string" ? rfc3339ToLocalInput(value) : "" }
  }
  return { kind: "text", text: valueToText(type, value) }
}

function defaultByteUnit(field: FieldDef): ByteUnit {
  if (typeof field.default === "number") return splitBytes(field.default).unit
  return "B"
}

function valueToText(type: FieldType, value: unknown): string {
  if (value === null || value === undefined) return ""
  if (type === "string_list") {
    return Array.isArray(value) ? value.map(String).join("\n") : String(value)
  }
  if (typeof value === "string") return value
  if (typeof value === "number" || typeof value === "boolean") return String(value)
  return JSON.stringify(value)
}

export type ParseResult =
  | { ok: true; value: unknown }
  | { ok: false; error: string }

/**
 * The JSON value a draft stands for: null = unset (the default). For a secret
 * in `keep` mode the result is `undefined`, meaning "no change".
 */
export function parseDraft(field: FieldDef, draft: Draft): ParseResult {
  const type = field.type as FieldType
  switch (draft.kind) {
    case "secret":
      if (draft.mode === "keep") return { ok: true, value: undefined }
      if (draft.mode === "clear") return { ok: true, value: null }
      // An empty replacement is "no change", not a clear — clearing is explicit.
      return { ok: true, value: draft.text === "" ? undefined : draft.text }
    case "bool":
      return { ok: true, value: draft.value }
    case "set": {
      if (draft.values.length === 0) return { ok: true, value: null }
      // In the order the options are declared, each once.
      const order = (field.options ?? []).map((o) => o.value)
      const picked = new Set(draft.values)
      const known = order.filter((v) => picked.has(v))
      const unknown = draft.values.filter((v, i) => !order.includes(v) && draft.values.indexOf(v) === i)
      return { ok: true, value: [...known, ...unknown] }
    }
    case "file":
      return { ok: true, value: draft.id }
    case "map": {
      const out: Record<string, string> = {}
      for (const e of draft.entries) {
        const key = e.key.trim()
        if (key === "" && e.value === "") continue // an untouched blank row
        if (key === "") return { ok: false, error: "Every entry needs a key" }
        if (key in out) return { ok: false, error: `"${key}" appears twice` }
        out[key] = e.value
      }
      return { ok: true, value: Object.keys(out).length === 0 ? null : out }
    }
    case "bytes": {
      const t = draft.text.trim()
      if (t === "") return { ok: true, value: null }
      if (!/^-?(\d+(\.\d*)?|\.\d+)$/.test(t)) return { ok: false, error: "Must be a number" }
      const n = parseFloat(t) * byteUnitFactor(draft.unit)
      if (!Number.isInteger(n)) return { ok: false, error: "Must be a whole number of bytes" }
      return { ok: true, value: n }
    }
    case "text": {
      const raw = draft.text
      if (type === "string_list") {
        const items = raw
          .split("\n")
          .map((l) => l.trim())
          .filter((l) => l !== "")
        return { ok: true, value: items.length === 0 ? null : items }
      }
      const t = raw.trim()
      if (t === "") return { ok: true, value: null }
      if (type === "int") {
        if (!/^[-+]?\d+$/.test(t)) return { ok: false, error: "Must be a whole number" }
        return { ok: true, value: parseInt(t, 10) }
      }
      if (type === "number") {
        const n = Number(t)
        if (!Number.isFinite(n)) return { ok: false, error: "Must be a number" }
        return { ok: true, value: n }
      }
      if (type === "duration") {
        if (parseGoDuration(t) === null) return { ok: false, error: `Invalid duration. ${DURATION_HINT}` }
        return { ok: true, value: t }
      }
      if (type === "datetime") {
        const v = localInputToRFC3339(t)
        return v === null ? { ok: false, error: "Not a date and time" } : { ok: true, value: v }
      }
      // string / url / enum / secret-as-text keep inner whitespace verbatim.
      return { ok: true, value: type === "enum" ? t : raw }
    }
  }
}

// ---------------------------------------------------------------------------
// Validation — mirrors the server's checks so errors show inline before save
// ---------------------------------------------------------------------------

function boundNumber(type: FieldType, bound: unknown): number | null {
  if (bound === null || bound === undefined) return null
  if (type === "duration") {
    return typeof bound === "string" ? parseGoDuration(bound) : null
  }
  if (type === "datetime") return rfc3339Millis(bound)
  return typeof bound === "number" ? bound : null
}

function formatBound(type: FieldType, bound: unknown): string {
  if (type === "bytes" && typeof bound === "number") return humanizeBytes(bound)
  if (type === "datetime") return formatValue(type, bound)
  return String(bound)
}

export interface ValidateOptions {
  /**
   * The value the property falls back to when nothing is stored (the
   * module's default). `required` only fails when the value is unset AND
   * there is no fallback.
   */
  fallback?: unknown
}

/** Error message for a parsed value, or null when valid. */
export function validateValue(
  field: FieldDef,
  value: unknown,
  opts: ValidateOptions = {},
): string | null {
  const type = field.type as FieldType
  if (value === undefined) return null // secret kept as-is
  if (value === null || value === "" || (Array.isArray(value) && value.length === 0 && type === "enum_list")) {
    const hasFallback = opts.fallback !== null && opts.fallback !== undefined
    if (field.required && !hasFallback) return "Required"
    return null
  }

  switch (type) {
    case "string":
    case "url":
    case "secret": {
      if (typeof value !== "string") return "Must be text"
      if (field.maxLength != null && [...value].length > field.maxLength) {
        return `At most ${field.maxLength} characters`
      }
      if (field.pattern && !matchesPattern(field.pattern, value)) {
        return `Must match ${field.pattern}`
      }
      if (type === "url" && !isUrl(value)) return "Must be a valid URL"
      return null
    }
    case "enum": {
      const options = field.options ?? []
      if (!options.some((o) => o.value === value)) return "Not one of the allowed options"
      return null
    }
    case "bool":
      return typeof value === "boolean" ? null : "Must be true or false"
    case "int":
    case "bytes":
    case "number": {
      if (typeof value !== "number" || !Number.isFinite(value)) return "Must be a number"
      if (type !== "number" && !Number.isInteger(value)) return "Must be a whole number"
      return checkBounds(field, type, value)
    }
    case "duration": {
      if (typeof value !== "string") return "Must be a duration"
      const ns = parseGoDuration(value)
      if (ns === null) return `Invalid duration. ${DURATION_HINT}`
      return checkBounds(field, type, ns)
    }
    case "datetime": {
      const ms = rfc3339Millis(value)
      if (ms === null) return "Must be a date and time"
      return checkBounds(field, type, ms)
    }
    case "enum_list": {
      if (!Array.isArray(value)) return "Must be a list of options"
      if (field.maxItems != null && value.length > field.maxItems) return `At most ${field.maxItems}`
      const options = field.options ?? []
      for (const [i, item] of value.entries()) {
        if (!options.some((o) => o.value === item)) return `"${String(item)}" is not one of the allowed options`
        if (value.indexOf(item) !== i) return `"${String(item)}" is picked twice`
      }
      return null
    }
    case "file":
      return typeof value === "string" && value !== "" ? null : "Must be a file"
    case "string_list": {
      if (!Array.isArray(value)) return "Must be a list"
      if (field.maxItems != null && value.length > field.maxItems) return `At most ${field.maxItems} items`
      for (const item of value) {
        if (typeof item !== "string") return "Items must be text"
        if (field.maxLength != null && [...item].length > field.maxLength) {
          return `Each item at most ${field.maxLength} characters`
        }
        if (field.pattern && !matchesPattern(field.pattern, item)) {
          return `"${item}" must match ${field.pattern}`
        }
      }
      return null
    }
    case "string_map": {
      if (!isPlainObject(value)) return "Must be a set of keys and values"
      const entries = Object.entries(value)
      if (field.maxItems != null && entries.length > field.maxItems) return `At most ${field.maxItems} entries`
      for (const [key, item] of entries) {
        if (typeof item !== "string") return `"${key}" must be text`
        if (field.keyPattern && !matchesPattern(field.keyPattern, key)) {
          return `"${key}" is not a valid key (must match ${field.keyPattern})`
        }
        if (field.maxLength != null && [...item].length > field.maxLength) {
          return `"${key}": at most ${field.maxLength} characters`
        }
        if (field.pattern && !matchesPattern(field.pattern, item)) {
          return `"${key}": value must match ${field.pattern}`
        }
      }
      return null
    }
    default:
      return null
  }
}

export function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

function checkBounds(field: FieldDef, type: FieldType, n: number): string | null {
  const min = boundNumber(type, field.min)
  const max = boundNumber(type, field.max)
  if (min !== null && n < min) return `Must be at least ${formatBound(type, field.min)}`
  if (max !== null && n > max) return `Must be at most ${formatBound(type, field.max)}`
  return null
}

// Go's regexp.MatchString is unanchored, so this is too. A pattern JS cannot
// compile (RE2-only syntax) is left to the server rather than blocking input.
function matchesPattern(pattern: string, value: string): boolean {
  try {
    return new RegExp(pattern).test(value)
  } catch {
    return true
  }
}

function isUrl(value: string): boolean {
  try {
    const u = new URL(value)
    return u.protocol !== "" && u.host !== ""
  } catch {
    return false
  }
}

/** Parse then validate a draft; the error from either step. */
export function checkDraft(
  field: FieldDef,
  draft: Draft,
  opts: ValidateOptions = {},
): ParseResult {
  const parsed = parseDraft(field, draft)
  if (!parsed.ok) return parsed
  const error = validateValue(field, parsed.value, opts)
  return error ? { ok: false, error } : parsed
}

// ---------------------------------------------------------------------------
// Display
// ---------------------------------------------------------------------------

export const SECRET_MASK = "•••"

/** Human-readable value. null → "—"; unit appended where it adds meaning. */
export function formatValue(type: string, value: unknown, unit?: string | null): string {
  if (value === null || value === undefined) return "—"
  if (type === "secret") return SECRET_MASK
  if (type === "bytes" && typeof value === "number") return humanizeBytes(value)
  if (type === "datetime" && typeof value === "string") {
    const ms = Date.parse(value)
    return Number.isNaN(ms)
      ? value
      : new Date(ms).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })
  }
  if (type === "bool" && typeof value === "boolean") return value ? "true" : "false"
  if (Array.isArray(value)) {
    return value.length === 0 ? "(empty)" : value.map((v) => (typeof v === "string" ? v : JSON.stringify(v))).join(", ")
  }
  if (type === "string_map" && isPlainObject(value)) {
    const entries = Object.entries(value)
    return entries.length === 0 ? "(empty)" : entries.map(([k, v]) => `${k}: ${String(v)}`).join(", ")
  }
  let text: string
  if (typeof value === "string") text = value
  else if (typeof value === "number" || typeof value === "boolean") text = String(value)
  else text = JSON.stringify(value)
  if (unit && type !== "duration" && type !== "bool") return `${text} ${unit}`
  return text
}

export function prettyJson(value: unknown): string {
  if (value === undefined) return ""
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function valuesEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (a === null || a === undefined || b === null || b === undefined) {
    return (a ?? null) === (b ?? null)
  }
  return JSON.stringify(a) === JSON.stringify(b)
}

// ---------------------------------------------------------------------------
// Grouping
// ---------------------------------------------------------------------------

export const DEFAULT_GROUP = "General"

export function compareFields(a: FieldDef, b: FieldDef): number {
  return a.order - b.order || a.label.localeCompare(b.label)
}

/**
 * Items grouped by `field.group`, each group sorted by order then label.
 * Groups are ordered by their first field in that sort; ungrouped fields form
 * the "General" group.
 */
export function groupByField<T>(
  items: readonly T[],
  getField: (item: T) => FieldDef,
): { group: string; items: T[] }[] {
  const sorted = [...items].sort((a, b) => compareFields(getField(a), getField(b)))
  const groups = new Map<string, T[]>()
  for (const item of sorted) {
    const g = getField(item).group?.trim() || DEFAULT_GROUP
    const list = groups.get(g)
    if (list) list.push(item)
    else groups.set(g, [item])
  }
  return [...groups].map(([group, items]) => ({ group, items }))
}

// ---------------------------------------------------------------------------
// Config form: initial values, diff
// ---------------------------------------------------------------------------

/**
 * A property as the config form sees it. Config is per instance: `value` is
 * what is stored for the instance (null when unset — and always for secrets),
 * `effective` is that value or else the module's default, and `source` says
 * which ("set" | "default").
 */
export interface PropertyLike {
  field: FieldDef
  effective?: unknown
  source: string
  value?: unknown
  isSet: boolean
}

/** What a property's control starts with: the effective value (never a secret). */
export function initialValue(p: PropertyLike): unknown {
  return p.field.type === "secret" ? null : (p.effective ?? null)
}

export function initialDrafts(properties: readonly PropertyLike[]): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const p of properties) out[p.field.key] = initialDraft(p.field, initialValue(p))
  return out
}

/** The draft that resets a property to the module's default (saved as null). */
export function resetDraft(field: FieldDef): Draft {
  return field.type === "secret"
    ? { kind: "secret", mode: "clear", text: "" }
    : emptyDraft(field)
}

export interface DiffEntry {
  key: string
  label: string
  type: string
  /** The stored value before (null when it was the default). */
  old: unknown
  /** The value to store; null resets to the default. */
  new: unknown
  /** Display strings; secrets masked, defaults marked. */
  oldText: string
  newText: string
}

export interface FormEvaluation {
  diff: DiffEntry[]
  errors: Record<string, string>
}

function defaultText(field: FieldDef): string {
  return `${formatValue(field.type, field.default ?? null, field.unit)} (default)`
}

/**
 * Validate every draft and diff it against the instance's current config.
 * Controls start from the effective value, so a property is changed when its
 * draft differs from that; an emptied control (or "Reset to default") resets a
 * stored value to the default, and is no change for one that is already the
 * default. Only changed properties are validated — an untouched field the
 * server already accepted is not the user's problem to fix here.
 */
export function evaluateForm(
  properties: readonly PropertyLike[],
  drafts: Record<string, Draft>,
): FormEvaluation {
  const diff: DiffEntry[] = []
  const errors: Record<string, string> = {}
  for (const p of [...properties].sort((a, b) => compareFields(a.field, b.field))) {
    const f = p.field
    const draft = drafts[f.key]
    if (!draft) continue
    const parsed = parseDraft(f, draft)
    if (!parsed.ok) {
      errors[f.key] = parsed.error
      continue
    }
    const next = parsed.value
    if (next === undefined) continue // secret kept
    const isSecret = f.type === "secret"
    const isSet = isSecret ? p.isSet : p.source === "set"
    if (next === null && !isSet) continue // resetting what is already the default
    if (!isSecret && next !== null && valuesEqual(initialValue(p), next)) continue
    // Secrets have no visible default, so a required one cannot be cleared.
    const error = validateValue(f, next, { fallback: isSecret ? null : f.default })
    if (error) {
      errors[f.key] = error
      continue
    }
    const old = isSecret ? (isSet ? SECRET_MASK : null) : isSet ? (p.value ?? null) : null
    let oldText: string
    let newText: string
    if (isSecret) {
      oldText = isSet ? SECRET_MASK : "Not set"
      newText = next === null ? "Not set" : SECRET_MASK
    } else {
      oldText = isSet ? formatValue(f.type, old, f.unit) : defaultText(f)
      newText = next === null ? defaultText(f) : formatValue(f.type, next, f.unit)
    }
    diff.push({ key: f.key, label: f.label, type: f.type, old, new: next, oldText, newText })
  }
  return { diff, errors }
}

// ---------------------------------------------------------------------------
// Action params
// ---------------------------------------------------------------------------

export function initialParamDrafts(params: readonly FieldDef[]): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of params) out[f.key] = initialDraft(f, f.default ?? null, { secretAsText: true })
  return out
}

/** Params object for invokeModuleAction (unset params omitted) plus per-key errors. */
export function evaluateParams(
  params: readonly FieldDef[],
  drafts: Record<string, Draft>,
): { values: Record<string, unknown>; errors: Record<string, string> } {
  const values: Record<string, unknown> = {}
  const errors: Record<string, string> = {}
  for (const f of params) {
    const draft = drafts[f.key] ?? emptyDraft(f, { secretAsText: true })
    const res = checkDraft(f, draft)
    if (!res.ok) errors[f.key] = res.error
    else if (res.value !== null && res.value !== undefined) values[f.key] = res.value
  }
  return { values, errors }
}

// ---------------------------------------------------------------------------
// Activity feed
// ---------------------------------------------------------------------------

type ConfigChange = ModuleConfigChangesQuery["moduleConfigChanges"][number]

export type ActivityItem =
  | { kind: "config"; at: string; change: ConfigChange }
  | { kind: "action"; at: string; invocation: ModuleActionInvocationFieldsFragment }

/** Config changes and action invocations, newest first. */
export function mergeActivity(
  changes: readonly ConfigChange[],
  invocations: readonly ModuleActionInvocationFieldsFragment[],
): ActivityItem[] {
  const items: ActivityItem[] = [
    ...changes.map((change) => ({ kind: "config" as const, at: change.at, change })),
    ...invocations.map((invocation) => ({
      kind: "action" as const,
      at: invocation.startedAt,
      invocation,
    })),
  ]
  return items.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

/** Milliseconds between two RFC3339 timestamps, or null if unfinished. */
export function durationMs(startedAt: string, finishedAt?: string | null): number | null {
  if (!finishedAt) return null
  const ms = Date.parse(finishedAt) - Date.parse(startedAt)
  return Number.isFinite(ms) ? ms : null
}

export function formatElapsed(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1).replace(/\.0$/, "")}s`
  const m = Math.floor(ms / 60_000)
  const s = Math.round((ms % 60_000) / 1000)
  return s ? `${m}m${s}s` : `${m}m`
}
