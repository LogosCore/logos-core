import { describe, expect, it } from "vitest"
import { EMPTY_LOG, logSections, logText, mergeLogPage, openByDefault, type LogStage } from "./build-log"

const stages: LogStage[] = [
  { id: "core.queue", label: "Queue", core: true, state: "SUCCEEDED" },
  { id: "badge", label: "Badge", core: false, state: "SUCCEEDED" },
  { id: "compile", label: "Compile", core: false, state: "FAILED" },
  { id: "upload", label: "Upload", core: false, state: "SKIPPED" },
]

describe("mergeLogPage", () => {
  it("appends a live log's pages", () => {
    const one = mergeLogPage(EMPTY_LOG, {
      live: true, cursor: 2, more: false, trimmed: 0, lines: [{ step: "badge", text: "a" }],
    })
    const two = mergeLogPage(one, { live: true, cursor: 3, more: false, trimmed: 4, lines: [{ step: "compile", text: "b" }] })
    expect(two.lines.map((l) => l.text)).toEqual(["a", "b"])
    expect(two).toMatchObject({ live: true, cursor: 3, trimmed: 4 })
  })

  it("replaces a result's log, which comes whole each time", () => {
    const page = { live: false, cursor: 0, more: false, trimmed: 0, lines: [{ text: "x" }] }
    expect(mergeLogPage(mergeLogPage(EMPTY_LOG, page), page).lines).toHaveLength(1)
  })
})

describe("logSections", () => {
  it("groups lines by step in the plan's order, unknown and stepless last", () => {
    const sections = logSections(
      [
        { step: "compile", text: "c1" },
        { step: "badge", text: "b1" },
        { step: "compile", text: "c2" },
        { step: null, text: "o1" },
        { step: "sign", text: "s1" },
      ],
      stages,
    )
    expect(sections.map((s) => [s.label, s.lines.map((l) => l.text)])).toEqual([
      ["Badge", ["b1"]],
      ["Compile", ["c1", "c2"]],
      ["sign", ["s1"]],
      ["Output", ["o1"]],
    ])
    expect(sections[1]).toMatchObject({ id: "compile", state: "FAILED", count: 2 })
    expect(sections[2].state).toBeNull()
  })

  it("keeps a mark with the line before it, out of the count", () => {
    const [badge, compile] = logSections(
      [
        { step: "badge", text: "b1" },
        { step: "compile", text: "c1" },
        { text: "", missing: 3 },
        { step: "compile", text: "c2" },
      ],
      stages,
    )
    expect(badge.lines).toHaveLength(1)
    expect(compile.lines.map((l) => l.missing ?? l.text)).toEqual(["c1", 3, "c2"])
    expect(compile.count).toBe(2)
  })
})

describe("logText", () => {
  it("writes a live log by step with times, a result's log as it is", () => {
    const at = new Date(2026, 10, 12, 15, 4, 5).toISOString()
    const live = mergeLogPage(EMPTY_LOG, {
      live: true, cursor: 1, more: false, trimmed: 2,
      lines: [{ step: "compile", at, text: "go build" }, { text: "", missing: 0 }],
    })
    expect(logText(live, logSections(live.lines, stages))).toBe(
      "… 2 earlier lines dropped by core\n== Compile ==\n15:04:05  go build\n… some lines never reached core\n",
    )
    const result = mergeLogPage(EMPTY_LOG, { live: false, cursor: 0, more: false, trimmed: 0, lines: [{ text: "a" }, { text: "b" }] })
    expect(logText(result, logSections(result.lines, stages))).toBe("a\nb\n")
    expect(logText(EMPTY_LOG, [])).toBe("")
  })
})

describe("openByDefault", () => {
  it("opens the running or failed step, or the only one", () => {
    const sections = logSections([{ step: "badge", text: "b" }, { step: "compile", text: "c" }], stages)
    expect(sections.map((s) => openByDefault(s, sections))).toEqual([false, true])
    expect(openByDefault(sections[0], [sections[0]])).toBe(true)
  })
})
