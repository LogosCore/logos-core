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
  it("is the record without its log, the build first and what it was given last", () => {
    const b = {
      id: "m-1",
      operationId: "op",
      description: "d",
      moduleName: "bob",
      target: "dummy",
      platform: "any/any",
      schemaVersion: 1,
      state: "SUCCEEDED",
      stages: [],
      capabilities: [],
      warnings: [],
      values: { greeting: "hi" },
      connections: [],
      connectionDescriptions: [],
      createdBy: { type: "user", id: "u", name: "ann" },
      createdAt: "2026-10-28T10:00:00Z",
      deadline: "2026-10-28T11:00:00Z",
      log: "x".repeat(70_000),
    } as unknown as BuildRecord
    const json = buildRecordJson(b)
    expect(json).not.toHaveProperty("log")
    expect(Object.keys(json)[0]).toBe("id")
    expect(Object.keys(json).slice(-3)).toEqual(["values", "connections", "connectionDescriptions"])
    expect(json.finishedAt).toBeNull()
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
