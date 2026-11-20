import { describe, expect, it } from "vitest"
import { compositeStands, ownOpText, phaseWord, stopsAtOnce, subtaskText, type Composite } from "./composite-values"

const composite = (c: Partial<Composite> = {}): Composite => ({
  status: null,
  ownOp: null,
  ownArgs: null,
  ownEnd: null,
  subtasksUnderWay: 0,
  subtasksEnded: 0,
  phase: null,
  timerAt: null,
  silentSince: null,
  stoppedAt: null,
  calls: 0,
  ...c,
})

describe("compositeStands", () => {
  it("says what a composite task waits for", () => {
    expect(compositeStands({ state: "COMPOSING", composite: composite() })).toBe("its factory decides")
    expect(compositeStands({ state: "RUNNING", composite: composite({ ownOp: "out" }) })).toBe("its own op is out")
    expect(compositeStands({ state: "COMPOSING", composite: composite({ phase: "stopping" }) })).toMatch(/^stopping/)
    expect(compositeStands({ state: "COMPOSING", composite: composite({ phase: "cleanup" }) })).toMatch(/cleaning up/)
    expect(compositeStands({ state: "COMPOSING", composite: composite({ phase: "finishing" }) })).toMatch(/^finishing/)
    expect(compositeStands({ state: "COMPOSING", composite: composite({ phase: "closed" }) })).toMatch(/session closed/)
  })
  it("says nothing of one that ended, or another task", () => {
    expect(compositeStands({ state: "DONE", composite: composite({ phase: "finishing" }) })).toBeNull()
    expect(compositeStands({ state: "RUNNING", composite: null })).toBeNull()
  })
})

describe("phaseWord", () => {
  it("says the phase while the own op is out", () => {
    expect(phaseWord({ state: "RUNNING", composite: composite({ phase: "stopping" }) })).toBe("stopping")
    expect(phaseWord({ state: "SENT", composite: composite({ phase: "cleanup" }) })).toBe("cleaning up")
    expect(phaseWord({ state: "QUEUED", composite: composite({ phase: "closed" }) })).toBe("its session closed")
    expect(phaseWord({ state: "RUNNING", composite: composite() })).toBeNull()
  })
  it("leaves it to COMPOSING's line, and says nothing once it ended", () => {
    expect(phaseWord({ state: "COMPOSING", composite: composite({ phase: "stopping" }) })).toBeNull()
    expect(phaseWord({ state: "CANCELLED", composite: composite({ phase: "stopping" }) })).toBeNull()
    expect(phaseWord({ state: "RUNNING", composite: null })).toBeNull()
  })
})

describe("ownOpText", () => {
  it("says how the own op stands", () => {
    expect(ownOpText({ ownOp: null, ownEnd: null })).toBeNull()
    expect(ownOpText({ ownOp: "not_run", ownEnd: null })).toBe("not run")
    expect(ownOpText({ ownOp: "out", ownEnd: null })).toBe("out on the minion")
    const end = (e: Partial<NonNullable<Composite["ownEnd"]>>) => ({ state: "done", ok: true, exit: null, err: null, at: "2026-11-20T10:00:00Z", ...e })
    expect(ownOpText({ ownOp: "ended", ownEnd: end({ exit: 0 }) })).toBe("done, exit 0")
    expect(ownOpText({ ownOp: "ended", ownEnd: end({ ok: false, exit: 3 }) })).toBe("ended not ok, exit 3")
    expect(ownOpText({ ownOp: "ended", ownEnd: end({ state: "refused", ok: false, err: "path: required" }) })).toBe("refused: path: required")
    expect(ownOpText({ ownOp: "ended", ownEnd: end({ state: "failed", ok: false, err: "too large" }) })).toBe("failed: too large")
    expect(ownOpText({ ownOp: "ended", ownEnd: end({ state: "cancelled", ok: false }) })).toBe("cancelled")
  })
})

describe("stopsAtOnce", () => {
  it("is a second stop's", () => {
    expect(stopsAtOnce({ composite: composite() })).toBe(false)
    expect(stopsAtOnce({ composite: composite({ phase: "finishing" }) })).toBe(false)
    expect(stopsAtOnce({ composite: composite({ phase: "stopping" }) })).toBe(true)
    expect(stopsAtOnce({ composite: composite({ phase: "cleanup" }) })).toBe(true)
    expect(stopsAtOnce({ composite: composite({ phase: "closed" }) })).toBe(true)
    expect(stopsAtOnce({ composite: null })).toBe(false)
  })
})

describe("subtaskText", () => {
  it("says a subtask's line, a cleanup one so", () => {
    expect(subtaskText({ parentId: null, subtaskAs: "x", cleanup: false })).toBeNull()
    expect(subtaskText({ parentId: "p", subtaskAs: "fetch the archive", cleanup: false })).toBe("fetch the archive")
    expect(subtaskText({ parentId: "p", subtaskAs: null, cleanup: false })).toBeNull()
    expect(subtaskText({ parentId: "p", subtaskAs: "remove it", cleanup: true })).toBe("cleanup: remove it")
    expect(subtaskText({ parentId: "p", subtaskAs: null, cleanup: true })).toBe("cleanup")
  })
})
