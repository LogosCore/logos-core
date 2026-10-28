import { describe, expect, it } from "vitest"
import type { FieldDef } from "@/components/modules/management/field-values"
import { buildRecordJson, defaultBuildTab, valueRows, type BuildRecord } from "./build-record"

function field(f: Partial<FieldDef> & Pick<FieldDef, "key" | "type">): FieldDef {
  return { label: f.key, order: 0, required: false, ...f } as FieldDef
}

describe("defaultBuildTab", () => {
  it("opens on the pipeline until the build has succeeded", () => {
    for (const state of ["QUEUED", "BUILDING", "FAILED", "CANCELLED"]) expect(defaultBuildTab(state)).toBe("pipeline")
    expect(defaultBuildTab("SUCCEEDED")).toBe("overview")
  })
})

describe("buildRecordJson", () => {
  it("is what it takes to build it, as a new build's JSON writes it", () => {
    const b = {
      id: "m-1",
      operationId: "op",
      description: "EU dry run",
      moduleName: "bob",
      target: "dummy",
      platform: "any/any",
      schemaVersion: 1,
      state: "SUCCEEDED",
      stages: [{ id: "core.queue" }],
      warnings: ["debug build"],
      artifact: { filename: "bob.json" },
      values: { greeting: "hi" },
      commands: ["sleep", "hello"],
      connections: [
        {
          endpoint: {
            format: "logos.channel-endpoint.v1",
            name: "edge",
            channel: { module_name: "http", schema_version: 2 },
            params: { base_url: "https://edge.example.com", request_timeout: "40s" },
          },
          values: { method: "POST" },
        },
      ],
      connectionDescriptions: ["the EU front"],
      createdBy: { type: "user", id: "u", name: "ann" },
      createdAt: "2026-10-28T10:00:00Z",
      deadline: "2026-10-28T11:00:00Z",
      log: "x".repeat(70_000),
    } as unknown as BuildRecord
    expect(buildRecordJson(b)).toEqual({
      factory: { module_name: "bob", schema_version: 1 },
      description: "EU dry run",
      target: "dummy",
      platform: "any/any",
      commands: ["sleep", "hello"],
      values: { greeting: "hi" },
      connections: [
        {
          channel: "http",
          name: "edge",
          description: "the EU front",
          settings: { base_url: "https://edge.example.com", request_timeout: "40s" },
          values: { method: "POST" },
        },
      ],
    })
  })
})

describe("valueRows", () => {
  const fields = [
    field({ key: "greeting", type: "string", default: "hello" }),
    field({ key: "rounds", type: "int" }),
    field({ key: "punches", type: "enum_list", default: [] }),
  ]

  it("lists each field with what the build was given, marking defaults", () => {
    const rows = valueRows(fields, { greeting: "hello", punches: ["jab"], colour: "red" })
    expect(rows.map((r) => [r.key, r.value, r.isDefault, r.field !== null])).toEqual([
      ["greeting", "hello", true, true],
      ["rounds", undefined, false, true],
      ["punches", ["jab"], false, true],
      ["colour", "red", false, false],
    ])
  })

  it("survives values that are not an object", () => {
    expect(valueRows(fields, null).map((r) => r.value)).toEqual([undefined, undefined, undefined])
  })
})
