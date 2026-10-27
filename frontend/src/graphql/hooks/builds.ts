import { useMemo } from "react"
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import {
  FactoryKindsDocument,
  BuildFilesDocument,
  DeleteBuildFileDocument,
  BuildsDocument,
  BuildDocument,
  CreateBuildDocument,
  CancelBuildDocument,
  DeleteBuildArtifactDocument,
  type CreateBuildMutationVariables,
} from "@/graphql/gql/graphql"

export const buildKeys = {
  all: ["builds"] as const,
  kinds: () => [...buildKeys.all, "kinds"] as const,
  files: (operationId: string) => [...buildKeys.all, "files", operationId] as const,
  list: (operationId: string) => [...buildKeys.all, "list", operationId] as const,
  infiniteList: (params: BuildListParams) => [...buildKeys.list(params.operationId), params] as const,
  detail: (id: string) => [...buildKeys.all, "detail", id] as const,
}

/** How often a list or a build with unfinished builds is refreshed. */
const LIVE_LIST_MS = 2000
const LIVE_BUILD_MS = 1500

const UNFINISHED = new Set(["QUEUED", "BUILDING"])

export function isUnfinished(state: string): boolean {
  return UNFINISHED.has(state)
}

/** Factory kinds that declared a build schema, with their current one. */
export function useFactoryKinds() {
  return useQuery({
    queryKey: buildKeys.kinds(),
    queryFn: () => graphqlClient(FactoryKindsDocument),
  })
}

/** Factory kinds' icons by module name ("" when a kind sent none). */
export function useFactoryIcons(): Map<string, string> {
  const { data } = useFactoryKinds()
  return useMemo(() => new Map((data?.factoryKinds ?? []).map((k) => [k.moduleName, k.icon])), [data])
}

/** An operation's build files, newest first. */
export function useBuildFiles(operationId: string) {
  return useQuery({
    queryKey: buildKeys.files(operationId),
    queryFn: () => graphqlClient(BuildFilesDocument, { operationId }),
    enabled: operationId !== "",
  })
}

export function useInvalidateBuildFiles() {
  const queryClient = useQueryClient()
  return (operationId: string) => queryClient.invalidateQueries({ queryKey: buildKeys.files(operationId) })
}

export function useDeleteBuildFile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DeleteBuildFileDocument, { id }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: [...buildKeys.all, "files"] }),
  })
}

/** What the Builds page lists: the server searches and filters. */
export interface BuildListParams {
  operationId: string
  search: string | null
  states: string[] | null
  moduleNames: string[] | null
}

/**
 * An operation's builds, newest first, a page at a time as the table scrolls.
 * The loaded pages refresh while any of their builds is unfinished.
 */
export function useInfiniteBuilds(params: BuildListParams) {
  return useInfiniteQuery({
    queryKey: buildKeys.infiniteList(params),
    queryFn: ({ pageParam }) =>
      graphqlClient(BuildsDocument, { ...params, first: 20, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.builds.pageInfo.hasNextPage ? (lastPage.builds.pageInfo.endCursor ?? undefined) : undefined,
    enabled: params.operationId !== "",
    refetchInterval: (query) =>
      query.state.data?.pages.some((p) => p.builds.edges.some((e) => isUnfinished(e.node.state))) ? LIVE_LIST_MS : false,
  })
}

/** One build; refreshed until it finishes. */
export function useBuild(id: string) {
  return useQuery({
    queryKey: buildKeys.detail(id),
    queryFn: () => graphqlClient(BuildDocument, { id }),
    enabled: id !== "",
    refetchInterval: (query) => {
      const b = query.state.data?.build
      return b && isUnfinished(b.state) ? LIVE_BUILD_MS : false
    },
  })
}

function useInvalidateBuilds() {
  const queryClient = useQueryClient()
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: [...buildKeys.all, "list"] }),
      queryClient.invalidateQueries({ queryKey: [...buildKeys.all, "detail"] }),
    ])
}

export function useCreateBuild() {
  const invalidate = useInvalidateBuilds()
  return useMutation({
    mutationFn: (vars: CreateBuildMutationVariables) => graphqlClient(CreateBuildDocument, vars),
    onSuccess: invalidate,
  })
}

export function useCancelBuild() {
  const invalidate = useInvalidateBuilds()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(CancelBuildDocument, { id }),
    onSuccess: invalidate,
  })
}

export function useDeleteBuildArtifact() {
  const invalidate = useInvalidateBuilds()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DeleteBuildArtifactDocument, { id }),
    onSuccess: invalidate,
  })
}
