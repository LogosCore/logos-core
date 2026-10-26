// Pure value handling for minion builds: factory kinds as the forms use them,
// the endpoints a target can connect through, the build and preset form's
// drafts and what it submits, and display helpers. Field rules are the ones of
// field-values.ts; errors are keyed like the server's problems ("values.<key>",
// "connections[<i>].values.<key>", ...) so both land on the same control.
// No React here, so all of it is unit-tested directly.

import {
  compareFields,
  emptyDraft,
  humanizeBytes,
  initialDraft,
  parseDraft,
  validateValue,
  type Draft,
  type FieldDef,
} from "@/components/modules/management/field-values"
import type { BuildSchemaFieldsFragment } from "@/graphql/gql/graphql"

// Secrets in a build are operation data, edited as text like the rest.
const AS_TEXT = { secretAsText: true }

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

export interface FileLike {
  name: string
  size: number
  mediaType?: string | null
}

/**
 * Why a file does not fit a file field (accept, max size), or null when it
 * does. Mirrors protocol.ValidateFile: an extension matches the end of the
 * name, a media type matches exactly or by "type/*", case-insensitively.
 */
export function fileFits(field: FieldDef, file: FileLike): string | null {
  const maxSize = typeof field.maxSize === "number" ? field.maxSize : null
  if (maxSize !== null && maxSize > 0 && file.size > maxSize) {
    return `Larger than ${humanizeBytes(maxSize)}`
  }
  const accept = field.accept ?? []
  if (accept.length === 0) return null
  const name = file.name.toLowerCase()
  const media = (file.mediaType ?? "").split(";")[0].trim().toLowerCase()
  for (const raw of accept) {
    const a = raw.toLowerCase()
    if (a.startsWith(".")) {
      if (name.endsWith(a)) return null
    } else if (a.endsWith("/*")) {
      if (media !== "" && media.startsWith(a.slice(0, -1))) return null
    } else if (media === a) {
      return null
    }
  }
  return `Must be ${accept.join(", ")}`
}

/** What a file field takes, for the line under its control. */
export function acceptText(field: FieldDef): string {
  const parts: string[] = []
  parts.push(field.accept && field.accept.length > 0 ? field.accept.join(", ") : "Any file")
  if (typeof field.maxSize === "number" && field.maxSize > 0) parts.push(`up to ${humanizeBytes(field.maxSize)}`)
  return parts.join(" · ")
}

// ---------------------------------------------------------------------------
// Factory kinds
// ---------------------------------------------------------------------------

export interface Target {
  id: string
  label: string
  description: string | null
  format: string
  platforms: string[]
  /** The target's own fields; the kind's shared ones come first in a form. */
  fields: FieldDef[]
  /** Channel kinds and connection schema versions the target can use. */
  channels: { moduleName: string; versions: number[] }[]
}

export interface FactoryKind {
  name: string
  /** The kind's own SVG icon; "" for the default. */
  icon: string
  version: number
  /** Registered instances declaring this version: a build needs one. */
  instances: number
  crypto: string[]
  /** Fields every target shares, sorted. */
  fields: FieldDef[]
  /** The factory's connection parameters per channel kind, sorted. */
  channelFields: Map<string, FieldDef[]>
  targets: Target[]
}

const asText = (f: FieldDef): FieldDef => (f.type === "secret" ? { ...f, type: "string" } : f)
const sorted = (fields: readonly FieldDef[]) => fields.map(asText).sort(compareFields)

/** Factory kinds by name from the factoryKinds query. */
export function factoryKindsByName(
  kinds: readonly { moduleName: string; icon?: string; instances: number; schema: BuildSchemaFieldsFragment }[],
): Map<string, FactoryKind> {
  const out = new Map<string, FactoryKind>()
  for (const k of kinds) {
    out.set(k.moduleName, {
      name: k.moduleName,
      icon: k.icon ?? "",
      version: k.schema.version,
      instances: k.instances,
      crypto: k.schema.crypto,
      fields: sorted(k.schema.fields),
      channelFields: new Map(k.schema.channels.map((c) => [c.moduleName, sorted(c.fields)])),
      targets: k.schema.targets.map((t) => ({
        id: t.id,
        label: t.label,
        description: t.description ?? null,
        format: t.format,
        platforms: t.platforms,
        fields: sorted(t.fields),
        channels: t.channels.map((c) => ({ moduleName: c.moduleName, versions: c.versions })),
      })),
    })
  }
  return out
}

/** The fields a build of target takes: the shared ones, then its own. */
export function targetFields(kind: FactoryKind, target: Target | undefined): FieldDef[] {
  return target ? [...kind.fields, ...target.fields] : kind.fields
}

// ---------------------------------------------------------------------------
// Endpoints a target can connect through
// ---------------------------------------------------------------------------

export interface EndpointLike {
  id: string
  name: string
  moduleName: string
  problems: readonly { key?: string | null; message: string }[]
}

/**
 * Why an endpoint cannot be used for target, or null when it can: it must
 * fit its channel's current schema, of a kind and version the target speaks.
 * channelVersions are the channel kinds' current connection schema versions.
 */
export function endpointUnusable(
  target: Target | undefined,
  ep: EndpointLike,
  channelVersions: ReadonlyMap<string, number>,
): string | null {
  if (ep.problems.length > 0) return "Does not fit its channel's settings"
  if (!target) return "Pick a target first"
  const supported = target.channels.find((c) => c.moduleName === ep.moduleName)
  if (!supported) return `This target does not speak ${ep.moduleName}`
  const version = channelVersions.get(ep.moduleName)
  if (version === undefined || !supported.versions.includes(version)) {
    return `The ${ep.moduleName} channel is at settings v${version ?? "?"}; this factory reads v${supported.versions.join(", v")}`
  }
  return null
}

// ---------------------------------------------------------------------------
// The form
// ---------------------------------------------------------------------------

export interface ConnectionDraft {
  endpointId: string
  /** The factory's connection parameters for the endpoint's channel kind. */
  drafts: Record<string, Draft>
}

export interface BuildForm {
  target: string
  platform: string
  drafts: Record<string, Draft>
  connections: ConnectionDraft[]
}

/** What a form is filled from: a preset, or an earlier build. */
export interface FormSource {
  target: string
  platform?: string | null
  values?: Record<string, unknown> | null
  connections?: readonly { endpointId: string; values?: Record<string, unknown> | null }[]
}

function draftsFor(fields: readonly FieldDef[], values: Record<string, unknown> | null | undefined): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of fields) out[f.key] = initialDraft(f, values?.[f.key] ?? null, AS_TEXT)
  return out
}

/** Drafts for an endpoint's connection parameters. */
export function connectionDraft(
  kind: FactoryKind,
  endpointId: string,
  moduleName: string | undefined,
  values?: Record<string, unknown> | null,
): ConnectionDraft {
  return { endpointId, drafts: draftsFor(kind.channelFields.get(moduleName ?? "") ?? [], values) }
}

/**
 * A form's start: the source's settings, or the first target with its only
 * platform when it has one. endpointKinds maps endpoint ids to channel kinds.
 */
export function initialForm(
  kind: FactoryKind,
  source: FormSource | null,
  endpointKinds: ReadonlyMap<string, string>,
): BuildForm {
  const target = kind.targets.find((t) => t.id === source?.target) ?? kind.targets[0]
  const platform =
    source?.platform && target?.platforms.includes(source.platform)
      ? source.platform
      : target?.platforms.length === 1
        ? target.platforms[0]
        : ""
  return {
    target: target?.id ?? "",
    platform,
    drafts: draftsFor(targetFields(kind, target), source?.values),
    connections: (source?.connections ?? []).map((c) =>
      connectionDraft(kind, c.endpointId, endpointKinds.get(c.endpointId), c.values),
    ),
  }
}

/** What createBuild / a preset write takes from the form. */
export interface FormInput {
  target: string
  platform: string
  values: Record<string, unknown>
  connections: { endpointId: string; values: Record<string, unknown> }[]
}

function evaluateFields(
  prefix: string,
  fields: readonly FieldDef[],
  drafts: Record<string, Draft>,
  partial: boolean,
  errors: Record<string, string>,
): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  for (const f of fields) {
    const parsed = parseDraft(f, drafts[f.key] ?? emptyDraft(f, AS_TEXT))
    if (!parsed.ok) {
      errors[prefix + f.key] = parsed.error
      continue
    }
    const v = parsed.value
    // A preset may leave required values for the build form.
    const error = validateValue(f, v, { fallback: partial ? true : f.default })
    if (error) errors[prefix + f.key] = error
    else if (v !== null && v !== undefined) values[f.key] = v
  }
  return values
}

/**
 * The form as createBuild or a preset takes it, and its errors. A build
 * (partial false) needs a platform, an endpoint and every required value; a
 * preset may leave them out.
 */
export function evaluateForm(
  kind: FactoryKind,
  form: BuildForm,
  partial: boolean,
  endpointKinds: ReadonlyMap<string, string>,
): { input: FormInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {}
  const target = kind.targets.find((t) => t.id === form.target)
  if (!target) errors.target = "Pick a target"
  else if (form.platform === "" && !partial) errors.platform = "Pick a platform"
  else if (form.platform !== "" && !target.platforms.includes(form.platform)) errors.platform = "Not a platform of this target"

  const values = evaluateFields("values.", targetFields(kind, target), form.drafts, partial, errors)
  if (form.connections.length === 0 && !partial) errors.connections = "Add at least one endpoint"
  const connections = form.connections.map((c, i) => {
    const fields = kind.channelFields.get(endpointKinds.get(c.endpointId) ?? "") ?? []
    return { endpointId: c.endpointId, values: evaluateFields(`connections[${i}].values.`, fields, c.drafts, partial, errors) }
  })
  return { input: { target: form.target, platform: form.platform, values, connections }, errors }
}

/** The form with another target: drafts for its fields, kept where they carry over. */
export function withTarget(kind: FactoryKind, form: BuildForm, targetId: string): BuildForm {
  const target = kind.targets.find((t) => t.id === targetId)
  const drafts: Record<string, Draft> = {}
  for (const f of targetFields(kind, target)) drafts[f.key] = form.drafts[f.key] ?? emptyDraft(f, AS_TEXT)
  const platform = target?.platforms.includes(form.platform)
    ? form.platform
    : target?.platforms.length === 1
      ? target.platforms[0]
      : ""
  return { ...form, target: targetId, platform, drafts }
}

/** Moves item i of a list one place up (-1) or down (+1). */
export function move<T>(items: readonly T[], i: number, by: -1 | 1): T[] {
  const j = i + by
  if (j < 0 || j >= items.length) return [...items]
  const out = [...items]
  ;[out[i], out[j]] = [out[j], out[i]]
  return out
}

// ---------------------------------------------------------------------------
// Display
// ---------------------------------------------------------------------------

export type BuildState = "QUEUED" | "BUILDING" | "SUCCEEDED" | "FAILED" | "CANCELLED"

export const STATE_LABEL: Record<string, string> = {
  QUEUED: "Queued",
  BUILDING: "Building",
  SUCCEEDED: "Succeeded",
  FAILED: "Failed",
  CANCELLED: "Cancelled",
}

/** "os/arch" as people say it: "any/any" is "Any platform". */
export function platformLabel(platform: string): string {
  if (platform === "any/any") return "Any platform"
  const [os, arch] = platform.split("/")
  const osName: Record<string, string> = { windows: "Windows", linux: "Linux", darwin: "macOS", freebsd: "FreeBSD", android: "Android", ios: "iOS", any: "Any OS" }
  return `${osName[os] ?? os} ${arch === "any" ? "(any arch)" : arch}`
}

/** How long a build took, or has been running. */
export function buildElapsed(b: { createdAt: string; finishedAt?: string | null }, now = Date.now()): number {
  const end = b.finishedAt ? Date.parse(b.finishedAt) : now
  return Math.max(0, end - Date.parse(b.createdAt))
}

/** The last reported percent of a running build, if any. */
export function lastPercent(steps: readonly { percent?: number | null }[]): number | null {
  for (let i = steps.length - 1; i >= 0; i--) {
    const p = steps[i].percent
    if (typeof p === "number") return p
  }
  return null
}
