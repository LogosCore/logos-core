import { describe, expect, test } from "vitest"
import { HOP, KIND_FLOWS, KNOWN_KINDS, crossing, hops, stepAt } from "./connection-kind-flows"

describe("connection kind flows", () => {
  test("every kind the protocol defines has a flow, its events in order and in its loop, each step reached", () => {
    for (const k of KNOWN_KINDS) {
      const f = KIND_FLOWS[k]
      expect(f, k).toBeDefined()
      const ats = f.events.map((e) => e.at)
      expect(ats, k).toEqual([...ats].sort((a, b) => a - b))
      for (const e of f.events) {
        expect(e.at + (e.path ? hops(e.path).length * HOP : 0), `${k} ${e.label ?? e.badge}`).toBeLessThanOrEqual(f.period)
      }
      const reached = new Set(f.events.map((e) => e.step))
      f.steps.forEach((_, i) => expect(reached.has(i), `${k} step ${i}`).toBe(true))
    }
  })

  test("a request lights only the links it crosses, while it crosses them", () => {
    const f = KIND_FLOWS["reverse-call"]
    expect(crossing(f.events, 0.3 + HOP / 2, "mc")).toBe(true)
    expect(crossing(f.events, 0.3 + HOP / 2, "ck")).toBe(false)
    expect(crossing(f.events, 3.5, "mc")).toBe(false)
    expect(f.links(3.5, f.events)).toEqual({ mc: "none", ck: "none" })
    // A persistent connection stays open between messages.
    const p = KIND_FLOWS["reverse-persistent"]
    expect(p.links(5.5, p.events)).toEqual({ mc: "open", ck: "open" })
  })

  test("the step follows the latest event begun", () => {
    const f = KIND_FLOWS["reverse-persistent"]
    expect(stepAt(f, 0)).toBe(0)
    expect(stepAt(f, 3.6)).toBe(2)
    expect(stepAt(f, 8.9)).toBe(4)
  })
})
