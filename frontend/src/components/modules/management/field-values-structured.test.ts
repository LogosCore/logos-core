import { describe, expect, it } from "vitest"
import { checkDraft, formatValue, initialDraft, structuredProblem, MAX_STRUCTURED_DEPTH, type FieldDef } from "./field-values"
import { draftFromText } from "@/components/command-form/command-drafts"

const field = { key: "targets", type: "structured", label: "Targets", required: true } as unknown as FieldDef

/** A value depth levels deep: arrays in arrays. */
const nested = (depth: number): unknown => (depth === 0 ? "x" : [nested(depth - 1)])

describe("a structured param", () => {
  it("is edited as JSON, indented", () => {
    expect(initialDraft(field, null)).toEqual({ kind: "json", text: "" })
    expect(initialDraft(field, { a: [1] })).toEqual({ kind: "json", text: '{\n  "a": [\n    1\n  ]\n}' })
    expect(draftFromText(field, '{"a":1}')).toEqual({ kind: "json", text: '{\n  "a": 1\n}' })
    expect(draftFromText(field, "{a")).toEqual({ kind: "json", text: "{a" })
    expect(formatValue("structured", { a: 1 })).toBe('{"a":1}')
  })

  it("is the JSON's value, checked as core checks it", () => {
    expect(checkDraft(field, { kind: "json", text: ' {"hosts": ["a"]} ' })).toEqual({ ok: true, value: { hosts: ["a"] } })
    expect(checkDraft(field, { kind: "json", text: "" })).toEqual({ ok: false, error: "Required" })
    expect(checkDraft({ ...field, required: false }, { kind: "json", text: " " })).toEqual({ ok: true, value: null })
    expect(checkDraft(field, { kind: "json", text: "{a" })).toMatchObject({ ok: false, error: expect.stringMatching(/^Not JSON/) })
    expect(checkDraft(field, { kind: "json", text: '{"a": [1, null]}' })).toEqual({
      ok: false,
      error: "at .a[1]: null is not taken: leave the value out",
    })
  })

  it("nests as deep as core takes", () => {
    expect(structuredProblem(nested(MAX_STRUCTURED_DEPTH))).toBeNull()
    expect(structuredProblem(nested(MAX_STRUCTURED_DEPTH + 1))).toMatch(/nested deeper than 12/)
  })
})
