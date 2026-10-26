import { describe, expect, it } from "vitest"
import { actionBlocker } from "./action-blocker"

const can = { canInvoke: true, canInvokeHigh: true, registered: true }
const drain = { risk: "high", available: true, unavailableReason: null }

describe("actionBlocker", () => {
  it("lets an available action run", () => {
    expect(actionBlocker(drain, can)).toBeUndefined()
  })

  it("says why an unavailable action cannot run", () => {
    expect(actionBlocker({ ...drain, available: false, unavailableReason: "Already draining" }, can)).toBe(
      "Already draining",
    )
    expect(actionBlocker({ ...drain, available: false, unavailableReason: null }, can)).toBe("Not available now")
  })

  it("puts permissions and the instance before the action's conditions", () => {
    const unavailable = { ...drain, available: false, unavailableReason: "Already draining" }
    expect(actionBlocker(unavailable, { ...can, canInvoke: false })).toBe("Requires module:action:invoke")
    expect(actionBlocker(unavailable, { ...can, canInvokeHigh: false })).toMatch(/High-risk/)
    expect(actionBlocker({ ...unavailable, risk: "low" }, { ...can, canInvokeHigh: false })).toBe("Already draining")
    expect(actionBlocker(unavailable, { ...can, registered: false })).toBe("The instance is not registered")
  })
})
