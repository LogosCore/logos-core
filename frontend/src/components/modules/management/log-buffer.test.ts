import { describe, expect, it } from "vitest"
import { applyEvent, emptyBuffer, formatAttrs, lineMatches, lineText, MAX_LINES, type LogEventLike } from "./log-buffer"

const rec = (sec: number, message: string, attrs?: unknown) => ({
  time: new Date(Date.UTC(2026, 9, 25, 12, 0, sec)).toISOString(),
  level: "INFO" as const,
  message,
  attrs,
})

const ev = (over: Partial<LogEventLike>): LogEventLike => ({
  backlog: false,
  dropped: 0,
  lost: false,
  records: [],
  ...over,
})

describe("applyEvent", () => {
  it("appends records and tracks state, drops and losses", () => {
    let b = applyEvent(emptyBuffer, ev({ state: "STREAMING" }))
    b = applyEvent(b, ev({ backlog: true, records: [rec(1, "a"), rec(2, "b")] }))
    b = applyEvent(b, ev({ records: [rec(3, "c")], dropped: 2, lost: true }))
    expect(b.lines.map((l) => l.message)).toEqual(["a", "b", "c"])
    expect(b.lines.map((l) => l.id)).toEqual([1, 2, 3])
    expect(b.dropped).toBe(2)
    expect(b.lost).toBe(1)
    expect(b.state).toBe("STREAMING")
  })

  it("skips backlog records already shown after a stream restart", () => {
    let b = applyEvent(emptyBuffer, ev({ backlog: true, records: [rec(1, "a"), rec(2, "b")] }))
    b = applyEvent(b, ev({ backlog: true, records: [rec(1, "a"), rec(2, "b"), rec(3, "c")] }))
    expect(b.lines.map((l) => l.message)).toEqual(["a", "b", "c"])
  })

  it("keeps live records even with an older timestamp", () => {
    let b = applyEvent(emptyBuffer, ev({ records: [rec(5, "late")] }))
    b = applyEvent(b, ev({ records: [rec(4, "early")] }))
    expect(b.lines).toHaveLength(2)
  })

  it("keeps the reason only with the state it came with", () => {
    let b = applyEvent(emptyBuffer, ev({ state: "DISCONNECTED", reason: "unroutable" }))
    b = applyEvent(b, ev({ records: [rec(1, "x")] }))
    expect(b.reason).toBe("unroutable")
    b = applyEvent(b, ev({ state: "STREAMING" }))
    expect(b.reason).toBeNull()
  })

  it("caps the buffer", () => {
    const many = Array.from({ length: MAX_LINES + 10 }, (_, i) => rec(0, `m${i}`))
    const b = applyEvent(emptyBuffer, ev({ records: many }))
    expect(b.lines).toHaveLength(MAX_LINES)
    expect(b.lines[0].message).toBe("m10")
  })
})

describe("formatting and search", () => {
  const line = applyEvent(emptyBuffer, ev({ records: [rec(1, "request failed", { stage: "unpack", err: "too short", n: 3 })] })).lines[0]

  it("formats attrs sorted, quoting values with spaces", () => {
    expect(formatAttrs(line.attrs)).toBe('err="too short" n=3 stage=unpack')
    expect(lineText(line)).toBe(`${line.time} INFO  request failed err="too short" n=3 stage=unpack`)
  })

  it("matches every term across level, message and attrs", () => {
    expect(lineMatches(line, "")).toBe(true)
    expect(lineMatches(line, "FAILED unpack")).toBe(true)
    expect(lineMatches(line, "info short")).toBe(true)
    expect(lineMatches(line, "failed served")).toBe(false)
  })
})
