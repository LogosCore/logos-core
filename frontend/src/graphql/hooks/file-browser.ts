import { useEffect, useRef } from "react"
import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useSubscription } from "@/hooks/use-subscription"
import { crumbs } from "@/components/minion-sessions/dir-listing"
import {
  FileBrowserChangedDocument,
  FileBrowserDocument,
  FileBrowserEntriesDocument,
  FileBrowserSearchDocument,
  MinionTaskDocument,
} from "@/graphql/gql/graphql"

// The file browser's reads (core: file_browser.graphql): a machine's
// snapshot, asked for by one of its sessions. Read again where it changes:
// core says which folder as each task that changes one ends.

export const fileBrowserKeys = {
  all: ["fileBrowser"] as const,
  session: (sessionId: string) => [...fileBrowserKeys.all, sessionId] as const,
  machine: (sessionId: string) => [...fileBrowserKeys.session(sessionId), "machine"] as const,
  entries: (sessionId: string, path: string | null, filter: FsEntriesFilter, foldersOnly: boolean) =>
    [...fileBrowserKeys.session(sessionId), "entries", { path, foldersOnly, ...filter }] as const,
  search: (sessionId: string, query: string, showDeleted: boolean) =>
    [...fileBrowserKeys.session(sessionId), "search", { query, showDeleted }] as const,
}

/** How a folder's entries are read: by core, a page at a time. */
export interface FsEntriesFilter {
  /** name (folders first, then by name), size or modified. */
  sort: "name" | "size" | "modified"
  desc: boolean
  /** Names that hold it, any case. */
  search: string
  showDeleted: boolean
}

const PAGE = 200

/** The session's machine: its key and the sessions that feed it. */
export function useFileBrowser(sessionId: string) {
  return useQuery({
    queryKey: fileBrowserKeys.machine(sessionId),
    queryFn: () => graphqlClient(FileBrowserDocument, { sessionId }),
    enabled: sessionId !== "",
  })
}

/** A folder's entries (path null: the machine's roots), a page at a time. */
export function useFileBrowserEntries(
  sessionId: string,
  path: string | null,
  filter: FsEntriesFilter,
  { foldersOnly = false, enabled = true }: { foldersOnly?: boolean; enabled?: boolean } = {},
) {
  return useInfiniteQuery({
    queryKey: fileBrowserKeys.entries(sessionId, path, filter, foldersOnly),
    queryFn: ({ pageParam }) =>
      graphqlClient(FileBrowserEntriesDocument, {
        sessionId,
        path,
        after: pageParam,
        first: PAGE,
        search: filter.search || null,
        sort: filter.sort,
        desc: filter.desc,
        showDeleted: filter.showDeleted,
        foldersOnly,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.fileBrowserEntries.next ?? undefined,
    enabled: enabled && sessionId !== "",
    // A new sort or search keeps what was shown until its first page is in.
    placeholderData: keepPreviousData,
  })
}

/** The machine's entries anywhere whose names match query (a part, or a glob). */
export function useFileBrowserSearch(sessionId: string, query: string, showDeleted: boolean) {
  return useInfiniteQuery({
    queryKey: fileBrowserKeys.search(sessionId, query, showDeleted),
    queryFn: ({ pageParam }) =>
      graphqlClient(FileBrowserSearchDocument, { sessionId, query, after: pageParam, first: PAGE, showDeleted }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.fileBrowserSearch.next ?? undefined,
    enabled: sessionId !== "" && query.trim().length >= 2,
    placeholderData: keepPreviousData,
  })
}

/** How long changes gather before what they touch is read again. */
const CHANGE_BATCH_MS = 1000

/**
 * Reads again what changed folders touch, once a second at most: a walk
 * changes dozens of folders a second, and reading the tree, the folder and
 * the searches again for each one ran to thousands of requests a second.
 * Only what is on screen goes to core again; the rest when it is next shown.
 */
export function useFileBrowserChanged(sessionId: string) {
  const queryClient = useQueryClient()
  const pending = useRef(new Set<string>())
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current)
      timer.current = null
      pending.current.clear()
    },
    [sessionId],
  )
  useSubscription(
    FileBrowserChangedDocument,
    { sessionId },
    {
      enabled: sessionId !== "",
      onData: (data) => {
        pending.current.add(data.fileBrowserChanged.folder)
        if (timer.current) return
        timer.current = setTimeout(() => {
          timer.current = null
          const changed = [...pending.current]
          pending.current.clear()
          void queryClient.invalidateQueries({
            queryKey: fileBrowserKeys.session(sessionId),
            predicate: (q) => touches(q.queryKey[2], q.queryKey[3], changed),
          })
        }, CHANGE_BATCH_MS)
      },
    },
  )
}

/**
 * Whether a file browser query shows what changed folders touch: a folder's
 * entries (and its sub-folders, the tree's) are its own and, as the folder
 * itself is one of them (listed now, or not read), its parent's; the roots
 * when a root changed; every search.
 */
export function touches(kind: unknown, vars: unknown, changed: readonly string[]): boolean {
  if (kind === "search") return true
  if (kind !== "entries" || typeof vars !== "object" || vars === null) return false
  const path = (vars as { path?: string | null }).path ?? null
  return changed.some((folder) => {
    const parent = parentPath(folder)
    if (path === null) return parent === null
    return samePath(path, folder) || (parent !== null && samePath(path, parent))
  })
}

/** A host path's folder; null for a root. */
function parentPath(path: string): string | null {
  const sep = path.startsWith("\\\\") || /^[A-Za-z]:/.test(path) ? "\\" : "/"
  const way = crumbs(path, sep)
  return way.length > 1 ? way[way.length - 2].path : null
}

/** Paths as hosts compare them: a Windows one (\) without case. */
export function samePath(a: string, b: string): boolean {
  return a === b || (a.includes("\\") && a.toLowerCase() === b.toLowerCase())
}

/** A task, with its file: what a downloaded mark opens. */
export function useMinionTask(id: string | null) {
  return useQuery({
    queryKey: ["minionTasks", "one", id ?? ""],
    queryFn: () => graphqlClient(MinionTaskDocument, { id: id! }),
    enabled: id !== null,
  })
}
