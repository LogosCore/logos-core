import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  GrowStorageBucketDocument,
  ReclaimStorageDocument,
  StorageActionsDocument,
  StorageConsumersDocument,
  StorageStatusDocument,
} from "@/graphql/gql/graphql"

// The admins' Storage page (core: storage.graphql). Core reads the store once
// a minute: the page reads core's snapshot every 30 seconds, and again after
// an action; the sidebar's mark (storage-warnings.ts), every minute.

export const storageKeys = {
  all: ["storage"] as const,
  status: () => [...storageKeys.all, "status"] as const,
  consumers: (bucket: string, search: string) => [...storageKeys.all, "consumers", bucket, search] as const,
  actions: () => [...storageKeys.all, "actions"] as const,
}

/** The store as core last read it; every 10 seconds while an action runs (busy), else 30. */
export function useStorageStatus(busy = false) {
  return useQuery({
    queryKey: storageKeys.status(),
    queryFn: () => graphqlClient(StorageStatusDocument),
    refetchInterval: busy ? 10_000 : 30_000,
  })
}

/** What core keeps in a bucket, the largest first, a page at a time, searched by the server. */
export function useStorageConsumers(bucket: string | null, search: string) {
  return useInfiniteQuery({
    queryKey: storageKeys.consumers(bucket ?? "", search),
    queryFn: ({ pageParam }) =>
      graphqlClient(StorageConsumersDocument, { bucket: bucket ?? "", search: search || null, first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.storageConsumers.pageInfo.hasNextPage ? (last.storageConsumers.pageInfo.endCursor ?? undefined) : undefined,
    enabled: bucket !== null,
    placeholderData: keepPreviousData,
  })
}

/** The maintenance actions run, the latest first, a page at a time; read again every 10 seconds while one runs. */
export function useStorageActions() {
  return useInfiniteQuery({
    queryKey: storageKeys.actions(),
    queryFn: ({ pageParam }) => graphqlClient(StorageActionsDocument, { first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.storageActions.pageInfo.hasNextPage ? (last.storageActions.pageInfo.endCursor ?? undefined) : undefined,
    refetchInterval: (query) =>
      query.state.data?.pages.some((p) => p.storageActions.edges.some((e) => e.node.status === "running")) ? 10_000 : false,
  })
}

/** After an action: the store, its warnings and the actions, read again. */
function useRefresh() {
  const queryClient = useQueryClient()
  return () => void queryClient.invalidateQueries({ queryKey: storageKeys.all })
}

export function useReclaimStorage() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: () => graphqlClient(ReclaimStorageDocument),
    onSettled: refresh,
  })
}

export function useGrowStorageBucket() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: (bucket: string) => graphqlClient(GrowStorageBucketDocument, { bucket }),
    onSettled: refresh,
  })
}
