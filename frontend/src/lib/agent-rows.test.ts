import { describe, expect, it } from "vitest"
import {
  buildAgentRows,
  countAgents,
  filterAgentRows,
  sortAgentRows,
  type AgentActivityInput,
  type AgentKeyInput,
} from "./agent-rows"

function key(overrides: Partial<AgentKeyInput> = {}): AgentKeyInput {
  return {
    id: "key-1",
    keyId: "abc123",
    name: "Claude — Nightfall",
    enabled: true,
    maxRole: "OPERATOR",
    allowWrites: true,
    operationScopes: [],
    lastUsedAt: null,
    createdAt: "2026-09-01T10:00:00Z",
    ...overrides,
  }
}

function activity(
  overrides: Partial<AgentActivityInput> = {},
): AgentActivityInput {
  return {
    agentKeyId: "key-1",
    actions: 42,
    operations: 2,
    lastSeen: "2026-09-20T10:00:00Z",
    ...overrides,
  }
}

describe("buildAgentRows", () => {
  it("joins a key to its activity", () => {
    const [row] = buildAgentRows([key()], [activity()])
    expect(row.actions).toBe(42)
    expect(row.operationsTouched).toBe(2)
    expect(row.lastSeen).toBe("2026-09-20T10:00:00Z")
    expect(row.neverUsed).toBe(false)
  })

  it("reports a key with no activity as never used", () => {
    const [row] = buildAgentRows([key()], [])
    expect(row.actions).toBe(0)
    expect(row.operationsTouched).toBe(0)
    expect(row.lastSeen).toBeNull()
    expect(row.neverUsed).toBe(true)
  })

  it("takes the later of lastUsedAt and lastSeen", () => {
    const [authenticatedSince] = buildAgentRows(
      [key({ lastUsedAt: "2026-09-25T10:00:00Z" })],
      [activity({ lastSeen: "2026-09-20T10:00:00Z" })],
    )
    expect(authenticatedSince.lastSeen).toBe("2026-09-25T10:00:00Z")

    const [calledSince] = buildAgentRows(
      [key({ lastUsedAt: "2026-09-10T10:00:00Z" })],
      [activity({ lastSeen: "2026-09-20T10:00:00Z" })],
    )
    expect(calledSince.lastSeen).toBe("2026-09-20T10:00:00Z")
  })

  it("counts a key that authenticated but called nothing as used", () => {
    const [row] = buildAgentRows([key({ lastUsedAt: "2026-09-25T10:00:00Z" })], [])
    expect(row.neverUsed).toBe(false)
    expect(row.actions).toBe(0)
  })

  it("ignores activity whose key is gone", () => {
    const rows = buildAgentRows([key()], [activity({ agentKeyId: "deleted" })])
    expect(rows).toHaveLength(1)
    expect(rows[0].actions).toBe(0)
  })

  // The create form warns about this combination; the row must not then render
  // a badge claiming the write access the server will refuse.
  it("does not treat a viewer-capped key as writable", () => {
    const [viewer] = buildAgentRows(
      [key({ maxRole: "VIEWER", allowWrites: true })],
      [],
    )
    expect(viewer.canWrite).toBe(false)

    const [operator] = buildAgentRows(
      [key({ maxRole: "OPERATOR", allowWrites: true })],
      [],
    )
    expect(operator.canWrite).toBe(true)
  })
})

describe("filterAgentRows", () => {
  const rows = buildAgentRows(
    [
      key({ id: "a", name: "Claude — Nightfall", keyId: "aaa111" }),
      key({
        id: "b",
        name: "Recon bot",
        keyId: "bbb222",
        enabled: false,
        allowWrites: false,
        operationScopes: [{ id: "op-1", name: "Operation Tidewater" }],
      }),
      key({ id: "c", name: "Read-only auditor", maxRole: "VIEWER", keyId: "ccc333" }),
    ],
    [activity({ agentKeyId: "a" })],
  )

  it("matches on name, key id and scoped operation name", () => {
    expect(filterAgentRows(rows, "nightfall", null).map((r) => r.id)).toEqual(["a"])
    expect(filterAgentRows(rows, "bbb", null).map((r) => r.id)).toEqual(["b"])
    expect(filterAgentRows(rows, "tidewater", null).map((r) => r.id)).toEqual(["b"])
  })

  it("filters by status", () => {
    expect(filterAgentRows(rows, "", "paused").map((r) => r.id)).toEqual(["b"])
    expect(filterAgentRows(rows, "", "active").map((r) => r.id)).toEqual(["a", "c"])
    expect(filterAgentRows(rows, "", "writable").map((r) => r.id)).toEqual(["a"])
    expect(filterAgentRows(rows, "", "readonly").map((r) => r.id)).toEqual(["b", "c"])
    expect(filterAgentRows(rows, "", "unused").map((r) => r.id)).toEqual(["b", "c"])
  })

  it("applies search and status together", () => {
    expect(filterAgentRows(rows, "o", "paused").map((r) => r.id)).toEqual(["b"])
  })

  it("returns everything with no query and no status", () => {
    expect(filterAgentRows(rows, "  ", null)).toHaveLength(3)
  })
})

describe("sortAgentRows", () => {
  const rows = buildAgentRows(
    [
      key({ id: "a", name: "Alpha", createdAt: "2026-09-01T00:00:00Z" }),
      key({ id: "b", name: "Bravo", createdAt: "2026-09-03T00:00:00Z" }),
      key({ id: "c", name: "Charlie", createdAt: "2026-09-02T00:00:00Z" }),
    ],
    [
      activity({ agentKeyId: "a", lastSeen: "2026-09-10T00:00:00Z" }),
      activity({ agentKeyId: "b", lastSeen: "2026-09-20T00:00:00Z" }),
    ],
  )

  it("sorts by name", () => {
    expect(
      sortAgentRows(rows, { field: "NAME", direction: "ASC" }).map((r) => r.name),
    ).toEqual(["Alpha", "Bravo", "Charlie"])
    expect(
      sortAgentRows(rows, { field: "NAME", direction: "DESC" }).map((r) => r.name),
    ).toEqual(["Charlie", "Bravo", "Alpha"])
  })

  it("sorts by created", () => {
    expect(
      sortAgentRows(rows, { field: "CREATED", direction: "DESC" }).map((r) => r.id),
    ).toEqual(["b", "c", "a"])
  })

  it("puts never-used keys last whichever way last seen is sorted", () => {
    expect(
      sortAgentRows(rows, { field: "LAST_SEEN", direction: "DESC" }).map((r) => r.id),
    ).toEqual(["b", "a", "c"])
    expect(
      sortAgentRows(rows, { field: "LAST_SEEN", direction: "ASC" }).map((r) => r.id),
    ).toEqual(["a", "b", "c"])
  })

  it("does not mutate its input", () => {
    const before = rows.map((r) => r.id)
    sortAgentRows(rows, { field: "NAME", direction: "DESC" })
    expect(rows.map((r) => r.id)).toEqual(before)
  })
})

describe("countAgents", () => {
  it("counts by state", () => {
    const rows = buildAgentRows(
      [
        key({ id: "a" }),
        key({ id: "b", enabled: false }),
        key({ id: "c" }),
      ],
      [activity({ agentKeyId: "a" })],
    )
    expect(countAgents(rows)).toEqual({
      total: 3,
      active: 2,
      paused: 1,
      unused: 2,
    })
  })
})
