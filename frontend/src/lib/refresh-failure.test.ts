import { describe, expect, it } from "vitest"
import { classifyRefreshFailure } from "./refresh-failure"

describe("classifyRefreshFailure", () => {
  it("ends the session on an explicit auth rejection", () => {
    expect(classifyRefreshFailure(401)).toBe("rejected")
    expect(classifyRefreshFailure(403)).toBe("rejected")
  })

  it("treats gateway statuses as the backend being unreachable", () => {
    for (const status of [502, 503, 504]) {
      expect(classifyRefreshFailure(status)).toBe("unreachable")
    }
  })

  it("does not report a backend 500 as unreachable", () => {
    // The stale-session loop: core answered 500, the SPA called it an
    // outage, and the /status poll "recovered" it every two seconds.
    expect(classifyRefreshFailure(500)).toBe("failed")
    expect(classifyRefreshFailure(429)).toBe("failed")
  })
})
