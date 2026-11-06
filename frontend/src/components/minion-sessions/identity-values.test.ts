import { describe, expect, it } from "vitest"
import { howLabel, identityLabel, identityUser, scopeLabel, shortUser } from "./identity-values"

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

describe("shortUser", () => {
  it("drops the machine's own name: a local account", () => {
    expect(shortUser("DESKTOP-4VC4EKT\\alice", "DESKTOP-4VC4EKT")).toBe("alice")
    expect(shortUser("DESKTOP-4VC4EKT\\alice", "desktop-4vc4ekt")).toBe("alice")
    expect(shortUser("WEB01\\svc", "web01.corp.local")).toBe("svc")
  })
  it("keeps a domain, NT AUTHORITY, and names with no prefix", () => {
    expect(shortUser("CORP\\alice", "WEB01")).toBe("CORP\\alice")
    expect(shortUser("NT AUTHORITY\\SYSTEM", "WEB01")).toBe("NT AUTHORITY\\SYSTEM")
    expect(shortUser("root", "web01")).toBe("root")
    expect(shortUser("alice@corp.local", "web01")).toBe("alice@corp.local")
    expect(shortUser("WEB01\\svc", null)).toBe("WEB01\\svc")
    expect(shortUser("\\odd", "web01")).toBe("\\odd")
  })
})
