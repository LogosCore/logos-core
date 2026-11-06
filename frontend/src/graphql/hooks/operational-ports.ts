import { keepPreviousData, useInfiniteQuery, useQueryClient, type QueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { applyRowRemoval, applyRowUpdate, scheduleRefresh, type LiveListSpec } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  OperationalPortChangedDocument,
  OperationalPortsDocument,
  type OperationalPortChangedSubscription,
  type OperationalPortFieldsFragment,
  type OperationalPortsQuery,
} from "@/graphql/gql/graphql"

export type PortPages = { pages: OperationalPortsQuery[]; pageParams: (string | undefined)[] }

// An operation's operational ports in use: tunnel tasks' SOCKS proxies
// (core: operational_ports.graphql), the latest opened first a page at a
// time, searched by the server, and kept live by the operation's port
// events (lib/live-lists).

export interface PortListParams {
  operationId: string
  search: string
}

export const operationalPortKeys = {
  all: ["operationalPorts"] as const,
  lists: () => [...operationalPortKeys.all, "list"] as const,
  list: (params: PortListParams) => [...operationalPortKeys.lists(), params] as const,
}

// The search reads the port, the session's number, who opened it and for
// what; the list is in the order ports opened, which an update never moves.
const portLists: LiveListSpec<OperationalPortFieldsFragment> = {
  lists: operationalPortKeys.lists(),
  sensitive: (params) => !!(params as PortListParams).search,
  membership: (p) => [p.port, p.sessionNumber, p.openedByName, p.purpose],
}

export function useOperationalPorts(params: PortListParams) {
  return useInfiniteQuery({
    queryKey: operationalPortKeys.list(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(OperationalPortsDocument, { operationId: params.operationId, search: params.search || null, first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.operationalPorts.pageInfo.hasNextPage ? (last.operationalPorts.pageInfo.endCursor ?? undefined) : undefined,
    enabled: params.operationId !== "",
    placeholderData: keepPreviousData,
  })
}

/**
 * Keeps the operation's ports live where they show: a port opened comes in
 * where it belongs (a refresh), one whose streams' numbers changed is
 * patched in place, one closed goes.
 */
export function useOperationalPortsSubscription(operationId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    OperationalPortChangedDocument,
    { operationId },
    {
      enabled: operationId !== "",
      onData: (data) => applyPortEvent(queryClient, data.operationalPortChanged),
    },
  )
}

/** Folds one port event into the cached lists. */
export function applyPortEvent(queryClient: QueryClient, e: OperationalPortChangedSubscription["operationalPortChanged"]) {
  if (e.action === "DELETED") applyRowRemoval(queryClient, portLists, e.id)
  else if (e.action === "UPDATED" && e.port) applyRowUpdate(queryClient, portLists, e.port)
  else scheduleRefresh(queryClient, portLists.lists)
}
