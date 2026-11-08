import { keepPreviousData, useInfiniteQuery, useQueryClient, type QueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { applyRowRemoval, applyRowUpdate, scheduleRefresh, type LiveListSpec } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  VpnTunnelChangedDocument,
  VpnTunnelsDocument,
  type VpnTunnelChangedSubscription,
  type VpnTunnelFieldsFragment,
} from "@/graphql/gql/graphql"

// An operation's VPN tunnels (core: vpn_tunnels.graphql), the latest
// opened first a page at a time, searched by the server, and kept live by
// the operation's VPN events (lib/live-lists).

export interface VPNTunnelListParams {
  operationId: string
  search: string
}

export const vpnTunnelKeys = {
  all: ["vpnTunnels"] as const,
  lists: () => [...vpnTunnelKeys.all, "list"] as const,
  list: (params: VPNTunnelListParams) => [...vpnTunnelKeys.lists(), params] as const,
}

// The search reads the session's number, who opened it and the routes; the
// list is in the order the tunnels opened, which an update never moves.
const vpnTunnelLists: LiveListSpec<VpnTunnelFieldsFragment> = {
  lists: vpnTunnelKeys.lists(),
  sensitive: (params) => !!(params as VPNTunnelListParams).search,
  membership: (v) => [v.sessionNumber, v.openedByName, v.routes.join(" ")],
}

export function useVPNTunnels(params: VPNTunnelListParams) {
  return useInfiniteQuery({
    queryKey: vpnTunnelKeys.list(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(VpnTunnelsDocument, { operationId: params.operationId, search: params.search || null, first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.vpnTunnels.pageInfo.hasNextPage ? (last.vpnTunnels.pageInfo.endCursor ?? undefined) : undefined),
    enabled: params.operationId !== "",
    placeholderData: keepPreviousData,
  })
}

/**
 * Keeps the operation's VPN tunnels live where they show: one opened comes
 * in where it belongs (a refresh), one whose machines or streams changed is
 * patched in place, one closed goes.
 */
export function useVPNTunnelsSubscription(operationId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    VpnTunnelChangedDocument,
    { operationId },
    {
      enabled: operationId !== "",
      onData: (data) => applyVPNTunnelEvent(queryClient, data.vpnTunnelChanged),
    },
  )
}

/** Folds one VPN tunnel event into the cached lists. */
export function applyVPNTunnelEvent(queryClient: QueryClient, e: VpnTunnelChangedSubscription["vpnTunnelChanged"]) {
  if (e.action === "DELETED") applyRowRemoval(queryClient, vpnTunnelLists, e.id)
  else if (e.action === "UPDATED" && e.tunnel) applyRowUpdate(queryClient, vpnTunnelLists, e.tunnel)
  else scheduleRefresh(queryClient, vpnTunnelLists.lists)
}
