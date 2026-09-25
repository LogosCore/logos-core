import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"
import { QueryClient } from "@tanstack/react-query"
import {
  applyRowRemoval,
  applyRowUpdate,
  mapListRows,
  patchDetail,
  REFRESH_WINDOW_MS,
  scheduleRefresh,
  type LiveListSpec,
} from "./live-lists"

interface Thing {
  id: string
  name: string
  note?: string
  operation?: { id: string; name: string }
}

interface ThingParams {
  search?: string | null
}

const lists = ["things", "list"] as const
const listKey = (params: ThingParams) => [...lists, "infinite", params] as const

const spec: LiveListSpec<Thing> = {
  lists,
  sensitive: (params) => !!(params as ThingParams).search,
  membership: (t) => [t.name],
}

// Two pages of a connection, as useInfiniteQuery caches them.
function pages(...perPage: Thing[][]) {
  const total = perPage.flat().length
  return {
    pageParams: perPage.map((_, i) => (i === 0 ? undefined : `c${i}`)),
    pages: perPage.map((rows) => ({
      things: {
        edges: rows.map((node) => ({ node, cursor: `c-${node.id}` })),
        totalCount: total,
      },
    })),
  }
}

type Cached = ReturnType<typeof pages>

function rowsOf(data: Cached | undefined) {
  return data?.pages.flatMap((p) => p.things.edges.map((e) => e.node)) ?? []
}

let qc: QueryClient
let invalidate: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  vi.useFakeTimers()
  qc = new QueryClient()
  invalidate = vi.spyOn(qc, "invalidateQueries")
})

afterEach(() => {
  vi.useRealTimers()
})

describe("mapListRows", () => {
  test("returns the same data when no row changes, so nothing re-renders", () => {
    const data = pages([{ id: "a", name: "A" }])
    expect(mapListRows(data, (r) => r)).toBe(data)
  })

  test("a removal comes off every page's totalCount", () => {
    const data = pages([{ id: "a", name: "A" }], [{ id: "b", name: "B" }])
    const next = mapListRows(data, (r) => (r.id === "b" ? null : r)) as Cached
    expect(rowsOf(next).map((r) => r.id)).toEqual(["a"])
    expect(next.pages.map((p) => p.things.totalCount)).toEqual([1, 1])
    // The untouched first page keeps its edges, only its total moves.
    expect(next.pages[0].things.edges).toBe(data.pages[0].things.edges)
  })
})

describe("applyRowUpdate", () => {
  test("patches an unfiltered list in place without refetching it", () => {
    qc.setQueryData(listKey({}), pages([{ id: "a", name: "A" }]))
    applyRowUpdate(qc, spec, { id: "a", name: "Renamed" })
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)

    expect(rowsOf(qc.getQueryData(listKey({})))[0].name).toBe("Renamed")
    expect(invalidate).not.toHaveBeenCalled()
  })

  test("keeps the fields a view has that the event lacks", () => {
    const operation = { id: "op", name: "Op" }
    qc.setQueryData(listKey({}), pages([{ id: "a", name: "A", operation }]))
    applyRowUpdate(qc, spec, { id: "a", name: "A", note: "new" })

    const row = rowsOf(qc.getQueryData(listKey({})))[0]
    expect(row).toEqual({ id: "a", name: "A", note: "new", operation })
  })

  test("trusts a filtered list when no field it filters on changed", () => {
    qc.setQueryData(listKey({ search: "a" }), pages([{ id: "a", name: "A" }]))
    applyRowUpdate(qc, spec, { id: "a", name: "A", note: "commented" })
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)

    expect(rowsOf(qc.getQueryData(listKey({ search: "a" })))[0].note).toBe("commented")
    expect(invalidate).not.toHaveBeenCalled()
  })

  test("refreshes a filtered list, and only it, when a filtered field changed", () => {
    qc.setQueryData(listKey({}), pages([{ id: "a", name: "A" }]))
    qc.setQueryData(listKey({ search: "a" }), pages([{ id: "a", name: "A" }]))
    applyRowUpdate(qc, spec, { id: "a", name: "Zed" })
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)

    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: listKey({ search: "a" }), exact: true })
    // The row still shows its new name until the refetch lands.
    expect(rowsOf(qc.getQueryData(listKey({ search: "a" })))[0].name).toBe("Zed")
  })

  test("refreshes filtered lists when the row's previous state is unknown", () => {
    // A row entering a filter from outside every loaded page.
    qc.setQueryData(listKey({ search: "z" }), pages([{ id: "b", name: "Bee" }]))
    applyRowUpdate(qc, spec, { id: "new", name: "Zed" })
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)

    expect(invalidate).toHaveBeenCalledWith({ queryKey: listKey({ search: "z" }), exact: true })
  })
})

describe("applyRowRemoval", () => {
  test("drops the row at once and refreshes every list", () => {
    qc.setQueryData(listKey({}), pages([{ id: "a", name: "A" }, { id: "b", name: "B" }]))
    applyRowRemoval(qc, spec, "a")

    expect(rowsOf(qc.getQueryData(listKey({}))).map((r) => r.id)).toEqual(["b"])
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: lists, exact: false })
  })
})

describe("scheduleRefresh", () => {
  test("folds a burst into one refetch per key", () => {
    for (let i = 0; i < 50; i++) scheduleRefresh(qc, lists)
    scheduleRefresh(qc, ["other"])
    vi.advanceTimersByTime(REFRESH_WINDOW_MS - 1)
    expect(invalidate).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(invalidate).toHaveBeenCalledTimes(2)
  })

  test("a key a scheduled prefix covers is not refetched twice", () => {
    // A kanban drag refreshes the lists prefix while its echo refreshes the
    // exact columns; invalidating both would cancel and restart the fetch.
    scheduleRefresh(qc, listKey({ search: "a" }), { exact: true })
    scheduleRefresh(qc, lists)
    vi.advanceTimersByTime(REFRESH_WINDOW_MS)

    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: lists, exact: false })
  })

  test("a steady stream still refreshes once per window", () => {
    // Joining a window must not postpone it, or a busy stream never lands.
    for (let t = 0; t < 3 * REFRESH_WINDOW_MS; t += 50) {
      scheduleRefresh(qc, lists)
      vi.advanceTimersByTime(50)
    }
    expect(invalidate).toHaveBeenCalledTimes(3)
  })
})

describe("patchDetail", () => {
  test("merges into a loaded detail", () => {
    qc.setQueryData(["thing", "a"], { thing: { id: "a", name: "A", note: "keep" } })
    patchDetail(qc, ["thing", "a"], "thing", { id: "a", name: "B" })
    expect(qc.getQueryData(["thing", "a"])).toEqual({
      thing: { id: "a", name: "B", note: "keep" },
    })
  })

  test("leaves a detail nobody loaded alone", () => {
    patchDetail(qc, ["thing", "b"], "thing", { id: "b", name: "B" })
    expect(qc.getQueryData(["thing", "b"])).toBeUndefined()
  })
})
