import { describe, expect, it } from "vitest"
import type { FieldDef } from "@/components/modules/management/field-values"
import type { BuildSchemaFieldsFragment } from "@/graphql/gql/graphql"
import {
  acceptText,
  endpointUnusable,
  evaluateForm,
  factoryKindsByName,
  fileFits,
  initialForm,
  move,
  platformLabel,
  withTarget,
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
const endpointKinds = new Map([
  ["ep-edge", "http"],
  ["ep-dns", "dns"],
])

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

describe("endpointUnusable", () => {
  const dummy = kind.targets[0]
  const versions = new Map([["http", 2], ["dns", 1]])
  it("takes only fitting endpoints the target speaks", () => {
    expect(endpointUnusable(dummy, { id: "ep-edge", name: "edge", moduleName: "http", problems: [] }, versions)).toBeNull()
    expect(endpointUnusable(dummy, { id: "ep-dns", name: "dns", moduleName: "dns", problems: [] }, versions)).toMatch(/does not speak dns/)
    expect(
      endpointUnusable(dummy, { id: "x", name: "x", moduleName: "http", problems: [{ key: "base_url", message: "required" }] }, versions),
    ).toMatch(/does not fit/i)
    expect(endpointUnusable(dummy, { id: "ep-edge", name: "edge", moduleName: "http", problems: [] }, new Map([["http", 3]]))).toMatch(/v3/)
  })
})

describe("the build form", () => {
  it("starts from the first target and its only platform", () => {
    const form = initialForm(kind, null, endpointKinds)
    expect(form.target).toBe("dummy")
    expect(form.platform).toBe("any/any")
    expect(Object.keys(form.drafts)).toEqual(["greeting", "kill_date", "punches", "badge", "token"])
  })

  it("submits set values, defaults left to the server", () => {
    const form = initialForm(kind, null, endpointKinds)
    form.drafts.punches = { kind: "set", values: ["hook", "jab"] }
    form.drafts.badge = { kind: "file", id: "file-1" }
    form.connections = [{ endpointId: "ep-edge", drafts: { path_prefix: { kind: "text", text: "/cdn/" } } }]
    const { input, errors } = evaluateForm(kind, form, false, endpointKinds)
    expect(errors).toEqual({})
    expect(input).toEqual({
      target: "dummy",
      platform: "any/any",
      values: { punches: ["jab", "hook"], badge: "file-1" },
      connections: [{ endpointId: "ep-edge", values: { path_prefix: "/cdn/" } }],
    })
  })

  it("keys errors like the server's problems", () => {
    let form = initialForm(kind, null, endpointKinds)
    form = withTarget(kind, form, "service")
    form.connections = [{ endpointId: "ep-edge", drafts: { path_prefix: { kind: "text", text: "cdn" } } }]
    form.drafts.kill_date = { kind: "text", text: "not a date" }
    const { errors } = evaluateForm(kind, form, false, endpointKinds)
    expect(errors).toEqual({
      platform: "Pick a platform",
      "values.service_name": "Required",
      "values.kill_date": "Not a date and time",
      "connections[0].values.path_prefix": "Must match ^/",
    })
  })

  it("lets a preset leave the platform, endpoints and required values", () => {
    const form = withTarget(kind, initialForm(kind, null, endpointKinds), "service")
    const build = evaluateForm(kind, form, false, endpointKinds).errors
    expect(build.connections).toBeDefined()
    expect(build["values.service_name"]).toBeDefined()
    expect(evaluateForm(kind, form, true, endpointKinds).errors).toEqual({})
  })

  it("keeps values that carry over to another target", () => {
    const form = initialForm(kind, null, endpointKinds)
    form.drafts.greeting = { kind: "text", text: "hi" }
    const service = withTarget(kind, form, "service")
    expect(service.drafts.greeting).toEqual({ kind: "text", text: "hi" })
    expect(service.drafts.service_name).toEqual({ kind: "text", text: "" })
    expect(service.platform).toBe("") // two platforms: the user picks
  })

  it("fills in from a preset or a build", () => {
    const form = initialForm(
      kind,
      {
        target: "service",
        platform: "windows/arm64",
        values: { service_name: "WinTelemetry", kill_date: "2027-01-31T00:00:00Z", punches: ["jab"] },
        connections: [{ endpointId: "ep-edge", values: { method: "GET" } }],
      },
      endpointKinds,
    )
    expect(form.platform).toBe("windows/arm64")
    expect(form.drafts.punches).toEqual({ kind: "set", values: ["jab"] })
    expect(form.connections[0].drafts.method).toEqual({ kind: "text", text: "GET" })
    const { input, errors } = evaluateForm(kind, form, false, endpointKinds)
    expect(errors).toEqual({})
    expect(input.values.kill_date).toBe("2027-01-31T00:00:00Z")
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
