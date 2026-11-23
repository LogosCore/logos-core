import { useEffect, useRef } from "react"
import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { isOlder } from "@/lib/live-lists"
import { useSubscription } from "@/hooks/use-subscription"
import {
  CancelMinionTaskDocument,
  RunMinionTaskNowDocument,
  CreateMinionVpnKeyDocument,
  DeleteRecordingDocument,
  RecordingsDocument,
  DisconnectMinionVpnClientDocument,
  SetMinionTunnelRoutesDocument,
  MinionDesktopSettingsDocument,
  MinionFileStringsDocument,
  MinionSessionCommandsDocument,
  MinionStreamLinkDocument,
  MinionTaskChangedDocument,
  MinionTaskDocumentDocument,
  MinionTaskFeedbackDocument,
  MinionTaskImportantCountDocument,
  MinionTaskOutputDocument,
  MinionTaskProcessesDocument,
  SetMinionProcessMarkDocument,
  ResetMinionProcessMarkDocument,
  MinionTasksDocument,
  MinionTunnelStreamsDocument,
  QueueMinionTaskDocument,
  SetMinionTaskImportantDocument,
  SetMinionTunnelCapDocument,
  type RecordingsQuery,
  type MinionProcessFold,
  type MinionSessionMarkInput,
  type MinionTaskProcessesQuery,
  type MinionProcessOrder,
  type MinionTaskFieldsFragment,
  type MinionTasksQuery,
} from "@/graphql/gql/graphql"

// A session's tasks: a list per session and filter, newest first a page at
// a time, kept current in place by what a mutation returns and by the
// session's live updates.

/**
 * What a session's task list is narrowed to: core's search of each task's
 * line as the console shows it, the tasks marked important alone, and a
 * composite command's task with its subtasks alone (composite, its id; ""
 * for none).
 */
export interface MinionTaskFilter {
  search: string
  important: boolean
  composite: string
}

export const NO_TASK_FILTER: MinionTaskFilter = { search: "", important: false, composite: "" }

/** Whether a filter narrows the list at all (a blank search does not). */
export function narrows(f: MinionTaskFilter): boolean {
  return f.important || f.search.trim() !== "" || f.composite !== ""
}

export const minionTaskKeys = {
  all: ["minionTasks"] as const,
  /** Every list of a session's tasks, whatever narrows it. */
  session: (sessionId: string) => [...minionTaskKeys.all, "session", sessionId] as const,
  list: (sessionId: string, f: MinionTaskFilter) =>
    [...minionTaskKeys.session(sessionId), { search: f.search.trim(), important: f.important, composite: f.composite }] as const,
  importantCount: (sessionId: string) => [...minionTaskKeys.all, "importantCount", sessionId] as const,
  commands: (sessionId: string) => [...minionTaskKeys.all, "commands", sessionId] as const,
  desktopSettings: (front: string) => [...minionTaskKeys.all, "desktopSettings", front] as const,
  output: (id: string) => [...minionTaskKeys.all, "output", id] as const,
  document: (id: string) => [...minionTaskKeys.all, "document", id] as const,
  feedback: (id: string) => [...minionTaskKeys.all, "feedback", id] as const,
  fileStrings: (fileId: string, filter: MinionFileStringsFilter) =>
    [...minionTaskKeys.all, "fileStrings", fileId, filter] as const,
  tunnelStreams: (taskId: string, filter: TunnelStreamsFilter) => [...minionTaskKeys.all, "tunnelStreams", taskId, filter] as const,
  recordings: (taskId: string) => [...minionTaskKeys.all, "recordings", taskId] as const,
  processes: (taskId: string, view: ProcessesView) => [...minionTaskKeys.all, "processes", taskId, view] as const,
}

/**
 * How a process list is read: searched by core, ordered (desc: the other
 * way); in the tree, folded as fold says, flip's pids the other way.
 */
export interface ProcessesView {
  search: string
  order: MinionProcessOrder
  desc: boolean
  fold: MinionProcessFold
  flip: readonly number[]
}

/** The rows a page of a process list reads, after the first. */
export const PROCESS_PAGE = 100

/**
 * A process_list task's processes, a page of 100 at a time (the first of
 * firstSize: after a fold, as many as the view had, so it stays where it
 * was), ordered, searched and folded by core; read again when version (its
 * parts, its state) moves.
 */
export function useMinionTaskProcesses(taskId: string, view: ProcessesView, version: string, firstSize = PROCESS_PAGE, enabled = true) {
  return useInfiniteQuery({
    queryKey: [...minionTaskKeys.processes(taskId, view), version],
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionTaskProcessesDocument, {
        taskId,
        search: view.search.trim() || null,
        order: view.order,
        desc: view.desc,
        fold: view.fold,
        flip: [...view.flip],
        first: pageParam === undefined ? firstSize : PROCESS_PAGE,
        after: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.minionTaskProcesses.pageInfo.hasNextPage ? (last.minionTaskProcesses.pageInfo.endCursor ?? undefined) : undefined,
    placeholderData: keepPreviousData,
    enabled,
  })
}

/** What a tunnel task's streams are narrowed by: the server's search, and OPEN or ENDED ("" for all). */
export interface TunnelStreamsFilter {
  search: string
  state: "" | "OPEN" | "ENDED"
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

/**
 * A session's tasks, newest first, those filter keeps; older pages on
 * fetchNextPage. Another filter shows the session's rows before it until
 * its own come.
 */
export function useMinionTasks(sessionId: string, filter: MinionTaskFilter = NO_TASK_FILTER) {
  const search = filter.search.trim()
  return useInfiniteQuery({
    queryKey: minionTaskKeys.list(sessionId, filter),
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionTasksDocument, {
        sessionId,
        last: PAGE,
        before: pageParam,
        search: search || null,
        important: filter.important || null,
        composite: filter.composite || null,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.minionTasks.before ?? undefined,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[2] === sessionId ? prev : undefined),
    enabled: sessionId !== "",
  })
}

/** How many of a session's tasks are marked important. */
export function useMinionTaskImportantCount(sessionId: string) {
  return useQuery({
    queryKey: minionTaskKeys.importantCount(sessionId),
    queryFn: () => graphqlClient(MinionTaskImportantCountDocument, { sessionId }),
    select: (data) => data.minionTaskImportantCount,
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
 * What its minion said of a task as feedback, read while enabled (it has
 * some) and again whenever version changes (more came, it was marked read).
 */
export function useMinionTaskFeedback(id: string, version: string, enabled: boolean) {
  return useQuery({
    queryKey: [...minionTaskKeys.feedback(id), version],
    queryFn: () => graphqlClient(MinionTaskFeedbackDocument, { id }),
    enabled,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[2] === id ? prev : undefined),
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
/**
 * A file's strings a page at a time. available: how much of it there is to
 * read (all of it once kept; for a file still coming, what has come): a page
 * that read all that has come waits, and the next is read once more has.
 */
export function useMinionFileStrings(fileId: string, filter: MinionFileStringsFilter, available: number) {
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
    getNextPageParam: (last) =>
      last.minionFileStrings.done || last.minionFileStrings.readTo >= available ? undefined : last.minionFileStrings.next,
    // What has come of a file never changes: what was read stays good.
    staleTime: Infinity,
    gcTime: 60_000,
  })
}

/**
 * A tunnel task's streams, the latest first a page at a time, as the server
 * searches and filters them. The pages read are read again, in place, when
 * version changes (the tunnel's numbers, which its task's live updates
 * carry): a list scrolled down stays where it is. Core keeps them only
 * while the task runs: enabled false reads none.
 */
export function useMinionTunnelStreams(taskId: string, filter: TunnelStreamsFilter, version: string, enabled = true) {
  const query = useInfiniteQuery({
    queryKey: minionTaskKeys.tunnelStreams(taskId, filter),
    queryFn: ({ pageParam }) =>
      graphqlClient(MinionTunnelStreamsDocument, {
        taskId,
        last: 50,
        before: pageParam,
        search: filter.search || null,
        state: filter.state || null,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.minionTunnelStreams.before ?? undefined,
    // A new search shows the rows before it until its own come.
    placeholderData: keepPreviousData,
    enabled,
  })
  const { refetch } = query
  const seen = useRef(version)
  useEffect(() => {
    if (seen.current === version) return
    seen.current = version
    if (enabled) void refetch()
  }, [version, refetch, enabled])
  return query
}

/**
 * A desktop task's recordings, the latest first a page at a time. The pages
 * read are read again, in place, when version changes (the task's rev:
 * core bumps it as one starts, ends or is deleted, and its live update
 * says so).
 */
export function useRecordings(taskId: string, version: string, enabled = true) {
  const query = useInfiniteQuery({
    queryKey: minionTaskKeys.recordings(taskId),
    queryFn: ({ pageParam }) => graphqlClient(RecordingsDocument, { taskId, last: 50, before: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.recordings.before ?? undefined,
    enabled,
  })
  const { refetch } = query
  const seen = useRef(version)
  useEffect(() => {
    if (seen.current === version) return
    seen.current = version
    if (enabled) void refetch()
  }, [version, refetch, enabled])
  return query
}

type RecordingPages = InfiniteData<RecordingsQuery, string | null>

/** Deletes a recording that ended: gone from its task's list at once. */
export function useDeleteRecording(taskId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(DeleteRecordingDocument, { id }),
    onSuccess: (_data, id) =>
      queryClient.setQueryData<RecordingPages>(minionTaskKeys.recordings(taskId), (data) =>
        data
          ? {
              ...data,
              pages: data.pages.map((p) => ({
                ...p,
                recordings: {
                  ...p.recordings,
                  recordings: p.recordings.recordings.filter((r) => r.id !== id),
                },
              })),
            }
          : data,
      ),
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

/** The settings a desktop of front opens with: core's, the same until it is built again. */
export function useMinionDesktopSettings(front: string, enabled = true) {
  return useQuery({
    queryKey: minionTaskKeys.desktopSettings(front),
    queryFn: () => graphqlClient(MinionDesktopSettingsDocument, { front }),
    enabled: enabled && front !== "",
    staleTime: 30 * 60_000,
  })
}

/**
 * Whether a task a narrowed list does not hold may belong in it now, so the
 * list is read again: one marked important, for a list of those; a
 * composite task's new subtask, for a list of it and its subtasks; a new
 * one (rev 1), for a search, which reads its line. A task's line does not
 * change, so a search that did not find it never will.
 */
export function mayJoin(filter: MinionTaskFilter, task: MinionTaskFieldsFragment): boolean {
  if (filter.composite && task.parentId !== filter.composite && task.id !== filter.composite) return false
  if (filter.important) return !!task.importantAt
  return task.rev === 1
}

/**
 * Puts a task in its session's lists: in its place in each that holds it
 * (an unmarked one stays in a list of the important ones until it is read
 * again); a new one at the top of the whole list. A narrowed list that may
 * now hold it is read again (mayJoin), and so is the count of important
 * tasks when the task's mark may have changed.
 */
export function upsertMinionTask(queryClient: QueryClient, task: MinionTaskFieldsFragment) {
  let held = false
  let flipped = false
  for (const [key, data] of queryClient.getQueriesData<Pages>({ queryKey: minionTaskKeys.session(task.sessionId) })) {
    if (!data) continue
    let found = false
    const pages = data.pages.map((p) => {
      const tasks = p.minionTasks.tasks.map((t) => {
        if (t.id !== task.id) return t
        found = true
        // An older copy (a mutation's answer after the live updates it
        // raced) does not take the task back.
        if (isOlder(t, task)) return t
        if (!!t.importantAt !== !!task.importantAt) flipped = true
        return task
      })
      return { ...p, minionTasks: { ...p.minionTasks, tasks } }
    })
    held ||= found
    const filter = key[3] as MinionTaskFilter | undefined
    if (!found && filter && narrows(filter)) {
      if (mayJoin(filter, task)) void queryClient.invalidateQueries({ queryKey: key, exact: true })
      continue
    }
    if (!found && pages.length > 0) {
      const first = pages[0]
      pages[0] = { ...first, minionTasks: { ...first.minionTasks, tasks: [task, ...first.minionTasks.tasks] } }
    }
    queryClient.setQueryData<Pages>(key, { ...data, pages })
  }
  if (flipped || (!held && task.importantAt)) {
    void queryClient.invalidateQueries({ queryKey: minionTaskKeys.importantCount(task.sessionId) })
  }
}

export function useQueueMinionTask() {
  const queryClient = useQueryClient()
  return useMutation({
    // at: a time for it (RFC 3339): it waits SCHEDULED until then.
    mutationFn: (vars: { sessionId: string; command: string; args: Record<string, unknown> | null; at?: string | null }) =>
      graphqlClient(QueueMinionTaskDocument, vars),
    onSuccess: (data) => upsertMinionTask(queryClient, data.queueMinionTask),
  })
}

/** Takes a scheduled task on at once: it goes on as queued now. */
export function useRunMinionTaskNow() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(RunMinionTaskNowDocument, { id }),
    onSuccess: (data) => upsertMinionTask(queryClient, data.runMinionTaskNow),
  })
}

/** Marks a task important for the team, or no longer. */
export function useSetMinionTaskImportant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; important: boolean }) => graphqlClient(SetMinionTaskImportantDocument, vars),
    onSuccess: (data) => upsertMinionTask(queryClient, data.setMinionTaskImportant),
  })
}

export function useCancelMinionTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => graphqlClient(CancelMinionTaskDocument, { id }),
    onSuccess: (data) => upsertMinionTask(queryClient, data.cancelMinionTask),
  })
}

/** Changes a running tunnel's bandwidth cap: bytes a second each way, 0 for none. Its task's live update says the new one. */
export function useSetMinionTunnelCap() {
  return useMutation({
    mutationFn: (vars: { taskId: string; cap: number }) => graphqlClient(SetMinionTunnelCapDocument, vars),
  })
}

/** Changes a running VPN tunnel's routes: its clients take them at once. */
export function useSetMinionTunnelRoutes() {
  return useMutation({
    mutationFn: (vars: { taskId: string; routes: string[] }) => graphqlClient(SetMinionTunnelRoutesDocument, vars),
  })
}

/** Makes the caller a key for one client of a running VPN tunnel. */
export function useCreateMinionVPNKey() {
  return useMutation({
    mutationFn: (vars: { taskId: string }) => graphqlClient(CreateMinionVpnKeyDocument, vars),
  })
}

/** Ends a client of a VPN tunnel. */
export function useDisconnectMinionVPNClient() {
  return useMutation({
    mutationFn: (vars: { taskId: string; client: string }) => graphqlClient(DisconnectMinionVpnClientDocument, vars),
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

/** A process's mark as a process list carries it. */
type ProcessMark = MinionTaskProcessesQuery["minionTaskProcesses"]["edges"][number]["node"]["mark"]

/**
 * A mark set on the processes of a name, in every process list cached, of
 * every task: each process of the name, whatever its case, takes it (null:
 * none), as core gives it on the next read.
 */
export function applyProcessMark(qc: QueryClient, name: string, mark: ProcessMark) {
  const key = name.trim().toLowerCase()
  qc.setQueriesData<InfiniteData<MinionTaskProcessesQuery>>({ queryKey: [...minionTaskKeys.all, "processes"] }, (old) =>
    old
      ? {
          ...old,
          pages: old.pages.map((page) => ({
            ...page,
            minionTaskProcesses: {
              ...page.minionTaskProcesses,
              edges: page.minionTaskProcesses.edges.map((e) => (e.node.name.toLowerCase() === key ? { ...e, node: { ...e.node, mark } } : e)),
            },
          })),
        }
      : old,
  )
}

/** Goes back to a name's built-in mark (the caller's own, or their clearing, taken away), in every list at once. */
export function useResetMinionProcessMark() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => graphqlClient(ResetMinionProcessMarkDocument, { name }),
    onSuccess: (data, name) => applyProcessMark(qc, name, data.resetMinionProcessMark ?? null),
  })
}

/** Marks the processes of a name for the caller (null clears it), in every list at once. */
export function useSetMinionProcessMark() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { name: string; mark: MinionSessionMarkInput | null }) => graphqlClient(SetMinionProcessMarkDocument, v),
    onSuccess: (data, v) => applyProcessMark(qc, v.name, data.setMinionProcessMark ?? null),
  })
}
