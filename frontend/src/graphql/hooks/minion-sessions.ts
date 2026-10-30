import { useCallback, useEffect, useMemo, useRef } from "react"
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { mapListRows, mergeRow, scheduleRefresh } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  MinionSessionsDocument,
  MinionSessionChangedDocument,
  MinionSessionValuesDocument,
  MinionSessionDocument,
  MinionSessionsLayoutDocument,
  SetMinionSessionsLayoutDocument,
  SetMinionSessionAddressDocument,
  SetMinionSessionArchivedDocument,
  SetMinionSessionsArchivedDocument,
  SetMinionSessionDescriptionDocument,
  SetMinionSessionMarkDocument,
  SetMinionSessionsMarkDocument,
  type SetMinionSessionsMarkMutationVariables,
  type MinionSessionMarkInput,
  type SetMinionSessionsArchivedMutationVariables,
  type MinionSessionFieldsFragment,
  type MinionSessionsLayoutQuery,
  type MinionSessionsQuery,
} from "@/graphql/gql/graphql"
import {
  SESSION_COLUMN_BY_ID,
  dependentColumns,
  isClockFilter,
  readSessionsLayout,
  sessionsLayoutInput,
  type SessionFilter,
  type SessionSort,
  type SessionsLayout,
} from "@/components/minion-sessions/sessions-layout"

/** What a sessions list shows: the server searches, filters and sorts. */
export interface MinionSessionListParams {
  operationId: string
  /** The archived sessions instead of the others. */
  archived: boolean
  search: string | null
  filters: SessionFilter[]
  sort: SessionSort | null
}

export const minionSessionKeys = {
  all: ["minion-sessions"] as const,
  lists: () => [...minionSessionKeys.all, "list"] as const,
  infiniteList: (params: MinionSessionListParams) => [...minionSessionKeys.lists(), params] as const,
  values: (operationId: string, column: string) => [...minionSessionKeys.all, "values", operationId, column] as const,
  layout: () => [...minionSessionKeys.all, "layout"] as const,
  detail: (id: string) => [...minionSessionKeys.all, "detail", id] as const,
}

// How often a list with a filter on the clock ("seen within 5m") refetches:
// rows leave it as time passes, with no event to say so.
const CLOCK_REFETCH_MS = 30_000

/** An operation's sessions as params ask, a page at a time as the table scrolls. */
export function useInfiniteMinionSessions(params: MinionSessionListParams, options?: { enabled?: boolean }) {
  return useInfiniteQuery({
    queryKey: minionSessionKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionSessionsDocument, {
        operationId: params.operationId,
        archived: params.archived,
        search: params.search,
        filters: params.filters,
        sort: params.sort,
        first: 50,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.minionSessions.pageInfo.hasNextPage
        ? (lastPage.minionSessions.pageInfo.endCursor ?? undefined)
        : undefined,
    enabled: params.operationId !== "" && (options?.enabled ?? true),
    refetchInterval: params.filters.some(isClockFilter) ? CLOCK_REFETCH_MS : false,
  })
}

/** The distinct values of an enum column, for its filter. */
export function useMinionSessionValues(operationId: string, column: string, enabled: boolean) {
  return useQuery({
    queryKey: minionSessionKeys.values(operationId, column),
    queryFn: () => graphqlClient(MinionSessionValuesDocument, { operationId, column }),
    enabled: enabled && operationId !== "",
    staleTime: 30_000,
  })
}

// A layout change waits this long for the next one before it is saved: a
// drag or a few quick clicks make one write.
const SAVE_DELAY_MS = 600

/**
 * The caller's Sessions table layout, kept in core for all their operations
 * and browsers. Changes show at once and are saved shortly after; a failed
 * save reloads what core has.
 */
export function useMinionSessionsLayout() {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: minionSessionKeys.layout(),
    queryFn: () => graphqlClient(MinionSessionsLayoutDocument),
    staleTime: Infinity,
  })
  const layout = useMemo(() => readSessionsLayout(query.data?.minionSessionsLayout), [query.data])
  const save = useMutation({
    mutationFn: (next: SessionsLayout | null) =>
      graphqlClient(SetMinionSessionsLayoutDocument, { layout: next ? sessionsLayoutInput(next) : null }),
    onError: () => queryClient.invalidateQueries({ queryKey: minionSessionKeys.layout() }),
  })

  const pending = useRef<{ layout: SessionsLayout | null } | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const { mutate } = save
  const flush = useCallback(() => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = null
    if (pending.current) {
      mutate(pending.current.layout)
      pending.current = null
    }
  }, [mutate])
  // A change still waiting is saved when the page goes.
  useEffect(() => flush, [flush])

  const setLayout = useCallback(
    (next: SessionsLayout | null) => {
      queryClient.setQueryData<MinionSessionsLayoutQuery>(minionSessionKeys.layout(), {
        minionSessionsLayout: next as MinionSessionsLayoutQuery["minionSessionsLayout"],
      })
      pending.current = { layout: next }
      if (timer.current) clearTimeout(timer.current)
      timer.current = setTimeout(flush, SAVE_DELAY_MS)
    },
    [queryClient, flush],
  )

  return { layout, isLoading: query.isLoading, setLayout }
}

type ListData = InfiniteData<MinionSessionsQuery>

// The columns the app's search reads (core's minionSessionListFilter).
const SEARCHED = ["peer", "host", "user", "domain", "proc", "os", "arch", "addrs", "version", "description", "buildDescription", "build", "target", "platform"] as const

/**
 * Folds one session change into every cached list. A new session refetches
 * (it lands wherever the sort puts it). A changed one patches its row in
 * place unless the change moves it in or out of a list, or within it: then
 * that list alone refetches. Which changes move it depends on the list's
 * filters, sort and search; a check-in moves rows only in a list filtered or
 * sorted by last seen.
 */
export function applyMinionSessionEvent(
  queryClient: QueryClient,
  action: string,
  session: MinionSessionFieldsFragment | null | undefined,
) {
  if (action === "CREATED" || !session) {
    scheduleRefresh(queryClient, minionSessionKeys.lists())
    return
  }
  // The session's own query (its Details), where loaded.
  if (queryClient.getQueryData(minionSessionKeys.detail(session.id))) {
    queryClient.setQueryData(minionSessionKeys.detail(session.id), { minionSession: session })
  }
  for (const [key, data] of queryClient.getQueriesData<ListData>({ queryKey: minionSessionKeys.lists() })) {
    if (!data) continue
    const params = key[key.length - 1] as MinionSessionListParams
    const before = data.pages.flatMap((p) => p.minionSessions.edges).find((e) => e.node.id === session.id)?.node
    // Archiving or restoring moves the row between the two lists.
    if (before && before.archived !== session.archived) {
      scheduleRefresh(queryClient, minionSessionKeys.lists())
      continue
    }
    const narrowed = params.archived || params.search !== null || params.filters.length > 0 || params.sort !== null
    if (!before) {
      // Not loaded here: it may belong now, unless the list is every
      // session newest first, which nothing but a new session changes.
      if (narrowed) scheduleRefresh(queryClient, key, { exact: true })
      continue
    }
    const columns = [...dependentColumns(params), ...(params.search ? SEARCHED : [])]
    const moved = columns.some((id) => {
      const value = SESSION_COLUMN_BY_ID.get(id)!.value
      return JSON.stringify(value(before)) !== JSON.stringify(value(session))
    })
    if (moved) {
      scheduleRefresh(queryClient, key, { exact: true })
    } else {
      const next = mapListRows(data as never, (r) => (r.id === session.id ? mergeRow(r, session) : r))
      if (next !== (data as never)) queryClient.setQueryData(key, next)
    }
  }
}

/** Keeps the sessions table live: see applyMinionSessionEvent. */
export function useMinionSessionChangedSubscription(operationId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    MinionSessionChangedDocument,
    { operationId },
    {
      onData: (data) => {
        const { action, session } = data.minionSessionChanged
        applyMinionSessionEvent(queryClient, action, session)
      },
      enabled: operationId !== "",
    },
  )
}

/** Picks the address a session shows, for the whole operation; null goes back to core's guess. */
export function useSetMinionSessionAddress() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; address: string | null }) => graphqlClient(SetMinionSessionAddressDocument, vars),
    onSuccess: (data) => applyMinionSessionEvent(queryClient, "UPDATED", data.setMinionSessionAddress),
  })
}

/** One session (closed ones too). */
export function useMinionSession(id: string) {
  return useQuery({
    queryKey: minionSessionKeys.detail(id),
    queryFn: () => graphqlClient(MinionSessionDocument, { id }),
    enabled: id !== "",
  })
}

/** Archives a session or restores it, for the whole operation. */
export function useSetMinionSessionArchived() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; archived: boolean }) => graphqlClient(SetMinionSessionArchivedDocument, vars),
    onSuccess: (data) => applyMinionSessionEvent(queryClient, "UPDATED", data.setMinionSessionArchived),
  })
}

/**
 * Archives or restores many sessions at once: by ids, or every one a list's
 * view and filters match. The lists refetch; returns how many changed.
 */
export function useSetMinionSessionsArchived() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: SetMinionSessionsArchivedMutationVariables) => graphqlClient(SetMinionSessionsArchivedDocument, vars),
    onSuccess: () => scheduleRefresh(queryClient, minionSessionKeys.lists()),
  })
}

/** Replaces a session's description, for the whole operation. */
export function useSetMinionSessionDescription() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; description: string }) => graphqlClient(SetMinionSessionDescriptionDocument, vars),
    onSuccess: (data) => applyMinionSessionEvent(queryClient, "UPDATED", data.setMinionSessionDescription),
  })
}

/** Marks a session for the caller alone (null clears their mark). */
export function useSetMinionSessionMark() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; mark: MinionSessionMarkInput | null }) => graphqlClient(SetMinionSessionMarkDocument, vars),
    onSuccess: (data) => applyMinionSessionEvent(queryClient, "UPDATED", data.setMinionSessionMark),
  })
}

/** Marks many sessions alike for the caller: by ids, or all a list matches. */
export function useSetMinionSessionsMark() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: SetMinionSessionsMarkMutationVariables) => graphqlClient(SetMinionSessionsMarkDocument, vars),
    onSuccess: () => scheduleRefresh(queryClient, minionSessionKeys.lists()),
  })
}
