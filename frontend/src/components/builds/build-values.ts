// Pure value handling for minion builds: factory kinds as the forms use them,
// the channels a target can connect through, the build form's drafts and what
// it submits, and display helpers. Field rules are the ones of
// field-values.ts; errors are keyed like the server's problems ("values.<key>",
// "connections[<i>].settings.<key>", ...) so both land on the same control.
// No React here, so all of it is unit-tested directly.

import {
  compareFields,
  DEFAULT_GROUP,
  emptyDraft,
  humanizeBytes,
  initialDraft,
  parseDraft,
  validateValue,
  type Draft,
  type FieldDef,
} from "@/components/modules/management/field-values"
import {
  evaluateEndpoint,
  freeName,
  isRecord,
  nameError,
  sameJSON,
  settingRows,
  type Kind,
} from "@/components/endpoints/endpoint-values"
import type { BuildSchemaFieldsFragment } from "@/graphql/gql/graphql"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"

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

/** A command a factory's minions can be built with. */
export interface Command {
  id: string
  label: string
  description: string | null
  /** The heading it is listed under; "" for none. */
  group: string
  /** The targets whose builds can have it. */
  targets: string[]
  /** Picked in a new build until the operator unpicks it. */
  default: boolean
  /** In every build of its targets: it cannot be unpicked. */
  required: boolean
  /** What a task of the command takes, sorted. */
  params: FieldDef[]
}

export interface FactoryKind {
  name: string
  /** The kind's own icon; its SVGs are "" for the default. */
  icon: ModuleIconSvgs
  version: number
  /** Registered instances declaring this version: a build needs one. */
  instances: number
  crypto: string[]
  /** Fields every target shares, sorted. */
  fields: FieldDef[]
  /** The factory's connection parameters per channel kind, sorted. */
  channelFields: Map<string, FieldDef[]>
  targets: Target[]
  /** What the minions can do, in the factory's order. */
  commands: Command[]
}

const asText = (f: FieldDef): FieldDef => (f.type === "secret" ? { ...f, type: "string" } : f)
const sorted = (fields: readonly FieldDef[]) => fields.map(asText).sort(compareFields)

/** Factory kinds by name from the factoryKinds query. */
export function factoryKindsByName(
  kinds: readonly {
    moduleName: string
    icon?: string
    iconLight?: string
    instances: number
    schema: BuildSchemaFieldsFragment
  }[],
): Map<string, FactoryKind> {
  const out = new Map<string, FactoryKind>()
  for (const k of kinds) {
    out.set(k.moduleName, {
      name: k.moduleName,
      icon: { icon: k.icon ?? "", iconLight: k.iconLight ?? "" },
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
      commands: k.schema.commands.map((c) => ({
        id: c.id,
        label: c.label,
        description: c.description ?? null,
        group: c.group ?? "",
        targets: c.targets,
        default: c.default,
        required: c.required,
        params: sorted(c.params),
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
// Commands
// ---------------------------------------------------------------------------

/** The commands a build of target can have, in the factory's order. */
export function targetCommands(kind: FactoryKind, target: Target | undefined): Command[] {
  return target ? kind.commands.filter((c) => c.targets.includes(target.id)) : []
}

/** What a new build of target starts with, and one naming none gets: its default and required commands. */
export function defaultCommands(kind: FactoryKind, target: Target | undefined): string[] {
  return targetCommands(kind, target)
    .filter((c) => c.default || c.required)
    .map((c) => c.id)
}

/**
 * The commands picked, as a build of target has them: the target's among
 * them, with its required ones, in the factory's order.
 */
export function pickedCommands(kind: FactoryKind, target: Target | undefined, picked: Iterable<string>): string[] {
  const on = new Set(picked)
  return targetCommands(kind, target)
    .filter((c) => c.required || on.has(c.id))
    .map((c) => c.id)
}

/**
 * Commands under their headings, in the factory's order: the first command of
 * each heading places it. One without a heading goes under fields' default.
 */
export function groupCommands(commands: readonly Command[]): { group: string; items: Command[] }[] {
  const groups = new Map<string, Command[]>()
  for (const c of commands) {
    const g = c.group.trim() || DEFAULT_GROUP
    const list = groups.get(g)
    if (list) list.push(c)
    else groups.set(g, [c])
  }
  return [...groups].map(([group, items]) => ({ group, items }))
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
  /** The operators' note on the connection: core keeps it, the factory never gets it. */
  description: string
  /** The description is the operator's: the next fill keeps it. */
  described: boolean
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
  /** What the build is for: the operators' note, required. */
  description: string
  target: string
  platform: string
  drafts: Record<string, Draft>
  /** The commands picked, the target's required ones among them, in the factory's order. */
  commands: string[]
  connections: ConnectionDraft[]
}

/** A connection as an earlier build's turns into ("Build again"). */
export interface ConnectionSource {
  moduleName: string
  name?: string | null
  description?: string | null
  /** The channel's settings version they were saved against. */
  schemaVersion?: number | null
  settings?: Record<string, unknown> | null
  values?: Record<string, unknown> | null
  notes?: readonly string[]
}

/** What a form is filled from: an earlier build ("Build again"). */
export interface FormSource {
  description?: string | null
  target: string
  platform?: string | null
  values?: Record<string, unknown> | null
  /** The commands it had; none named: the target's defaults. */
  commands?: readonly string[] | null
  connections?: readonly ConnectionSource[]
}

/**
 * Drafts for fields from values, a field values do not set filled in with its
 * default: the form shows what the build will use, not empty fields.
 */
export function draftsFor(
  fields: readonly FieldDef[],
  values: Record<string, unknown> | null | undefined,
): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of fields) out[f.key] = initialDraft(f, values?.[f.key] ?? f.default ?? null, AS_TEXT)
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
    description: source.description ?? "",
    described: !!source.description?.trim(),
    filledFrom: null,
    settings: draftsFor(ch?.fields ?? [], source.settings),
    drafts: draftsFor(kind.channelFields.get(source.moduleName) ?? [], source.values),
    notes,
  }
}

/**
 * The connection with its settings replaced by an endpoint's values. It takes
 * the endpoint's name and description too, unless the operator wrote their
 * own: the name as the first of name, name-2, -3… no other connection is
 * called (taken).
 */
export function fillFromEndpoint(
  c: ConnectionDraft,
  ch: ChannelKind,
  ep: { name: string; description?: string | null; values?: unknown },
  taken: Iterable<string> = [],
): ConnectionDraft {
  return {
    ...c,
    name: c.named ? c.name : freeName(ep.name, taken, ""),
    description: c.described ? c.description : (ep.description ?? ""),
    filledFrom: ep.name,
    settings: draftsFor(ch.fields, isRecord(ep.values) ? ep.values : null),
    notes: [],
  }
}

/** A connection as a new endpoint of its channel, and what keeps it from being one. */
export interface EndpointDraft {
  /** The connection's name, or its default, made free of taken. */
  name: string
  description: string
  /** The settings set, as an endpoint stores them; the factory's parameters are no endpoint's. */
  values: Record<string, unknown>
  /** Settings an endpoint cannot take as they are: it needs every required one, valid. */
  problems: { label: string; message: string }[]
}

/**
 * What saving connection i (c) as an endpoint makes. Its name is the
 * connection's; the form asks the server for a free one like it.
 */
export function endpointFromConnection(c: ConnectionDraft, i: number, channel: ChannelKind): EndpointDraft {
  const { values, errors } = evaluateEndpoint(channel.fields, c.settings)
  return {
    name: c.name.trim() || defaultConnectionName(c.moduleName, i),
    description: c.description.trim(),
    values,
    problems: channel.fields.filter((f) => errors[f.key]).map((f) => ({ label: f.label, message: errors[f.key] })),
  }
}

/** The names the connections go by, theirs or the server's default. */
export function connectionNames(connections: readonly Pick<ConnectionDraft, "moduleName" | "name">[]): string[] {
  return connections.map((c, i) => c.name.trim() || defaultConnectionName(c.moduleName, i))
}

/** The first count settings with a value, for one line: "https://edge.example.com · 10s". */
export function settingsSummary(channel: ChannelKind | undefined, values: unknown, count = 2): string {
  return settingRows(channel ?? null, isRecord(values) ? values : {})
    .slice(0, count)
    .map((r) => r.text)
    .join(" · ")
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
    description: source?.description ?? "",
    target: target?.id ?? "",
    platform,
    drafts: draftsFor(targetFields(kind, target), source?.values),
    commands: source?.commands ? pickedCommands(kind, target, source.commands) : defaultCommands(kind, target),
    connections: (source?.connections ?? []).map((c) => connectionDraft(kind, channels.get(c.moduleName), c)),
  }
}

/** A build as its record holds it, for what is read back from it. */
export interface BuildRecordLike {
  description?: string | null
  target: string
  platform: string
  values?: unknown
  /** The commands the minion was built with. */
  commands?: readonly string[] | null
  /** What the factory got: [{endpoint: <export>, values}]. */
  connections?: unknown
  /** The operators' notes on the connections, by position. */
  connectionDescriptions?: readonly string[] | null
}

/** One of a build record's connections, as far as it reads. */
function recordedConnection(raw: unknown) {
  const rec = isRecord(raw) ? raw : {}
  const ep = isRecord(rec.endpoint) ? rec.endpoint : {}
  const channel = isRecord(ep.channel) ? ep.channel : {}
  return {
    moduleName: typeof channel.module_name === "string" ? channel.module_name : "",
    name: typeof ep.name === "string" ? ep.name : "",
    schemaVersion: typeof channel.schema_version === "number" ? channel.schema_version : null,
    params: isRecord(ep.params) ? ep.params : {},
    values: isRecord(rec.values) ? rec.values : {},
  }
}

/** A build record's connections as far as they read, with the operators' notes. */
export function recordedConnections(build: Pick<BuildRecordLike, "connections" | "connectionDescriptions">) {
  const records = Array.isArray(build.connections) ? build.connections : []
  return records.map((raw: unknown, i) => ({
    ...recordedConnection(raw),
    description: build.connectionDescriptions?.[i] ?? "",
  }))
}

/** A build's connections as its page lists them: who, where, and the operators' note. */
export function buildConnectionRows(
  build: Pick<BuildRecordLike, "connections" | "connectionDescriptions">,
  channels: ReadonlyMap<string, ChannelKind>,
): { moduleName: string; name: string; address: string; description: string }[] {
  const records = Array.isArray(build.connections) ? build.connections : []
  return records.map((raw: unknown, i) => {
    const c = recordedConnection(raw)
    return {
      moduleName: c.moduleName,
      name: c.name,
      // The channel's first setting, usually where the minion connects.
      address: settingsSummary(channels.get(c.moduleName), c.params, 1),
      description: build.connectionDescriptions?.[i] ?? "",
    }
  })
}

/**
 * What "build again" fills the form with. A build holds every default
 * filled in: values equal to today's defaults are left out, so they read as
 * defaults; so are connection names the server gave, and settings the channel
 * no longer declares, with a note. Its commands are picked again, those the
 * target no longer has aside.
 */
export function sourceFromBuild(
  kind: FactoryKind,
  build: BuildRecordLike,
  channels: ReadonlyMap<string, ChannelKind>,
): FormSource {
  const target = kind.targets.find((t) => t.id === build.target)
  const records = Array.isArray(build.connections) ? build.connections : []
  const connections = records.map((raw: unknown, i): ConnectionSource => {
    const { moduleName, name, schemaVersion, params, values } = recordedConnection(raw)
    const ch = channels.get(moduleName)
    const notes: string[] = []
    const dropped = ch ? Object.keys(params).filter((k) => !ch.fields.some((f) => f.key === k)) : []
    if (ch && dropped.length > 0) {
      notes.push(`Left out, not settings of ${moduleName} v${ch.version}: ${dropped.sort().join(", ")}.`)
    }
    return {
      moduleName,
      name: name !== "" && name !== defaultConnectionName(moduleName, i) ? name : null,
      description: build.connectionDescriptions?.[i] || null,
      schemaVersion,
      settings: withoutDefaults(ch?.fields ?? [], params),
      values: withoutDefaults(kind.channelFields.get(moduleName) ?? [], values),
      notes,
    }
  })
  return {
    description: build.description,
    target: build.target,
    platform: build.platform,
    values: withoutDefaults(targetFields(kind, target), isRecord(build.values) ? build.values : {}),
    commands: build.commands ?? null,
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

/** One connection as createBuild takes it. */
export interface ConnectionInput {
  moduleName: string
  name: string | null
  description: string | null
  settings: Record<string, unknown>
  values: Record<string, unknown>
}

/** What createBuild takes from the form. */
export interface FormInput {
  description: string
  target: string
  platform: string
  values: Record<string, unknown>
  commands: string[]
  connections: ConnectionInput[]
}

function evaluateFields(
  prefix: string,
  fields: readonly FieldDef[],
  drafts: Record<string, Draft>,
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
    const error = validateValue(f, v, { fallback: f.default })
    if (error) errors[prefix + f.key] = error
    // At its default (the form starts there), left to the server's default.
    else if (v !== null && v !== undefined && !sameJSON(v, f.default)) values[f.key] = v
  }
  return values
}

/** The longest description core takes, of a build and of a connection, in characters. */
const MAX_DESCRIPTION = 500

/** What checkBuild looks at: a build's own parts, its values aside. */
export interface BuildShape {
  description: string
  target: string
  platform: string
  commands: readonly string[]
  connections: readonly { moduleName: string; name: string; description: string }[]
}

/**
 * Adds to errors what a build needs besides valid values: a description, a
 * target and one of its platforms, the target's required commands, a
 * connection, each through a channel the target speaks and named apart from
 * the others. Keyed like the server's problems ("connections[1].name").
 */
export function checkBuild(
  kind: FactoryKind,
  b: BuildShape,
  channels: ReadonlyMap<string, ChannelKind>,
  errors: Record<string, string>,
) {
  const description = b.description.trim()
  if (description === "") errors.description = "Required"
  else if ([...description].length > MAX_DESCRIPTION) errors.description = `At most ${MAX_DESCRIPTION} characters`
  const target = kind.targets.find((t) => t.id === b.target)
  if (!target) errors.target = "Pick a target"
  else if (b.platform === "") errors.platform = "Pick a platform"
  else if (!target.platforms.includes(b.platform)) errors.platform = "Not a platform of this target"

  // The form cannot leave a required command out; the JSON tab can.
  const listed = new Set(b.commands)
  const missing = targetCommands(kind, target).filter((c) => c.required && !listed.has(c.id))
  if (missing.length > 0) {
    errors.commands = `The ${target!.id} target requires ${missing.map((c) => c.id).join(", ")}`
  }

  if (b.connections.length === 0) errors.connections = "Add at least one connection"
  const named = new Map<string, number>()
  b.connections.forEach((c, i) => {
    const at = `connections[${i}]`
    const why = channelUnusable(target, c.moduleName, channels)
    if (why) errors[at] = why
    const name = c.name.trim()
    const resolved = name || defaultConnectionName(c.moduleName, i)
    const nameErr = name === "" ? null : nameError(name)
    if (nameErr) errors[`${at}.name`] = nameErr
    else if (named.has(resolved)) errors[`${at}.name`] = `Connection ${named.get(resolved)! + 1} is called ${resolved} too`
    else named.set(resolved, i)
    if ([...c.description.trim()].length > MAX_DESCRIPTION) errors[`${at}.description`] = `At most ${MAX_DESCRIPTION} characters`
  })
}

/**
 * The form as createBuild takes it, and its errors: a build needs a
 * description, a platform, a connection and every required value and
 * setting.
 */
export function evaluateForm(
  kind: FactoryKind,
  form: BuildForm,
  channels: ReadonlyMap<string, ChannelKind>,
): { input: FormInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {}
  checkBuild(kind, form, channels, errors)
  const target = kind.targets.find((t) => t.id === form.target)
  const values = evaluateFields("values.", targetFields(kind, target), form.drafts, errors)
  const connections = form.connections.map((c, i): ConnectionInput => {
    const at = `connections[${i}]`
    return {
      moduleName: c.moduleName,
      name: c.name.trim() || null,
      description: c.description.trim() || null,
      settings: evaluateFields(`${at}.settings.`, channels.get(c.moduleName)?.fields ?? [], c.settings, errors),
      values: evaluateFields(`${at}.values.`, kind.channelFields.get(c.moduleName) ?? [], c.drafts, errors),
    }
  })
  const input = {
    description: form.description.trim(),
    target: form.target,
    platform: form.platform,
    values,
    commands: form.commands,
    connections,
  }
  return { input, errors }
}

/**
 * The form with another target: drafts for its fields, kept where they carry
 * over, and so its commands: one both targets have keeps its pick, the new
 * target's others start as in a new build.
 */
export function withTarget(kind: FactoryKind, form: BuildForm, targetId: string): BuildForm {
  const target = kind.targets.find((t) => t.id === targetId)
  const drafts: Record<string, Draft> = {}
  for (const f of targetFields(kind, target)) {
    drafts[f.key] = form.drafts[f.key] ?? initialDraft(f, f.default ?? null, AS_TEXT)
  }
  const before = kind.targets.find((t) => t.id === form.target)
  const had = new Set(targetCommands(kind, before).map((c) => c.id))
  const picked = targetCommands(kind, target).filter((c) => (had.has(c.id) ? form.commands.includes(c.id) : c.default))
  const platform = target?.platforms.includes(form.platform)
    ? form.platform
    : target?.platforms.length === 1
      ? target.platforms[0]
      : ""
  return { ...form, target: targetId, platform, drafts, commands: pickedCommands(kind, target, picked.map((c) => c.id)) }
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

/** One stage of a build's pipeline, as Build.stages has it. */
export interface StageLike {
  id: string
  label: string
  /** Core's own stage (the queue, the artifact check), not the factory's. */
  core: boolean
  /** PENDING | RUNNING | SUCCEEDED | FAILED | CANCELLED | SKIPPED */
  state: string
  message?: string | null
  startedAt?: string | null
  finishedAt?: string | null
}

/** How long a stage took, or has been running; null when its times are not known. */
export function stageDuration(s: StageLike, now = Date.now()): number | null {
  if (!s.startedAt) return null
  const end = s.finishedAt ? Date.parse(s.finishedAt) : s.state === "RUNNING" ? now : null
  return end === null ? null : Math.max(0, end - Date.parse(s.startedAt))
}

/** The stage a build is at: the running one, or the one it stopped in when it did not succeed. */
export function currentStage<S extends StageLike>(stages: readonly S[]): S | null {
  return stages.find((s) => s.state === "RUNNING") ?? stages.find((s) => s.state === "FAILED" || s.state === "CANCELLED") ?? null
}
