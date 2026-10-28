import { parseDraft, validateValue, type Draft, type FieldDef } from "@/components/modules/management/field-values"
import { childPointer, formatJson, parseJson, type JsonHints, type JsonProblem } from "@/lib/json-edit"
import {
  ENDPOINT_EXPORT_FORMAT,
  isRecord,
  MAX_DESCRIPTION_LENGTH,
  nameError,
  sameJSON,
  type Kind,
} from "./endpoint-values"

// An endpoint as the endpoint modal's JSON tab edits it: the shape of its
// export (Copy / Download as JSON), so an export pastes straight in, with
// the description, and every setting of its channel listed, null where the
// channel's default applies.
//
//   { "format": "logos.channel-endpoint.v1", "name": "…", "description": "…",
//     "channel": { "module_name": "http", "schema_version": 2 },
//     "params": { "base_url": "https://…", "tls_verify": null } }
//
// "format" and "channel" say what the document is and stay as they are.

/** What the JSON tab and the form share. values holds set settings only. */
export interface EndpointDocument {
  name: string
  description: string
  values: Record<string, unknown>
}

/** The parts the JSON tab locks. */
export const ENDPOINT_JSON_LOCKED: readonly string[] = ["/format", "/channel"]

const KEYS = ["format", "name", "description", "channel", "params"]

/** The JSON tab's text for an endpoint of kind. */
export function endpointJson(kind: Kind, doc: EndpointDocument): string {
  const params: Record<string, unknown> = {}
  for (const f of kind.fields) params[f.key] = doc.values[f.key] ?? null
  return formatJson({
    format: ENDPOINT_EXPORT_FORMAT,
    name: doc.name,
    description: doc.description,
    channel: { module_name: kind.name, schema_version: kind.version },
    params,
  })
}

/**
 * The form's settings as JSON values: each set field's value, or the text
 * typed into it where that does not parse (a number field holding "abc"), so
 * the JSON tab shows what the form holds and marks it.
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

/** The JSON tab's text read back: not an endpoint at all, or one with its problems. */
export type EndpointJsonRead =
  | { ok: false; message: string }
  | { ok: true; doc: EndpointDocument; problems: JsonProblem[] }

/** Reads the JSON tab's text, checking it as the form checks its fields. */
export function readEndpointJson(text: string, kind: Kind): EndpointJsonRead {
  const parsed = parseJson(text)
  if (!parsed.ok) return { ok: false, message: parsed.message }
  const root = parsed.value
  if (!isRecord(root)) return { ok: false, message: "An endpoint is a JSON object." }

  const problems: JsonProblem[] = []
  const at = (path: string, message: string) => problems.push({ path, message })
  for (const key of Object.keys(root)) {
    if (!KEYS.includes(key)) at(childPointer("", key), `Not part of an endpoint, which has ${KEYS.join(", ")}`)
  }
  if (root.format !== undefined && root.format !== ENDPOINT_EXPORT_FORMAT) {
    at("/format", `Must stay "${ENDPOINT_EXPORT_FORMAT}"`)
  }
  if (root.channel !== undefined && !sameJSON(root.channel, { module_name: kind.name, schema_version: kind.version })) {
    at("/channel", `Must stay this endpoint's channel: ${kind.name}, schema v${kind.version}`)
  }

  const name = textOf(root.name, "/name", at)
  const nameErr = nameError(name)
  if (nameErr) at("/name", nameErr)
  const description = textOf(root.description, "/description", at)
  if ([...description].length > MAX_DESCRIPTION_LENGTH) at("/description", `At most ${MAX_DESCRIPTION_LENGTH} characters`)

  const values: Record<string, unknown> = {}
  const params = root.params ?? {}
  if (!isRecord(params)) {
    at("/params", "Must be an object of settings")
  } else {
    const fields = new Map(kind.fields.map((f) => [f.key, f]))
    for (const [key, value] of Object.entries(params)) {
      if (!fields.has(key)) at(childPointer("/params", key), `Not a setting of ${kind.name} v${kind.version}`)
      else if (value !== null) values[key] = value
    }
    for (const f of kind.fields) {
      const error = validateValue(f, params[f.key] ?? null, { fallback: f.default })
      if (error) at(childPointer("/params", f.key), error)
    }
  }
  return { ok: true, doc: { name, description, values }, problems }
}

/** A text property's value: "" when absent or null, a problem when not text. */
function textOf(value: unknown, path: string, at: (path: string, message: string) => void): string {
  if (typeof value === "string") return value
  if (value !== undefined && value !== null) at(path, "Must be text")
  return ""
}

/** What the JSON tab offers as you type, and says on hover. */
export function endpointJsonHints(kind: Kind): JsonHints {
  return {
    "": [
      { key: "name", detail: "text, required", info: "Unique in this operation: lowercase letters, digits, '.', '_' and '-'." },
      { key: "description", detail: "text", info: "What this endpoint is for, for the rest of the team." },
      { key: "params", detail: "object", info: `The ${kind.name} channel's settings; null leaves one at its default.` },
    ],
    "/params": kind.fields.map((f) => ({
      key: f.key,
      detail: f.required ? `${f.type}, required` : f.type,
      info: [
        f.label,
        f.description,
        f.default !== null && f.default !== undefined ? `Default: ${JSON.stringify(f.default)}` : "No default",
      ]
        .filter(Boolean)
        .join("\n"),
      values:
        f.type === "enum" ? [...(f.options ?? []).map((o) => o.value), null] : f.type === "bool" ? [true, false, null] : undefined,
    })),
  }
}
