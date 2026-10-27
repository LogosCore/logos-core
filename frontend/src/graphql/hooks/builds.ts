import { useMemo } from "react"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
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

/** An operation's builds, newest first; refreshed while any is unfinished. */
export function useBuilds(operationId: string, search = "") {
  return useQuery({
    queryKey: [...buildKeys.list(operationId), search],
    queryFn: () => graphqlClient(BuildsDocument, { operationId, limit: 100, search: search || null }),
    // The last results stay while the next search loads.
    placeholderData: keepPreviousData,
    enabled: operationId !== "",
    refetchInterval: (query) =>
      query.state.data?.builds.some((b) => isUnfinished(b.state)) ? LIVE_LIST_MS : false,
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
