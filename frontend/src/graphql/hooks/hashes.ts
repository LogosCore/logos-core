import {
  useQuery,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  applyRowRemoval,
  applyRowUpdate,
  findCachedRow,
  patchDetail,
  scheduleRefresh,
  type LiveListSpec,
} from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import { credentialKeys } from "@/graphql/hooks/credentials"
import type {
  HashFieldsFragment,
  HashQuery,
  CreateHashInput,
  UpdateHashInput,
  BulkImportHashesInput,
  MarkHashCrackedInput,
  HashStatus,
} from "@/graphql/gql/graphql"
import {
  HashDocument,
  HashChipDocument,
  HashesDocument,
  HashTagsDocument,
  HashBacklinksDocument,
  MyHashesDocument,
  MyHashTagsDocument,
  CreateHashDocument,
  UpdateHashDocument,
  DeleteHashDocument,
  BulkImportHashesDocument,
  MarkHashCrackedDocument,
  HashChangedDocument,
  MyHashChangedDocument,
} from "@/graphql/gql/graphql"

export type HashListParams = {
  operationId: string
  search?: string | null
  statuses?: HashStatus[] | null
  tags?: string[] | null
  hasCredential?: boolean | null
  first?: number
}

export type MyHashListParams = {
  operationIds: string[] | null
  search?: string | null
  statuses?: HashStatus[] | null
  tags?: string[] | null
  hasCredential?: boolean | null
  first?: number
}

// Query key factory. Mirrors credentialKeys structure so dev expectations
// carry over between the two findings tabs.
export const hashKeys = {
  all: ["hashes"] as const,
  lists: () => [...hashKeys.all, "list"] as const,
  infiniteList: (params: HashListParams) =>
    [...hashKeys.lists(), "infinite", params] as const,
  infiniteMyList: (params: MyHashListParams) =>
    [...hashKeys.lists(), "infinite-my", params] as const,
  details: () => [...hashKeys.all, "detail"] as const,
  detail: (id: string) => [...hashKeys.details(), id] as const,
  // The inline chip's lighter projection of the same row (useHashChip).
  chip: (id: string) => [...hashKeys.all, "chip", id] as const,
  tagSets: () => [...hashKeys.all, "tags"] as const,
  tagSet: (operationId: string) => [...hashKeys.tagSets(), operationId] as const,
  myTagSet: (operationIds: string[] | null) =>
    [...hashKeys.tagSets(), "my", operationIds] as const,
  backlinks: (hashId: string) =>
    [...hashKeys.all, "backlinks", hashId] as const,
}

// How hash events and mutations keep the lists live (lib/live-lists).
// Membership lists every field a list filter or search reads; hash lists only
// sort by creation time.
const hashLists: LiveListSpec<HashFieldsFragment> = {
  lists: hashKeys.lists(),
  sensitive: (params) => {
    const p = params as HashListParams | MyHashListParams
    return !!p.search || !!p.statuses?.length || !!p.tags?.length || p.hasCredential != null
  },
  membership: (h) => [h.value, h.comment, h.status, h.tags, h.credentialId],
}

// patchHashDetail merges a row into a loaded detail and chip. The detail also
// carries the linked credential, which the row does not; when the link
// changed that copy is stale, so the detail refetches instead. The chip holds
// only the id of the link, so it always takes the row.
function patchHashDetail(queryClient: QueryClient, hash: HashFieldsFragment) {
  const key = hashKeys.detail(hash.id)
  const cached = queryClient.getQueryData<HashQuery>(key)?.hash
  if (cached && cached.credentialId !== hash.credentialId) {
    scheduleRefresh(queryClient, key, { exact: true })
  } else {
    patchDetail(queryClient, key, "hash", hash)
  }
  patchDetail(queryClient, hashKeys.chip(hash.id), "hash", hash)
}

// updatedHash applies an updated row to the detail and the lists. A hash that
// just gained a credential link may have created that credential (cracking
// does), so the credential lists refresh too — only then, rather than on
// every hash event.
function updatedHash(queryClient: QueryClient, hash: HashFieldsFragment) {
  const before = findCachedRow(queryClient, hashKeys.lists(), hash.id) as
    | HashFieldsFragment
    | undefined
  patchHashDetail(queryClient, hash)
  applyRowUpdate(queryClient, hashLists, hash)
  if (hash.credentialId && before?.credentialId !== hash.credentialId) {
    scheduleRefresh(queryClient, credentialKeys.lists())
  }
}

// applyHashEvent folds one hash event into the cache, like
// applyCredentialEvent. A bulk import arrives as one event with no hashId —
// no single row to patch — and refreshes the lists like a create does.
function applyHashEvent(
  queryClient: QueryClient,
  action: string,
  hashId: string,
  hash: HashFieldsFragment | null | undefined,
) {
  if (action === "DELETED" && hashId) {
    queryClient.removeQueries({ queryKey: hashKeys.detail(hashId) })
    queryClient.removeQueries({ queryKey: hashKeys.chip(hashId) })
    applyRowRemoval(queryClient, hashLists, hashId)
  } else if (hashId && hash && action !== "CREATED") {
    updatedHash(queryClient, hash)
  } else {
    scheduleRefresh(queryClient, hashKeys.lists())
  }
  // The credential details dialog lists the hashes that produced it.
  scheduleRefresh(queryClient, [...credentialKeys.all, "sourceHashes"])
}

// --- Queries ---

export function useHash(id: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: hashKeys.detail(id),
    queryFn: () => graphqlClient(HashDocument, { id }),
    enabled: !!id && (options?.enabled ?? true),
  })
}

// useHashChip loads what an inline chip and its context menu read, without
// the linked credential and createdBy the details dialog resolves.
export function useHashChip(id: string, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: hashKeys.chip(id),
    queryFn: () => graphqlClient(HashChipDocument, { id }),
    enabled: !!id && (options?.enabled ?? true),
  })
}

export function useInfiniteHashes(params: HashListParams) {
  return useInfiniteQuery({
    queryKey: hashKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(HashesDocument, {
        operationId: params.operationId,
        search: params.search ?? null,
        statuses: params.statuses && params.statuses.length > 0 ? params.statuses : null,
        tags: params.tags && params.tags.length > 0 ? params.tags : null,
        hasCredential: params.hasCredential ?? null,
        first: params.first ?? 20,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.hashes.pageInfo.hasNextPage
        ? lastPage.hashes.pageInfo.endCursor ?? undefined
        : undefined,
    enabled: !!params.operationId,
  })
}

export function useHashTags(operationId: string) {
  return useQuery({
    queryKey: hashKeys.tagSet(operationId),
    queryFn: () => graphqlClient(HashTagsDocument, { operationId }),
    enabled: !!operationId,
  })
}

// Wiki documents that reference this hash inline. Loaded on demand by the hash
// details dialog — capped at 200 server-side and filtered to active documents.
// Live-invalidation rides on the hashChanged subscription, which blanket-
// invalidates the backlinks prefix. Mirrors useCredentialBacklinks.
export function useHashBacklinks(hashId: string) {
  return useQuery({
    queryKey: hashKeys.backlinks(hashId),
    queryFn: () => graphqlClient(HashBacklinksDocument, { hashId }),
    enabled: !!hashId,
  })
}

export function useInfiniteMyHashes(
  params: MyHashListParams,
  options: { enabled?: boolean } = {},
) {
  return useInfiniteQuery({
    queryKey: hashKeys.infiniteMyList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(MyHashesDocument, {
        operationIds: params.operationIds,
        search: params.search ?? null,
        statuses: params.statuses && params.statuses.length > 0 ? params.statuses : null,
        tags: params.tags && params.tags.length > 0 ? params.tags : null,
        hasCredential: params.hasCredential ?? null,
        first: params.first ?? 20,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.myHashes.pageInfo.hasNextPage
        ? lastPage.myHashes.pageInfo.endCursor ?? undefined
        : undefined,
    enabled: options.enabled ?? true,
  })
}

export function useMyHashTags(
  operationIds: string[] | null,
  options: { enabled?: boolean } = {},
) {
  return useQuery({
    queryKey: hashKeys.myTagSet(operationIds),
    queryFn: () => graphqlClient(MyHashTagsDocument, { operationIds }),
    enabled: options.enabled ?? true,
  })
}

// --- Mutations ---

export function useCreateHash() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { operationId: string; input: CreateHashInput }) =>
      graphqlClient(CreateHashDocument, vars),
    // Nothing has this hash's detail loaded yet, and the returned row lacks
    // the linked credential the detail selects, so it is not seeded.
    onSuccess: () => {
      scheduleRefresh(queryClient, hashKeys.lists())
      scheduleRefresh(queryClient, hashKeys.tagSets())
    },
  })
}

export function useUpdateHash() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; input: UpdateHashInput }) =>
      graphqlClient(UpdateHashDocument, vars),
    onSuccess: (data) => {
      updatedHash(queryClient, data.updateHash)
      scheduleRefresh(queryClient, hashKeys.tagSets())
    },
  })
}

export function useDeleteHash() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DeleteHashDocument, { id }),
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: hashKeys.detail(id) })
      queryClient.removeQueries({ queryKey: hashKeys.chip(id) })
      applyRowRemoval(queryClient, hashLists, id)
      scheduleRefresh(queryClient, hashKeys.tagSets())
    },
  })
}

export function useBulkImportHashes() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { operationId: string; input: BulkImportHashesInput }) =>
      graphqlClient(BulkImportHashesDocument, vars),
    onSuccess: () => {
      // Bulk import can produce dozens of new rows — refresh wholesale.
      scheduleRefresh(queryClient, hashKeys.lists())
      scheduleRefresh(queryClient, hashKeys.tagSets())
    },
  })
}

// markHashCracked may create a credential server-side, so the credential
// lists refresh too. It returns the full detail row, linked credential
// included, so the detail takes it whole.
export function useMarkHashCracked() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; input: MarkHashCrackedInput }) =>
      graphqlClient(MarkHashCrackedDocument, vars),
    onSuccess: (data) => {
      const hash = data.markHashCracked
      queryClient.setQueryData(hashKeys.detail(hash.id), { hash })
      patchDetail(queryClient, hashKeys.chip(hash.id), "hash", hash)
      applyRowUpdate(queryClient, hashLists, hash)
      scheduleRefresh(queryClient, hashKeys.tagSets())
      scheduleRefresh(queryClient, credentialKeys.lists())
      scheduleRefresh(queryClient, [...credentialKeys.all, "sourceHashes"])
    },
  })
}

// --- Subscriptions ---

// hash events with empty hashId carry a bulk-import signal — the server
// publishes one summary event per bulk insert and intentionally leaves the
// id blank because there is no single subject. applyHashEvent refreshes the
// lists for it rather than trying to splice individual rows.
export function useHashChangedSubscription(operationId: string) {
  const queryClient = useQueryClient()

  useSubscription(HashChangedDocument, { operationId }, {
    onData: (data) => {
      const { action, hashId, hash } = data.hashChanged
      applyHashEvent(queryClient, action, hashId, hash)
      scheduleRefresh(queryClient, hashKeys.tagSet(operationId))
      // A hash delete strips its id from wiki hash_references, so cached
      // backlinks rows can drift. Refresh them all — the prefix matches every
      // per-hash entry and the data is light.
      scheduleRefresh(queryClient, [...hashKeys.all, "backlinks"])
    },
    enabled: !!operationId,
  })
}

export function useMyHashChangedSubscription(
  operationIds: string[] | null,
  options: { enabled?: boolean } = {},
) {
  const queryClient = useQueryClient()

  useSubscription(
    MyHashChangedDocument,
    { operationIds },
    {
      onData: (data) => {
        const { action, hashId, hash } = data.myHashChanged
        applyHashEvent(queryClient, action, hashId, hash)
        scheduleRefresh(queryClient, hashKeys.tagSets())
        scheduleRefresh(queryClient, [...hashKeys.all, "backlinks"])
      },
      enabled: options.enabled ?? true,
    },
  )
}
