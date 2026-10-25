import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useSubscription } from "@/hooks/use-subscription"
import {
  ModulesDocument,
  RemoveModuleDocument,
  ModuleChangedDocument,
  ModuleDocument,
  ModuleActionInvocationsDocument,
  ModuleConfigChangesDocument,
  UpdateModuleConfigDocument,
  InvokeModuleActionDocument,
  type ModuleQuery,
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
 * Soft-remove (deregister) a module instance. The list is invalidated by the
 * moduleChanged subscription, but we also invalidate here so the table reflects
 * the change immediately even if the subscription stream is momentarily paused.
 */
export function useRemoveModule() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (instance: string) =>
      graphqlClient(RemoveModuleDocument, { instance }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: moduleKeys.lists() })
    },
  })
}

/**
 * Subscribe to real-time module lifecycle changes.
 *
 * When a module registers, deregisters, or is declared dead — including the
 * "removed but still alive → re-registers" flip — the list query is invalidated
 * so the table refetches. The full row travels on the event, but since the list
 * is unpaginated a plain invalidate is simpler than surgically patching it.
 */
export function useModuleChangedSubscription() {
  const queryClient = useQueryClient()

  useSubscription(ModuleChangedDocument, undefined, {
    onData: () => {
      queryClient.invalidateQueries({ queryKey: moduleKeys.lists() })
    },
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
 * Real-time updates for the detail page: any moduleChanged event for this
 * instance (lifecycle, heartbeats, config sync progress, actions) refetches
 * the detail, its config history and its action history. The list is refreshed too so
 * going back shows current rows.
 */
export function useModuleDetailSubscription(instance: string) {
  const queryClient = useQueryClient()

  useSubscription(ModuleChangedDocument, undefined, {
    enabled: instance !== "",
    onData: (data) => {
      queryClient.invalidateQueries({ queryKey: moduleKeys.lists() })
      if (data.moduleChanged.instance !== instance) return
      queryClient.invalidateQueries({ queryKey: moduleKeys.detail(instance) })
      queryClient.invalidateQueries({
        queryKey: moduleKeys.configChanges(instance),
      })
      // Actions run by another admin (or from another tab) land here too.
      queryClient.invalidateQueries({
        queryKey: moduleKeys.invocations(instance),
      })
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
