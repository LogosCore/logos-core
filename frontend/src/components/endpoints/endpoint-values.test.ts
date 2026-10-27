import { describe, expect, it } from "vitest"
import { GraphQLRequestError } from "@/lib/graphql-client"
import {
  duplicateName,
  endpointDrafts,
  evaluateEndpoint,
  groupByKind,
  nameError,
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

describe("groupByKind", () => {
  it("groups by module name in name order", () => {
    const groups = groupByKind([
      { moduleName: "http", n: 1 },
      { moduleName: "dns", n: 2 },
      { moduleName: "http", n: 3 },
    ])
    expect(groups.map(([k, items]) => [k, items.map((i) => i.n)])).toEqual([
      ["dns", [2]],
      ["http", [1, 3]],
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

describe("duplicateName", () => {
  it("adds -dup, then a number while the name is taken", () => {
    expect(duplicateName("edge", [])).toBe("edge-dup")
    expect(duplicateName("edge", ["edge", "edge-dup"])).toBe("edge-dup-2")
    expect(duplicateName("edge", ["edge-dup", "edge-dup-2", "edge-dup-3"])).toBe("edge-dup-4")
  })

  it("stays a valid name at the length limit", () => {
    const long = "e".repeat(63)
    const dup = duplicateName(long, [])
    expect(dup).toHaveLength(63)
    expect(dup.endsWith("-dup")).toBe(true)
    expect(nameError(dup)).toBeNull()
    expect(duplicateName(long, [dup])).toMatch(/^e+-dup-2$/)
    expect(duplicateName(long, [dup])).toHaveLength(63)
  })
})
