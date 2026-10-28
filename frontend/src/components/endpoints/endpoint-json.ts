import {
  fieldHints,
  jsonFieldValues,
  readFieldValues,
} from "@/components/modules/management/field-json"
import { childPointer, formatJson, isRecord, parseJson, sameJSON, type JsonHints, type JsonProblem } from "@/lib/json-edit"
import { ENDPOINT_EXPORT_FORMAT, MAX_DESCRIPTION_LENGTH, nameError, type Kind } from "./endpoint-values"

export { formValues } from "@/components/modules/management/field-json"

// An endpoint as the endpoint modal's JSON tab edits it: the shape of its
// export (Copy / Download as JSON), so an export pastes straight in, with
// the description, and every setting of its channel listed with the value
// the endpoint uses, the channel's default where none is set, as the form
// shows it (modules/management/field-json.ts has the rules). A value left at
// the default is not stored, so the setting keeps following the channel's
// default. An empty list or map stands for itself where the setting has a
// default ("none, instead of it"): core keeps an endpoint's as it is.
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
  return formatJson({
    format: ENDPOINT_EXPORT_FORMAT,
    name: doc.name,
    description: doc.description,
    channel: { module_name: kind.name, schema_version: kind.version },
    params: jsonFieldValues(kind.fields, doc.values),
  })
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

  const values = readFieldValues(
    kind.fields,
    root.params,
    "/params",
    { unknown: `Not a setting of ${kind.name} v${kind.version}`, notObject: "Must be an object of settings" },
    problems,
    { emptyOverridesDefault: true },
  )
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
    "/params": fieldHints(kind.fields),
  }
}
