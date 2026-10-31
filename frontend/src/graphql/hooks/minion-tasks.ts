import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { isOlder } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  CancelMinionTaskDocument,
  MinionFileStringsDocument,
  MinionSessionCommandsDocument,
  MinionStreamLinkDocument,
  MinionTaskChangedDocument,
  MinionTaskDocumentDocument,
  MinionTaskOutputDocument,
  MinionTasksDocument,
  QueueMinionTaskDocument,
  type MinionTaskFieldsFragment,
  type MinionTasksQuery,
} from "@/graphql/gql/graphql"

// A session's tasks: one list per session, newest first a page at a time,
// kept current in place by what a mutation returns and by the session's
// live updates.

export const minionTaskKeys = {
  all: ["minionTasks"] as const,
  session: (sessionId: string) => [...minionTaskKeys.all, "session", sessionId] as const,
  commands: (sessionId: string) => [...minionTaskKeys.all, "commands", sessionId] as const,
  output: (id: string) => [...minionTaskKeys.all, "output", id] as const,
  document: (id: string) => [...minionTaskKeys.all, "document", id] as const,
  fileStrings: (fileId: string, filter: MinionFileStringsFilter) =>
    [...minionTaskKeys.all, "fileStrings", fileId, filter] as const,
}

/** What the strings of a file are read with: unset for core's defaults. */
export interface MinionFileStringsFilter {
  minLength: number
  encoding: "" | "ascii" | "utf16le"
  search: string
}

export const DEFAULT_STRINGS_FILTER: MinionFileStringsFilter = { minLength: 4, encoding: "", search: "" }

const PAGE = 50

type Pages = InfiniteData<MinionTasksQuery, string | null>

/** A session's tasks, newest first; older pages on fetchNextPage. */
export function useMinionTasks(sessionId: string) {
  return useInfiniteQuery({
    queryKey: minionTaskKeys.session(sessionId),
    queryFn: ({ pageParam }) => graphqlClient(MinionTasksDocument, { sessionId, last: PAGE, before: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.minionTasks.before ?? undefined,
    enabled: sessionId !== "",
  })
}

/**
 * A task's output and data, read again whenever version changes (its parts
 * and state, say): what was shown stays until the new read is in. A
 * finished task's is kept a while, so going back to it shows it at once.
 */
export function useMinionTaskOutput(id: string | null, version: string, finished: boolean) {
  return useQuery({
    queryKey: [...minionTaskKeys.output(id ?? ""), version],
    queryFn: () => graphqlClient(MinionTaskOutputDocument, { id: id! }),
    enabled: id !== null,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[2] === id ? prev : undefined),
    // Up to 4 MiB each: the reads of a growing output go soon, its last one
    // (the task ended) stays as long as any other query.
    gcTime: finished ? 5 * 60_000 : 10_000,
  })
}

/**
 * A task's stored document as JSON (the console's JSON tab), read while
 * enabled and again whenever version changes.
 */
export function useMinionTaskDocument(id: string, version: string, enabled: boolean) {
  return useQuery({
    queryKey: [...minionTaskKeys.document(id), version],
    queryFn: () => graphqlClient(MinionTaskDocumentDocument, { id }),
    enabled,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[2] === id ? prev : undefined),
    gcTime: 30_000,
  })
}

/**
 * A kept file's printable strings, a page at a time from where the last
 * stopped (an offset in the file), as core reads them: a page may be short,
 * or empty, and not the last.
 */
export function useMinionFileStrings(fileId: string, filter: MinionFileStringsFilter) {
  return useInfiniteQuery({
    queryKey: minionTaskKeys.fileStrings(fileId, filter),
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionFileStringsDocument, {
        fileId,
        after: pageParam,
        first: 500,
        minLength: filter.minLength,
        encoding: filter.encoding || null,
        search: filter.search || null,
      }),
    initialPageParam: 0,
    getNextPageParam: (last) => (last.minionFileStrings.done ? undefined : last.minionFileStrings.next),
    // The file never changes: what was read stays good.
    staleTime: Infinity,
    gcTime: 60_000,
  })
}

/** A signed link to download a stream without a session (for curl): a
 *  path under the API's base. */
export async function fetchMinionStreamLink(taskId: string): Promise<string> {
  const data = await graphqlClient(MinionStreamLinkDocument, { taskId })
  return data.minionStreamLink.path
}

/** A task's output text, for copying it from where it is not shown. */
export async function fetchMinionTaskOutput(id: string): Promise<string> {
  const data = await graphqlClient(MinionTaskOutputDocument, { id })
  return data.minionTask?.output ?? ""
}

/** The commands a session's minion takes, with their params. */
export function useMinionSessionCommands(sessionId: string) {
  return useQuery({
    queryKey: minionTaskKeys.commands(sessionId),
    queryFn: () => graphqlClient(MinionSessionCommandsDocument, { sessionId }),
    enabled: sessionId !== "",
    staleTime: 5 * 60_000,
  })
}

/** Puts a task in its session's list: in its place, or newest at the top. */
export function upsertMinionTask(queryClient: QueryClient, task: MinionTaskFieldsFragment) {
  queryClient.setQueryData<Pages>(minionTaskKeys.session(task.sessionId), (data) => {
    if (!data) return data
    let found = false
    const pages = data.pages.map((p) => {
      const tasks = p.minionTasks.tasks.map((t) => {
        if (t.id !== task.id) return t
        found = true
        // An older copy (a mutation's answer after the live updates it
        // raced) does not take the task back.
        return isOlder(t, task) ? t : task
      })
      return { ...p, minionTasks: { ...p.minionTasks, tasks } }
    })
    if (!found && pages.length > 0) {
      const first = pages[0]
      pages[0] = { ...first, minionTasks: { ...first.minionTasks, tasks: [task, ...first.minionTasks.tasks] } }
    }
    return { ...data, pages }
  })
}

export function useQueueMinionTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { sessionId: string; command: string; args: Record<string, unknown> | null }) =>
      graphqlClient(QueueMinionTaskDocument, vars),
    onSuccess: (data) => upsertMinionTask(queryClient, data.queueMinionTask),
  })
}

export function useCancelMinionTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(CancelMinionTaskDocument, { id }),
    onSuccess: (data) => upsertMinionTask(queryClient, data.cancelMinionTask),
  })
}

/** Keeps a session's tasks live: queued, sent, output, ended. */
export function useMinionTaskSubscription(sessionId: string) {
  const queryClient = useQueryClient()
  useSubscription(
    MinionTaskChangedDocument,
    { sessionId },
    {
      enabled: sessionId !== "",
      onData: (data) => upsertMinionTask(queryClient, data.minionTaskChanged),
    },
  )
}
