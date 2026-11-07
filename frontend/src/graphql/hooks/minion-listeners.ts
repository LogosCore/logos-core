import { keepPreviousData, useInfiniteQuery, useQueryClient, type QueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { applyRowRemoval, applyRowUpdate, scheduleRefresh, type LiveListSpec } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  MinionListenerChangedDocument,
  MinionListenersDocument,
  type MinionListenerChangedSubscription,
  type MinionListenerFieldsFragment,
} from "@/graphql/gql/graphql"

// An operation's reverse port forwards' listeners: the ports its tasks'
// minions listen on (core: minion_listeners.graphql), the latest opened
// first a page at a time, searched by the server, and kept live by the
// operation's listener events (lib/live-lists).

export interface ListenerListParams {
  operationId: string
  search: string
}

export const minionListenerKeys = {
  all: ["minionListeners"] as const,
  lists: () => [...minionListenerKeys.all, "list"] as const,
  list: (params: ListenerListParams) => [...minionListenerKeys.lists(), params] as const,
}

// The search reads the port, the session's number, who opened it, where it
// listens and what to; the list is in the order listeners opened, which an
// update never moves.
const listenerLists: LiveListSpec<MinionListenerFieldsFragment> = {
  lists: minionListenerKeys.lists(),
  sensitive: (params) => !!(params as ListenerListParams).search,
  membership: (l) => [l.listen, l.sessionNumber, l.openedByName, l.bind, l.to, l.bound],
}

export function useMinionListeners(params: ListenerListParams) {
  return useInfiniteQuery({
    queryKey: minionListenerKeys.list(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionListenersDocument, { operationId: params.operationId, search: params.search || null, first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.minionListeners.pageInfo.hasNextPage ? (last.minionListeners.pageInfo.endCursor ?? undefined) : undefined,
    enabled: params.operationId !== "",
    placeholderData: keepPreviousData,
  })
}

/**
 * Keeps the operation's listeners live where they show: one opened comes in
 * where it belongs (a refresh), one whose minion said where it listens or
 * whose streams' numbers changed is patched in place, one closed goes.
 */
export function useMinionListenersSubscription(operationId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    MinionListenerChangedDocument,
    { operationId },
    {
      enabled: operationId !== "",
      onData: (data) => applyListenerEvent(queryClient, data.minionListenerChanged),
    },
  )
}

/** Folds one listener event into the cached lists. */
export function applyListenerEvent(queryClient: QueryClient, e: MinionListenerChangedSubscription["minionListenerChanged"]) {
  if (e.action === "DELETED") applyRowRemoval(queryClient, listenerLists, e.id)
  else if (e.action === "UPDATED" && e.listener) applyRowUpdate(queryClient, listenerLists, e.listener)
  else scheduleRefresh(queryClient, listenerLists.lists)
}
