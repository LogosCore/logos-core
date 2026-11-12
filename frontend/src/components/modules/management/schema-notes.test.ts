import { describe, expect, it } from "vitest"
import type { FieldDef } from "./field-values"
import { channelsText, fieldNotes, paramsNote, versionsText } from "./schema-notes"

function field(f: Partial<FieldDef> & Pick<FieldDef, "key" | "type">): FieldDef {
  return { label: f.key, order: 0, required: false, ...f } as FieldDef
}

describe("fieldNotes", () => {
  it("gives the default, formatted for its type", () => {
    expect(fieldNotes(field({ key: "greeting", type: "string", default: "hello from BOB" }))).toEqual(["default hello from BOB"])
    expect(fieldNotes(field({ key: "checkin_interval", type: "duration", default: "60s" }))).toEqual(["default 60s"])
    expect(fieldNotes(field({ key: "tls_verify", type: "bool", default: false }))).toEqual(["default false"])
    expect(fieldNotes(field({ key: "jitter", type: "int", default: 20, unit: "%" }))).toEqual(["default 20 %"])
    expect(fieldNotes(field({ key: "kill_date", type: "datetime" }))).toEqual([])
  })

  it("says the platforms a build field is for", () => {
    expect(
      fieldNotes(field({ key: "goarm", type: "enum", default: "7", options: [{ value: "7" }, { value: "6" }], platforms: ["linux/arm"] })),
    ).toEqual(["default 7", "7, 6", "only for linux/arm"])
  })

  it("shows a secret's declared default, not a mask", () => {
    expect(fieldNotes(field({ key: "token", type: "secret", default: "changeme" }))).toEqual(["default changeme"])
  })

  it("lists the options an enum takes", () => {
    const method = field({ key: "method", type: "enum", default: "POST", options: [{ value: "GET" }, { value: "POST" }, { value: "PUT" }] })
    expect(fieldNotes(method)).toEqual(["default POST", "GET, POST, PUT"])
    const punches = field({ key: "punches", type: "enum_list", options: [{ value: "jab" }, { value: "hook" }] })
    expect(fieldNotes(punches)).toEqual(["jab, hook"])
  })

  it("says what a file field accepts", () => {
    expect(fieldNotes(field({ key: "badge", type: "file", accept: ["image/*"], maxSize: 256 * 1024 }))).toEqual([
      "image/* · up to 256 KiB",
    ])
    expect(fieldNotes(field({ key: "any", type: "file" }))).toEqual(["Any file"])
  })
})

describe("channelsText", () => {
  it("names each channel kind with the versions read", () => {
    expect(channelsText([{ moduleName: "http", versions: [2] }])).toBe("http v2")
    expect(channelsText([{ moduleName: "http", versions: [2] }, { moduleName: "dns", versions: [1, 2] }])).toBe("http v2 · dns v1, v2")
    expect(channelsText([])).toBe("")
    expect(versionsText([1, 2])).toBe("v1, v2")
  })
})

describe("paramsNote", () => {
  it("names a command's params by number, as a task does", () => {
    expect(paramsNote([{ key: "path", num: 1 }, { key: "recursive", num: 2 }])).toBe("params 1 path, 2 recursive")
  })
  it("leaves out a param without a number (core's own), and says nothing for none", () => {
    expect(paramsNote([{ key: "front", num: null }, { key: "timeout", num: 2 }])).toBe("params 2 timeout")
    expect(paramsNote([{ key: "front", num: null }])).toBeNull()
    expect(paramsNote([])).toBeNull()
  })
})
