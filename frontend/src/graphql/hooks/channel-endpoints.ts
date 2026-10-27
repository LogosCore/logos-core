import { useMemo } from "react"
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  ChannelKindsDocument,
  ChannelEndpointsDocument,
  ChannelEndpointDocument,
  FreeChannelEndpointNameDocument,
  ChannelEndpointExportDocument,
  CreateChannelEndpointDocument,
  UpdateChannelEndpointDocument,
  DeleteChannelEndpointDocument,
  type CreateChannelEndpointMutationVariables,
  type UpdateChannelEndpointMutationVariables,
} from "@/graphql/gql/graphql"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"

export const channelEndpointKeys = {
  all: ["channel-endpoints"] as const,
  list: (operationId: string) => [...channelEndpointKeys.all, "list", operationId] as const,
  infiniteList: (params: EndpointListParams) => [...channelEndpointKeys.list(params.operationId), params] as const,
  count: (operationId: string, moduleNames: readonly string[]) =>
    [...channelEndpointKeys.list(operationId), "count", moduleNames] as const,
  freeName: (operationId: string, name: string, tag: string) =>
    [...channelEndpointKeys.list(operationId), "free-name", name, tag] as const,
  detail: (id: string) => [...channelEndpointKeys.all, "detail", id] as const,
  kinds: () => [...channelEndpointKeys.all, "kinds"] as const,
}

/** Channel kinds that declared a connection schema, with their current one. */
export function useChannelKinds() {
  return useQuery({
    queryKey: channelEndpointKeys.kinds(),
    queryFn: () => graphqlClient(ChannelKindsDocument),
  })
}

/** Channel kinds' icons by module name ("" when a kind sent none). */
export function useChannelIcons(): Map<string, ModuleIconSvgs> {
  const { data } = useChannelKinds()
  return useMemo(
    () => new Map((data?.channelKinds ?? []).map((k) => [k.moduleName, { icon: k.icon, iconLight: k.iconLight }])),
    [data],
  )
}

/** What an endpoints list shows: the server searches and filters. */
export interface EndpointListParams {
  operationId: string
  search: string | null
  /** Channel kinds to list; all when null. */
  moduleNames: string[] | null
}

/** An operation's endpoints by name, a page at a time as the list scrolls. */
export function useInfiniteChannelEndpoints(params: EndpointListParams, options?: { enabled?: boolean }) {
  return useInfiniteQuery({
    queryKey: channelEndpointKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(ChannelEndpointsDocument, { ...params, first: 20, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.channelEndpoints.pageInfo.hasNextPage
        ? (lastPage.channelEndpoints.pageInfo.endCursor ?? undefined)
        : undefined,
    enabled: params.operationId !== "" && (options?.enabled ?? true),
  })
}

/** How many endpoints of these channel kinds the operation has. */
export function useChannelEndpointCount(operationId: string, moduleNames: readonly string[]) {
  const query = useQuery({
    queryKey: channelEndpointKeys.count(operationId, moduleNames),
    queryFn: () =>
      graphqlClient(ChannelEndpointsDocument, { operationId, moduleNames: [...moduleNames], first: 1 }),
    enabled: operationId !== "" && moduleNames.length > 0,
  })
  return query.data?.channelEndpoints.totalCount ?? null
}

/**
 * The name to suggest for a new endpoint like name: the first of name+tag,
 * name+tag-2, -3… the operation has no endpoint called (the server's
 * freeChannelEndpointName). Null until known; enabled=false skips it.
 */
export function useFreeChannelEndpointName(operationId: string, name: string, tag: string, enabled = true) {
  const query = useQuery({
    queryKey: channelEndpointKeys.freeName(operationId, name, tag),
    queryFn: () => graphqlClient(FreeChannelEndpointNameDocument, { operationId, name, tag }),
    enabled: enabled && operationId !== "",
    // A suggestion, read once: it is checked again when the endpoint is saved.
    staleTime: Infinity,
  })
  return query.data?.freeChannelEndpointName ?? null
}

/** One endpoint (the editor). */
export function useChannelEndpoint(id: string) {
  return useQuery({
    queryKey: channelEndpointKeys.detail(id),
    queryFn: () => graphqlClient(ChannelEndpointDocument, { id }),
    enabled: id !== "",
  })
}

/**
 * Every write refetches the lists: names are unique per operation and sorted,
 * so patching the cache in place buys little.
 */
function useInvalidateEndpoints() {
  const queryClient = useQueryClient()
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: [...channelEndpointKeys.all, "list"] }),
      queryClient.invalidateQueries({ queryKey: [...channelEndpointKeys.all, "detail"] }),
    ])
}

export function useCreateChannelEndpoint() {
  const invalidate = useInvalidateEndpoints()
  return useMutation({
    mutationFn: (vars: CreateChannelEndpointMutationVariables) =>
      graphqlClient(CreateChannelEndpointDocument, vars),
    onSuccess: invalidate,
  })
}

export function useUpdateChannelEndpoint() {
  const invalidate = useInvalidateEndpoints()
  return useMutation({
    mutationFn: (vars: UpdateChannelEndpointMutationVariables) =>
      graphqlClient(UpdateChannelEndpointDocument, vars),
    onSuccess: invalidate,
  })
}

export function useDeleteChannelEndpoint() {
  const invalidate = useInvalidateEndpoints()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DeleteChannelEndpointDocument, { id }),
    onSuccess: invalidate,
  })
}

/** An endpoint as resolved connection settings (values over defaults), fetched on demand. */
export async function fetchChannelEndpointExport(id: string): Promise<unknown> {
  const res = await graphqlClient(ChannelEndpointExportDocument, { id })
  return res.channelEndpointExport
}
