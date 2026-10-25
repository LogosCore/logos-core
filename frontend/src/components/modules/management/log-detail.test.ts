import { describe, expect, it } from "vitest"
import { detailSections, formatValue, humanBytes, recordJSON, statusTone, unflatten } from "./log-detail"
import type { LogLine } from "./log-buffer"

const requestAttrs = {
  request_id: "01ABC",
  method: "POST",
  path: "/api/messages",
  query: "",
  status: 404,
  duration_ms: 0.026,
  outcome: "bad_request",
  stage: "unpack",
  err: "body too short",
  client_ip: "10.0.0.1",
  remote_addr: "10.0.0.1:5000",
  body_bytes: 2048,
  "headers.user-agent": "curl/8",
  "headers.accept": "*/*",
  component: "http",
}

describe("detailSections", () => {
  it("sorts a request line into named sections", () => {
    const s = detailSections(requestAttrs)
    expect(s.map((x) => x.title)).toEqual(["Summary", "Error", "Request", "Client", "Fields", "Headers"])
    expect(s[0].fields.map((f) => f.key)).toEqual(["outcome", "status", "duration_ms", "request_id"])
    expect(s[5].fields.map((f) => f.label)).toEqual(["accept", "user-agent"])
    expect(s[4].fields.map((f) => f.key)).toEqual(["component"])
  })

  it("puts unknown attrs of other modules under Fields", () => {
    const s = detailSections({ profile: "p1", "job.id": 7 })
    expect(s.map((x) => x.title)).toEqual(["Fields", "Job"])
  })

  it("returns nothing without attrs", () => {
    expect(detailSections(null)).toEqual([])
  })
})

describe("formatValue", () => {
  it("adds units", () => {
    expect(formatValue("duration_ms", 40.0931)).toBe("40.093 ms")
    expect(formatValue("body_bytes", 54)).toBe("54 B")
    expect(formatValue("content_length", -1)).toBe("-1")
    expect(formatValue("query", "")).toBe("(empty)")
    expect(formatValue("x", null)).toBe("—")
  })

  it("humanizes larger sizes", () => {
    expect(humanBytes(1048576)).toBe("1 MiB (1,048,576 B)")
  })
})

describe("recordJSON and unflatten", () => {
  it("nests dotted keys", () => {
    expect(unflatten({ "headers.accept": "*/*", "headers.user-agent": "x", status: 200 })).toEqual({
      headers: { accept: "*/*", "user-agent": "x" },
      status: 200,
    })
  })

  it("keeps a conflicting dotted key flat", () => {
    expect(unflatten({ a: 1, "a.b": 2 })).toEqual({ a: 1, "a.b": 2 })
  })

  it("renders the whole record", () => {
    const line: LogLine = { id: 1, time: "2026-10-25T13:07:07Z", ms: 0, level: "INFO", message: "m", attrs: { "h.x": 1 } }
    expect(JSON.parse(recordJSON(line))).toEqual({ time: "2026-10-25T13:07:07Z", level: "INFO", message: "m", attrs: { h: { x: 1 } } })
  })
})

describe("statusTone", () => {
  it("grades HTTP statuses", () => {
    expect([statusTone(200), statusTone(404), statusTone(502), statusTone(0), statusTone("x")]).toEqual(["ok", "warn", "bad", null, null])
  })
})
