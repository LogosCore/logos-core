import { describe, expect, it } from "vitest"
import { RecoveryRecheckGate } from "./recovery-recheck"

describe("RecoveryRecheckGate", () => {
  it("allows a re-check after an isolated recovery", () => {
    const gate = new RecoveryRecheckGate(3, 60_000)
    expect(gate.shouldRecheck(0)).toBe(true)
    expect(gate.shouldRecheck(120_000)).toBe(true)
    expect(gate.shouldRecheck(240_000)).toBe(true)
    expect(gate.shouldRecheck(360_000)).toBe(true)
  })

  it("stops re-checking when recoveries come back to back", () => {
    const gate = new RecoveryRecheckGate(3, 60_000)
    const results = [0, 2_000, 4_000, 6_000, 8_000].map((t) => gate.shouldRecheck(t))
    expect(results).toEqual([true, true, true, false, false])
  })

  it("re-arms once the connection has been quiet", () => {
    const gate = new RecoveryRecheckGate(3, 60_000)
    for (const t of [0, 2_000, 4_000, 6_000]) gate.shouldRecheck(t)
    expect(gate.shouldRecheck(70_000)).toBe(true)
  })
})
