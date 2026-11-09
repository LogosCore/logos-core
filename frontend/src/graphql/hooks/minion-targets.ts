import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useSubscription } from "@/hooks/use-subscription"
import {
  AddMinionTargetDocument,
  BuildBindConnectionsDocument,
  CheckMinionTargetDocument,
  ConnectMinionTargetDocument,
  DelegateSessionsDocument,
  DisconnectMinionTargetDocument,
  MinionTargetChangedDocument,
  MinionSessionTargetDocument,
  MinionTargetsDocument,
  RemoveMinionTargetDocument,
  UpdateMinionTargetDocument,
  type AddMinionTargetMutationVariables,
  type UpdateMinionTargetMutationVariables,
} from "@/graphql/gql/graphql"

// Targets: where bind minions (a page on a web server) are reached. Core
// calls a target through its build connection's channel; the page's hello
// opens a session, linked to the target from then on.

export const minionTargetKeys = {
  all: ["minion-targets"] as const,
  lists: () => [...minionTargetKeys.all, "list"] as const,
  list: (params: TargetListParams) => [...minionTargetKeys.lists(), params] as const,
  bindConnections: (buildId: string) => [...minionTargetKeys.all, "bind-connections", buildId] as const,
  ofSession: (sessionId: string) => [...minionTargetKeys.all, "session", sessionId] as const,
}

/** What a targets list shows: the server searches and filters. */
export interface TargetListParams {
  operationId: string
  search: string | null
  /** One build's targets; the operation's when null. */
  buildId: string | null
}

/** An operation's (or a build's) targets, newest first, a page at a time. */
export function useInfiniteMinionTargets(params: TargetListParams) {
  return useInfiniteQuery({
    queryKey: minionTargetKeys.list(params),
    queryFn: ({ pageParam }) => graphqlClient(MinionTargetsDocument, { ...params, first: 30, after: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.minionTargets.pageInfo.hasNextPage ? (last.minionTargets.pageInfo.endCursor ?? undefined) : undefined,
    enabled: params.operationId !== "",
  })
}

/** What a delegate picker lists: sessions that can carry a target of a channel kind, searched by the server. */
export interface DelegateSessionParams {
  operationId: string
  moduleName: string
  kind: string
  version: number
  search: string | null
}

/** The sessions whose minions can be a target's delegate, a page at a time. */
export function useInfiniteDelegateSessions(params: DelegateSessionParams, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: [...minionTargetKeys.all, "delegates", params] as const,
    queryFn: ({ pageParam }) =>
      graphqlClient(DelegateSessionsDocument, {
        operationId: params.operationId,
        delegatesFor: { moduleName: params.moduleName, kind: params.kind, version: params.version },
        search: params.search,
        first: 30,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.minionSessions.pageInfo.hasNextPage ? (last.minionSessions.pageInfo.endCursor ?? undefined) : undefined,
    enabled: enabled && params.operationId !== "",
  })
}

/** A build's connections of a bind kind, with the channel's target fields: what targets are added to. */
export function useBindConnections(buildId: string) {
  return useQuery({
    queryKey: minionTargetKeys.bindConnections(buildId),
    queryFn: () => graphqlClient(BuildBindConnectionsDocument, { id: buildId }),
    enabled: buildId !== "",
    select: (data) => data.build?.bindConnections ?? [],
  })
}

function sessionTargetQuery(sessionId: string) {
  return {
    queryKey: minionTargetKeys.ofSession(sessionId),
    queryFn: () => graphqlClient(MinionSessionTargetDocument, { id: sessionId }),
    enabled: sessionId !== "",
  }
}

/** The target whose page a session is; null for any other session. */
export function useSessionTarget(sessionId: string) {
  return useQuery({ ...sessionTargetQuery(sessionId), select: (data) => data.minionSession?.bindTarget ?? null })
}

/**
 * A session as targets see it: whether it is a bind-call one (a page core
 * calls), its operation, and its target (null when it has none).
 */
export function useSessionReach(sessionId: string) {
  return useQuery({
    ...sessionTargetQuery(sessionId),
    select: (data) => ({
      bind: data.minionSession?.connKind === "bind-call",
      operationId: data.minionSession?.operationId ?? "",
      target: data.minionSession?.bindTarget ?? null,
    }),
  })
}

/**
 * Every write refetches what shows targets (the lists, a session's): a
 * target's state moves with each call anyway.
 */
function useInvalidateTargets() {
  const queryClient = useQueryClient()
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: minionTargetKeys.lists() }),
      queryClient.invalidateQueries({ queryKey: [...minionTargetKeys.all, "session"] }),
    ])
}

export function useAddMinionTarget() {
  const invalidate = useInvalidateTargets()
  return useMutation({
    mutationFn: (vars: AddMinionTargetMutationVariables) => graphqlClient(AddMinionTargetDocument, vars),
    onSuccess: invalidate,
  })
}

export function useUpdateMinionTarget() {
  const invalidate = useInvalidateTargets()
  return useMutation({
    mutationFn: (vars: UpdateMinionTargetMutationVariables) => graphqlClient(UpdateMinionTargetDocument, vars),
    onSuccess: invalidate,
  })
}

/** Check now: core calls the target and answers once the call is done. */
export function useCheckMinionTarget() {
  const invalidate = useInvalidateTargets()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(CheckMinionTargetDocument, { id }),
    onSuccess: invalidate,
  })
}

/** Connect: a bind-persistent target's connection opens now, whatever its policy. */
export function useConnectMinionTarget() {
  const invalidate = useInvalidateTargets()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(ConnectMinionTargetDocument, { id }),
    onSuccess: invalidate,
  })
}

/** Disconnect: a bind-persistent target's connection closes (one of policy always opens again). */
export function useDisconnectMinionTarget() {
  const invalidate = useInvalidateTargets()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DisconnectMinionTargetDocument, { id }),
    onSuccess: invalidate,
  })
}

export function useRemoveMinionTarget() {
  const invalidate = useInvalidateTargets()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(RemoveMinionTargetDocument, { id }),
    onSuccess: invalidate,
  })
}

/**
 * Keeps an operation's targets live where they show: any change (added,
 * called, edited, removed) refetches them. Targets change once a call ends, seldom
 * enough that refetching beats patching rows in place.
 */
export function useMinionTargetChangedSubscription(operationId: string) {
  const invalidate = useInvalidateTargets()
  useSubscription(
    MinionTargetChangedDocument,
    { operationId },
    {
      enabled: operationId !== "",
      onData: () => void invalidate(),
    },
  )
}
