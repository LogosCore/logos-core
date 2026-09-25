import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  applyRowRemoval,
  applyRowUpdate,
  mergeRow,
  patchDetail,
  scheduleRefresh,
  type LiveListSpec,
} from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import type {
  CreateHostInput,
  HostFieldsFragment,
  HostSortField,
  SortDirection,
  UpdateHostInput,
} from "@/graphql/gql/graphql"
import {
  HostsDocument,
  HostDocument,
  CreateHostDocument,
  UpdateHostDocument,
  DeleteHostDocument,
  HostChangedDocument,
} from "@/graphql/gql/graphql"

export type HostListParams = {
  operationId: string
  search?: string | null
  // Sort params live in the query key (via the params object), so changing
  // the sort automatically restarts pagination from the first page.
  sortBy?: HostSortField | null
  sortDirection?: SortDirection | null
  first?: number
}

// Query key factory. The table/topology read through the infinite list, but
// the inline /host wiki chip resolves a single host by id (it persists only the
// id), so there is also a per-host detail key. Mutations and the live
// subscription fold each change into all three through applyHostEvent.
export const hostKeys = {
  all: ["hosts"] as const,
  lists: () => [...hostKeys.all, "list"] as const,
  infiniteList: (params: HostListParams) =>
    [...hostKeys.lists(), "infinite", params] as const,
  // The topology needs the whole operation in one snapshot, not a page, so it
  // gets its own key independent of the list's search/pagination params.
  topology: (operationId: string) =>
    [...hostKeys.all, "topology", operationId] as const,
  // Per-host detail, keyed by id — backs the inline /host wiki reference chip.
  details: () => [...hostKeys.all, "detail"] as const,
  detail: (id: string) => [...hostKeys.details(), id] as const,
}

// How host events and mutations keep the table live (lib/live-lists).
// Membership lists every field the search or a sort reads.
const hostLists: LiveListSpec<HostFieldsFragment> = {
  lists: hostKeys.lists(),
  sensitive: (params) => {
    const p = params as HostListParams
    return !!p.search || (p.sortBy != null && p.sortBy !== "CREATED_AT")
  },
  membership: (h) => [h.hostname, h.os, h.description, h.interfaces],
}

interface TopologySnapshot {
  hosts: HostFieldsFragment[]
  truncated: boolean
}

// patchTopologies folds a host change into every loaded topology snapshot.
// A snapshot is every host of its operation, unfiltered, so a change maps
// onto it exactly and the whole-operation drain behind it is not repeated.
// The exception is the render cap: past it, which hosts belong in the
// snapshot is the server's call, so a truncated snapshot refetches.
function patchTopologies(
  queryClient: QueryClient,
  action: string,
  hostId: string,
  host: HostFieldsFragment | null | undefined,
) {
  for (const [key, snap] of queryClient.getQueriesData<TopologySnapshot>({
    queryKey: [...hostKeys.all, "topology"],
  })) {
    if (!snap) continue
    const operationId = key[key.length - 1]
    const holds = snap.hosts.some((h) => h.id === hostId)
    let hosts: HostFieldsFragment[] | undefined
    if (action === "CREATED" && host && host.operationId === operationId) {
      if (snap.truncated || snap.hosts.length >= MAX_TOPOLOGY_HOSTS) {
        scheduleRefresh(queryClient, key, { exact: true })
        continue
      }
      hosts = [...snap.hosts, host]
    } else if (action === "DELETED" && holds) {
      if (snap.truncated) {
        scheduleRefresh(queryClient, key, { exact: true })
        continue
      }
      hosts = snap.hosts.filter((h) => h.id !== hostId)
    } else if (action !== "CREATED" && action !== "DELETED" && holds) {
      if (!host) {
        scheduleRefresh(queryClient, key, { exact: true })
        continue
      }
      hosts = snap.hosts.map((h) => (h.id === hostId ? mergeRow(h, host) : h))
    }
    if (hosts) queryClient.setQueryData(key, { ...snap, hosts })
  }
}

// applyHostEvent folds one host change — from the subscription or from the
// caller's own mutation — into the table, the chip details and the topology.
function applyHostEvent(
  queryClient: QueryClient,
  action: string,
  hostId: string,
  host: HostFieldsFragment | null | undefined,
) {
  if (action === "DELETED") {
    queryClient.removeQueries({ queryKey: hostKeys.detail(hostId) })
    applyRowRemoval(queryClient, hostLists, hostId)
  } else if (host && action !== "CREATED") {
    patchDetail(queryClient, hostKeys.detail(hostId), "host", host)
    applyRowUpdate(queryClient, hostLists, host)
  } else {
    scheduleRefresh(queryClient, hostKeys.lists())
  }
  patchTopologies(queryClient, action, hostId, host)
}

// The topology cross-references every host against every other (a route's
// gateway is matched against all interface IPs), so a partial set produces a
// WRONG graph — real routers misread as phantoms. We must fetch the complete
// operation. Page size trades request count vs. per-request payload; the cap is
// a render-perf guard (React Flow is comfortable into the few-hundreds of
// nodes). Past the cap we surface `truncated` so the UI can warn rather than
// silently present an incomplete map.
const TOPOLOGY_PAGE = 100
export const MAX_TOPOLOGY_HOSTS = 1000

// --- Queries ---

// Single-host fetch for the inline /host wiki reference chip. Mirrors useHash:
// the chip persists only a hostId and resolves the host live so renames and
// topology edits flow through without rewriting the document. `enabled` lets
// the NodeView gate the request on viewport visibility.
export function useHost(id: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: hostKeys.detail(id),
    queryFn: () => graphqlClient(HostDocument, { id }),
    enabled: !!id && (options?.enabled ?? true),
  })
}

export function useInfiniteHosts(params: HostListParams) {
  return useInfiniteQuery({
    queryKey: hostKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(HostsDocument, {
        operationId: params.operationId,
        search: params.search ?? null,
        sortBy: params.sortBy ?? null,
        sortDirection: params.sortDirection ?? null,
        first: params.first ?? 20,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.hosts.pageInfo.hasNextPage
        ? lastPage.hosts.pageInfo.endCursor ?? undefined
        : undefined,
    enabled: !!params.operationId,
  })
}

// Drains the whole operation's hosts (ignoring search — a filtered subset would
// derive false phantoms) into one flat array for the topology view. Keyed
// separately from the list so the two views don't share cache state, but it
// hangs off `hostKeys.all` so the same subscription invalidation refreshes it.
export function useAllHosts(operationId: string) {
  return useQuery({
    queryKey: hostKeys.topology(operationId),
    enabled: !!operationId,
    queryFn: async () => {
      const hosts: HostFieldsFragment[] = []
      let after: string | undefined
      let truncated = false
      for (;;) {
        const page = await graphqlClient(HostsDocument, {
          operationId,
          search: null,
          first: TOPOLOGY_PAGE,
          after,
        })
        hosts.push(...page.hosts.edges.map((e) => e.node))
        const hasNext = page.hosts.pageInfo.hasNextPage
        if (!hasNext) break
        if (hosts.length >= MAX_TOPOLOGY_HOSTS) {
          truncated = true
          break
        }
        after = page.hosts.pageInfo.endCursor ?? undefined
        if (!after) break
      }
      return { hosts, truncated }
    },
  })
}

// --- Mutations ---

// The mutations apply their result the same way the subscription applies the
// event it echoes back; applying both is harmless, and their refreshes fold
// into one.
export function useCreateHost() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { operationId: string; input: CreateHostInput }) =>
      graphqlClient(CreateHostDocument, vars),
    onSuccess: (data) => {
      applyHostEvent(queryClient, "CREATED", data.createHost.id, data.createHost)
    },
  })
}

export function useUpdateHost() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; input: UpdateHostInput }) =>
      graphqlClient(UpdateHostDocument, vars),
    onSuccess: (data) => {
      applyHostEvent(queryClient, "UPDATED", data.updateHost.id, data.updateHost)
    },
  })
}

export function useDeleteHost() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DeleteHostDocument, { id }),
    onSuccess: (_data, id) => {
      applyHostEvent(queryClient, "DELETED", id, null)
    },
  })
}

// --- Subscriptions ---

// Keeps every operator's Hosts table, chips and topology live. Hosts don't
// cross-link other entities, so there are no credential/wiki refreshes
// (unlike the hash subscription).
export function useHostChangedSubscription(operationId: string) {
  const queryClient = useQueryClient()

  useSubscription(
    HostChangedDocument,
    { operationId },
    {
      onData: (data) => {
        const { action, hostId, host } = data.hostChanged
        applyHostEvent(queryClient, action, hostId, host)
      },
      enabled: !!operationId,
    },
  )
}
