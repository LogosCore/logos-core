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
  effectiveKind,
  paramFields,
  settingFields,
  pickedCommands,
  targetCommands,
  targetFields,
  type BuildForm,
  type ChannelKind,
  type FactoryKind,
  type FormInput,
  type PersistedChoice,
  type Target,
} from "./build-values"
import {
  DEFAULT_FAILOVER,
  readConnectionFailover,
  readFailover,
  type ConnectionFailover,
  type FailoverPolicy,
} from "./failover-values"
import { DEFAULT_MAX_TASKS, MAX_TASKS_LIMIT, parseMaxTasks } from "@/components/minion-sessions/max-tasks-values"

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
// "failover" is how the minion moves between its connections on its own
// ({enabled, fail_after, fail_for, wrap, cycle_pause, failback_after}; keys
// left out take the defaults), and a connection's "failover" its own
// ({auto, fail_after}).
//
// "max_tasks", for a factory whose minions take a limit: how many tasks the
// minion runs at once, 0 to 64 (left out: 5); its sessions change it live.
//
// "compression", for a factory whose minions inflate packed items: whether
// the build packs its messages (left out: true).
//
// "persisted_session", for a factory whose minions claim persisted sessions:
// false (none), true (a new one, reserved with the build), or an existing
// session's id, which the minion takes over.
//
// "factory" says what builds it and stays as it is. "commands" are what the
// minion can do, by id: the target's required ones stay in, and leaving the
// list out takes the target's defaults. A connection's "settings" are its
// channel's connection settings, its "values" the factory's parameters for
// that channel.

/** A connection as the JSON tab and the form share it; settings and values hold set ones only. */
export interface ConnectionDocument {
  moduleName: string
  /** The connection kind; "" for the default. */
  kind: string
  name: string
  description: string
  settings: Record<string, unknown>
  values: Record<string, unknown>
  failover?: ConnectionFailover
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
  failover: FailoverPolicy
  /** How many tasks at once, typed text that does not read kept as typed; absent where the factory has none. */
  maxTasks?: number | string
  /** Whether the build packs its messages; absent where the factory has none, or the JSON leaves it out (on). */
  compression?: boolean
  /** The persisted session; absent where the factory has none, or the JSON leaves it out. */
  persisted?: PersistedChoice
}

/** The parts the JSON tab locks. */
export const BUILD_JSON_LOCKED: readonly string[] = ["/factory"]

const KEYS = [
  "factory",
  "description",
  "target",
  "platform",
  "commands",
  "values",
  "connections",
  "failover",
  "max_tasks",
  "compression",
  "persisted_session",
]
const CONNECTION_KEYS = ["channel", "kind", "name", "description", "settings", "values", "failover"]

/**
 * A connection's settings and the factory's values for it: those of its
 * kind, or of the first the target and the channel both support when it
 * names none.
 */
function connectionFields(
  kind: FactoryKind,
  channels: ReadonlyMap<string, ChannelKind>,
  target: Target | undefined,
  c: { moduleName: string; kind: string },
) {
  const ch = channels.get(c.moduleName)
  const connKind = effectiveKind(c, target, ch)
  return { settings: settingFields(ch, connKind), values: paramFields(kind, c.moduleName, connKind) }
}

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
    connections: doc.connections.map((c) => {
      const fields = connectionFields(kind, channels, target, c)
      return {
        channel: c.moduleName,
        ...(c.kind ? { kind: c.kind } : {}),
        name: c.name,
        description: c.description,
        settings: jsonFieldValues(fields.settings, c.settings),
        values: jsonFieldValues(fields.values, c.values),
        ...(c.failover ? { failover: c.failover } : {}),
      }
    }),
    failover: doc.failover,
    ...(doc.maxTasks !== undefined ? { max_tasks: doc.maxTasks } : {}),
    ...(doc.compression !== undefined ? { compression: doc.compression } : {}),
    ...(doc.persisted ? { persisted_session: persistedJson(doc.persisted) } : {}),
  })
}

function persistedJson(c: PersistedChoice): boolean | string {
  return c.kind === "same" ? c.id : c.kind === "new"
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/** "persisted_session" read back; undefined when absent. */
function readPersisted(raw: unknown, at: (path: string, message: string) => void): PersistedChoice | undefined {
  if (raw === undefined) return undefined
  if (raw === false || raw === null) return { kind: "off" }
  if (raw === true) return { kind: "new" }
  if (typeof raw === "string" && UUID.test(raw)) return { kind: "same", id: raw.toLowerCase() }
  at("/persisted_session", "false, true (a new session) or a session id")
  return undefined
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
    connections: form.connections.map((c) => {
      const fields = connectionFields(kind, channels, target, c)
      return {
        moduleName: c.moduleName,
        kind: c.kind,
        name: c.name,
        description: c.description,
        settings: formValues(fields.settings, c.settings),
        values: formValues(fields.values, c.drafts),
        ...(c.failover ? { failover: c.failover } : {}),
      }
    }),
    failover: form.failover,
    ...(kind.maxTasks ? { maxTasks: maxTasksOf(form.maxTasks) } : {}),
    ...(kind.compression ? { compression: form.compression } : {}),
  }
}

/** A limit as typed: its number when it reads as one, the text otherwise. */
function maxTasksOf(text: string): number | string {
  const parsed = parseMaxTasks(text)
  return parsed.ok ? parsed.value : text
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
      connectionDraft(
        kind,
        channels.get(c.moduleName),
        {
          moduleName: c.moduleName,
          kind: c.kind || null,
          name: c.name || null,
          description: c.description || null,
          settings: c.settings,
          values: c.values,
          failover: c.failover,
        },
        target,
      ),
    ),
    failover: doc.failover,
    maxTasks: kind.maxTasks ? String(doc.maxTasks ?? DEFAULT_MAX_TASKS) : "",
    compression: doc.compression ?? true,
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
      kind: c.kind || null,
      name: c.name.trim() || null,
      description: c.description.trim() || null,
      settings: c.settings,
      values: c.values,
      failover: c.failover ?? null,
    })),
    failover: doc.failover,
    maxTasks: typeof doc.maxTasks === "number" ? doc.maxTasks : null,
    compression: doc.compression ?? null,
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
    // A build's own JSON lists its keys; a new build gets fresh ones.
    if (key === "crypto") continue
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
      const connKind = textOf(raw.kind, childPointer(here, "kind"), at)
      const fields = connectionFields(kind, channels, target, { moduleName, kind: connKind })
      const kindText = effectiveKind({ moduleName, kind: connKind }, target, channels.get(moduleName))
      connections.push({
        moduleName,
        kind: connKind,
        name: textOf(raw.name, childPointer(here, "name"), at),
        description: textOf(raw.description, childPointer(here, "description"), at),
        settings: readFieldValues(
          fields.settings,
          raw.settings,
          childPointer(here, "settings"),
          {
            unknown: `Not a ${kindText || "connection"} setting of ${moduleName || "this channel"}`,
            notObject: "Must be an object of settings",
          },
          problems,
        ),
        values: readFieldValues(
          fields.values,
          raw.values,
          childPointer(here, "values"),
          {
            unknown: `Not one of ${kind.name}'s ${kindText || "connection"} values for ${moduleName || "this channel"}`,
            notObject: "Must be an object of values",
          },
          problems,
        ),
        failover: readConnectionFailover(raw.failover, here, at),
      })
    }
  }

  // The checks the form makes on itself, said the JSON's way.
  const errors: Record<string, string> = {}
  const failover = readFailover(root.failover, at)
  const doc: BuildDocument = { description, target: targetId, platform, commands, values, connections, failover }
  if (root.max_tasks !== undefined && root.max_tasks !== null) {
    if (!kind.maxTasks) at("/max_tasks", `${kind.name}'s minions take no limit of tasks`)
    else {
      const raw = root.max_tasks
      const parsed = typeof raw === "number" ? parseMaxTasks(String(raw)) : null
      if (parsed?.ok) doc.maxTasks = parsed.value
      else {
        at("/max_tasks", `A whole number 0 to ${MAX_TASKS_LIMIT}`)
        doc.maxTasks = typeof raw === "string" || typeof raw === "number" ? raw : JSON.stringify(raw)
      }
    }
  }
  if (root.compression !== undefined && root.compression !== null) {
    if (!kind.compression) at("/compression", `${kind.name}'s minions inflate nothing`)
    else if (typeof root.compression === "boolean") doc.compression = root.compression
    else at("/compression", "true or false")
  }
  if (root.persisted_session !== undefined) {
    if (!kind.persistedSessions) at("/persisted_session", `${kind.name}'s minions do not claim persisted sessions`)
    else doc.persisted = readPersisted(root.persisted_session, at)
  }
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
  const failover = /^failover\.(.+)$/.exec(key)
  if (failover) return childPointer("/failover", failover[1])
  const connFailover = /^connections\[(\d+)\]\.failover\.(.+)$/.exec(key)
  if (connFailover) return childPointer(childPointer(childPointer("/connections", connFailover[1]), "failover"), connFailover[2])
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
 * keys, the target's values, and for each connection, by its channel and
 * connection kind, the channel's settings and the factory's values for it.
 */
export function buildJsonHints(
  kind: FactoryKind,
  channels: ReadonlyMap<string, ChannelKind>,
  targetId: string,
  connections: readonly { moduleName: string; kind: string }[],
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
      {
        key: "failover",
        detail: "object",
        info: kind.failover
          ? "How the minion moves between its connections on its own when the one in use keeps failing."
          : `How the minion moves between its connections on its own. ${kind.name} does not say its minions apply it: it may ignore these.`,
      },
      ...(kind.maxTasks
        ? [
            {
              key: "max_tasks",
              detail: `number, 0 to ${MAX_TASKS_LIMIT}`,
              info: `How many tasks the minion runs at once as it starts; 0: none until a session sets more. Left out: ${DEFAULT_MAX_TASKS}. Its sessions change it live.`,
            },
          ]
        : []),
      ...(kind.compression
        ? [
            {
              key: "compression",
              detail: "boolean",
              info: "Whether the build packs its messages: each item that shrinks deflated, both ways. Left out: true. Off shows no compression in the traffic's sizes.",
            },
          ]
        : []),
      ...(kind.persistedSessions
        ? [
            {
              key: "persisted_session",
              detail: "false, true or a session id",
              info: "A minion that comes back to its session when it restarts. true: a new session, reserved with the build; a session id: that session, which the minion takes over. Chosen now, fixed once built.",
            },
          ]
        : []),
    ],
    "/values": fieldHints(targetFields(kind, target)),
    "/connections/*": [
      {
        key: "channel",
        detail: "text, required",
        info: "The channel the minion connects through.",
        values: target?.channels.map((c) => c.moduleName),
      },
      {
        key: "kind",
        detail: "text",
        info: "The connection kind; left out: the first the target and the channel both support.",
        values: [...new Set(target?.channels.flatMap((c) => c.kinds) ?? [])],
      },
      { key: "name", detail: "text", info: 'What the minion calls the connection; "" for <channel>-<position>.' },
      { key: "description", detail: "text", info: "A note for the team. Core keeps it; the factory never gets it." },
      { key: "settings", detail: "object", info: "The channel's settings for the connection kind." },
      { key: "values", detail: "object", info: `${kind.name}'s parameters for the channel and connection kind.` },
      {
        key: "failover",
        detail: "object",
        info: "This connection's own failover settings: auto (may failover move here; default yes, no for bind-persistent), fail_after (default the build's).",
      },
    ],
    "/failover": [
      { key: "enabled", detail: "true or false", info: `The minion moves on its own; false: only an operator's switch. Default ${DEFAULT_FAILOVER.enabled}.` },
      { key: "fail_after", detail: "number, 1 to 1000", info: `Failed exchanges in a row before moving off a connection. Default ${DEFAULT_FAILOVER.fail_after}.` },
      { key: "fail_for", detail: "duration", info: `How long it must also have been failing; "0s": the count alone. Default "${DEFAULT_FAILOVER.fail_for}".` },
      { key: "wrap", detail: "true or false", info: `After the last connection, the first again; false: stay on the last. Default ${DEFAULT_FAILOVER.wrap}.` },
      { key: "cycle_pause", detail: "duration", info: `The wait after a round where every connection failed. Default "${DEFAULT_FAILOVER.cycle_pause}".` },
      { key: "failback_after", detail: "duration", info: `How long away from the first connection before trying it again; "0s": never. Default "${DEFAULT_FAILOVER.failback_after}".` },
    ],
    "/connections/*/failover": [
      { key: "auto", detail: "true or false", info: "Failover may move the minion here. Default true; false for bind-persistent (core has to dial it)." },
      { key: "fail_after", detail: "number", info: "Failures in a row before the minion moves off this one; default the build's." },
    ],
  }
  connections.forEach((c, i) => {
    const fields = connectionFields(kind, channels, target, c)
    hints[`/connections/${i}/settings`] = fieldHints(fields.settings)
    hints[`/connections/${i}/values`] = fieldHints(fields.values)
  })
  return hints
}
