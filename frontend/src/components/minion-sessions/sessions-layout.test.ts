import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"
import { QueryClient } from "@tanstack/react-query"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { applyMinionSessionEvent, minionSessionKeys, type MinionSessionListParams } from "@/graphql/hooks/minion-sessions"
import { REFRESH_WINDOW_MS } from "@/lib/live-lists"
import {
  DEFAULT_SESSIONS_LAYOUT,
  SESSION_COLUMN_BY_ID,
  addressMatcher,
  formatAgo,
  formatAgoExact,
  isClockFilter,
  lateness,
  shownAddress,
  cycleSort,
  describeFilter,
  isComplete,
  moveColumn,
  readSessionsLayout,
  setFilter,
  shiftColumn,
  toggleColumn,
  type SessionsLayout,
} from "./sessions-layout"

describe("layout", () => {
  test("a stored layout drops what this build does not know", () => {
    const layout = readSessionsLayout({
      columns: ["host", "gone", "host", "lastSeen"],
      sort: { column: "buildId", direction: "ASC" }, // ids do not sort
      filters: [
        { column: "os", op: "IN", values: ["linux"], text: null, min: null, max: null, seconds: null, from: null, to: null },
        { column: "gone", op: "EMPTY", values: null, text: null, min: null, max: null, seconds: null, from: null, to: null },
      ],
    })
    expect(layout.columns).toEqual(["host", "lastSeen"])
    expect(layout.sort).toBeNull()
    expect(layout.filters).toEqual([{ column: "os", op: "IN", values: ["linux"] }])
    expect(readSessionsLayout(null)).toBe(DEFAULT_SESSIONS_LAYOUT)
    expect(readSessionsLayout({ columns: ["gone"], sort: null, filters: [], split: null }).columns).toEqual(DEFAULT_SESSIONS_LAYOUT.columns)
  })

  test("a stored split keeps to its range", () => {
    expect(readSessionsLayout({ columns: ["host"], sort: null, filters: [], split: 0.02 }).split).toBe(0.1)
    expect(readSessionsLayout({ columns: ["host"], sort: null, filters: [], split: 0.45 }).split).toBe(0.45)
  })

  test("columns move, show and hide; the last one stays", () => {
    const l: SessionsLayout = { columns: ["number", "host", "os"], sort: null, filters: [], split: null }
    expect(moveColumn(l, "os", "number").columns).toEqual(["os", "number", "host"])
    expect(shiftColumn(l, "host", 1).columns).toEqual(["number", "os", "host"])
    expect(shiftColumn(l, "number", -1)).toBe(l)
    expect(toggleColumn(l, "pid").columns).toEqual(["number", "host", "os", "pid"])
    expect(toggleColumn(l, "pid", "host").columns).toEqual(["number", "host", "pid", "os"])
    expect(toggleColumn(l, "pid", "os").columns).toEqual(["number", "host", "os", "pid"])
    expect(toggleColumn(l, "host").columns).toEqual(["number", "os"])
    const one: SessionsLayout = { columns: ["host"], sort: null, filters: [], split: null }
    expect(toggleColumn(one, "host")).toBe(one)
  })

  test("a header click sorts its first way, then the other, then back to newest first", () => {
    let l = DEFAULT_SESSIONS_LAYOUT
    l = cycleSort(l, "lastSeen")
    expect(l.sort).toEqual({ column: "lastSeen", direction: "DESC" })
    l = cycleSort(l, "lastSeen")
    expect(l.sort).toEqual({ column: "lastSeen", direction: "ASC" })
    expect(cycleSort(l, "lastSeen").sort).toBeNull()
    expect(cycleSort(l, "host").sort).toEqual({ column: "host", direction: "ASC" })
    expect(cycleSort(l, "buildId")).toBe(l)
  })

  test("one filter per column", () => {
    let l = setFilter(DEFAULT_SESSIONS_LAYOUT, "host", { column: "host", op: "CONTAINS", text: "web" })
    l = setFilter(l, "host", { column: "host", op: "EMPTY", text: null })
    expect(l.filters).toEqual([{ column: "host", op: "EMPTY" }])
    expect(setFilter(l, "host", null).filters).toEqual([])
  })

  test("filters read in words and know when they are complete", () => {
    expect(describeFilter({ column: "host", op: "CONTAINS", text: "web" })).toBe('contains "web"')
    expect(describeFilter({ column: "os", op: "NOT_IN", values: ["linux", "windows"] })).toBe("is not Linux, Windows")
    expect(describeFilter({ column: "staged", op: "IN", values: ["false"] })).toBe("is not staged")
    expect(describeFilter({ column: "pid", op: "RANGE", min: 5, max: 20 })).toBe("5–20")
    expect(describeFilter({ column: "pid", op: "RANGE", min: 5 })).toBe("≥ 5")
    expect(describeFilter({ column: "lastSeen", op: "WITHIN", seconds: 300 })).toBe("within 5m")
    expect(describeFilter({ column: "lastSeen", op: "OLDER_THAN", seconds: 86400 })).toBe("over 1d ago")
    const col = (id: string) => SESSION_COLUMN_BY_ID.get(id)!
    expect(isComplete({ column: "host", op: "CONTAINS", text: "" }, col("host"))).toBe(false)
    expect(isComplete({ column: "buildId", op: "EQUALS", text: "nope" }, col("buildId"))).toBe(false)
    expect(isComplete({ column: "buildId", op: "EQUALS", text: "160a41b2-e202-4841-9766-5dedfb861947" }, col("buildId"))).toBe(true)
    expect(isComplete({ column: "lastSeen", op: "NEVER" }, col("lastSeen"))).toBe(true)
    expect(isComplete({ column: "pid", op: "RANGE" }, col("pid"))).toBe(false)
  })
})

describe("formatAgo", () => {
  test("one unit: seconds under a minute, minutes under an hour, then hours", () => {
    expect(formatAgo(0)).toBe("just now")
    expect(formatAgo(-500)).toBe("just now")
    expect(formatAgo(3_999)).toBe("just now")
    expect(formatAgo(4_000)).toBe("4s ago")
    expect(formatAgo(59_999)).toBe("59s ago")
    expect(formatAgo(245_000)).toBe("4m ago")
    expect(formatAgo(3 * 3600_000 + 12 * 60_000)).toBe("3h ago")
    expect(formatAgo(86400_000)).toBeNull()
  })
  test("the hover's exact one", () => {
    expect(formatAgoExact(47_000)).toBe("47s ago")
    expect(formatAgoExact(245_000)).toBe("4m 05s ago")
    expect(formatAgoExact(3 * 3600_000 + 12 * 60_000 + 9_000)).toBe("3h 12m 09s ago")
    expect(formatAgoExact(2 * 86400_000 + 3 * 3600_000)).toBe("2d 3h ago")
  })
})

describe("lateness", () => {
  test("on time before late_at, late after, lost past lost_at, unknown without a schedule", () => {
    const at = Date.parse("2026-10-30T10:00:00Z")
    const s = { lateAt: "2026-10-30T10:00:00Z", lostAt: "2026-10-30T10:05:00Z" }
    expect(lateness(s, at - 1)).toBe("live")
    expect(lateness(s, at)).toBe("late")
    expect(lateness(s, at + 300_000)).toBe("lost")
    expect(lateness({ lateAt: "2026-10-30T10:00:00Z", lostAt: null }, at + 3600_000)).toBe("late")
    expect(lateness({ lateAt: null, lostAt: null }, at)).toBeNull()
  })
  test("live and late filters read and refresh with the clock", () => {
    expect(describeFilter({ column: "lastSeen", op: "LATE" })).toBe("late")
    expect(isClockFilter({ column: "lastSeen", op: "LIVE" })).toBe(true)
    expect(describeFilter({ column: "lastSeen", op: "LOST" })).toBe("lost")
  })
})

describe("the shown address", () => {
  const row = { addrs: ["172.17.0.1", "10.0.0.5", "192.168.1.9"], address: "10.0.0.5", addressPicked: false }
  test("core's guess, unless a filter or the search matched another, unless someone picked one", () => {
    expect(shownAddress(row, addressMatcher([], null))).toBe("10.0.0.5")
    expect(shownAddress(row, addressMatcher([{ column: "addrs", op: "CONTAINS", text: "192.168" }], null))).toBe("192.168.1.9")
    expect(shownAddress(row, addressMatcher([], '"172.17"'))).toBe("172.17.0.1")
    expect(shownAddress(row, addressMatcher([{ column: "host", op: "CONTAINS", text: "192" }], "nothing"))).toBe("10.0.0.5")
    const picked = { ...row, address: "172.17.0.1", addressPicked: true }
    expect(shownAddress(picked, addressMatcher([{ column: "addrs", op: "CONTAINS", text: "192.168" }], null))).toBe("172.17.0.1")
    expect(shownAddress({ addrs: [], address: null, addressPicked: false }, addressMatcher([], null))).toBeNull()
  })
})

describe("live updates", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  const session = (id: string, patch: Partial<MinionSessionFieldsFragment> = {}): MinionSessionFieldsFragment => ({
    id,
    state: "OPEN",
    archived: false,
    mark: null,
    number: 1,
    operationId: "op",
    buildId: "b",
    buildDescription: "",
    description: "",
    moduleName: "bob",
    target: "linux-elf",
    platform: "linux/amd64",
    staged: true,
    pid: 1,
    os: "linux",
    arch: "amd64",
    host: "web01",
    user: null,
    domain: null,
    proc: null,
    integrity: null,
    addrs: [],
    address: null,
    addressPicked: false,
    peerAddress: null,
    peers: [],
    sleepInterval: null,
    sleepJitter: null,
    lateAt: null,
    lostAt: null,
    persisted: false,
    restartCount: 0,
    restarts: [],
    version: null,
    x: null,
    createdAt: "2026-10-30T10:00:00Z",
    lastSeenAt: "2026-10-30T10:00:00Z",
    ...patch,
  })
  const listData = (...rows: MinionSessionFieldsFragment[]) => ({
    pageParams: [undefined],
    pages: [
      {
        minionSessions: {
          edges: rows.map((node) => ({ node, cursor: node.id })),
          pageInfo: { hasNextPage: false, endCursor: null },
          totalCount: rows.length,
        },
      },
    ],
  })
  const params = (patch: Partial<MinionSessionListParams> = {}): MinionSessionListParams => ({
    operationId: "op",
    archived: false,
    search: null,
    filters: [],
    sort: null,
    ...patch,
  })

  test("a check-in patches rows in place unless the list sorts or filters by last seen", () => {
    const qc = new QueryClient()
    const plain = minionSessionKeys.infiniteList(params())
    const byLastSeen = minionSessionKeys.infiniteList(params({ sort: { column: "lastSeen", direction: "DESC" } }))
    const byOS = minionSessionKeys.infiniteList(params({ filters: [{ column: "os", op: "IN", values: ["linux"] }] }))
    for (const key of [plain, byLastSeen, byOS]) qc.setQueryData(key, listData(session("a")))
    const invalidate = vi.spyOn(qc, "invalidateQueries")

    const checkedIn = session("a", { lastSeenAt: "2026-10-30T10:00:05Z" })
    applyMinionSessionEvent(qc, "UPDATED", checkedIn)
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)

    for (const key of [plain, byOS]) {
      const data = qc.getQueryData<ReturnType<typeof listData>>(key)!
      expect(data.pages[0].minionSessions.edges[0].node.lastSeenAt).toBe("2026-10-30T10:00:05Z")
    }
    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: byLastSeen, exact: true })
  })

  test("a check-in from a new external address moves rows only where the list depends on it", () => {
    const qc = new QueryClient()
    const plain = minionSessionKeys.infiniteList(params())
    const byPeer = minionSessionKeys.infiniteList(params({ sort: { column: "peer", direction: "ASC" } }))
    const seen = { address: "203.0.113.7", firstSeen: "2026-10-30T10:00:00Z", lastSeen: "2026-10-30T10:00:00Z", count: 1 }
    const row = session("a", { peerAddress: "203.0.113.7", peers: [seen] })
    for (const key of [plain, byPeer]) qc.setQueryData(key, listData(row))
    const invalidate = vi.spyOn(qc, "invalidateQueries")
    // The same address again: counted, nothing moves.
    applyMinionSessionEvent(qc, "UPDATED", { ...row, peers: [{ ...seen, count: 2 }] })
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)
    expect(invalidate).not.toHaveBeenCalled()
    // Another one: the list sorted by it refetches.
    applyMinionSessionEvent(qc, "UPDATED", { ...row, peerAddress: "198.51.100.2", peers: [{ ...seen, address: "198.51.100.2" }, seen] })
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)
    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: byPeer, exact: true })
  })

  test("a session not loaded refetches only lists it may now belong to", () => {
    const qc = new QueryClient()
    const plain = minionSessionKeys.infiniteList(params())
    const filtered = minionSessionKeys.infiniteList(params({ filters: [{ column: "os", op: "IN", values: ["linux"] }] }))
    qc.setQueryData(plain, listData(session("a")))
    qc.setQueryData(filtered, listData(session("a")))
    const invalidate = vi.spyOn(qc, "invalidateQueries")
    applyMinionSessionEvent(qc, "UPDATED", session("b", { os: "linux" }))
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)
    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: filtered, exact: true })
  })

  test("archiving or restoring refetches the lists: the row moves between them", () => {
    const qc = new QueryClient()
    qc.setQueryData(minionSessionKeys.infiniteList(params()), listData(session("a")))
    const invalidate = vi.spyOn(qc, "invalidateQueries")
    applyMinionSessionEvent(qc, "UPDATED", session("a", { archived: true }))
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: minionSessionKeys.lists(), exact: false })
  })

  test("a new session refetches the lists", () => {
    const qc = new QueryClient()
    qc.setQueryData(minionSessionKeys.infiniteList(params()), listData(session("a")))
    const invalidate = vi.spyOn(qc, "invalidateQueries")
    applyMinionSessionEvent(qc, "CREATED", session("b"))
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: minionSessionKeys.lists(), exact: false })
  })
})
