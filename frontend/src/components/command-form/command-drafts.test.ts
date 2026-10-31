import { describe, expect, test } from "vitest"
import { evaluateParams, type FieldDef } from "@/components/modules/management/field-values"
import { draftFromText, draftsFromTexts } from "./command-drafts"

const field = (key: string, type: string, extra: Partial<FieldDef> = {}): FieldDef =>
  ({ key, type, label: key, required: false, default: null, options: null, ...extra }) as FieldDef

describe("a command form's fields from a typed line", () => {
  test("each type's text as its field's draft", () => {
    expect(draftFromText(field("a", "bool"), "yes")).toEqual({ kind: "bool", value: true })
    expect(draftFromText(field("a", "bool"), "maybe")).toEqual({ kind: "bool", value: null })
    expect(draftFromText(field("a", "string_list"), "a, b,,c")).toEqual({ kind: "list", items: ["a", "b", "c"] })
    expect(draftFromText(field("a", "enum_list"), "x,y")).toEqual({ kind: "set", values: ["x", "y"] })
    expect(draftFromText(field("a", "string_map"), "k=v, j = w ,bare")).toEqual({
      kind: "map",
      entries: [
        { key: "k", value: "v" },
        { key: "j", value: "w" },
        { key: "bare", value: "" },
      ],
    })
    expect(draftFromText(field("a", "bytes"), "4096")).toEqual({ kind: "bytes", text: "4096", unit: "B" })
    expect(draftFromText(field("a", "datetime"), "not a time")).toEqual({ kind: "text", text: "not a time" })
    expect(draftFromText(field("a", "duration"), "30s")).toEqual({ kind: "text", text: "30s" })
  })

  test("defaults, with what was typed over them; a bad text stays to show its error", () => {
    const fields = [field("interval", "duration", { required: true }), field("jitter", "int", { default: 10 })]
    const drafts = draftsFromTexts(fields, new Map([["interval", "abc"]]))
    expect(drafts.interval).toEqual({ kind: "text", text: "abc" })
    expect(drafts.jitter).toEqual({ kind: "text", text: "10" })
    const { values, errors } = evaluateParams(fields, drafts)
    expect(errors.interval).toBeDefined()
    expect(values).toEqual({ jitter: 10 })
  })
})
