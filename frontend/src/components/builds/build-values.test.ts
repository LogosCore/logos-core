import { describe, expect, it } from "vitest"
import type { FieldDef } from "@/components/modules/management/field-values"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import type { BuildSchemaFieldsFragment } from "@/graphql/gql/graphql"
import {
  acceptText,
  buildConnectionRows,
  channelUnusable,
  connectionDraft,
  connectionNames,
  currentStage,
  endpointFromConnection,
  endpointSearchText,
  evaluateForm,
  factoryKindsByName,
  fileFits,
  fillFromEndpoint,
  initialForm,
  matchesQuery,
  move,
  platformLabel,
  settingsSummary,
  sourceFromBuild,
  stageDuration,
  withTarget,
  type BuildForm,
} from "./build-values"

function field(f: Partial<FieldDef> & Pick<FieldDef, "key" | "type">): FieldDef {
  return { label: f.key, order: 0, required: false, ...f } as FieldDef
}

// BOB's build schema, plus a second target with a required field.
const schema: BuildSchemaFieldsFragment = {
  version: 1,
  crypto: [],
  fields: [
    field({ key: "greeting", type: "string", default: "hello from BOB", order: 1 }),
    field({ key: "kill_date", type: "datetime", order: 2 }),
    field({ key: "punches", type: "enum_list", order: 3, options: [{ value: "jab" }, { value: "hook" }] }),
    field({ key: "badge", type: "file", order: 4, accept: ["image/*"], maxSize: 1024 }),
    field({ key: "token", type: "secret", order: 5 }),
  ],
  channels: [
    {
      moduleName: "http",
      versions: [2],
      fields: [
        field({ key: "method", type: "enum", default: "POST", options: [{ value: "GET" }, { value: "POST" }] }),
        field({ key: "path_prefix", type: "string", default: "/", pattern: "^/" }),
      ],
    },
  ],
  targets: [
    { id: "dummy", label: "Dummy minion", description: null, format: "json", platforms: ["any/any"], fields: [], channels: [{ moduleName: "http", versions: [2] }] },
    {
      id: "service",
      label: "Service",
      description: null,
      format: "exe",
      platforms: ["windows/amd64", "windows/arm64"],
      fields: [field({ key: "service_name", type: "string", required: true })],
      channels: [{ moduleName: "http", versions: [2] }],
    },
  ],
}

const kind = factoryKindsByName([{ moduleName: "bob", instances: 1, schema }]).get("bob")!

// The HTTP channel's connection settings; BOB's minions do not speak dns.
const channels = kindsByName([
  {
    moduleName: "http",
    schema: {
      version: 2,
      fields: [
        field({ key: "base_url", type: "url", required: true, order: 1 }),
        field({ key: "request_timeout", type: "duration", default: "40s", order: 2 }),
        field({ key: "token", type: "secret", order: 3 }),
      ],
    },
  },
  { moduleName: "dns", schema: { version: 1, fields: [field({ key: "zone", type: "string" })] } },
])
const http = channels.get("http")!

/** A blank connection through channel moduleName, with settings and parameters typed in. */
function connection(
  moduleName: string,
  typed: { name?: string; description?: string; settings?: Record<string, string>; params?: Record<string, string> } = {},
) {
  const c = connectionDraft(kind, channels.get(moduleName), { moduleName, name: typed.name, description: typed.description })
  for (const [k, text] of Object.entries(typed.settings ?? {})) c.settings[k] = { kind: "text", text }
  for (const [k, text] of Object.entries(typed.params ?? {})) c.drafts[k] = { kind: "text", text }
  return c
}

const edge = { settings: { base_url: "https://edge.example.com" } }

describe("fileFits", () => {
  const f = field({ key: "icon", type: "file", accept: [".ico", "image/*"], maxSize: 100 })
  it("matches extensions and media types like the server", () => {
    expect(fileFits(f, { name: "APP.ICO", size: 10 })).toBeNull()
    expect(fileFits(f, { name: "logo", size: 10, mediaType: "image/png; charset=binary" })).toBeNull()
    expect(fileFits(f, { name: "app.exe", size: 10, mediaType: "application/octet-stream" })).toMatch(/must be/i)
    expect(fileFits(f, { name: "app.ico", size: 101 })).toMatch(/larger/i)
    expect(fileFits(field({ key: "any", type: "file" }), { name: "x.bin", size: 1e9 })).toBeNull()
  })
  it("says what a field takes", () => {
    expect(acceptText(f)).toBe(".ico, image/* · up to 100 B")
    expect(acceptText(field({ key: "any", type: "file" }))).toBe("Any file")
  })
})

describe("factoryKindsByName", () => {
  it("sorts fields and edits secrets as text", () => {
    expect(kind.fields.map((f) => f.key)).toEqual(["greeting", "kill_date", "punches", "badge", "token"])
    expect(kind.fields.find((f) => f.key === "token")?.type).toBe("string")
    expect(kind.channelFields.get("http")?.map((f) => f.key)).toEqual(["method", "path_prefix"])
  })
})

describe("channelUnusable", () => {
  const dummy = kind.targets[0]
  it("takes the channels the target speaks, at a version it reads", () => {
    expect(channelUnusable(dummy, "http", channels)).toBeNull()
    expect(channelUnusable(dummy, "dns", channels)).toMatch(/does not speak dns/)
    expect(channelUnusable(dummy, "smtp", channels)).toMatch(/no smtp channel/i)
    expect(channelUnusable(undefined, "http", channels)).toMatch(/pick a target/i)
    const v3 = new Map([["http", { ...http, version: 3 }]])
    expect(channelUnusable(dummy, "http", v3)).toMatch(/settings v3; this factory reads v2/)
  })
})

describe("the build form", () => {
  it("starts from the first target and its only platform", () => {
    const form = initialForm(kind, null, channels)
    expect(form.target).toBe("dummy")
    expect(form.platform).toBe("any/any")
    expect(Object.keys(form.drafts)).toEqual(["greeting", "kill_date", "punches", "badge", "token"])
    expect(form.connections).toEqual([])
    expect(form.description).toBe("")
  })

  it("submits set values and settings, defaults left to the server", () => {
    const form = initialForm(kind, null, channels)
    form.description = " EU front dry run "
    form.drafts.punches = { kind: "set", values: ["hook", "jab"] }
    form.drafts.badge = { kind: "file", id: "file-1" }
    form.connections = [
      connection("http", {
        name: " edge ",
        description: " EU front ",
        settings: { base_url: "https://edge.example.com", token: "s3cret" },
        params: { path_prefix: "/cdn/" },
      }),
      connection("http", { ...edge, description: "  " }),
    ]
    const { input, errors } = evaluateForm(kind, form, channels)
    expect(errors).toEqual({})
    expect(input).toEqual({
      description: "EU front dry run",
      target: "dummy",
      platform: "any/any",
      values: { punches: ["jab", "hook"], badge: "file-1" },
      connections: [
        {
          moduleName: "http",
          name: "edge",
          description: "EU front",
          settings: { base_url: "https://edge.example.com", token: "s3cret" },
          values: { path_prefix: "/cdn/" },
        },
        { moduleName: "http", name: null, description: null, settings: { base_url: "https://edge.example.com" }, values: {} },
      ],
    })
  })

  it("keys errors like the server's problems", () => {
    let form = initialForm(kind, null, channels)
    form = withTarget(kind, form, "service")
    form.connections = [
      connection("http", { name: "Edge Node", settings: { base_url: "edge", request_timeout: "soon" }, params: { path_prefix: "cdn" } }),
      connection("dns"),
    ]
    form.drafts.kill_date = { kind: "text", text: "not a date" }
    const { errors } = evaluateForm(kind, form, channels)
    expect(errors).toMatchObject({
      description: "Required",
      platform: "Pick a platform",
      "values.service_name": "Required",
      "values.kill_date": "Not a date and time",
      "connections[0].name": expect.stringMatching(/lowercase letters/i),
      "connections[0].settings.base_url": "Must be a valid URL",
      "connections[0].settings.request_timeout": expect.stringMatching(/invalid duration/i),
      "connections[0].values.path_prefix": "Must match ^/",
      "connections[1]": "This target does not speak dns",
    })
    expect(Object.keys(errors)).toHaveLength(9)
  })

  it("wants connection names unique, the server's defaults counted", () => {
    const form = initialForm(kind, null, channels)
    form.description = "dry run"
    form.connections = [connection("http", { ...edge, name: "edge" }), connection("http", { ...edge, name: "edge" })]
    expect(evaluateForm(kind, form, channels).errors).toEqual({ "connections[1].name": "Connection 1 is called edge too" })
    form.connections = [connection("http", { ...edge, name: "http-2" }), connection("http", edge)]
    expect(evaluateForm(kind, form, channels).errors).toEqual({ "connections[1].name": "Connection 1 is called http-2 too" })
  })

  it("needs a description, a connection, and every required value and setting", () => {
    const form = initialForm(kind, null, channels)
    form.description = "  "
    expect(evaluateForm(kind, form, channels).errors).toEqual({ description: "Required", connections: "Add at least one connection" })
    form.description = "dry run"
    form.connections = [connection("http")]
    expect(evaluateForm(kind, form, channels).errors).toEqual({ "connections[0].settings.base_url": "Required" })
  })

  it("keeps values that carry over to another target", () => {
    const form = initialForm(kind, null, channels)
    form.drafts.greeting = { kind: "text", text: "hi" }
    const service = withTarget(kind, form, "service")
    expect(service.drafts.greeting).toEqual({ kind: "text", text: "hi" })
    expect(service.drafts.service_name).toEqual({ kind: "text", text: "" })
    expect(service.platform).toBe("") // two platforms: the user picks
  })

  it("fills in from an earlier build's settings", () => {
    const form = initialForm(
      kind,
      {
        description: "WinTelemetry for the dry run",
        target: "service",
        platform: "windows/arm64",
        values: { service_name: "WinTelemetry", kill_date: "2027-01-31T00:00:00Z", punches: ["jab"] },
        connections: [
          {
            moduleName: "http",
            name: "edge",
            description: "EU front",
            schemaVersion: 1,
            settings: { base_url: "https://edge.example.com" },
            values: { method: "GET" },
          },
        ],
      },
      channels,
    )
    expect(form.platform).toBe("windows/arm64")
    expect(form.description).toBe("WinTelemetry for the dry run")
    expect(form.drafts.punches).toEqual({ kind: "set", values: ["jab"] })
    const c = form.connections[0]
    expect(c.name).toBe("edge")
    expect(c.settings.base_url).toEqual({ kind: "text", text: "https://edge.example.com" })
    expect(c.drafts.method).toEqual({ kind: "text", text: "GET" })
    expect(c.notes).toEqual(["Saved for http settings v1; the channel is at v2. Check the settings."])
    const { input, errors } = evaluateForm(kind, form, channels)
    expect(errors).toEqual({})
    expect(input.values.kill_date).toBe("2027-01-31T00:00:00Z")
    expect(c.described).toBe(true)
    expect(input.connections[0]).toEqual({
      moduleName: "http",
      name: "edge",
      description: "EU front",
      settings: { base_url: "https://edge.example.com" },
      values: { method: "GET" },
    })
  })
})

describe("fillFromEndpoint", () => {
  const eu = { name: "eu", values: { base_url: "https://eu.example.com", request_timeout: "10s", gone: true } }
  const us = { name: "us", values: { base_url: "https://us.example.com" } }

  it("copies the endpoint's settings and name, until the operator names the connection", () => {
    const c = fillFromEndpoint(connection("http", { settings: { token: "typed" } }), http, eu)
    expect(c.name).toBe("eu")
    expect(c.named).toBe(false)
    expect(c.filledFrom).toBe("eu")
    expect(c.settings).toEqual({
      base_url: { kind: "text", text: "https://eu.example.com" },
      request_timeout: { kind: "text", text: "10s" },
      token: { kind: "text", text: "" },
    })
    expect(fillFromEndpoint(c, http, us).name).toBe("us")
    expect(fillFromEndpoint({ ...c, name: "primary", named: true }, http, us).name).toBe("primary")
    // An earlier build's own name counts as the operator's.
    expect(fillFromEndpoint(connection("http", { name: "primary" }), http, us).name).toBe("primary")
  })

  it("gives the endpoint's name free of the other connections'", () => {
    const blank = connection("http")
    expect(fillFromEndpoint(blank, http, eu, ["eu", "eu-2", "http-1"]).name).toBe("eu-3")
    expect(fillFromEndpoint(blank, http, eu, ["us"]).name).toBe("eu")
    expect(connectionNames([connection("http", { name: " eu " }), connection("http"), connection("dns")])).toEqual([
      "eu",
      "http-2",
      "dns-3",
    ])
  })

  it("copies the endpoint's description, until the operator writes one", () => {
    const c = fillFromEndpoint(connection("http"), http, { ...eu, description: "EU front" })
    expect(c.description).toBe("EU front")
    expect(c.described).toBe(false)
    expect(fillFromEndpoint(c, http, us).description).toBe("")
    expect(fillFromEndpoint({ ...c, description: "mine", described: true }, http, us).description).toBe("mine")
    expect(fillFromEndpoint(connection("http", { description: "kept" }), http, eu).description).toBe("kept")
  })

  it("leaves the factory's parameters and drops old notes", () => {
    const c = { ...connection("http", { params: { method: "GET" } }), notes: ["Saved for v1"] }
    const filled = fillFromEndpoint(c, http, us)
    expect(filled.drafts.method).toEqual({ kind: "text", text: "GET" })
    expect(filled.notes).toEqual([])
    expect(filled.key).toBe(c.key)
  })
})

describe("endpointFromConnection", () => {
  it("takes the settings, not the factory's parameters, under a free name", () => {
    const c = connection("http", {
      name: "edge",
      description: " EU front ",
      settings: { base_url: "https://edge.example.com", request_timeout: "10s" },
      params: { path_prefix: "/cdn/" },
    })
    expect(endpointFromConnection(c, 0, http, ["edge", "other"])).toEqual({
      name: "edge-2",
      description: "EU front",
      values: { base_url: "https://edge.example.com", request_timeout: "10s" },
      problems: [],
    })
  })

  it("names an unnamed one as the build does, and says what an endpoint cannot take", () => {
    const d = endpointFromConnection(connection("http", { settings: { request_timeout: "soon" } }), 2, http, [])
    expect(d.name).toBe("http-3")
    expect(d.problems).toEqual([
      { label: "base_url", message: "Required" },
      { label: "request_timeout", message: expect.stringMatching(/invalid duration/i) },
    ])
  })
})

describe("finding endpoints", () => {
  const eu = {
    name: "eu-front",
    description: "CDN in Frankfurt",
    moduleName: "http",
    values: { base_url: "https://cdn.eu.example.com", request_timeout: "10s", token: "s3cret" },
  }

  it("searches names, descriptions, channels and settings, every word", () => {
    const text = endpointSearchText(eu, http)
    for (const q of ["", "  ", "EU", "frankfurt", "http", "cdn.eu.example", "10s", "eu cdn", "Front  Frankfurt"]) {
      expect(matchesQuery(text, q), q).toBe(true)
    }
    for (const q of ["us", "eu paris", "dns"]) expect(matchesQuery(text, q), q).toBe(false)
    // Without the channel's settings, its raw values still count.
    expect(matchesQuery(endpointSearchText(eu, undefined), "cdn.eu")).toBe(true)
  })

  it("sums up the first settings", () => {
    expect(settingsSummary(http, eu.values)).toBe("https://cdn.eu.example.com · 10s")
    expect(settingsSummary(http, {})).toBe("")
    expect(settingsSummary(http, null)).toBe("")
  })
})

describe("sourceFromBuild", () => {
  it("takes what the build set, not the defaults it holds", () => {
    const build = {
      description: "the EU front's dry run",
      target: "dummy",
      platform: "any/any",
      values: { greeting: "hello from BOB", punches: ["jab"] },
      connections: [
        {
          endpoint: {
            format: "logos.channel-endpoint.v1",
            name: "http-1",
            channel: { module_name: "http", schema_version: 2 },
            params: { base_url: "https://a.example.com", request_timeout: "40s", retired: "x" },
          },
          values: { method: "POST", path_prefix: "/cdn/" },
        },
        {
          endpoint: { name: "edge", channel: { module_name: "http", schema_version: 1 }, params: { base_url: "https://b.example.com" } },
          values: { method: "GET", path_prefix: "/" },
        },
      ],
    }
    const source = sourceFromBuild(kind, { ...build, connectionDescriptions: ["", "fallback"] }, channels)
    expect(source.values).toEqual({ punches: ["jab"] })
    expect(source.description).toBe("the EU front's dry run")
    expect(source.connections).toEqual([
      {
        moduleName: "http",
        name: null,
        description: null,
        schemaVersion: 2,
        settings: { base_url: "https://a.example.com" },
        values: { path_prefix: "/cdn/" },
        notes: ["Left out, not settings of http v2: retired."],
      },
      {
        moduleName: "http",
        name: "edge",
        description: "fallback",
        schemaVersion: 1,
        settings: { base_url: "https://b.example.com" },
        values: { method: "GET" },
        notes: [],
      },
    ])

    const form: BuildForm = initialForm(kind, source, channels)
    expect(form.connections[1].notes).toEqual(["Saved for http settings v1; the channel is at v2. Check the settings."])
    expect(evaluateForm(kind, form, channels).errors).toEqual({})
  })

  it("lists the connections with their notes", () => {
    const build = {
      connections: [
        { endpoint: { name: "http-1", channel: { module_name: "http" }, params: { request_timeout: "40s", base_url: "https://a.example.com" } } },
        { endpoint: { name: "edge", channel: { module_name: "http" }, params: { base_url: "https://b.example.com" } } },
      ],
      connectionDescriptions: ["EU front"],
    }
    expect(buildConnectionRows(build, channels)).toEqual([
      { moduleName: "http", name: "http-1", address: "https://a.example.com", description: "EU front" },
      { moduleName: "http", name: "edge", address: "https://b.example.com", description: "" },
    ])
    expect(buildConnectionRows({ connections: null }, channels)).toEqual([])
  })

  it("survives what it does not know", () => {
    const source = sourceFromBuild(kind, { target: "dummy", platform: "any/any", values: null, connections: [null, { endpoint: 1 }] }, channels)
    expect(source.values).toEqual({})
    expect(source.connections?.map((c) => c.moduleName)).toEqual(["", ""])
  })
})

describe("pipeline stages", () => {
  const stage = (state: string, startedAt?: string, finishedAt?: string) => ({
    id: state.toLowerCase(),
    label: state,
    core: false,
    state,
    startedAt: startedAt ?? null,
    finishedAt: finishedAt ?? null,
  })
  const now = Date.parse("2026-10-27T09:00:20Z")

  it("times a stage, a running one until now", () => {
    expect(stageDuration(stage("SUCCEEDED", "2026-10-27T09:00:10Z", "2026-10-27T09:00:12.5Z"), now)).toBe(2500)
    expect(stageDuration(stage("RUNNING", "2026-10-27T09:00:15Z"), now)).toBe(5000)
    expect(stageDuration(stage("SUCCEEDED"), now)).toBeNull() // done without a report
    expect(stageDuration(stage("FAILED", "2026-10-27T09:00:15Z"), now)).toBeNull() // no end known
  })

  it("finds where a build is: running, or where it stopped", () => {
    expect(currentStage([stage("SUCCEEDED"), stage("RUNNING"), stage("PENDING")])?.state).toBe("RUNNING")
    expect(currentStage([stage("SUCCEEDED"), stage("FAILED"), stage("SKIPPED")])?.state).toBe("FAILED")
    expect(currentStage([stage("SUCCEEDED"), stage("CANCELLED"), stage("SKIPPED")])?.state).toBe("CANCELLED")
    expect(currentStage([stage("SUCCEEDED"), stage("SUCCEEDED")])).toBeNull()
  })
})

describe("helpers", () => {
  it("moves list items", () => {
    expect(move(["a", "b", "c"], 0, 1)).toEqual(["b", "a", "c"])
    expect(move(["a", "b", "c"], 0, -1)).toEqual(["a", "b", "c"])
  })
  it("names platforms", () => {
    expect(platformLabel("any/any")).toBe("Any platform")
    expect(platformLabel("windows/amd64")).toBe("Windows amd64")
    expect(platformLabel("darwin/any")).toBe("macOS (any arch)")
  })
})
