import { useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  ChannelKindsDocument,
  ChannelEndpointsDocument,
  ChannelEndpointDocument,
  ChannelEndpointExportDocument,
  CreateChannelEndpointDocument,
  UpdateChannelEndpointDocument,
  DeleteChannelEndpointDocument,
  type CreateChannelEndpointMutationVariables,
  type UpdateChannelEndpointMutationVariables,
} from "@/graphql/gql/graphql"

export const channelEndpointKeys = {
  all: ["channel-endpoints"] as const,
  list: (operationId: string) => [...channelEndpointKeys.all, "list", operationId] as const,
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
export function useChannelIcons(): Map<string, string> {
  const { data } = useChannelKinds()
  return useMemo(() => new Map((data?.channelKinds ?? []).map((k) => [k.moduleName, k.icon])), [data])
}

/** An operation's endpoints, by kind and name. */
export function useChannelEndpoints(operationId: string) {
  return useQuery({
    queryKey: channelEndpointKeys.list(operationId),
    queryFn: () => graphqlClient(ChannelEndpointsDocument, { operationId }),
    enabled: operationId !== "",
  })
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
