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
// Bounds (min / max) are numbers, or duration strings for duration.

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
// Drafts — what a control holds while the user edits
// ---------------------------------------------------------------------------

/**
 * `text` covers every type typed into an input (and enum's selected value);
 * an empty text means "not set at this scope". `bool` is null when not set.
 * Secrets never carry their stored value: `keep` leaves it alone, `replace`
 * sends `text`, `clear` removes it at this scope.
 */
export type Draft =
  | { kind: "text"; text: string }
  | { kind: "bool"; value: boolean | null }
  | { kind: "bytes"; text: string; unit: ByteUnit }
  | { kind: "secret"; mode: "keep" | "replace" | "clear"; text: string }

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
  if (type === "bytes") {
    if (typeof value === "number") return { kind: "bytes", ...splitBytes(value) }
    return { kind: "bytes", text: "", unit: defaultByteUnit(field) }
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
 * The JSON value a draft stands for: null = unset at this scope. For a secret
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
  return typeof bound === "number" ? bound : null
}

function formatBound(type: FieldType, bound: unknown): string {
  if (type === "bytes" && typeof bound === "number") return humanizeBytes(bound)
  return String(bound)
}

export interface ValidateOptions {
  /**
   * The value the property falls back to when this scope holds nothing (the
   * other scope's value or the default). `required` only fails when the value
   * is unset here AND nothing below it supplies one.
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
  if (value === null || value === "") {
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
    case "string_list": {
      if (!Array.isArray(value)) return "Must be a list"
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
    default:
      return null
  }
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
  if (type === "bool" && typeof value === "boolean") return value ? "true" : "false"
  if (Array.isArray(value)) {
    return value.length === 0 ? "(empty)" : value.map((v) => (typeof v === "string" ? v : JSON.stringify(v))).join(", ")
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
// Config form: scope values, diff
// ---------------------------------------------------------------------------

export type Scope = "MODULE" | "INSTANCE"

export interface PropertyLike {
  field: FieldDef
  effective?: unknown
  source: string
  moduleValue?: unknown
  instanceValue?: unknown
  isSet: boolean
}

export function scopeValue(p: PropertyLike, scope: Scope): unknown {
  return (scope === "MODULE" ? p.moduleValue : p.instanceValue) ?? null
}

/** What the property resolves to when the given scope holds nothing. */
export function fallbackValue(p: PropertyLike, scope: Scope): unknown {
  const below = scope === "INSTANCE" ? (p.moduleValue ?? null) : null
  return below ?? p.field.default ?? null
}

/**
 * Whether a secret is stored at this scope. The API only says whether one is
 * stored anywhere and where the effective value comes from, so this is exact
 * for the scope that wins and a best guess (unknown → false) below it.
 */
export function secretSetAtScope(p: PropertyLike, scope: Scope): boolean {
  if (!p.isSet) return false
  return p.source === (scope === "MODULE" ? "module" : "instance")
}

export function initialDrafts(
  properties: readonly PropertyLike[],
  scope: Scope,
): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const p of properties) out[p.field.key] = initialDraft(p.field, scopeValue(p, scope))
  return out
}

export interface DiffEntry {
  key: string
  label: string
  type: string
  old: unknown
  new: unknown
  /** Display strings; secrets masked. */
  oldText: string
  newText: string
}

export interface FormEvaluation {
  diff: DiffEntry[]
  errors: Record<string, string>
}

/**
 * Validate every draft and diff it against what is stored at `scope`. Only
 * properties that changed are validated for `required` — an untouched empty
 * field the server already accepted is not the user's problem to fix here.
 */
export function evaluateForm(
  properties: readonly PropertyLike[],
  drafts: Record<string, Draft>,
  scope: Scope,
): FormEvaluation {
  const diff: DiffEntry[] = []
  const errors: Record<string, string> = {}
  for (const p of [...properties].sort((a, b) => compareFields(a.field, b.field))) {
    const draft = drafts[p.field.key]
    if (!draft) continue
    const parsed = parseDraft(p.field, draft)
    if (!parsed.ok) {
      errors[p.field.key] = parsed.error
      continue
    }
    const isSecret = p.field.type === "secret"
    const old = isSecret ? (secretSetAtScope(p, scope) ? SECRET_MASK : null) : scopeValue(p, scope)
    const next = parsed.value
    if (next === undefined) continue // secret kept
    if (isSecret && next === null && old === null) continue // clearing nothing
    if (!isSecret && valuesEqual(old, next)) continue
    // A secret's fallback is unknowable (values are never sent); assume one
    // exists when the secret is stored somewhere other than this scope.
    const fallback = isSecret
      ? p.isSet && !secretSetAtScope(p, scope)
        ? SECRET_MASK
        : null
      : fallbackValue(p, scope)
    const error = validateValue(p.field, next, { fallback })
    if (error) {
      errors[p.field.key] = error
      continue
    }
    diff.push({
      key: p.field.key,
      label: p.field.label,
      type: p.field.type,
      old,
      new: next,
      oldText: isSecret ? (old === null ? "Not set" : SECRET_MASK) : formatValue(p.field.type, old, p.field.unit),
      newText: isSecret ? (next === null ? "Not set" : SECRET_MASK) : formatValue(p.field.type, next, p.field.unit),
    })
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
