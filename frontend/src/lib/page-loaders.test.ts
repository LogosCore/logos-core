import { describe, expect, it } from "vitest"
import { pageLoaders, pageOf } from "./page-loaders"

describe("pageOf", () => {
  it("finds a path's page: its own, or its section's", () => {
    expect(pageOf("/sessions")).toBe("/sessions")
    expect(pageOf("/sessions/")).toBe("/sessions")
    expect(pageOf("/wiki/abc-123")).toBe("/wiki")
    expect(pageOf("/builds?build=b-1")).toBe("/builds")
    expect(pageOf("/agents/activity")).toBe("/agents")
    expect(pageOf("/swagger/index.html")).toBeNull()
    expect(pageOf("/")).toBeNull()
  })
  it("has a loader for every page the sidebar links to", () => {
    for (const path of ["/sessions", "/wiki", "/findings", "/tasks", "/timeline", "/endpoints", "/builds", "/ports", "/operations", "/users", "/modules", "/skills"]) {
      expect(pageOf(path), path).not.toBeNull()
      expect(typeof pageLoaders[pageOf(path)!]).toBe("function")
    }
  })
})
