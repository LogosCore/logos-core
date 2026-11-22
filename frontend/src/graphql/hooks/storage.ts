import { useEffect, useRef } from "react"
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  CleanUpStorageDocument,
  GrowStorageBucketDocument,
  ReclaimStorageDocument,
  SetStorageRetentionDocument,
  StorageActionsDocument,
  StorageCleanupPreviewDocument,
  StorageConsumersDocument,
  StorageRetentionDocument,
  StorageStatusDocument,
} from "@/graphql/gql/graphql"

// The admins' Storage page (core: storage.graphql). Core reads the store once
// a minute: the page reads core's snapshot every 30 seconds, and again after
// an action; the sidebar's mark (storage-warnings.ts), every minute. A
// cleanup's preview is read as an admin asks for it, every 10 seconds while a
// cleanup runs, and again once it ended.

export const storageKeys = {
  all: ["storage"] as const,
  status: () => [...storageKeys.all, "status"] as const,
  consumers: (bucket: string, search: string) => [...storageKeys.all, "consumers", bucket, search] as const,
  actions: () => [...storageKeys.all, "actions"] as const,
  retention: () => [...storageKeys.all, "retention"] as const,
  preview: (days: number) => [...storageKeys.all, "preview", days] as const,
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

/** The retention policy; every 10 seconds while a cleanup runs (busy), else 30. */
export function useStorageRetention(busy = false) {
  return useQuery({
    queryKey: storageKeys.retention(),
    queryFn: () => graphqlClient(StorageRetentionDocument),
    refetchInterval: busy ? 10_000 : 30_000,
  })
}

/** What a cleanup at an age in days would take now, its operations a page at a time; none while the age is not one (null). */
export function useStorageCleanupPreview(days: number | null, busy = false) {
  return useInfiniteQuery({
    queryKey: storageKeys.preview(days ?? 0),
    queryFn: ({ pageParam }) =>
      graphqlClient(StorageCleanupPreviewDocument, { olderThanDays: days ?? 0, first: 50, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.storageCleanupPreview.operations.pageInfo.hasNextPage
        ? (last.storageCleanupPreview.operations.pageInfo.endCursor ?? undefined)
        : undefined,
    enabled: days !== null,
    placeholderData: keepPreviousData,
    refetchInterval: busy ? 10_000 : false,
  })
}

/** After an action: the store, its warnings and the actions, read again. */
function useRefresh() {
  const queryClient = useQueryClient()
  return () => void queryClient.invalidateQueries({ queryKey: storageKeys.all })
}

/** Reads the page again once the actions that ran (running true) all ended: the store, the policy, a preview. */
export function useRefreshWhenEnded(running: boolean) {
  const queryClient = useQueryClient()
  const was = useRef(running)
  useEffect(() => {
    if (was.current && !running) void queryClient.invalidateQueries({ queryKey: storageKeys.all })
    was.current = running
  }, [running, queryClient])
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

export function useSetStorageRetention() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: (v: { enabled: boolean; olderThanDays: number }) => graphqlClient(SetStorageRetentionDocument, v),
    onSettled: refresh,
  })
}

/** Starts a cleanup of the recordings that ended the age ago, of one operation (null: of every one). */
export function useCleanUpStorage() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: (v: { olderThanDays: number; operationId: string | null }) => graphqlClient(CleanUpStorageDocument, v),
    onSettled: refresh,
  })
}
