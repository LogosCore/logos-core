import { describe, expect, it } from "vitest"
import { checkDraft, initialDraft, type FieldDef } from "./field-values"
import { draftFromText } from "@/components/command-form/command-drafts"

const field = { key: "stream", type: "stream", label: "File", required: true } as unknown as FieldDef

describe("a stream param", () => {
  it("is the picked file's name, or the name typed for curl", () => {
    const d = initialDraft(field, null)
    expect(d).toEqual({ kind: "stream", file: null, name: "" })
    const picked = { ...d, file: new File(["abc"], "tool.exe") } as typeof d
    expect(checkDraft(field, picked)).toEqual({ ok: true, value: "tool.exe" })
    expect(checkDraft(field, { kind: "stream", file: null, name: " big.iso " })).toEqual({ ok: true, value: "big.iso" })
    // None picked, none named: no value; core names it by the path.
    expect(checkDraft({ ...field, required: false }, { kind: "stream", file: null, name: "" })).toEqual({ ok: true, value: null })
  })
  it("is a name without a path", () => {
    const r = checkDraft(field, { kind: "stream", file: null, name: "../x" })
    expect(r.ok).toBe(false)
  })
  it("typed on a line, is for curl", () => {
    expect(draftFromText(field, "big.iso")).toEqual({ kind: "stream", file: null, name: "big.iso" })
  })
})
