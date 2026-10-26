import { describe, expect, it } from "vitest"
import { adminActions } from "./module-admin"

describe("adminActions", () => {
  it("offers what each state takes", () => {
    expect(adminActions("registered")).toEqual(["block"])
    expect(adminActions("deregistered")).toEqual(["block", "delete"])
    expect(adminActions("dead")).toEqual(["block", "delete"])
    expect(adminActions("blocked")).toEqual(["unblock"])
  })
})
