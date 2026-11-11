import { DEFAULT_FAILOVER } from "./failover-values"
import { describe, expect, it } from "vitest"
import type { FieldDef } from "@/components/modules/management/field-values"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import type { BuildSchemaFieldsFragment } from "@/graphql/gql/graphql"
import { formatJson, hintsAt } from "@/lib/json-edit"
import { buildRecordJson, type BuildRecord } from "./build-record"
import { evaluateForm, factoryKindsByName, initialForm } from "./build-values"
import {
  buildJson,
  buildJsonHints,
  buildProblemPointer,
  documentInput,
  formDocument,
  formFromDocument,
  readBuildJson,
  type BuildDocument,
} from "./build-json"

function field(f: Partial<FieldDef> & Pick<FieldDef, "key" | "type">): FieldDef {
  return { label: f.key, order: 0, required: false, ...f } as FieldDef
}

// BOB's build schema (as build-values.test.ts has it), and the HTTP channel.
const schema: BuildSchemaFieldsFragment = {
  version: 1,
  crypto: null,
  failover: false,
  maxTasks: false,
  compression: [],
  persistedSessions: false,
  fields: [
    field({ key: "greeting", type: "string", default: "hello from BOB", order: 1 }),
    field({ key: "punches", type: "enum_list", order: 2, options: [{ value: "jab" }, { value: "hook" }] }),
    field({ key: "rounds", type: "int", order: 3, min: 1 }),
  ],
  channels: [
    {
      moduleName: "http",
      versions: [2],
      kinds: [
        {
          kind: "reverse-call",
          fields: [field({ key: "method", type: "enum", default: "POST", options: [{ value: "GET" }, { value: "POST" }] })],
        },
      ],
    },
  ],
  targets: [
    { id: "dummy", label: "Dummy", description: null, format: "json", platforms: ["any/any"], fields: [], channels: [{ moduleName: "http", versions: [2], kinds: ["reverse-call"] }] },
    {
      id: "service",
      label: "Service",
      description: null,
      format: "exe",
      platforms: ["windows/amd64", "windows/arm64"],
      fields: [field({ key: "service_name", type: "string", required: true })],
      channels: [{ moduleName: "http", versions: [2], kinds: ["reverse-call"] }],
    },
  ],
  commands: [
    { id: "sleep", op: 1, label: "Sleep", description: null, targets: ["dummy", "service"], default: false, required: true, kinds: [], params: [] },
    { id: "hello", op: 2, label: "Say hello", description: "Answers.", targets: ["dummy", "service"], default: true, required: false, kinds: [], params: [] },
    { id: "shell", op: 3, label: "Shell", description: null, targets: ["dummy"], default: false, required: false, kinds: [], params: [] },
  ],
}
const kind = factoryKindsByName([{ moduleName: "bob", instances: 1, schema }]).get("bob")!
const channels = kindsByName([
  {
    moduleName: "http",
    schema: {
      version: 2,
      kinds: [
        {
          kind: "reverse-call",
          fields: [
            field({ key: "base_url", type: "url", required: true, order: 1 }),
            field({ key: "request_timeout", type: "duration", default: "40s", order: 2 }),
          ],
        },
      ],
    },
  },
  { moduleName: "dns", schema: { version: 1, kinds: [{ kind: "reverse-call", fields: [field({ key: "zone", type: "string" })] }] } },
])

const good: BuildDocument = {
  description: "EU dry run",
  target: "dummy",
  platform: "any/any",
  commands: ["sleep", "hello"],
  values: { punches: ["jab"] },
  connections: [
    { moduleName: "http", kind: "", name: "", description: "", settings: { base_url: "https://edge.example.com" }, values: {} },
  ],
  failover: { ...DEFAULT_FAILOVER },
}
const read = (doc: unknown) => readBuildJson(typeof doc === "string" ? doc : JSON.stringify(doc), kind, channels)
const problems = (doc: unknown) => {
  const r = read(doc)
  if (!r.ok) throw new Error(r.message)
  return Object.fromEntries(r.problems.map((p) => [p.path, p.message]))
}
const json = (over: Partial<BuildDocument> = {}) => JSON.parse(buildJson(kind, channels, { ...good, ...over }))

describe("buildJson", () => {
  it("is what createBuild takes, every value listed with the one the build will use", () => {
    expect(json()).toEqual({
      factory: { module_name: "bob", schema_version: 1 },
      description: "EU dry run",
      target: "dummy",
      platform: "any/any",
      commands: ["sleep", "hello"],
      values: { greeting: "hello from BOB", punches: ["jab"], rounds: null },
      connections: [
        {
          channel: "http",
          name: "",
          description: "",
          settings: { base_url: "https://edge.example.com", request_timeout: "40s" },
          values: { method: "POST" },
        },
      ],
      failover: DEFAULT_FAILOVER,
    })
  })

  it("starts a new build at the defaults, from the form", () => {
    const doc = JSON.parse(buildJson(kind, channels, formDocument(kind, channels, initialForm(kind, null, channels))))
    expect(doc.values).toEqual({ greeting: "hello from BOB", punches: [], rounds: null })
    expect(doc.commands).toEqual(["sleep", "hello"])
    expect(doc.connections).toEqual([])
  })
})

describe("readBuildJson", () => {
  it("reads what it wrote back, the defaults left to the server", () => {
    expect(read(buildJson(kind, channels, good))).toEqual({ ok: true, doc: good, problems: [] })
  })

  it("ignores a pasted build's crypto: a new build gets keys of its own", () => {
    const doc = { ...json(), crypto: { suite: "aes-256-cbc-hmac-sha256", material: {} } }
    expect(read(doc)).toEqual({ ok: true, doc: good, problems: [] })
  })

  it("is not a build when it is not a JSON object", () => {
    expect(read('{"target": ').ok).toBe(false)
    expect(read("[]")).toEqual({ ok: false, message: "A build is a JSON object." })
  })

  it("takes empty values for not set, as core does for a build", () => {
    const r = read({ ...json(), values: { greeting: "", punches: [], rounds: null } })
    expect(r.ok && r.doc.values).toEqual({})
  })

  it("marks what the form would, at its place", () => {
    expect(
      problems({
        ...json(),
        description: "",
        platform: "linux/amd64",
        values: { greeting: "hi", punches: ["kick"], rounds: 0 },
        connections: [
          { channel: "http", name: "Edge!", settings: { base_url: "" }, values: { method: "PUT" } },
          { channel: "dns", name: "" },
        ],
      }),
    ).toEqual({
      "/description": "Required",
      "/platform": "Not a platform of dummy: any/any",
      "/values/punches": '"kick" is not one of the allowed options',
      "/values/rounds": "Must be at least 1",
      "/connections/0/name": expect.any(String),
      "/connections/0/settings/base_url": "Required",
      "/connections/0/values/method": "Not one of the allowed options",
      "/connections/1/channel": "This target does not speak dns",
    })
  })

  it("wants a target of the factory, a connection, and names apart", () => {
    expect(problems({ ...json(), target: "drone" })["/target"]).toBe("Not a target of bob: dummy, service")
    expect(problems({ ...json(), connections: [] })["/connections"]).toBe("Add at least one connection")
    const twice = json().connections[0]
    expect(problems({ ...json(), connections: [twice, { ...twice, name: "http-1" }] })["/connections/1/name"]).toBe(
      "Connection 1 is called http-1 too",
    )
  })

  it("refuses keys a build, a connection or a field list does not have", () => {
    const c = json().connections[0]
    expect(
      Object.keys(
        problems({
          ...json(),
          id: "x",
          values: { colour: "red" },
          connections: [{ ...c, retries: 3, settings: { ...c.settings, zone: "z" }, values: { verb: "GET" } }],
        }),
      ).sort(),
    ).toEqual(["/connections/0/retries", "/connections/0/settings/zone", "/connections/0/values/verb", "/id", "/values/colour"])
  })

  it("keeps the factory as it is", () => {
    expect(problems({ ...json(), factory: { module_name: "bob", schema_version: 2 } })).toEqual({
      "/factory": "Must stay this build's factory: bob, schema v1",
    })
  })

  it("checks a target's own values", () => {
    expect(problems({ ...json(), target: "service", platform: "windows/amd64" })).toEqual({
      "/values/service_name": "Required",
    })
  })

  it("takes the commands the target has, each once, and wants its required ones", () => {
    const r = read({ ...json(), commands: ["sleep", 5, "sleep", "fly", "hello"] })
    expect(r.ok && r.doc.commands).toEqual(["sleep", "hello"])
    expect(r.ok && Object.fromEntries(r.problems.map((p) => [p.path, p.message]))).toEqual({
      "/commands/1": "Must be a command id",
      "/commands/2": "Listed twice",
      "/commands/3": "Not a command of bob",
    })
    const service = { ...json(), target: "service", platform: "windows/amd64", values: { service_name: "svc" } }
    expect(problems({ ...service, commands: ["sleep", "shell"] })).toEqual({ "/commands/1": "Not a command of the service target" })
    expect(problems({ ...json(), commands: ["hello"] })).toEqual({ "/commands": "The dummy target requires sleep" })
    expect(problems({ ...json(), commands: "sleep" })).toEqual({ "/commands": "Must be a list of command ids" })
  })

  it("takes the target's default commands when none are listed", () => {
    const without = json()
    delete without.commands
    for (const doc of [without, { ...json(), commands: null }]) {
      const r = read(doc)
      expect(r.ok && r.doc.commands).toEqual(["sleep", "hello"])
      expect(r.ok && r.problems).toEqual([])
    }
  })
})

describe("persisted_session", () => {
  const persisting = factoryKindsByName([{ moduleName: "bob", instances: 1, schema: { ...schema, persistedSessions: true } }]).get("bob")!
  const readP = (doc: unknown) => readBuildJson(JSON.stringify(doc), persisting, channels)
  const sid = "8fba1ac4-b8e7-46ca-b96e-bdddc9e52906"

  it("writes the choice: false, true or the session's id", () => {
    const write = (persisted: BuildDocument["persisted"]) =>
      JSON.parse(buildJson(persisting, channels, { ...good, persisted })).persisted_session
    expect(write({ kind: "off" })).toBe(false)
    expect(write({ kind: "new" })).toBe(true)
    expect(write({ kind: "same", id: sid })).toBe(sid)
    expect(write(undefined)).toBeUndefined()
  })

  it("reads it back, and refuses anything else", () => {
    const at = (value: unknown) => {
      const r = readP({ ...json(), persisted_session: value })
      if (!r.ok) throw new Error(r.message)
      return { persisted: r.doc.persisted, problems: r.problems.map((p) => p.path) }
    }
    expect(at(true).persisted).toEqual({ kind: "new" })
    expect(at(false).persisted).toEqual({ kind: "off" })
    expect(at(sid.toUpperCase()).persisted).toEqual({ kind: "same", id: sid })
    expect(at("nope").problems).toEqual(["/persisted_session"])
  })

  it("is not for a factory whose minions claim none", () => {
    expect(problems({ ...json(), persisted_session: true })["/persisted_session"]).toMatch(/do not claim/)
  })
})

describe("max_tasks", () => {
  const limited = factoryKindsByName([{ moduleName: "bob", instances: 1, schema: { ...schema, maxTasks: true } }]).get("bob")!
  const readM = (doc: unknown) => readBuildJson(JSON.stringify(doc), limited, channels)

  it("goes in and out of the form, and to createBuild", () => {
    const form = formFromDocument(limited, channels, { ...good, maxTasks: 8 })
    expect(form.maxTasks).toBe("8")
    expect(formDocument(limited, channels, { ...form, maxTasks: "3" }).maxTasks).toBe(3)
    expect(formDocument(limited, channels, { ...form, maxTasks: "lots" }).maxTasks).toBe("lots")
    expect(formFromDocument(limited, channels, good).maxTasks).toBe("5")
    expect(JSON.parse(buildJson(limited, channels, { ...good, maxTasks: 3 })).max_tasks).toBe(3)
    expect(documentInput({ ...good, maxTasks: 3 }).maxTasks).toBe(3)
    expect(documentInput(good).maxTasks).toBeNull()
  })

  it("reads it back, 0 to 64, and refuses anything else", () => {
    const at = (value: unknown) => {
      const r = readM({ ...json(), max_tasks: value })
      if (!r.ok) throw new Error(r.message)
      return { maxTasks: r.doc.maxTasks, problems: r.problems.map((p) => p.path) }
    }
    expect(at(0)).toEqual({ maxTasks: 0, problems: [] })
    expect(at(64).maxTasks).toBe(64)
    expect(at(65).problems).toEqual(["/max_tasks"])
    expect(at(2.5).problems).toEqual(["/max_tasks"])
    expect(at("5").problems).toEqual(["/max_tasks"])
  })

  it("is not for a factory whose minions take no limit", () => {
    expect(problems({ ...json(), max_tasks: 5 })["/max_tasks"]).toMatch(/take no limit/)
    expect(formFromDocument(kind, channels, good).maxTasks).toBe("")
  })
})

describe("compression", () => {
  const packing = factoryKindsByName([{ moduleName: "bob", instances: 1, schema: { ...schema, compression: ["deflate"] } }]).get("bob")!
  const readC = (doc: unknown) => readBuildJson(JSON.stringify(doc), packing, channels)

  it("is on unless turned off, in and out of the form, and to createBuild", () => {
    expect(formFromDocument(packing, channels, good).compression).toBe(true)
    const off = formFromDocument(packing, channels, { ...good, compression: false })
    expect(off.compression).toBe(false)
    expect(formDocument(packing, channels, off).compression).toBe(false)
    expect(JSON.parse(buildJson(packing, channels, { ...good, compression: false })).compression).toBe(false)
    expect(documentInput({ ...good, compression: false }).compression).toBe(false)
    expect(documentInput(good).compression).toBeNull()
  })

  it("reads back true or false, and refuses anything else", () => {
    const at = (value: unknown) => {
      const r = readC({ ...json(), compression: value })
      if (!r.ok) throw new Error(r.message)
      return { compression: r.doc.compression, problems: r.problems.map((p) => p.path) }
    }
    expect(at(false)).toEqual({ compression: false, problems: [] })
    expect(at(true)).toEqual({ compression: true, problems: [] })
    expect(at("deflate").problems).toEqual(["/compression"])
  })

  it("is not for a factory whose minions inflate nothing", () => {
    expect(problems({ ...json(), compression: true })["/compression"]).toMatch(/inflate nothing/)
    expect(formDocument(kind, channels, formFromDocument(kind, channels, good)).compression).toBeUndefined()
  })
})

describe("a finished build's JSON", () => {
  it("reads back as a new build's, the defaults it holds left to the server", () => {
    const record = {
      description: "EU dry run",
      moduleName: "bob",
      schemaVersion: 1,
      target: "dummy",
      platform: "any/any",
      values: { greeting: "hello from BOB", punches: ["jab"] },
      commands: ["sleep", "hello"],
      connections: [
        {
          endpoint: {
            name: "edge",
            channel: { module_name: "http", schema_version: 2 },
            params: { base_url: "https://edge.example.com", request_timeout: "40s" },
          },
          values: { method: "POST" },
        },
      ],
      connectionDescriptions: [""],
    } as unknown as BuildRecord
    expect(read(formatJson(buildRecordJson(record)))).toEqual({
      ok: true,
      doc: { ...good, connections: [{ ...good.connections[0], name: "edge" }] },
      problems: [],
    })
  })
})

describe("form and document", () => {
  it("carry each other over", () => {
    const form = formFromDocument(kind, channels, good)
    expect(form.drafts.greeting).toEqual({ kind: "text", text: "hello from BOB" }) // its default, filled in
    expect(form.commands).toEqual(["sleep", "hello"])
    // The form holds the required ones whatever the JSON says, in the factory's order.
    expect(formFromDocument(kind, channels, { ...good, commands: ["shell", "hello"] }).commands).toEqual(["sleep", "hello", "shell"])
    expect(formDocument(kind, channels, form)).toEqual({
      ...good,
      values: { greeting: "hello from BOB", punches: ["jab"] },
      connections: [
        {
          ...good.connections[0],
          settings: { base_url: "https://edge.example.com", request_timeout: "40s" },
          values: { method: "POST" },
        },
      ],
    })
    // What the form would send is what the JSON says.
    expect(evaluateForm(kind, form, channels).input.values).toEqual(good.values)
  })
})

describe("documentInput", () => {
  it("is what createBuild takes: texts trimmed, an unnamed connection named by the server", () => {
    const input = documentInput({
      ...good,
      description: "  EU dry run ",
      connections: [{ ...good.connections[0], name: " ", description: " note " }],
    })
    expect(input).toEqual({
      description: "EU dry run",
      target: "dummy",
      platform: "any/any",
      values: { punches: ["jab"] },
      commands: ["sleep", "hello"],
      connections: [
        {
          moduleName: "http",
          kind: null,
          name: null,
          description: "note",
          settings: { base_url: "https://edge.example.com" },
          values: {},
          failover: null,
        },
      ],
      failover: DEFAULT_FAILOVER,
      maxTasks: null,
      compression: null,
    })
  })
})

describe("buildProblemPointer", () => {
  it("places the form's and core's problem keys in the document", () => {
    expect(buildProblemPointer("description")).toBe("/description")
    expect(buildProblemPointer("values.greeting")).toBe("/values/greeting")
    expect(buildProblemPointer("commands")).toBe("/commands")
    expect(buildProblemPointer("commands[3]")).toBe("/commands/3")
    expect(buildProblemPointer("connections")).toBe("/connections")
    expect(buildProblemPointer("connections[2]")).toBe("/connections/2")
    expect(buildProblemPointer("connections[0].name")).toBe("/connections/0/name")
    expect(buildProblemPointer("connections[1].settings.base_url")).toBe("/connections/1/settings/base_url")
    expect(buildProblemPointer("connections[1].values.method")).toBe("/connections/1/values/method")
    expect(buildProblemPointer("failover.fail_for")).toBe("/failover/fail_for")
    expect(buildProblemPointer("connections[1].failover.fail_after")).toBe("/connections/1/failover/fail_after")
    expect(buildProblemPointer("")).toBe("")
  })
})

describe("buildJsonHints", () => {
  const hints = buildJsonHints(kind, channels, "dummy", [{ moduleName: "http", kind: "" }])

  it("offers the build's keys, its targets and the target's platforms", () => {
    expect(hints[""].map((h) => h.key)).toEqual(["description", "target", "platform", "commands", "values", "connections", "failover"])
    expect(hints[""].find((h) => h.key === "commands")!.items).toEqual([
      { value: "sleep", detail: "Sleep, required", info: undefined },
      { value: "hello", detail: "Say hello", info: "Answers." },
      { value: "shell", detail: "Shell", info: undefined },
    ])
    expect(hints[""].find((h) => h.key === "target")!.values).toEqual(["dummy", "service"])
    expect(hints[""].find((h) => h.key === "platform")!.values).toEqual(["any/any"])
    expect(hints["/values"].map((h) => h.key)).toEqual(["greeting", "punches", "rounds"])
  })

  it("offers any connection's keys, and each one's settings by its channel", () => {
    expect(hintsAt(hints, "/connections/5")!.map((h) => h.key)).toEqual([
      "channel",
      "kind",
      "name",
      "description",
      "settings",
      "values",
      "failover",
    ])
    expect(hintsAt(hints, "/connections/0")!.find((h) => h.key === "channel")!.values).toEqual(["http"])
    expect(hints["/connections/0/settings"].map((h) => h.key)).toEqual(["base_url", "request_timeout"])
    expect(hints["/connections/0/values"].map((h) => h.key)).toEqual(["method"])
  })
})
