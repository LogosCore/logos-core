// Keeping paginated lists live without refetching them.
//
// Every list here is a TanStack infinite query over a GraphQL connection, and
// every change to its entity arrives as a subscription event carrying the
// row. Invalidating the lists on each event re-requested every loaded page of
// every open list — per viewer, per event — so an agent importing fifty
// findings meant fifty full refetches in every open tab. These helpers fold
// an event into the cache instead, and fall back to a refetch only where a
// local patch could leave a list wrong: a row entering or leaving a filter,
// or moving under a sort. Those refetches are coalesced, so a burst of events
// (or a mutation and its own echo) costs one.
import type { InfiniteData, QueryClient, QueryKey } from "@tanstack/react-query"

export interface Row {
  id: string
}

interface Connection {
  edges: ReadonlyArray<{ node: Row; cursor?: string }>
  totalCount?: number
}

type Page = Record<string, unknown>
type ListData = InfiniteData<Page>

// A list page's connection is its one root field with an `edges` array. The
// field name varies within an entity (`credentials`, `myCredentials`), which
// is why this looks rather than being told.
function connectionField(page: Page): string | undefined {
  return Object.keys(page).find((key) =>
    Array.isArray((page[key] as Connection | null | undefined)?.edges),
  )
}

// mapListRows passes every cached row through fn: a returned row replaces it,
// null removes it. A removal also comes off every page's totalCount, since
// each page carries its own copy and the UI reads the first. Returns data
// itself when nothing changed, so React Query keeps the reference and nothing
// re-renders.
export function mapListRows(
  data: ListData | undefined,
  fn: (row: Row) => Row | null,
): ListData | undefined {
  // Anything that is not an infinite query's data passes through untouched.
  if (!Array.isArray(data?.pages)) return data
  let changed = false
  let removed = 0
  const pages = data.pages.map((page) => {
    const field = connectionField(page)
    if (!field) return page
    const conn = page[field] as Connection
    let pageChanged = false
    const edges: Array<Connection["edges"][number]> = []
    for (const edge of conn.edges) {
      const next = fn(edge.node)
      if (next === edge.node) {
        edges.push(edge)
        continue
      }
      pageChanged = true
      if (next === null) {
        removed++
      } else {
        edges.push({ ...edge, node: next })
      }
    }
    if (!pageChanged) return page
    changed = true
    return { ...page, [field]: { ...conn, edges } }
  })
  if (!changed) return data
  if (removed === 0) return { ...data, pages }
  return {
    ...data,
    pages: pages.map((page) => {
      const field = connectionField(page)
      const conn = field ? (page[field] as Connection) : undefined
      if (!field || conn?.totalCount == null) return page
      return {
        ...page,
        [field]: { ...conn, totalCount: Math.max(0, conn.totalCount - removed) },
      }
    }),
  }
}

// mergeRow overlays an event's row on a cached one. Views select different
// fragments of one entity — a cross-operation list adds `operation`, a detail
// adds a linked record — and an event carries the base fragment only, so the
// fields it lacks are kept rather than dropped.
export function mergeRow<T extends object>(cached: T, update: object): T {
  return { ...cached, ...update }
}

// findCachedRow returns the row with this id from any page of any list under
// prefix — its state before the event being applied.
export function findCachedRow(
  queryClient: QueryClient,
  prefix: QueryKey,
  id: string,
): Row | undefined {
  for (const [, data] of queryClient.getQueriesData<ListData>({ queryKey: prefix })) {
    for (const page of data?.pages ?? []) {
      const field = connectionField(page)
      if (!field) continue
      const edge = (page[field] as Connection).edges.find((e) => e.node.id === id)
      if (edge) return edge.node
    }
  }
  return undefined
}

// How long refreshes wait to fold together. Short enough to read as immediate
// after a mutation, long enough to absorb an event burst.
export const REFRESH_WINDOW_MS = 300

const pendingRefreshes = new WeakMap<
  QueryClient,
  Map<string, { queryKey: QueryKey; exact: boolean }>
>()

// scheduleRefresh invalidates queryKey at the end of the current window,
// however many times it is asked for within it. The first request opens the
// window; later ones join it rather than postponing it, so a steady stream of
// events still refreshes every REFRESH_WINDOW_MS instead of never.
export function scheduleRefresh(
  queryClient: QueryClient,
  queryKey: QueryKey,
  { exact = false }: { exact?: boolean } = {},
): void {
  let pending = pendingRefreshes.get(queryClient)
  if (!pending) {
    pending = new Map()
    pendingRefreshes.set(queryClient, pending)
  }
  if (pending.size === 0) {
    const batch = pending
    setTimeout(() => {
      const due = [...batch.values()]
      batch.clear()
      const prefixes = due.filter((d) => !d.exact)
      for (const d of due) {
        // A key a broader prefix in the same batch already covers is skipped:
        // invalidating it too would cancel and restart that refetch.
        const covered = prefixes.some(
          (p) => p !== d && p.queryKey.length <= d.queryKey.length && startsWith(d.queryKey, p.queryKey),
        )
        if (!covered) void queryClient.invalidateQueries({ queryKey: d.queryKey, exact: d.exact })
      }
    }, REFRESH_WINDOW_MS)
  }
  pending.set(JSON.stringify([queryKey, exact]), { queryKey, exact })
}

function startsWith(key: QueryKey, prefix: QueryKey): boolean {
  return prefix.every((part, i) => JSON.stringify(part) === JSON.stringify(key[i]))
}

// LiveListSpec describes one entity's paginated lists to the helpers below.
export interface LiveListSpec<R extends Row> {
  // Prefix shared by every paginated list of the entity; each list's key ends
  // with its params object.
  lists: QueryKey
  // Whether a list with these params can gain, lose or reorder rows when a
  // row's fields change: any filter or search, or a sort on anything but the
  // creation time.
  sensitive: (params: unknown) => boolean
  // The row fields any filter, search or sort reads. An update that leaves
  // them alone cannot move a row between or within lists.
  membership: (row: R) => unknown
}

function paramsOf(queryKey: QueryKey): unknown {
  return queryKey[queryKey.length - 1]
}

// applyRowUpdate folds an updated row into every cached list that holds it.
// When a field some list filters or sorts on changed — or the row's previous
// state is unknown — the lists that depend on such fields are refreshed as
// well; the rest are trusted.
export function applyRowUpdate<R extends Row>(
  queryClient: QueryClient,
  spec: LiveListSpec<R>,
  row: R,
): void {
  const before = findCachedRow(queryClient, spec.lists, row.id) as R | undefined
  const moved =
    before === undefined ||
    JSON.stringify(spec.membership(before)) !== JSON.stringify(spec.membership(row))
  for (const [queryKey, data] of queryClient.getQueriesData<ListData>({
    queryKey: spec.lists,
  })) {
    if (moved && spec.sensitive(paramsOf(queryKey))) {
      scheduleRefresh(queryClient, queryKey, { exact: true })
    }
    const next = mapListRows(data, (r) => (r.id === row.id ? mergeRow(r, row) : r))
    if (next !== data) queryClient.setQueryData(queryKey, next)
  }
}

// applyRowRemoval drops a deleted row from every cached list at once, then
// refreshes them: a list that had not loaded the row still counts it.
export function applyRowRemoval<R extends Row>(
  queryClient: QueryClient,
  spec: LiveListSpec<R>,
  id: string,
): void {
  for (const [queryKey, data] of queryClient.getQueriesData<ListData>({
    queryKey: spec.lists,
  })) {
    const next = mapListRows(data, (r) => (r.id === id ? null : r))
    if (next !== data) queryClient.setQueryData(queryKey, next)
  }
  scheduleRefresh(queryClient, spec.lists)
}

// patchDetail merges an event's row into a cached detail query shaped
// `{ [field]: row }`. A detail nobody has loaded is left alone: the event may
// carry fewer fields than the detail query selects, and caching it would
// serve that partial row as fresh.
export function patchDetail(
  queryClient: QueryClient,
  queryKey: QueryKey,
  field: string,
  row: object,
): void {
  // Checked before writing: even an unchanged setQueryData stamps the query
  // as freshly fetched.
  const old = queryClient.getQueryData<Record<string, object | null>>(queryKey)
  const cached = old?.[field]
  if (!cached) return
  queryClient.setQueryData(queryKey, { ...old, [field]: mergeRow(cached, row) })
}
