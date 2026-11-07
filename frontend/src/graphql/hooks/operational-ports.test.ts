import { QueryClient } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { OperationalPortFieldsFragment } from "@/graphql/gql/graphql"
import { applyPortEvent, operationalPortKeys, type PortPages } from "./operational-ports"

const port = (id: string, p: Partial<OperationalPortFieldsFragment> = {}): OperationalPortFieldsFragment => ({
  id,
  port: 41000,
  front: "socks",
  address: null,
  sessionId: "s1",
  sessionNumber: 3,
  taskId: id,
  openedBy: "u1",
  openedByName: "alice",
  openedAt: "2026-11-06T10:00:00Z",
  listening: true,
  open: 0,
  streams: 0,
  ...p,
})

const pages = (...ports: OperationalPortFieldsFragment[]): PortPages => ({
  pages: [
    {
      operationalPorts: {
        edges: ports.map((node) => ({ node, cursor: node.id })),
        pageInfo: { hasNextPage: false, endCursor: null },
        totalCount: ports.length,
      },
    },
  ],
  pageParams: [undefined],
})

describe("the operation's port events", () => {
  let qc: QueryClient
  const key = operationalPortKeys.list({ operationId: "op", search: "" })
  const rows = () => qc.getQueryData<PortPages>(key)!.pages[0].operationalPorts.edges.map((e) => e.node)

  beforeEach(() => {
    vi.useFakeTimers()
    qc = new QueryClient()
    qc.setQueryData(key, pages(port("a"), port("b")))
  })
  afterEach(() => vi.useRealTimers())

  it("patches a port whose streams changed, in place", () => {
    applyPortEvent(qc, { action: "UPDATED", id: "a", operationId: "op", port: port("a", { open: 2, streams: 7 }) })
    expect(rows().map((p) => [p.id, p.open, p.streams])).toEqual([
      ["a", 2, 7],
      ["b", 0, 0],
    ])
  })

  it("drops a port that closed", () => {
    applyPortEvent(qc, { action: "DELETED", id: "b", operationId: "op", port: null })
    expect(rows().map((p) => p.id)).toEqual(["a"])
  })

  it("reads the list again for a port that opened, to put it where it belongs", () => {
    const invalidate = vi.spyOn(qc, "invalidateQueries")
    applyPortEvent(qc, { action: "CREATED", id: "c", operationId: "op", port: port("c") })
    vi.runAllTimers()
    expect(invalidate).toHaveBeenCalled()
  })
})
