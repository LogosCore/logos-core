import { describe, expect, it } from "vitest"
import { howLabel, identityLabel, identityUser, scopeLabel } from "./identity-values"

describe("identity values", () => {
  it("writes the user with its domain", () => {
    expect(identityUser({ user: "admin", domain: "CORP" })).toBe("CORP\\admin")
    expect(identityUser({ user: "CORP\\admin", domain: "CORP" })).toBe("CORP\\admin")
    expect(identityUser({ user: null })).toBe("")
  })
  it("labels how, and says nothing for the process's own", () => {
    expect(identityLabel({ user: "root", how: "become" })).toBe("root (became)")
    expect(identityLabel({ user: "alice", how: null })).toBe("")
    expect(identityLabel(null)).toBe("")
    expect(howLabel("how 7")).toBe("how 7")
    expect(scopeLabel("task")).toBe("Task")
  })
})
