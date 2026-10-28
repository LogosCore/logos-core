import { describe, expect, it } from "vitest"
import { endpointDrafts, ENDPOINT_EXPORT_FORMAT, type Kind } from "./endpoint-values"
import { endpointJson, endpointJsonHints, formValues, readEndpointJson } from "./endpoint-json"

const kind: Kind = {
  name: "http",
  icon: { icon: "", iconLight: "" },
  version: 2,
  fields: [
    { key: "base_url", type: "url", label: "Base URL", order: 1, required: true },
    { key: "tls_verify", type: "bool", label: "Verify TLS certificate", order: 2, required: false, default: false },
    { key: "retries", type: "int", label: "Retries", order: 3, required: false, min: 0, max: 5 },
    {
      key: "mode",
      type: "enum",
      label: "Mode",
      order: 4,
      required: false,
      options: [
        { value: "fast", label: "Fast" },
        { value: "safe", label: "Safe" },
      ],
    },
  ],
}

const doc = (over: Record<string, unknown> = {}) =>
  JSON.stringify({
    format: ENDPOINT_EXPORT_FORMAT,
    name: "edge",
    description: "",
    channel: { module_name: "http", schema_version: 2 },
    params: { base_url: "https://a.example.com", tls_verify: null, retries: null, mode: null },
    ...over,
  })

describe("endpointJson", () => {
  it("lists every setting with the value it uses, its default where none is set, in the export's shape", () => {
    const text = endpointJson(kind, { name: "edge", description: "CDN", values: { base_url: "https://a.example.com", retries: 3 } })
    expect(JSON.parse(text)).toEqual({
      format: ENDPOINT_EXPORT_FORMAT,
      name: "edge",
      description: "CDN",
      channel: { module_name: "http", schema_version: 2 },
      params: { base_url: "https://a.example.com", tls_verify: false, retries: 3, mode: null },
    })
    expect(text).toContain('\n  "params": {\n    "base_url"') // indented two spaces
  })
})

describe("readEndpointJson", () => {
  it("reads what it wrote back", () => {
    const values = { base_url: "https://a.example.com", tls_verify: true, mode: "safe" }
    const read = readEndpointJson(endpointJson(kind, { name: "edge", description: "CDN", values }), kind)
    expect(read).toEqual({ ok: true, doc: { name: "edge", description: "CDN", values }, problems: [] })
  })

  it("is not an endpoint when it is not a JSON object", () => {
    expect(readEndpointJson('{"name": ', kind).ok).toBe(false)
    expect(readEndpointJson("[1]", kind)).toEqual({ ok: false, message: "An endpoint is a JSON object." })
  })

  it("marks what the form would, by place", () => {
    const read = readEndpointJson(
      doc({ name: "Not A Slug", params: { base_url: "nope", tls_verify: "yes", retries: 9, mode: "slow" } }),
      kind,
    )
    if (!read.ok) throw new Error(read.message)
    expect(Object.fromEntries(read.problems.map((p) => [p.path, p.message]))).toEqual({
      "/name": expect.any(String),
      "/params/base_url": "Must be a valid URL",
      "/params/tls_verify": "Must be true or false",
      "/params/retries": "Must be at most 5",
      "/params/mode": "Not one of the allowed options",
    })
  })

  it("leaves out values at their default, or null, so they follow the channel's default", () => {
    const read = readEndpointJson(
      doc({ params: { base_url: "https://a.example.com", tls_verify: false, retries: null, mode: "fast" } }),
      kind,
    )
    expect(read.ok && read.doc.values).toEqual({ base_url: "https://a.example.com", mode: "fast" })
  })

  it("asks for a required setting left null, and for a name", () => {
    const read = readEndpointJson(doc({ name: null, params: { base_url: null } }), kind)
    if (!read.ok) throw new Error(read.message)
    expect(read.problems.map((p) => p.path).sort()).toEqual(["/name", "/params/base_url"])
  })

  it("refuses keys an endpoint or its channel does not have", () => {
    const read = readEndpointJson(doc({ id: "x", params: { base_url: "https://a.example.com", proxy: "p" } }), kind)
    if (!read.ok) throw new Error(read.message)
    expect(read.problems.map((p) => p.path).sort()).toEqual(["/id", "/params/proxy"])
    expect(read.doc.values).toEqual({ base_url: "https://a.example.com" })
  })

  it("keeps format and channel as they are", () => {
    const read = readEndpointJson(doc({ format: "other", channel: { module_name: "dns", schema_version: 2 } }), kind)
    if (!read.ok) throw new Error(read.message)
    expect(read.problems.map((p) => p.path).sort()).toEqual(["/channel", "/format"])
    // Left out, they are not missed: a hand-written document need not carry them.
    const bare = readEndpointJson(JSON.stringify({ name: "edge", params: { base_url: "https://a.example.com" } }), kind)
    expect(bare.ok && bare.problems).toEqual([])
  })

  it("bounds the description", () => {
    const read = readEndpointJson(doc({ description: "x".repeat(501) }), kind)
    expect(read.ok && read.problems.map((p) => p.path)).toEqual(["/description"])
  })
})

describe("empty lists and maps", () => {
  const withLists: Kind = {
    ...kind,
    fields: [
      ...kind.fields,
      { key: "extra", type: "string_list", label: "Extra", order: 5, required: false },
      { key: "tags", type: "string_list", label: "Tags", order: 6, required: false, default: ["a"] },
      { key: "headers", type: "string_map", label: "Headers", order: 7, required: false },
    ],
  }

  it("shows one with no default as empty when not set, one with a default as its default", () => {
    const params = JSON.parse(endpointJson(withLists, { name: "e", description: "", values: {} })).params
    expect(params.extra).toEqual([])
    expect(params.headers).toEqual({})
    expect(params.tags).toEqual(["a"])
  })

  it("reads an empty one back as not set, unless it overrides a default", () => {
    const text = JSON.stringify({
      name: "edge",
      params: { base_url: "https://a.example.com", extra: [], headers: {}, tags: [] },
    })
    const read = readEndpointJson(text, withLists)
    if (!read.ok) throw new Error(read.message)
    expect(read.doc.values).toEqual({ base_url: "https://a.example.com", tags: [] })
    expect(read.problems).toEqual([])
  })
})

describe("formValues", () => {
  it("is the form's settings as JSON, with text that does not parse kept as typed", () => {
    const drafts = endpointDrafts(kind.fields, { base_url: "https://a.example.com", tls_verify: true })
    drafts.retries = { kind: "text", text: "abc" }
    expect(formValues(kind.fields, drafts)).toEqual({ base_url: "https://a.example.com", tls_verify: true, retries: "abc" })
  })
})

describe("endpointJsonHints", () => {
  it("offers the channel's settings, with an enum's and a switch's values", () => {
    const params = endpointJsonHints(kind)["/params"]
    expect(params.map((h) => h.key)).toEqual(["base_url", "tls_verify", "retries", "mode"])
    expect(params.find((h) => h.key === "mode")!.values).toEqual(["fast", "safe", null])
    expect(params.find((h) => h.key === "tls_verify")!.values).toEqual([true, false, null])
    expect(params.find((h) => h.key === "base_url")!.detail).toBe("url, required")
  })
})
