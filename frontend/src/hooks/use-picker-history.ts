// Picker history: the icon picker's "Frequently used" row and the operation
// picker's "Recent" section.
//
// Both live on the server, on the caller's user (`me.frequentIcons`,
// `me.recentOperations`), and reach the SPA through the Me query. They used to
// be kept in localStorage, which is per browser and per origin, and so
// emptied on a new browser, a different dev port, a cleared cache, or when
// the browser evicted site data on its own — which operators saw as the lists
// "disappearing after a while".
//
// This module stays free of the icon catalogs: usePickerHistorySync runs in
// the app shell, and a catalog import here would put it on first paint.

import { useEffect } from "react"
import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useMe, userKeys } from "@/graphql/hooks/users"
import {
  ImportLocalPreferencesDocument,
  RecordIconUseDocument,
  TouchRecentOperationDocument,
  type MeQuery,
} from "@/graphql/gql/graphql"
import {
  clearLegacyRecentOperations,
  readLegacyRecentOperationIds,
  useScopedOperationStore,
  type ScopedOperation,
} from "@/stores/scoped-operation"

/** Mirrors models.MaxRecentOperations; only used for the optimistic update. */
const RECENT_OPERATIONS_LIMIT = 8

type Me = MeQuery["me"]
type RecentOperation = Me["recentOperations"][number]

const NO_RECENTS: readonly RecentOperation[] = []

function patchMe(queryClient: QueryClient, patch: Partial<Me>) {
  queryClient.setQueryData<MeQuery>(userKeys.me(), (prev) =>
    prev?.me ? { ...prev, me: { ...prev.me, ...patch } } : prev,
  )
}

/** The list with `op` moved to the front, deduplicated, capped. */
export function withRecent(
  recents: readonly RecentOperation[],
  op: RecentOperation,
): RecentOperation[] {
  return [op, ...recents.filter((r) => r.id !== op.id)].slice(
    0,
    RECENT_OPERATIONS_LIMIT,
  )
}

/** The caller's recently scoped operations, newest first. */
export function useRecentOperations(): readonly RecentOperation[] {
  const { data } = useMe()
  return data?.me.recentOperations ?? NO_RECENTS
}

/**
 * Count one icon pick. Not optimistic: the picker closes on pick, and the
 * ranking needs counts the client does not hold, so the server's re-ranked
 * list goes straight into the cache for the next open. A failure costs one
 * uncounted click, which is not worth a toast.
 */
export function useRecordIconUse() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => graphqlClient(RecordIconUseDocument, { name }),
    onSuccess: (data) =>
      patchMe(queryClient, { frequentIcons: data.recordIconUse.frequentIcons }),
  })
}

function useTouchRecentOperation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (op: ScopedOperation) =>
      graphqlClient(TouchRecentOperationDocument, { operationId: op.id }),
    // Optimistic, so the picker already shows the new order if it is
    // reopened before the round trip lands.
    onMutate: async (op) => {
      await queryClient.cancelQueries({ queryKey: userKeys.me() })
      const previous = queryClient.getQueryData<MeQuery>(userKeys.me())
      if (previous?.me) {
        patchMe(queryClient, {
          recentOperations: withRecent(previous.me.recentOperations, {
            id: op.id,
            name: op.name,
            description: op.description,
          }),
        })
      }
      return { previous }
    },
    onError: (_err, _op, context) => {
      if (context?.previous) queryClient.setQueryData(userKeys.me(), context.previous)
    },
    onSuccess: (data) =>
      patchMe(queryClient, {
        recentOperations: data.touchRecentOperation.recentOperations,
      }),
  })
}

/**
 * Whether a scope store transition is the user choosing an operation — the
 * only thing that counts as "recent". Restoring the saved scope on load
 * (hydrate: `hydrated` flips false → true) is not, and neither is the guard
 * re-scoping the same operation to pick up a rename.
 */
export function isScopeChoice(
  state: { scopedOperation: ScopedOperation | null; hydrated: boolean },
  prev: { scopedOperation: ScopedOperation | null; hydrated: boolean },
): state is { scopedOperation: ScopedOperation; hydrated: boolean } {
  return (
    !!state.scopedOperation &&
    prev.hydrated &&
    state.scopedOperation.id !== prev.scopedOperation?.id
  )
}

// --- One-time import of the old localStorage lists ---

const LEGACY_ICONS_KEY = "wiki_frequent_icons"
// GraphQL Int is 32-bit; a hand-edited count past that would fail the whole
// import. The server clamps further.
const MAX_IMPORTED_COUNT = 1_000_000

/** The old localStorage icon list, shaped for the import mutation. */
export function readLegacyFrequentIcons(): {
  name: string
  count: number
  lastUsedAt: string
}[] {
  try {
    const raw = localStorage.getItem(LEGACY_ICONS_KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : []
    if (!Array.isArray(parsed)) return []
    return parsed.flatMap((e) => {
      if (!e || typeof e !== "object") return []
      const { name, count, lastUsed } = e as Record<string, unknown>
      if (typeof name !== "string" || typeof count !== "number") return []
      if (typeof lastUsed !== "number" || !Number.isFinite(lastUsed)) return []
      const at = new Date(lastUsed)
      if (Number.isNaN(at.getTime())) return []
      return [{
        name,
        count: Math.min(Math.max(Math.round(count), 1), MAX_IMPORTED_COUNT),
        lastUsedAt: at.toISOString(),
      }]
    })
  } catch {
    return []
  }
}

function clearLegacyFrequentIcons() {
  try {
    localStorage.removeItem(LEGACY_ICONS_KEY)
  } catch {
    // Storage unavailable: nothing to clear either.
  }
}

// Users an import was started for in this page load. Module-level, so a
// remount (StrictMode, route changes) does not send it twice.
const importStarted = new Set<string>()

function useImportLocalPickerHistory(userId: string | undefined) {
  const queryClient = useQueryClient()
  useEffect(() => {
    if (!userId || importStarted.has(userId)) return
    importStarted.add(userId)

    const frequentIcons = readLegacyFrequentIcons()
    const recentOperationIds = readLegacyRecentOperationIds(userId)
    if (frequentIcons.length === 0 && recentOperationIds.length === 0) return

    graphqlClient(ImportLocalPreferencesDocument, {
      input: { frequentIcons, recentOperationIds },
    })
      .then((data) => {
        // Cleared only once the server has them. The server writes each list
        // only while its own is empty, so clearing is right even when the
        // import changed nothing — this browser's copy is then just stale.
        clearLegacyFrequentIcons()
        clearLegacyRecentOperations(userId)
        const { frequentIcons, recentOperations } = data.importLocalPreferences
        patchMe(queryClient, { frequentIcons, recentOperations })
      })
      .catch(() => {
        // Keep the local copy and try again on the next page load.
        importStarted.delete(userId)
      })
  }, [userId, queryClient])
}

/**
 * Mounted once, by the route guard: records operation choices on the server,
 * and moves any old localStorage history over the first time a user loads a
 * build that has this.
 *
 * Recording hangs off the scope store rather than each place that scopes an
 * operation (switcher, operations table, onboarding, the wiki's foreign
 * operation banner), so a new entry point cannot forget to.
 */
export function usePickerHistorySync(userId: string | undefined) {
  useImportLocalPickerHistory(userId)

  const { mutate: touch } = useTouchRecentOperation()
  useEffect(
    () =>
      useScopedOperationStore.subscribe((state, prev) => {
        if (isScopeChoice(state, prev)) touch(state.scopedOperation)
      }),
    [touch],
  )
}
