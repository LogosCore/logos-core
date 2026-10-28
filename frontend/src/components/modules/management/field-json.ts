import { childPointer, isRecord, sameJSON, type JsonKeyHint, type JsonProblem } from "@/lib/json-edit"
import { parseDraft, validateValue, type Draft, type FieldDef } from "./field-values"

// How a record's field values (an endpoint's settings, a build's values)
// show in its JSON tab and read back from it. Every field is listed with the
// value the record uses: its own, else its default. A field with no default
// shows empty as its type is: "" for text (and anything written as text:
// URLs, secrets, durations, date-times, options), [] for a list, {} for a
// map; null for a number, a switch or a file, where 0 and false are values.
// Reading back, empty text and null mean not set, and so does a value at its
// default, so the field keeps following the default.

const TEXTS = new Set(["string", "url", "secret", "duration", "datetime", "enum"])
const LISTS = new Set(["string_list", "enum_list"])
const MAPS = new Set(["string_map"])

function hasDefault(f: FieldDef): boolean {
  return f.default !== null && f.default !== undefined
}

/** What a field shows when the record sets no value: its default, else its type's empty value. */
export function shownWhenUnset(f: FieldDef): unknown {
  if (hasDefault(f)) return f.default
  if (TEXTS.has(f.type)) return ""
  if (LISTS.has(f.type)) return []
  if (MAPS.has(f.type)) return {}
  return null
}

/** Every field, with the value the JSON shows for it. */
export function jsonFieldValues(fields: readonly FieldDef[], values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) out[f.key] = values[f.key] ?? shownWhenUnset(f)
  return out
}

export interface FieldReadOptions {
  /**
   * An empty list or map of a field that has a default stands for itself,
   * "none, instead of the default" (an endpoint: core keeps it), rather than
   * for not set (a build: core takes null, "" and [] for not set).
   */
  emptyOverridesDefault?: boolean
}

/** Whether a value read from the JSON leaves its field not set. */
export function leavesUnset(f: FieldDef, value: unknown, opts: FieldReadOptions = {}): boolean {
  if (value === null || (value === "" && TEXTS.has(f.type))) return true
  const empty =
    (LISTS.has(f.type) && Array.isArray(value) && value.length === 0) ||
    (MAPS.has(f.type) && isRecord(value) && Object.keys(value).length === 0)
  return empty && !(opts.emptyOverridesDefault && hasDefault(f))
}

/**
 * Reads the object of field values at pointer: the values set, empty ones
 * and those at their default left out (they follow the default). Adds the
 * problems to problems: keys that are none of the fields (unknown says
 * whose), values that do not fit their field, as the form checks them.
 */
export function readFieldValues(
  fields: readonly FieldDef[],
  raw: unknown,
  pointer: string,
  messages: { unknown: string; notObject: string },
  problems: JsonProblem[],
  opts: FieldReadOptions = {},
): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  const object = raw ?? {}
  if (!isRecord(object)) {
    problems.push({ path: pointer, message: messages.notObject })
    return values
  }
  const byKey = new Map(fields.map((f) => [f.key, f]))
  for (const [key, value] of Object.entries(object)) {
    const f = byKey.get(key)
    if (!f) problems.push({ path: childPointer(pointer, key), message: messages.unknown })
    else if (!leavesUnset(f, value, opts) && !sameJSON(value, f.default)) values[key] = value
  }
  for (const f of fields) {
    const v = object[f.key]
    const error = validateValue(f, v === undefined || leavesUnset(f, v, opts) ? null : v, { fallback: f.default })
    if (error) problems.push({ path: childPointer(pointer, f.key), message: error })
  }
  return values
}

/** What the JSON tab offers for an object of field values, and says on hover. */
export function fieldHints(fields: readonly FieldDef[]): JsonKeyHint[] {
  return fields.map((f) => ({
    key: f.key,
    detail: f.required ? `${f.type}, required` : f.type,
    info: [f.label, f.description, hasDefault(f) ? `Default: ${JSON.stringify(f.default)}` : "No default"]
      .filter(Boolean)
      .join("\n"),
    values:
      f.type === "enum"
        ? [...(f.options ?? []).map((o) => o.value), hasDefault(f) ? null : ""]
        : f.type === "bool"
          ? [true, false, null]
          : undefined,
  }))
}

/**
 * A form's drafts as JSON values: each set field's value, or the text typed
 * into it where that does not parse (a number field holding "abc"), so the
 * JSON tab shows what the form holds and marks it.
 */
export function formValues(fields: readonly FieldDef[], drafts: Record<string, Draft>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    const draft = drafts[f.key]
    if (!draft) continue
    const parsed = parseDraft(f, draft)
    if (!parsed.ok) out[f.key] = "text" in draft ? draft.text : null
    else if (parsed.value !== null && parsed.value !== undefined) out[f.key] = parsed.value
  }
  return out
}
