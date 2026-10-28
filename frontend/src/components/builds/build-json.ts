import {
  fieldHints,
  formValues,
  jsonFieldValues,
  readFieldValues,
} from "@/components/modules/management/field-json"
import {
  childPointer,
  formatJson,
  isRecord,
  parseJson,
  sameJSON,
  type JsonHints,
  type JsonKeyHint,
  type JsonProblem,
} from "@/lib/json-edit"
import {
  checkBuild,
  connectionDraft,
  defaultCommands,
  draftsFor,
  pickedCommands,
  targetCommands,
  targetFields,
  type BuildForm,
  type ChannelKind,
  type FactoryKind,
  type FormInput,
  type Target,
} from "./build-values"

// A new build as the build modal's JSON tab edits it: what createBuild takes,
// every value listed with the one the build will use, the factory's or the
// channel's default where none is set, as the form shows it (the rules are
// modules/management/field-json.ts'). A value at its default, or empty, is
// left to the server's default, which core does for null, "" and [] anyway.
//
//   { "factory": { "module_name": "bob", "schema_version": 1 },
//     "description": "…", "target": "dummy", "platform": "any/any",
//     "commands": ["sleep", "exit", "hello"],
//     "values": { "greeting": "hello from BOB" },
//     "connections": [
//       { "channel": "http", "name": "", "description": "",
//         "settings": { "base_url": "https://…" }, "values": { "method": "POST" } } ] }
//
// "factory" says what builds it and stays as it is. "commands" are what the
// minion can do, by id: the target's required ones stay in, and leaving the
// list out takes the target's defaults. A connection's "settings" are its
// channel's connection settings, its "values" the factory's parameters for
// that channel.

/** A connection as the JSON tab and the form share it; settings and values hold set ones only. */
export interface ConnectionDocument {
  moduleName: string
  name: string
  description: string
  settings: Record<string, unknown>
  values: Record<string, unknown>
}

/** What the JSON tab and the form share; values hold set ones only. */
export interface BuildDocument {
  description: string
  target: string
  platform: string
  /** The commands listed that the target has, as listed. */
  commands: string[]
  values: Record<string, unknown>
  connections: ConnectionDocument[]
}

/** The parts the JSON tab locks. */
export const BUILD_JSON_LOCKED: readonly string[] = ["/factory"]

const KEYS = ["factory", "description", "target", "platform", "commands", "values", "connections"]
const CONNECTION_KEYS = ["channel", "name", "description", "settings", "values"]

const channelFields = (channels: ReadonlyMap<string, ChannelKind>, moduleName: string) =>
  channels.get(moduleName)?.fields ?? []

/** The JSON tab's text for a build of kind. */
export function buildJson(kind: FactoryKind, channels: ReadonlyMap<string, ChannelKind>, doc: BuildDocument): string {
  const target = kind.targets.find((t) => t.id === doc.target)
  return formatJson({
    factory: { module_name: kind.name, schema_version: kind.version },
    description: doc.description,
    target: doc.target,
    platform: doc.platform,
    commands: doc.commands,
    values: jsonFieldValues(targetFields(kind, target), doc.values),
    connections: doc.connections.map((c) => ({
      channel: c.moduleName,
      name: c.name,
      description: c.description,
      settings: jsonFieldValues(channelFields(channels, c.moduleName), c.settings),
      values: jsonFieldValues(kind.channelFields.get(c.moduleName) ?? [], c.values),
    })),
  })
}

/** The form as the JSON tab's document: what it holds, typed text that does not parse kept as typed. */
export function formDocument(kind: FactoryKind, channels: ReadonlyMap<string, ChannelKind>, form: BuildForm): BuildDocument {
  const target = kind.targets.find((t) => t.id === form.target)
  return {
    description: form.description,
    target: form.target,
    platform: form.platform,
    commands: form.commands,
    values: formValues(targetFields(kind, target), form.drafts),
    connections: form.connections.map((c) => ({
      moduleName: c.moduleName,
      name: c.name,
      description: c.description,
      settings: formValues(channelFields(channels, c.moduleName), c.settings),
      values: formValues(kind.channelFields.get(c.moduleName) ?? [], c.drafts),
    })),
  }
}

/** The JSON tab's document as the form, for the Form tab and the close guard. */
export function formFromDocument(kind: FactoryKind, channels: ReadonlyMap<string, ChannelKind>, doc: BuildDocument): BuildForm {
  const target = kind.targets.find((t) => t.id === doc.target)
  return {
    description: doc.description,
    target: doc.target,
    platform: doc.platform,
    drafts: draftsFor(targetFields(kind, target), doc.values),
    commands: pickedCommands(kind, target, doc.commands),
    connections: doc.connections.map((c) =>
      connectionDraft(kind, channels.get(c.moduleName), {
        moduleName: c.moduleName,
        name: c.name || null,
        description: c.description || null,
        settings: c.settings,
        values: c.values,
      }),
    ),
  }
}

/** What createBuild takes from the JSON tab's document. */
export function documentInput(doc: BuildDocument): FormInput {
  return {
    description: doc.description.trim(),
    target: doc.target,
    platform: doc.platform,
    values: doc.values,
    commands: doc.commands,
    connections: doc.connections.map((c) => ({
      moduleName: c.moduleName,
      name: c.name.trim() || null,
      description: c.description.trim() || null,
      settings: c.settings,
      values: c.values,
    })),
  }
}

/** The JSON tab's text read back: not a build at all, or one with its problems. */
export type BuildJsonRead = { ok: false; message: string } | { ok: true; doc: BuildDocument; problems: JsonProblem[] }

/** Reads the JSON tab's text, checking it as the form checks itself. */
export function readBuildJson(text: string, kind: FactoryKind, channels: ReadonlyMap<string, ChannelKind>): BuildJsonRead {
  const parsed = parseJson(text)
  if (!parsed.ok) return { ok: false, message: parsed.message }
  const root = parsed.value
  if (!isRecord(root)) return { ok: false, message: "A build is a JSON object." }

  const problems: JsonProblem[] = []
  const at = (path: string, message: string) => problems.push({ path, message })
  for (const key of Object.keys(root)) {
    if (!KEYS.includes(key)) at(childPointer("", key), `Not part of a build, which has ${KEYS.join(", ")}`)
  }
  if (root.factory !== undefined && !sameJSON(root.factory, { module_name: kind.name, schema_version: kind.version })) {
    at("/factory", `Must stay this build's factory: ${kind.name}, schema v${kind.version}`)
  }
  const description = textOf(root.description, "/description", at)
  const targetId = textOf(root.target, "/target", at)
  const platform = textOf(root.platform, "/platform", at)
  const target = kind.targets.find((t) => t.id === targetId)
  const commands = readCommands(root.commands, kind, target, at)
  const values = readFieldValues(
    targetFields(kind, target),
    root.values,
    "/values",
    {
      unknown: target ? `Not a value of ${kind.name}'s ${target.id} target` : `Not a value of ${kind.name}`,
      notObject: "Must be an object of values",
    },
    problems,
  )

  const connections: ConnectionDocument[] = []
  const list: unknown[] = Array.isArray(root.connections) ? root.connections : []
  if (root.connections !== undefined && !Array.isArray(root.connections)) {
    at("/connections", "Must be a list of connections")
  } else {
    for (const [i, raw] of list.entries()) {
      const here = childPointer("/connections", i)
      if (!isRecord(raw)) {
        at(here, "Must be a connection: an object with its channel, settings and values")
        continue
      }
      for (const key of Object.keys(raw)) {
        if (!CONNECTION_KEYS.includes(key)) {
          at(childPointer(here, key), `Not part of a connection, which has ${CONNECTION_KEYS.join(", ")}`)
        }
      }
      const moduleName = textOf(raw.channel, childPointer(here, "channel"), at)
      connections.push({
        moduleName,
        name: textOf(raw.name, childPointer(here, "name"), at),
        description: textOf(raw.description, childPointer(here, "description"), at),
        settings: readFieldValues(
          channelFields(channels, moduleName),
          raw.settings,
          childPointer(here, "settings"),
          { unknown: `Not a setting of ${moduleName || "this channel"}`, notObject: "Must be an object of settings" },
          problems,
        ),
        values: readFieldValues(
          kind.channelFields.get(moduleName) ?? [],
          raw.values,
          childPointer(here, "values"),
          {
            unknown: `Not one of ${kind.name}'s values for ${moduleName || "this channel"}`,
            notObject: "Must be an object of values",
          },
          problems,
        ),
      })
    }
  }

  // The checks the form makes on itself, said the JSON's way.
  const errors: Record<string, string> = {}
  const doc: BuildDocument = { description, target: targetId, platform, commands, values, connections }
  checkBuild(kind, doc, channels, errors)
  for (const [key, message] of Object.entries(errors)) {
    if (key === "target" && targetId !== "") {
      at("/target", `Not a target of ${kind.name}: ${kind.targets.map((t) => t.id).join(", ")}`)
    } else if (key === "platform" && target && platform !== "") {
      at("/platform", `Not a platform of ${target.id}: ${target.platforms.join(", ")}`)
    } else {
      // A connection's own problem is its channel's: marked there.
      at(/^connections\[\d+\]$/.test(key) ? `${buildProblemPointer(key)}/channel` : buildProblemPointer(key), message)
    }
  }
  return { ok: true, doc, problems }
}

/**
 * The commands listed that target has, each a problem where it is not one of
 * them; none listed (no key, null), the target's defaults.
 */
function readCommands(
  raw: unknown,
  kind: FactoryKind,
  target: Target | undefined,
  at: (path: string, message: string) => void,
): string[] {
  if (raw === undefined || raw === null) return defaultCommands(kind, target)
  if (!Array.isArray(raw)) {
    // The one problem there: the form goes on with the defaults.
    at("/commands", "Must be a list of command ids")
    return defaultCommands(kind, target)
  }
  const own = new Set(targetCommands(kind, target).map((c) => c.id))
  const out: string[] = []
  raw.forEach((id: unknown, i) => {
    const here = childPointer("/commands", i)
    if (typeof id !== "string") at(here, "Must be a command id")
    else if (out.includes(id)) at(here, "Listed twice")
    else if (!kind.commands.some((c) => c.id === id)) at(here, `Not a command of ${kind.name}`)
    // Without a target there is nothing to tell; the target is marked.
    else if (target && !own.has(id)) at(here, `Not a command of the ${target.id} target`)
    else out.push(id)
  })
  return out
}

/** A text property's value: "" when absent or null, a problem when not text. */
function textOf(value: unknown, path: string, at: (path: string, message: string) => void): string {
  if (typeof value === "string") return value
  if (value !== undefined && value !== null) at(path, "Must be text")
  return ""
}

/**
 * A build problem's key, the form's and core's ("connections[1].settings.base_url"),
 * as a JSON Pointer into the JSON tab's document ("/connections/1/settings/base_url").
 */
export function buildProblemPointer(key: string): string {
  if (key === "") return ""
  const command = /^commands\[(\d+)\]$/.exec(key)
  if (command) return childPointer("/commands", command[1])
  const value = /^values\.(.+)$/.exec(key)
  if (value) return childPointer("/values", value[1])
  const field = /^connections\[(\d+)\]\.(settings|values)\.(.+)$/.exec(key)
  if (field) return childPointer(childPointer(childPointer("/connections", field[1]), field[2]), field[3])
  const connection = /^connections\[(\d+)\](?:\.(.+))?$/.exec(key)
  if (connection) {
    const here = childPointer("/connections", connection[1])
    return connection[2] ? childPointer(here, connection[2]) : here
  }
  return childPointer("", key)
}

/**
 * What the JSON tab offers as you type, and says on hover: the build's own
 * keys, the target's values, and for each connection, by its channel, the
 * channel's settings and the factory's values for it.
 */
export function buildJsonHints(
  kind: FactoryKind,
  channels: ReadonlyMap<string, ChannelKind>,
  targetId: string,
  connectionChannels: readonly string[],
): JsonHints {
  const target = kind.targets.find((t) => t.id === targetId)
  const hints: Record<string, readonly JsonKeyHint[]> = {
    "": [
      {
        key: "description",
        detail: "text, required",
        info: "What this build is for, for the rest of the team. Core keeps it with the build; the factory and the minion never get it.",
      },
      { key: "target", detail: "text, required", info: "What the factory produces.", values: kind.targets.map((t) => t.id) },
      { key: "platform", detail: "text, required", info: "Where the minion runs.", values: target?.platforms },
      {
        key: "commands",
        detail: "list",
        info: "What the minion can do, by command id: it has these and no others. The target's required ones stay in; null or none listed: its defaults.",
        items: targetCommands(kind, target).map((c) => ({
          value: c.id,
          detail: c.required ? `${c.label}, required` : c.label,
          info: c.description ?? undefined,
        })),
      },
      { key: "values", detail: "object", info: `${kind.name}'s values for the target; empty or null leaves one at its default.` },
      { key: "connections", detail: "list, required", info: "How the minion reaches its channels, tried in this order." },
    ],
    "/values": fieldHints(targetFields(kind, target)),
    "/connections/*": [
      {
        key: "channel",
        detail: "text, required",
        info: "The channel the minion connects through.",
        values: target?.channels.map((c) => c.moduleName),
      },
      { key: "name", detail: "text", info: 'What the minion calls the connection; "" for <channel>-<position>.' },
      { key: "description", detail: "text", info: "A note for the team. Core keeps it; the factory never gets it." },
      { key: "settings", detail: "object", info: "The channel's connection settings." },
      { key: "values", detail: "object", info: `${kind.name}'s parameters for the channel.` },
    ],
  }
  connectionChannels.forEach((moduleName, i) => {
    hints[`/connections/${i}/settings`] = fieldHints(channelFields(channels, moduleName))
    hints[`/connections/${i}/values`] = fieldHints(kind.channelFields.get(moduleName) ?? [])
  })
  return hints
}
