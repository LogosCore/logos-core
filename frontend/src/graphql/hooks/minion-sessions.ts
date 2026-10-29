import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { applyRowUpdate, scheduleRefresh, type LiveListSpec } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  MinionSessionsDocument,
  MinionSessionChangedDocument,
  type MinionSessionFieldsFragment,
} from "@/graphql/gql/graphql"

/** What a sessions list shows: the server searches. */
export interface MinionSessionListParams {
  operationId: string
  search: string | null
}

export const minionSessionKeys = {
  all: ["minion-sessions"] as const,
  lists: () => [...minionSessionKeys.all, "list"] as const,
  infiniteList: (params: MinionSessionListParams) => [...minionSessionKeys.lists(), params] as const,
}

// How session events keep the table live (lib/live-lists). Membership lists
// every field the search reads.
const sessionLists: LiveListSpec<MinionSessionFieldsFragment> = {
  lists: minionSessionKeys.lists(),
  sensitive: (params) => !!(params as MinionSessionListParams).search,
  membership: (s) => [s.host, s.user, s.domain, s.proc, s.os, s.arch, s.integrity, s.addrs, s.version],
}

/** An operation's sessions, newest first, a page at a time as the table scrolls. */
export function useInfiniteMinionSessions(params: MinionSessionListParams) {
  return useInfiniteQuery({
    queryKey: minionSessionKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionSessionsDocument, { ...params, first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.minionSessions.pageInfo.hasNextPage
        ? (lastPage.minionSessions.pageInfo.endCursor ?? undefined)
        : undefined,
    enabled: params.operationId !== "",
  })
}

/**
 * Keeps the sessions table live: a new session refetches the lists (it goes
 * on top), a check-in or rehello patches its row in place.
 */
export function useMinionSessionChangedSubscription(operationId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    MinionSessionChangedDocument,
    { operationId },
    {
      onData: (data) => {
        const { action, session } = data.minionSessionChanged
        if (action === "CREATED" || !session) {
          scheduleRefresh(queryClient, minionSessionKeys.lists())
        } else {
          applyRowUpdate(queryClient, sessionLists, session)
        }
      },
      enabled: operationId !== "",
    },
  )
}
