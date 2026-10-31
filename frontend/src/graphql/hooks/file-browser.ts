import { keepPreviousData, useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useSubscription } from "@/hooks/use-subscription"
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

/**
 * Reads again what a changed folder shows: its entries, the tree's folders
 * (a folder in it may be new or gone) and the machine's searches. Only what
 * is on screen goes to core again; the rest when it is next shown.
 */
export function useFileBrowserChanged(sessionId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    FileBrowserChangedDocument,
    { sessionId },
    {
      enabled: sessionId !== "",
      onData: (data) => {
        const folder = data.fileBrowserChanged.folder
        void queryClient.invalidateQueries({
          queryKey: fileBrowserKeys.session(sessionId),
          predicate: (q) => {
            const [, , kind, vars] = q.queryKey as [string, string, string, Record<string, unknown> | undefined]
            if (kind === "search" || kind === "machine") return true
            if (kind !== "entries" || !vars) return false
            return vars.foldersOnly === true || vars.path === null || samePath(vars.path as string, folder)
          },
        })
      },
    },
  )
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
