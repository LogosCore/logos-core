import { describe, expect, it } from "vitest"
import {
  DEFAULT_FAILOVER,
  connectionFailoverErrors,
  connectionFailoverInput,
  failoverAuto,
  failoverErrors,
  failoverSummary,
  readConnectionFailover,
  readFailover,
  recordConnectionFailover,
  recordFailover,
} from "./failover-values"

describe("failover settings", () => {
  it("checks the policy as core does, keyed as core keys it", () => {
    const ok: Record<string, string> = {}
    failoverErrors(DEFAULT_FAILOVER, ok)
    expect(ok).toEqual({})
    const errors: Record<string, string> = {}
    failoverErrors({ ...DEFAULT_FAILOVER, fail_after: 0, fail_for: "soon", cycle_pause: "25h", failback_after: "-1m" }, errors)
    expect(Object.keys(errors).sort()).toEqual(["failover.cycle_pause", "failover.fail_after", "failover.fail_for", "failover.failback_after"])
    const conn: Record<string, string> = {}
    connectionFailoverErrors("connections[1]", { fail_after: Number.NaN }, conn)
    expect(Object.keys(conn)).toEqual(["connections[1].failover.fail_after"])
  })

  it("leaves a listening connection out unless it says otherwise", () => {
    expect(failoverAuto("reverse-call", undefined)).toBe(true)
    expect(failoverAuto("bind-persistent", undefined)).toBe(false)
    expect(failoverAuto("bind-persistent", { auto: true })).toBe(true)
    expect(failoverAuto("reverse-call", { auto: false })).toBe(false)
  })

  it("sends a connection's settings only when they set something", () => {
    expect(connectionFailoverInput(undefined)).toBeNull()
    expect(connectionFailoverInput({})).toBeNull()
    expect(connectionFailoverInput({ fail_after: 3 })).toEqual({ fail_after: 3 })
  })

  it("reads a build's record: the defaults where it says none", () => {
    expect(recordFailover(undefined)).toEqual(DEFAULT_FAILOVER)
    expect(recordFailover({ ...DEFAULT_FAILOVER, fail_after: 10, wrap: false })).toEqual({ ...DEFAULT_FAILOVER, fail_after: 10, wrap: false })
    expect(recordConnectionFailover({ auto: false })).toEqual({ auto: false })
    expect(recordConnectionFailover({})).toBeUndefined()
  })

  it("reads the JSON tab's, marking what does not fit", () => {
    const problems: { path: string; message: string }[] = []
    const at = (path: string, message: string) => problems.push({ path, message })
    expect(readFailover({ fail_after: 10, wrap: "no", soon: 1 }, at)).toEqual({ ...DEFAULT_FAILOVER, fail_after: 10 })
    expect(problems.map((p) => p.path)).toEqual(["/failover/wrap", "/failover/soon"])
    expect(readFailover(undefined, at)).toEqual(DEFAULT_FAILOVER)
    expect(readConnectionFailover({ auto: false }, "/connections/0", at)).toEqual({ auto: false })
    expect(readConnectionFailover(null, "/connections/0", at)).toBeUndefined()
  })

  it("says the policy in a line", () => {
    expect(failoverSummary(DEFAULT_FAILOVER)).toBe(
      "moves after 5 failures in a row · round the list, 5m pause after a failed round · back to the first after 1h",
    )
    expect(failoverSummary({ ...DEFAULT_FAILOVER, enabled: false })).toMatch(/^Off/)
    expect(failoverSummary({ ...DEFAULT_FAILOVER, fail_for: "2m", failback_after: "0s" })).toContain("over at least 2m")
  })
})
