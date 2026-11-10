import {
  fieldHints,
  jsonFieldValues,
  readFieldValues,
} from "@/components/modules/management/field-json"
import { childPointer, formatJson, isRecord, parseJson, sameJSON, textOf, type JsonHints, type JsonProblem } from "@/lib/json-edit"
import {
  ENDPOINT_EXPORT_FORMAT,
  MAX_DESCRIPTION_LENGTH,
  connectionKind,
  endpointNameError,
  kindNames,
  type Kind,
} from "./endpoint-values"

export { formValues } from "@/components/modules/management/field-json"

// An endpoint as the endpoint modal's JSON tab edits it: the shape of its
// export (Copy / Download as JSON), so an export pastes straight in, with
// the description, and every setting of its connection kind listed with the
// value the endpoint uses, the channel's default where none is set, as the form
// shows it (modules/management/field-json.ts has the rules). A value left at
// the default is not stored, so the setting keeps following the channel's
// default. An empty list or map stands for itself where the setting has a
// default ("none, instead of it"): core keeps an endpoint's as it is.
//
//   { "format": "logos.channel-endpoint.v2", "name": "…", "description": "…",
//     "channel": { "module_name": "http", "schema_version": 2 },
//     "kind": "reverse-call",
//     "params": { "base_url": "https://…", "tls_verify": null } }
//
// "format" and "channel" say what the document is and stay as they are. "name"
// is any text (an export's is the endpoint's slug, which core makes from the
// name and the app never shows). "kind" is one of the channel's connection
// kinds; "params" are that kind's settings.

/** What the JSON tab and the form share. values holds set settings only. */
export interface EndpointDocument {
  name: string
  description: string
  /** The connection kind, e.g. "reverse-call". */
  kind: string
  values: Record<string, unknown>
}

/** The parts the JSON tab locks. */
export const ENDPOINT_JSON_LOCKED: readonly string[] = ["/format", "/channel"]

const KEYS = ["format", "name", "description", "channel", "kind", "params"]

/** The JSON tab's text for an endpoint of channel kind. */
export function endpointJson(kind: Kind, doc: EndpointDocument): string {
  return formatJson({
    format: ENDPOINT_EXPORT_FORMAT,
    name: doc.name,
    description: doc.description,
    channel: { module_name: kind.name, schema_version: kind.version },
    kind: doc.kind,
    params: jsonFieldValues(connectionKind(kind, doc.kind)?.fields ?? [], doc.values),
  })
}

/** The JSON tab's text read back: not an endpoint at all, or one with its problems. */
export type EndpointJsonRead =
  | { ok: false; message: string }
  | { ok: true; doc: EndpointDocument; problems: JsonProblem[] }

/**
 * Reads the JSON tab's text, checking it as the form checks its fields. Its
 * params are read by the fields of the connection kind it names; by
 * fallbackKind's (the form's) while it names none, or none the channel
 * carries.
 */
export function readEndpointJson(text: string, kind: Kind, fallbackKind: string): EndpointJsonRead {
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

  // Left out, the form's kind stays, as format and channel may be left out.
  let connKind = fallbackKind
  if (typeof root.kind === "string" && connectionKind(kind, root.kind)) connKind = root.kind
  else if (root.kind !== undefined) {
    at("/kind", `Must be one of the ${kind.name} channel's connection kinds: ${kindNames(kind).join(", ")}`)
  }
  const fields = connectionKind(kind, connKind)?.fields ?? []

  const name = textOf(root.name, "/name", at)
  const nameErr = endpointNameError(name)
  if (nameErr) at("/name", nameErr)
  const description = textOf(root.description, "/description", at)
  if ([...description].length > MAX_DESCRIPTION_LENGTH) at("/description", `At most ${MAX_DESCRIPTION_LENGTH} characters`)

  const values = readFieldValues(
    fields,
    root.params,
    "/params",
    { unknown: `Not a ${connKind} setting of ${kind.name} v${kind.version}`, notObject: "Must be an object of settings" },
    problems,
    { emptyOverridesDefault: true },
  )
  return { ok: true, doc: { name, description, kind: connKind, values }, problems }
}

/** What the JSON tab offers as you type, and says on hover: the settings of connection kind connKind. */
export function endpointJsonHints(kind: Kind, connKind: string): JsonHints {
  return {
    "": [
      { key: "name", detail: "text, required", info: "What you call this endpoint: any text." },
      { key: "description", detail: "text", info: "What this endpoint is for, for the rest of the team." },
      {
        key: "kind",
        detail: "text, required",
        info: "The connection kind. Each has its own settings.",
        values: kindNames(kind),
      },
      {
        key: "params",
        detail: "object",
        info: `The ${kind.name} channel's ${connKind} settings; null leaves one at its default.`,
      },
    ],
    "/params": fieldHints(connectionKind(kind, connKind)?.fields ?? []),
  }
}
