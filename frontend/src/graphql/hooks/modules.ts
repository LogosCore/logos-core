import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useSubscription } from "@/hooks/use-subscription"
import {
  ModulesDocument,
  BlockModuleDocument,
  UnblockModuleDocument,
  DeleteModuleDocument,
  ModuleChangedDocument,
  ModuleDocument,
  ModuleActionInvocationsDocument,
  ModuleConfigChangesDocument,
  UpdateModuleConfigDocument,
  InvokeModuleActionDocument,
  type ModuleQuery,
  type ModulesQuery,
  type ModuleChangedSubscription,
  type UpdateModuleConfigMutationVariables,
  type InvokeModuleActionMutationVariables,
} from "@/graphql/gql/graphql"

// Query key factory. The module list is small and unpaginated, so a single
// list namespace keyed by the status filter is enough.
export const moduleKeys = {
  all: ["modules"] as const,
  lists: () => [...moduleKeys.all, "list"] as const,
  list: (status: string[] | null) => [...moduleKeys.lists(), status] as const,
  details: () => [...moduleKeys.all, "detail"] as const,
  detail: (instance: string) => [...moduleKeys.details(), instance] as const,
  invocations: (instance: string) =>
    [...moduleKeys.all, "invocations", instance] as const,
  configChanges: (instance: string) =>
    [...moduleKeys.all, "config-changes", instance] as const,
}

export function useModules(status: string[] | null) {
  return useQuery({
    queryKey: moduleKeys.list(status),
    queryFn: () => graphqlClient(ModulesDocument, { status }),
  })
}

/**
 * An operator's lifecycle change on an instance. The moduleChanged subscription
 * refreshes the lists and the instance's page too; invalidating here as well
 * keeps the table right even if the stream is momentarily paused.
 */
function useModuleAdminMutation<V>(run: (vars: V & { instance: string }) => Promise<unknown>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: run,
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: moduleKeys.lists() })
      queryClient.invalidateQueries({ queryKey: moduleKeys.detail(vars.instance) })
    },
  })
}

/** Block an instance: core refuses its registrations and traffic until unblocked. */
export function useBlockModule() {
  return useModuleAdminMutation<{ reason: string | null }>((v) => graphqlClient(BlockModuleDocument, v))
}

/** Lift a block: the instance may register again. */
export function useUnblockModule() {
  return useModuleAdminMutation<object>((v) => graphqlClient(UnblockModuleDocument, { instance: v.instance }))
}

/** Delete a gone (deregistered or dead) instance's row. */
export function useDeleteModule() {
  return useModuleAdminMutation<object>((v) => graphqlClient(DeleteModuleDocument, { instance: v.instance }))
}

type ModuleEvent = ModuleChangedSubscription["moduleChanged"]

/**
 * Applies a moduleChanged event to the cached module lists. Lifecycle events
 * (register, deregister, dead) can move a row in or out of a status filter, so
 * they refetch. Heartbeats and management changes only change the row, which
 * travels on the event: it is patched in place, with no request — with every
 * module heartbeating every 30s, refetching the list on each would be a
 * steady stream of full list loads.
 */
function applyToLists(queryClient: ReturnType<typeof useQueryClient>, ev: ModuleEvent) {
  if ((ev.kind === "HEARTBEAT" || ev.kind === "CHANGED") && ev.module) {
    const row = ev.module
    queryClient.setQueriesData<ModulesQuery>({ queryKey: moduleKeys.lists() }, (old) =>
      old ? { modules: old.modules.map((m) => (m.instance === row.instance ? { ...m, ...row } : m)) } : old,
    )
    return
  }
  queryClient.invalidateQueries({ queryKey: moduleKeys.lists() })
}

/** Keeps the Modules list live (see applyToLists). */
export function useModuleChangedSubscription() {
  const queryClient = useQueryClient()

  useSubscription(ModuleChangedDocument, undefined, {
    onData: (data) => applyToLists(queryClient, data.moduleChanged),
  })
}

// --- Module detail / management ---

const HISTORY_LIMIT = 50

/**
 * One module with its management manifest. While a config revision is being
 * applied (PENDING) it also polls, as a fallback in case the moduleChanged
 * event announcing the ack is missed — the subscription is the primary path.
 */
export function useModule(instance: string) {
  return useQuery({
    queryKey: moduleKeys.detail(instance),
    queryFn: () => graphqlClient(ModuleDocument, { instance }),
    enabled: instance !== "",
    refetchInterval: (query) =>
      query.state.data?.module?.management?.syncState === "PENDING" ? 3000 : false,
  })
}

export function useModuleActionInvocations(instance: string) {
  return useQuery({
    queryKey: moduleKeys.invocations(instance),
    queryFn: () =>
      graphqlClient(ModuleActionInvocationsDocument, {
        instance,
        limit: HISTORY_LIMIT,
      }),
    enabled: instance !== "",
  })
}

export function useModuleConfigChanges(instance: string) {
  return useQuery({
    queryKey: moduleKeys.configChanges(instance),
    queryFn: () =>
      graphqlClient(ModuleConfigChangesDocument, {
        instance,
        limit: HISTORY_LIMIT,
      }),
    enabled: instance !== "",
  })
}

/**
 * Real-time updates for the detail page. Every event for this instance
 * refetches the detail (a heartbeat brings new status values, which the event
 * row does not carry). Only real changes — config written or applied, action
 * finished, lifecycle — also refetch the config and action histories; a
 * heartbeat changes neither. Cached lists are kept current as on the list page.
 */
export function useModuleDetailSubscription(instance: string) {
  const queryClient = useQueryClient()

  useSubscription(ModuleChangedDocument, undefined, {
    enabled: instance !== "",
    onData: (data) => {
      const ev = data.moduleChanged
      applyToLists(queryClient, ev)
      if (ev.instance !== instance) return
      queryClient.invalidateQueries({ queryKey: moduleKeys.detail(instance) })
      if (ev.kind === "HEARTBEAT") return
      queryClient.invalidateQueries({ queryKey: moduleKeys.configChanges(instance) })
      // Actions run by another admin (or from another tab) land here too.
      queryClient.invalidateQueries({ queryKey: moduleKeys.invocations(instance) })
    },
  })
}

/**
 * Set / clear an instance's property values. The mutation returns the updated
 * module, which replaces the cached detail so the sync badge flips to PENDING
 * without waiting for a refetch.
 */
export function useUpdateModuleConfig() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: UpdateModuleConfigMutationVariables) =>
      graphqlClient(UpdateModuleConfigDocument, vars),
    onSuccess: (data, vars) => {
      queryClient.setQueryData<ModuleQuery>(moduleKeys.detail(vars.instance), {
        module: data.updateModuleConfig,
      })
      queryClient.invalidateQueries({
        queryKey: moduleKeys.configChanges(vars.instance),
      })
    },
  })
}

export function useInvokeModuleAction() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: InvokeModuleActionMutationVariables) =>
      graphqlClient(InvokeModuleActionDocument, vars),
    onSettled: (_data, _err, vars) => {
      queryClient.invalidateQueries({
        queryKey: moduleKeys.invocations(vars.instance),
      })
    },
  })
}
