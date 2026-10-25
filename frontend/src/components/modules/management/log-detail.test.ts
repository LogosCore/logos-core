import { describe, expect, it } from "vitest"
import { detailSections, formatValue, recordJSON } from "./log-detail"
import type { LogLine } from "./log-buffer"

describe("detailSections", () => {
  it("follows the record's own structure, knowing no keys", () => {
    const s = detailSections({
      status: 404,
      request_id: "01ABC",
      headers: { accept: "*/*", "user-agent": "curl/8" },
      chat: { id: 7, user: { name: "x" } },
      tags: ["a", "b"],
    })
    expect(s.map((x) => x.path)).toEqual(["", "headers", "chat", "chat.user"])
    expect(s[0].rows.map((r) => r.label)).toEqual(["status", "request_id", "tags"])
    expect(s[1].rows.map((r) => [r.path, r.text])).toEqual([
      ["headers.accept", "*/*"],
      ["headers.user-agent", "curl/8"],
    ])
    expect(s[2].rows.map((r) => r.label)).toEqual(["id"])
    expect(s[3].rows[0].path).toBe("chat.user.name")
  })

  it("leaves out a top-level section with no scalar values", () => {
    expect(detailSections({ headers: { a: "1" } }).map((x) => x.path)).toEqual(["headers"])
  })

  it("returns nothing without attrs", () => {
    expect(detailSections(null)).toEqual([])
  })
})

describe("formatValue", () => {
  it("shows values verbatim", () => {
    expect(formatValue("body too short")).toBe("body too short")
    expect(formatValue("")).toBe("")
    expect(formatValue(0.026)).toBe("0.026")
    expect(formatValue(1048576)).toBe("1048576")
    expect(formatValue(true)).toBe("true")
    expect(formatValue(null)).toBe("null")
    expect(formatValue(["a", 1])).toBe('["a",1]')
  })
})

describe("recordJSON", () => {
  it("keeps attrs as sent", () => {
    const line: LogLine = {
      id: 1,
      time: "2026-10-25T13:07:07Z",
      ms: 0,
      level: "INFO",
      message: "m",
      attrs: { headers: { accept: "*/*" }, "dotted.key": 1 },
      attrsText: "",
      searchText: "",
    }
    expect(JSON.parse(recordJSON(line))).toEqual({
      time: "2026-10-25T13:07:07Z",
      level: "INFO",
      message: "m",
      attrs: { headers: { accept: "*/*" }, "dotted.key": 1 },
    })
  })
})
