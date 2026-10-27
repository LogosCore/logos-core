import { describe, expect, it } from "vitest"
import { GraphQLRequestError } from "@/lib/graphql-client"
import {
  ENDPOINT_EXPORT_FORMAT,
  endpointDrafts,
  endpointImportFrom,
  evaluateEndpoint,
  freeName,
  nameError,
  parseEndpointImport,
  problemsFromError,
  settingRows,
  type Kind,
} from "./endpoint-values"
import type { FieldDef } from "@/components/modules/management/field-values"

function field(f: Partial<FieldDef> & Pick<FieldDef, "key" | "type">): FieldDef {
  return { label: f.key, order: 0, required: false, ...f } as FieldDef
}

const fields: FieldDef[] = [
  field({ key: "base_url", type: "url", required: true, pattern: "^https?://[^/?#\\s]+$" }),
  field({ key: "tls_verify", type: "bool", default: false }),
  field({ key: "request_timeout", type: "duration", default: "40s", min: "31s" }),
  field({ key: "headers", type: "string_list" }),
  field({ key: "token", type: "secret" }),
]

describe("nameError", () => {
  it("accepts slugs and rejects the rest", () => {
    expect(nameError("edge-eu.1")).toBeNull()
    expect(nameError("")).toBe("Required")
    expect(nameError("Edge EU")).not.toBeNull()
    expect(nameError("-edge")).not.toBeNull()
  })
})

describe("evaluateEndpoint", () => {
  it("leaves unset fields out and validates the rest", () => {
    const drafts = endpointDrafts(fields, null)
    drafts.base_url = { kind: "text", text: "https://edge.example.com:8443" }
    drafts.headers = { kind: "list", items: ["X-A: 1", "", "X-B: 2"] }
    const { values, errors } = evaluateEndpoint(fields, drafts)
    expect(errors).toEqual({})
    expect(values).toEqual({ base_url: "https://edge.example.com:8443", headers: ["X-A: 1", "X-B: 2"] })
  })

  it("reports required, pattern and bound errors", () => {
    const drafts = endpointDrafts(fields, null)
    expect(evaluateEndpoint(fields, drafts).errors.base_url).toBe("Required")
    drafts.base_url = { kind: "text", text: "https://edge.example.com/prefix" }
    drafts.request_timeout = { kind: "text", text: "5s" }
    const { errors } = evaluateEndpoint(fields, drafts)
    expect(errors.base_url).toMatch(/must match/i)
    expect(errors.request_timeout).toMatch(/at least/i)
  })

  it("edits secrets as plain text", () => {
    const drafts = endpointDrafts(fields, { base_url: "https://a.example.com", token: "s3cret" })
    expect(drafts.token).toEqual({ kind: "text", text: "s3cret" })
    expect(evaluateEndpoint(fields, drafts).values.token).toBe("s3cret")
    drafts.token = { kind: "text", text: "" }
    expect("token" in evaluateEndpoint(fields, drafts).values).toBe(false)
  })

  it("starts from stored values", () => {
    const drafts = endpointDrafts(fields, { base_url: "https://a.example.com", tls_verify: true })
    expect(evaluateEndpoint(fields, drafts).values).toEqual({ base_url: "https://a.example.com", tls_verify: true })
  })
})

describe("settingRows", () => {
  const kind: Kind = {
    name: "http",
    icon: "",
    version: 2,
    fields: [
      { key: "base_url", type: "url", label: "Base URL", order: 1, required: true },
      { key: "tls_verify", type: "bool", label: "Verify TLS certificate", order: 2, required: false, default: false },
      { key: "request_timeout", type: "duration", label: "Request timeout", order: 3, required: false },
    ] as Kind["fields"],
  }

  it("lists set values in field order, formatted", () => {
    const rows = settingRows(kind, { request_timeout: "40s", base_url: "https://edge.example.com", tls_verify: true, gone: 1 })
    expect(rows).toEqual([
      { key: "base_url", label: "Base URL", text: "https://edge.example.com" },
      { key: "tls_verify", label: "Verify TLS certificate", text: "true" },
      { key: "request_timeout", label: "Request timeout", text: "40s" },
    ])
  })

  it("shows raw keys without a kind", () => {
    expect(settingRows(null, { base_url: "https://edge.example.com", retries: 3 })).toEqual([
      { key: "base_url", label: "base_url", text: "https://edge.example.com" },
      { key: "retries", label: "retries", text: "3" },
    ])
  })
})

describe("problemsFromError", () => {
  it("reads extensions.problems", () => {
    const err = new GraphQLRequestError([
      {
        message: "base_url: bad",
        extensions: { code: "BAD_USER_INPUT", problems: [{ key: "base_url", message: "bad" }, { message: "general" }] },
      },
    ])
    expect(problemsFromError(err)).toEqual({ base_url: "bad", "": "general" })
    expect(problemsFromError(new Error("x"))).toEqual({})
  })
})

describe("freeName", () => {
  it("adds the tag, then a number while the name is taken", () => {
    expect(freeName("edge", [], "-dup")).toBe("edge-dup")
    expect(freeName("edge", ["edge", "edge-dup"], "-dup")).toBe("edge-dup-2")
    expect(freeName("edge", ["edge", "edge-2"], "")).toBe("edge-3")
  })

  it("stays a valid name at the length limit", () => {
    const long = "e".repeat(63)
    const dup = freeName(long, [], "-dup")
    expect(dup).toHaveLength(63)
    expect(dup.endsWith("-dup")).toBe(true)
    expect(nameError(dup)).toBeNull()
    expect(freeName(long, [dup], "-dup")).toMatch(/^e+-dup-2$/)
    expect(freeName(long, [dup], "-dup")).toHaveLength(63)
  })
})

describe("parseEndpointImport", () => {
  const http: Kind = {
    name: "http",
    icon: "",
    version: 2,
    fields: [
      { key: "base_url", type: "url", label: "Base URL", order: 1, required: true },
      { key: "tls_verify", type: "bool", label: "Verify TLS", order: 2, required: false, default: false },
      { key: "request_timeout", type: "duration", label: "Request timeout", order: 3, required: false, default: "40s" },
      { key: "headers", type: "string_map", label: "Headers", order: 4, required: false },
    ] as Kind["fields"],
  }
  const kinds = new Map([["http", http]])
  const exported = (doc: Record<string, unknown>) =>
    JSON.stringify({ format: ENDPOINT_EXPORT_FORMAT, name: "edge", channel: { module_name: "http", schema_version: 2 }, ...doc })

  it("reads an export back: the values that are not defaults", () => {
    const r = parseEndpointImport(
      exported({
        params: { base_url: "https://edge.example.com", tls_verify: false, request_timeout: "60s", headers: { "X-A": "1" } },
      }),
      kinds,
    )
    expect(r).toEqual({
      ok: true,
      value: {
        kind: http,
        name: "edge",
        values: { base_url: "https://edge.example.com", request_timeout: "60s", headers: { "X-A": "1" } },
        notes: [],
      },
    })
  })

  it("picks a valid name (the form makes it a free one)", () => {
    const name = (doc: Record<string, unknown>) => {
      const r = parseEndpointImport(exported(doc), kinds)
      return r.ok ? r.value.name : r.error
    }
    expect(name({})).toBe("edge")
    expect(name({ name: "EU CDN / Edge" })).toBe("eu-cdn-edge")
    expect(name({ name: "--" })).toBe("imported")
    expect(name({ name: 7 })).toBe("imported")
  })

  it("says what it left out or could not match", () => {
    const r = parseEndpointImport(
      exported({ channel: { module_name: "http", schema_version: 1 }, params: { base_url: "https://a.example", proxy_auth: "x", zz: 1 } }),
      kinds,
    )
    expect(r.ok && r.value.notes).toEqual([
      "Exported for http schema v1; this channel is at v2. Check the values.",
      "Left out, not settings of http v2: proxy_auth, zz.",
    ])
    expect(r.ok && r.value.values).toEqual({ base_url: "https://a.example" })
  })

  it("refuses what is not an endpoint export it can use", () => {
    const error = (text: string) => {
      const r = parseEndpointImport(text, kinds)
      return r.ok ? "accepted" : r.error
    }
    expect(error("{oops")).toMatch(/^Not JSON/)
    expect(error("[]")).toMatch(/Not an endpoint export/)
    expect(error(JSON.stringify({ format: "other" }))).toMatch(/Not an endpoint export/)
    expect(error(JSON.stringify({ format: ENDPOINT_EXPORT_FORMAT }))).toMatch(/names no channel/)
    expect(error(exported({ channel: { module_name: "dns" } }))).toMatch(/No "dns" channel/)
    expect(error(exported({ params: ["a"] }))).toMatch(/must be an object/)
  })
})

describe("endpointImportFrom", () => {
  it("reads the import a location carries, and nothing else", () => {
    const imp = { name: "edge", values: { base_url: "https://a.example" }, notes: ["x", 3] }
    expect(endpointImportFrom({ endpointImport: imp })).toEqual({ name: "edge", values: imp.values, notes: ["x"] })
    expect(endpointImportFrom(null)).toBeNull()
    expect(endpointImportFrom({ endpointImport: { name: "edge" } })).toBeNull()
    expect(endpointImportFrom({ other: 1 })).toBeNull()
  })
})
