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
  patchDetail,
  scheduleRefresh,
  type LiveListSpec,
} from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import type {
  CredentialFieldsFragment,
  CreateCredentialInput,
  UpdateCredentialInput,
  CredentialType,
  CredentialSearchField,
  CredentialSortField,
  CredentialValidity,
  SortDirection,
} from "@/graphql/gql/graphql"
import {
  CredentialDocument,
  CredentialsDocument,
  CredentialTagsDocument,
  CredentialBacklinksDocument,
  CredentialSourceHashesDocument,
  CreateCredentialDocument,
  UpdateCredentialDocument,
  DeleteCredentialDocument,
  AddCredentialCommentDocument,
  UpdateCredentialCommentDocument,
  DeleteCredentialCommentDocument,
  CredentialChangedDocument,
  MyCredentialsDocument,
  MyCredentialTagsDocument,
  MyCredentialChangedDocument,
} from "@/graphql/gql/graphql"

export type CredentialListParams = {
  operationId: string
  search?: string | null
  // Empty/omitted = search all fields (backend default).
  searchFields?: CredentialSearchField[] | null
  type?: CredentialType | null
  tags?: string[] | null
  // Validity states to include. Empty/omitted = every state.
  validity?: CredentialValidity[] | null
  // Column sort; omitted = server default (CREATED_AT DESC). Cursors are
  // sort-specific, but the params live in the query key, so changing the
  // sort naturally starts a fresh query from page one.
  sortBy?: CredentialSortField | null
  sortDirection?: SortDirection | null
  first?: number
}

// Cross-operation list params for the global Findings view.
// operationIds: null = "all my operations" (server resolves to caller's
// membership set). Empty array = explicit empty selection.
export type MyCredentialListParams = {
  operationIds: string[] | null
  search?: string | null
  searchFields?: CredentialSearchField[] | null
  type?: CredentialType | null
  tags?: string[] | null
  validity?: CredentialValidity[] | null
  sortBy?: CredentialSortField | null
  sortDirection?: SortDirection | null
  first?: number
}

// Query key factory.
export const credentialKeys = {
  all: ["credentials"] as const,
  lists: () => [...credentialKeys.all, "list"] as const,
  infiniteList: (params: CredentialListParams) =>
    [...credentialKeys.lists(), "infinite", params] as const,
  infiniteMyList: (params: MyCredentialListParams) =>
    [...credentialKeys.lists(), "infinite-my", params] as const,
  details: () => [...credentialKeys.all, "detail"] as const,
  detail: (id: string) => [...credentialKeys.details(), id] as const,
  tagSets: () => [...credentialKeys.all, "tags"] as const,
  tagSet: (operationId: string) => [...credentialKeys.tagSets(), operationId] as const,
  myTagSet: (operationIds: string[] | null) =>
    [...credentialKeys.tagSets(), "my", operationIds] as const,
  backlinks: (credentialId: string) =>
    [...credentialKeys.all, "backlinks", credentialId] as const,
  sourceHashes: (credentialId: string) =>
    [...credentialKeys.all, "sourceHashes", credentialId] as const,
}

// How credential events and mutations keep the lists live (lib/live-lists).
// Membership lists every field a list filter, search field or sort reads.
const credentialLists: LiveListSpec<CredentialFieldsFragment> = {
  lists: credentialKeys.lists(),
  sensitive: (params) => {
    const p = params as CredentialListParams | MyCredentialListParams
    return (
      !!p.search ||
      !!p.type ||
      !!p.tags?.length ||
      !!p.validity?.length ||
      (p.sortBy != null && p.sortBy !== "CREATED_AT")
    )
  },
  membership: (c) => [c.name, c.username, c.password, c.properties, c.type, c.tags, c.validity],
}

// applyCredentialEvent folds one credential event into the cache. An update
// is patched into the lists and a loaded detail; a delete drops the row; a
// create has no place in a loaded page until the server orders it, so the
// lists refresh. So does an event whose row the server could not load.
function applyCredentialEvent(
  queryClient: QueryClient,
  action: string,
  credentialId: string,
  credential: CredentialFieldsFragment | null | undefined,
) {
  if (action === "DELETED") {
    queryClient.removeQueries({ queryKey: credentialKeys.detail(credentialId) })
    applyRowRemoval(queryClient, credentialLists, credentialId)
  } else if (credential && action !== "CREATED") {
    patchDetail(queryClient, credentialKeys.detail(credentialId), "credential", credential)
    applyRowUpdate(queryClient, credentialLists, credential)
  } else {
    scheduleRefresh(queryClient, credentialKeys.lists())
  }
}

// --- Queries ---

// `options.enabled` lets callers defer the fetch until the credential is
// actually about to be displayed — wiki-credential-chip uses this to gate
// queries on viewport intersection so a long doc with many inline chips
// doesn't fan out one round trip per chip on mount. The `!!id` guard still
// applies on top of the caller's flag so empty/broken refs are inert.
export function useCredential(
  id: string,
  options?: { enabled?: boolean },
) {
  return useQuery({
    queryKey: credentialKeys.detail(id),
    queryFn: () => graphqlClient(CredentialDocument, { id }),
    enabled: !!id && (options?.enabled ?? true),
  })
}

export function useInfiniteCredentials(params: CredentialListParams) {
  return useInfiniteQuery({
    queryKey: credentialKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(CredentialsDocument, {
        operationId: params.operationId,
        search: params.search ?? null,
        searchFields:
          params.searchFields && params.searchFields.length > 0
            ? params.searchFields
            : null,
        type: params.type ?? null,
        tags: params.tags && params.tags.length > 0 ? params.tags : null,
        validity:
          params.validity && params.validity.length > 0
            ? params.validity
            : null,
        sortBy: params.sortBy ?? null,
        sortDirection: params.sortDirection ?? null,
        first: params.first ?? 20,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.credentials.pageInfo.hasNextPage
        ? lastPage.credentials.pageInfo.endCursor ?? undefined
        : undefined,
    enabled: !!params.operationId,
  })
}

export function useCredentialTags(operationId: string) {
  return useQuery({
    queryKey: credentialKeys.tagSet(operationId),
    queryFn: () => graphqlClient(CredentialTagsDocument, { operationId }),
    enabled: !!operationId,
  })
}

// Wiki documents that reference this credential inline. Loaded on demand by
// the details dialog — capped at 200 server-side and filtered to active
// documents. Live-invalidation runs through the existing credentialChanged
// and wikiDocumentChanged subscriptions, which both blanket-invalidate the
// backlinks prefix.
// Hashes that produced this credential. Loaded on demand by the details
// dialog. Cache invalidates whenever any hash mutation runs (the hash hooks
// drop the entire credentials prefix on cracked, which covers this).
export function useCredentialSourceHashes(credentialId: string) {
  return useQuery({
    queryKey: credentialKeys.sourceHashes(credentialId),
    queryFn: () =>
      graphqlClient(CredentialSourceHashesDocument, { id: credentialId }),
    enabled: !!credentialId,
  })
}

export function useCredentialBacklinks(credentialId: string) {
  return useQuery({
    queryKey: credentialKeys.backlinks(credentialId),
    queryFn: () =>
      graphqlClient(CredentialBacklinksDocument, { credentialId }),
    enabled: !!credentialId,
  })
}

// Cross-operation list. Mirrors useInfiniteCredentials but talks to myCredentials
// and accepts an operationIds list (null = caller's full accessible set).
// Pass `enabled: false` to keep the hook in the call tree without firing the
// query (used from CredentialsTab when we're in scoped mode).
export function useInfiniteMyCredentials(
  params: MyCredentialListParams,
  options: { enabled?: boolean } = {},
) {
  return useInfiniteQuery({
    queryKey: credentialKeys.infiniteMyList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(MyCredentialsDocument, {
        operationIds: params.operationIds,
        search: params.search ?? null,
        searchFields:
          params.searchFields && params.searchFields.length > 0
            ? params.searchFields
            : null,
        type: params.type ?? null,
        tags: params.tags && params.tags.length > 0 ? params.tags : null,
        validity:
          params.validity && params.validity.length > 0
            ? params.validity
            : null,
        sortBy: params.sortBy ?? null,
        sortDirection: params.sortDirection ?? null,
        first: params.first ?? 20,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.myCredentials.pageInfo.hasNextPage
        ? lastPage.myCredentials.pageInfo.endCursor ?? undefined
        : undefined,
    enabled: options.enabled ?? true,
  })
}

export function useMyCredentialTags(
  operationIds: string[] | null,
  options: { enabled?: boolean } = {},
) {
  return useQuery({
    queryKey: credentialKeys.myTagSet(operationIds),
    queryFn: () => graphqlClient(MyCredentialTagsDocument, { operationIds }),
    enabled: options.enabled ?? true,
  })
}

// --- Mutations ---

export function useCreateCredential() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { operationId: string; input: CreateCredentialInput }) =>
      graphqlClient(CreateCredentialDocument, vars),
    // The actor's own event echo asks for the same refreshes; both fold into
    // one (lib/live-lists).
    onSuccess: (data) => {
      queryClient.setQueryData(credentialKeys.detail(data.createCredential.id), {
        credential: data.createCredential,
      })
      scheduleRefresh(queryClient, credentialKeys.lists())
      scheduleRefresh(queryClient, credentialKeys.tagSets())
    },
  })
}

// updatedCredential applies a mutation's returned row: the detail takes it
// whole, the lists get it patched in.
function updatedCredential(queryClient: QueryClient, credential: CredentialFieldsFragment) {
  queryClient.setQueryData(credentialKeys.detail(credential.id), { credential })
  applyRowUpdate(queryClient, credentialLists, credential)
}

export function useUpdateCredential() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; input: UpdateCredentialInput }) =>
      graphqlClient(UpdateCredentialDocument, vars),
    onSuccess: (data) => {
      updatedCredential(queryClient, data.updateCredential)
      scheduleRefresh(queryClient, credentialKeys.tagSets())
    },
  })
}

export function useDeleteCredential() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      graphqlClient(DeleteCredentialDocument, { id }),
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: credentialKeys.detail(id) })
      applyRowRemoval(queryClient, credentialLists, id)
      scheduleRefresh(queryClient, credentialKeys.tagSets())
    },
  })
}

export function useAddCredentialComment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { credentialId: string; text: string }) =>
      graphqlClient(AddCredentialCommentDocument, vars),
    onSuccess: (data) => updatedCredential(queryClient, data.addCredentialComment),
  })
}

export function useUpdateCredentialComment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { credentialId: string; commentId: string; text: string }) =>
      graphqlClient(UpdateCredentialCommentDocument, vars),
    onSuccess: (data) => updatedCredential(queryClient, data.updateCredentialComment),
  })
}

export function useDeleteCredentialComment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { credentialId: string; commentId: string }) =>
      graphqlClient(DeleteCredentialCommentDocument, vars),
    onSuccess: (data) => updatedCredential(queryClient, data.deleteCredentialComment),
  })
}

// --- Subscriptions ---

// Subscribe to real-time credential change events via SSE. The server pushes the
// full credential entity for non-delete actions; applyCredentialEvent folds it
// into the detail and list caches instead of refetching every open table.
export function useCredentialChangedSubscription(operationId: string) {
  const queryClient = useQueryClient()

  useSubscription(CredentialChangedDocument, { operationId }, {
    onData: (data) => {
      const { action, credentialId, credential } = data.credentialChanged
      applyCredentialEvent(queryClient, action, credentialId, credential)
      scheduleRefresh(queryClient, credentialKeys.tagSet(operationId))
      // A credential delete strips its id from wiki credential_references;
      // a rename surfaces in backlink list titles. Either way the cached
      // backlinks rows can drift, so refresh them all — the data is light
      // and the prefix matches every per-credential entry.
      scheduleRefresh(queryClient, [...credentialKeys.all, "backlinks"])
    },
    enabled: !!operationId,
  })
}

// Cross-operation real-time subscription for the global Findings view.
// Mirrors useCredentialChangedSubscription but talks to myCredentialChanged
// and accepts an operationIds list (null = caller's full accessible set, []
// = explicit empty — see MyCredentialsQuery for the same semantics).
//
// The tag refresh is broader than the scoped version: all tag sets, because a
// single event can affect either the global myTagSet key or any scoped one.
// The lists need no special case — applyCredentialEvent covers every list
// under credentialKeys.lists(), global and scoped alike.
export function useMyCredentialChangedSubscription(
  operationIds: string[] | null,
  options: { enabled?: boolean } = {},
) {
  const queryClient = useQueryClient()

  useSubscription(
    MyCredentialChangedDocument,
    { operationIds },
    {
      onData: (data) => {
        const { action, credentialId, credential } = data.myCredentialChanged
        applyCredentialEvent(queryClient, action, credentialId, credential)
        scheduleRefresh(queryClient, credentialKeys.tagSets())
        // Same rationale as the scoped subscription — see the comment in
        // useCredentialChangedSubscription.
        scheduleRefresh(queryClient, [...credentialKeys.all, "backlinks"])
      },
      enabled: options.enabled ?? true,
    },
  )
}
