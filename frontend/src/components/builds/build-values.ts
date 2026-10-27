// Pure value handling for minion builds: factory kinds as the forms use them,
// the channels a target can connect through, the build and preset form's
// drafts and what it submits, and display helpers. Field rules are the ones of
// field-values.ts; errors are keyed like the server's problems ("values.<key>",
// "connections[<i>].settings.<key>", ...) so both land on the same control.
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
import { freeName, isRecord, nameError, sameJSON, settingRows, type Kind } from "@/components/endpoints/endpoint-values"
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
// Channels a target can connect through
// ---------------------------------------------------------------------------

/** A channel kind as the build form uses it: its connection settings, sorted. */
export type ChannelKind = Kind

/**
 * Why target cannot connect through the channel kind moduleName, or null when
 * it can: the channel must declare connection settings, of a version the
 * target reads.
 */
export function channelUnusable(
  target: Target | undefined,
  moduleName: string,
  channels: ReadonlyMap<string, ChannelKind>,
): string | null {
  const ch = channels.get(moduleName)
  if (!ch) return `No ${moduleName} channel declares connection settings`
  if (!target) return "Pick a target first"
  const supported = target.channels.find((c) => c.moduleName === moduleName)
  if (!supported) return `This target does not speak ${moduleName}`
  if (!supported.versions.includes(ch.version)) {
    return `The ${moduleName} channel is at settings v${ch.version}; this factory reads v${supported.versions.join(", v")}`
  }
  return null
}

// ---------------------------------------------------------------------------
// The form
// ---------------------------------------------------------------------------

/** One connection: a channel kind, its settings, the factory's options for it. */
export interface ConnectionDraft {
  /** Tells connections apart while they move and go. */
  key: string
  moduleName: string
  /** What the minion calls the connection; "" for "<channel>-<position>". */
  name: string
  /** The name is the operator's, not one a fill gave: the next fill keeps it. */
  named: boolean
  /** The endpoint the settings were last filled from. */
  filledFrom: string | null
  /** The channel's connection settings. */
  settings: Record<string, Draft>
  /** The factory's connection parameters for the channel kind. */
  drafts: Record<string, Draft>
  /** What the form says about the connection: settings of another version, settings left out. */
  notes: string[]
}

export interface BuildForm {
  target: string
  platform: string
  drafts: Record<string, Draft>
  connections: ConnectionDraft[]
}

/** A connection as a preset holds it, or as an earlier build's turns into. */
export interface ConnectionSource {
  moduleName: string
  name?: string | null
  /** The channel's settings version they were saved against. */
  schemaVersion?: number | null
  settings?: Record<string, unknown> | null
  values?: Record<string, unknown> | null
  notes?: readonly string[]
}

/** What a form is filled from: a preset, or an earlier build. */
export interface FormSource {
  target: string
  platform?: string | null
  values?: Record<string, unknown> | null
  connections?: readonly ConnectionSource[]
}

function draftsFor(fields: readonly FieldDef[], values: Record<string, unknown> | null | undefined): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of fields) out[f.key] = initialDraft(f, values?.[f.key] ?? null, AS_TEXT)
  return out
}

let lastKey = 0

/** A connection through ch (undefined: the channel declares no settings now), from source. */
export function connectionDraft(kind: FactoryKind, ch: ChannelKind | undefined, source: ConnectionSource): ConnectionDraft {
  const notes = [...(source.notes ?? [])]
  if (ch && typeof source.schemaVersion === "number" && source.schemaVersion !== ch.version) {
    notes.unshift(`Saved for ${ch.name} settings v${source.schemaVersion}; the channel is at v${ch.version}. Check the settings.`)
  }
  return {
    key: `connection-${++lastKey}`,
    moduleName: source.moduleName,
    name: source.name ?? "",
    named: !!source.name?.trim(),
    filledFrom: null,
    settings: draftsFor(ch?.fields ?? [], source.settings),
    drafts: draftsFor(kind.channelFields.get(source.moduleName) ?? [], source.values),
    notes,
  }
}

/**
 * The connection with its settings replaced by an endpoint's values. It takes
 * the endpoint's name too, unless the operator named it: the first of name,
 * name-2, -3… no other connection is called (taken).
 */
export function fillFromEndpoint(
  c: ConnectionDraft,
  ch: ChannelKind,
  ep: { name: string; values?: unknown },
  taken: Iterable<string> = [],
): ConnectionDraft {
  return {
    ...c,
    name: c.named ? c.name : freeName(ep.name, taken, ""),
    filledFrom: ep.name,
    settings: draftsFor(ch.fields, isRecord(ep.values) ? ep.values : null),
    notes: [],
  }
}

/** The names the connections go by, theirs or the server's default. */
export function connectionNames(connections: readonly Pick<ConnectionDraft, "moduleName" | "name">[]): string[] {
  return connections.map((c, i) => c.name.trim() || defaultConnectionName(c.moduleName, i))
}

/** The first settings with a value, for one line: "https://edge.example.com · 10s". */
export function settingsSummary(channel: ChannelKind | undefined, values: unknown): string {
  return settingRows(channel ?? null, isRecord(values) ? values : {})
    .slice(0, 2)
    .map((r) => r.text)
    .join(" · ")
}

/** What a search for an endpoint looks through: its name, description, channel and settings. */
export function endpointSearchText(
  ep: { name: string; description?: string | null; moduleName: string; values?: unknown },
  channel: ChannelKind | undefined,
): string {
  const settings = settingRows(channel ?? null, isRecord(ep.values) ? ep.values : {}).map((r) => r.text)
  return [ep.name, ep.description ?? "", ep.moduleName, ...settings].join("\n").toLowerCase()
}

/** Whether text (from endpointSearchText) holds every word of query, case aside. */
export function matchesQuery(text: string, query: string): boolean {
  return query
    .toLowerCase()
    .split(/\s+/)
    .every((w) => text.includes(w))
}

/** The name the server gives connection i when it has none. */
export function defaultConnectionName(moduleName: string, i: number): string {
  return `${moduleName}-${i + 1}`
}

/** A form's start: the source's settings, or the first target with its only platform when it has one. */
export function initialForm(
  kind: FactoryKind,
  source: FormSource | null,
  channels: ReadonlyMap<string, ChannelKind>,
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
    connections: (source?.connections ?? []).map((c) => connectionDraft(kind, channels.get(c.moduleName), c)),
  }
}

/**
 * What "build again" fills the form with. A build holds every default
 * filled in: values equal to today's defaults are left out, so they read as
 * defaults; so are connection names the server gave, and settings the channel
 * no longer declares, with a note.
 */
export function sourceFromBuild(
  kind: FactoryKind,
  build: { target: string; platform: string; values?: unknown; connections?: unknown },
  channels: ReadonlyMap<string, ChannelKind>,
): FormSource {
  const target = kind.targets.find((t) => t.id === build.target)
  const records = Array.isArray(build.connections) ? build.connections : []
  const connections = records.map((raw: unknown, i): ConnectionSource => {
    const rec = isRecord(raw) ? raw : {}
    const ep = isRecord(rec.endpoint) ? rec.endpoint : {}
    const channel = isRecord(ep.channel) ? ep.channel : {}
    const moduleName = typeof channel.module_name === "string" ? channel.module_name : ""
    const params = isRecord(ep.params) ? ep.params : {}
    const ch = channels.get(moduleName)
    const notes: string[] = []
    const dropped = ch ? Object.keys(params).filter((k) => !ch.fields.some((f) => f.key === k)) : []
    if (ch && dropped.length > 0) {
      notes.push(`Left out, not settings of ${moduleName} v${ch.version}: ${dropped.sort().join(", ")}.`)
    }
    const name = typeof ep.name === "string" && ep.name !== defaultConnectionName(moduleName, i) ? ep.name : null
    return {
      moduleName,
      name,
      schemaVersion: typeof channel.schema_version === "number" ? channel.schema_version : null,
      settings: withoutDefaults(ch?.fields ?? [], params),
      values: withoutDefaults(kind.channelFields.get(moduleName) ?? [], isRecord(rec.values) ? rec.values : {}),
      notes,
    }
  })
  return {
    target: build.target,
    platform: build.platform,
    values: withoutDefaults(targetFields(kind, target), isRecord(build.values) ? build.values : {}),
    connections,
  }
}

/** values of fields, but those equal to their field's default. */
function withoutDefaults(fields: readonly FieldDef[], values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    const v = values[f.key]
    if (v !== undefined && v !== null && !sameJSON(v, f.default)) out[f.key] = v
  }
  return out
}

/** One connection as createBuild and the preset writes take it. */
export interface ConnectionInput {
  moduleName: string
  name: string | null
  settings: Record<string, unknown>
  values: Record<string, unknown>
}

/** What createBuild / a preset write takes from the form. */
export interface FormInput {
  target: string
  platform: string
  values: Record<string, unknown>
  connections: ConnectionInput[]
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
 * (partial false) needs a platform, a connection and every required value
 * and setting; a preset may leave them out.
 */
export function evaluateForm(
  kind: FactoryKind,
  form: BuildForm,
  partial: boolean,
  channels: ReadonlyMap<string, ChannelKind>,
): { input: FormInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {}
  const target = kind.targets.find((t) => t.id === form.target)
  if (!target) errors.target = "Pick a target"
  else if (form.platform === "" && !partial) errors.platform = "Pick a platform"
  else if (form.platform !== "" && !target.platforms.includes(form.platform)) errors.platform = "Not a platform of this target"

  const values = evaluateFields("values.", targetFields(kind, target), form.drafts, partial, errors)
  if (form.connections.length === 0 && !partial) errors.connections = "Add at least one connection"
  const named = new Map<string, number>()
  const connections = form.connections.map((c, i): ConnectionInput => {
    const at = `connections[${i}]`
    const why = channelUnusable(target, c.moduleName, channels)
    if (why) errors[at] = why
    const name = c.name.trim()
    const resolved = name || defaultConnectionName(c.moduleName, i)
    const nameErr = name === "" ? null : nameError(name)
    if (nameErr) errors[`${at}.name`] = nameErr
    else if (named.has(resolved)) errors[`${at}.name`] = `Connection ${named.get(resolved)! + 1} is called ${resolved} too`
    else named.set(resolved, i)
    return {
      moduleName: c.moduleName,
      name: name || null,
      settings: evaluateFields(`${at}.settings.`, channels.get(c.moduleName)?.fields ?? [], c.settings, partial, errors),
      values: evaluateFields(`${at}.values.`, kind.channelFields.get(c.moduleName) ?? [], c.drafts, partial, errors),
    }
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
