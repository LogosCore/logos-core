import { describe, expect, it } from "vitest"
import {
  leavesRecordModal,
  recordModalKey,
  recordModalOf,
  searchOf,
  withRecordModal,
  type RecordModal,
} from "./record-modals"

const params = (s: string) => new URLSearchParams(s)

describe("recordModalOf", () => {
  it("reads each modal", () => {
    expect(recordModalOf(params("build=b-1"))).toEqual({ kind: "build", id: "b-1" })
    expect(recordModalOf(params("newBuild=bob"))).toEqual({ kind: "new-build", factory: "bob", from: null })
    expect(recordModalOf(params("newBuild=bob&buildFrom=b-1"))).toEqual({ kind: "new-build", factory: "bob", from: "b-1" })
    expect(recordModalOf(params("endpoint=e-1"))).toEqual({ kind: "endpoint", id: "e-1" })
    expect(recordModalOf(params("newEndpoint=http&endpointFrom=e-1"))).toEqual({ kind: "new-endpoint", channel: "http", from: "e-1" })
  })

  it("reads a build's tab, and leaves out one it does not know", () => {
    expect(recordModalOf(params("build=b-1&buildTab=pipeline"))).toEqual({ kind: "build", id: "b-1", tab: "pipeline" })
    expect(recordModalOf(params("build=b-1&buildTab=logs"))).toEqual({ kind: "build", id: "b-1" })
    expect(withRecordModal(params(""), { kind: "build", id: "b-1", tab: "json" }).toString()).toBe("build=b-1&buildTab=json")
    // Another modal drops it.
    expect(withRecordModal(params("build=b-1&buildTab=json"), { kind: "endpoint", id: "e-1" }).toString()).toBe("endpoint=e-1")
  })

  it("is none without a modal key, or with an empty one", () => {
    expect(recordModalOf(params(""))).toBeNull()
    expect(recordModalOf(params("tab=hosts&build="))).toBeNull()
    expect(recordModalOf(params("buildFrom=b-1"))).toBeNull()
  })

  it("opens one modal when several are named", () => {
    expect(recordModalOf(params("endpoint=e-1&build=b-1"))?.kind).toBe("build")
  })
})

describe("withRecordModal", () => {
  it("keeps the page's own query and replaces the modal", () => {
    const p = params("tab=hosts&build=b-1")
    const next = withRecordModal(p, { kind: "new-build", factory: "bob", from: "b-1" })
    expect(next.toString()).toBe("tab=hosts&newBuild=bob&buildFrom=b-1")
    expect(p.toString()).toBe("tab=hosts&build=b-1") // not changed in place
    expect(withRecordModal(next, null).toString()).toBe("tab=hosts")
  })

  it("round-trips every modal", () => {
    const modals: RecordModal[] = [
      { kind: "build", id: "b-1" },
      { kind: "build", id: "b-1", tab: "overview" },
      { kind: "new-build", factory: "bob", from: null },
      { kind: "new-build", factory: "bob", from: "b-1" },
      { kind: "endpoint", id: "e-1" },
      { kind: "new-endpoint", channel: "http", from: "e-1" },
    ]
    for (const m of modals) expect(recordModalOf(withRecordModal(params("q=1"), m))).toEqual(m)
  })
})

describe("recordModalKey", () => {
  it("is equal for equal modals only", () => {
    expect(recordModalKey({ kind: "build", id: "b-1" })).toBe(recordModalKey({ kind: "build", id: "b-1" }))
    expect(recordModalKey({ kind: "build", id: "b-1" })).not.toBe(recordModalKey({ kind: "build", id: "b-2" }))
    expect(recordModalKey({ kind: "new-build", factory: "bob", from: null })).not.toBe(
      recordModalKey({ kind: "new-build", factory: "bob", from: "b-1" }),
    )
    expect(recordModalKey(null)).toBe("")
  })
})

describe("leavesRecordModal", () => {
  const at = (pathname: string, search: string) => ({ pathname, search })

  it("is closing the modal, or putting another in its place", () => {
    const editing = at("/builds", "?newBuild=bob")
    expect(leavesRecordModal(editing, at("/builds", ""))).toBe(true)
    expect(leavesRecordModal(editing, at("/builds", "?build=b-1"))).toBe(true)
    expect(leavesRecordModal(editing, at("/builds", "?newBuild=bob&buildFrom=b-1"))).toBe(true)
    expect(leavesRecordModal(editing, at("/wiki", "?newBuild=bob"))).toBe(true)
  })

  it("is not a change to the page's own query", () => {
    expect(leavesRecordModal(at("/findings", "?endpoint=e-1"), at("/findings", "?ops=all&endpoint=e-1"))).toBe(false)
  })

  it("is not anything when no modal is open", () => {
    expect(leavesRecordModal(at("/builds", "?q=1"), at("/wiki", ""))).toBe(false)
  })
})

describe("searchOf", () => {
  it("is ?query, or nothing", () => {
    expect(searchOf(params("a=1"))).toBe("?a=1")
    expect(searchOf(params(""))).toBe("")
  })
})
