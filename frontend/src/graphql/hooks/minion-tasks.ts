import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { useSubscription } from "@/hooks/use-subscription"
import {
  CancelMinionTaskDocument,
  MinionSessionCommandsDocument,
  MinionTaskChangedDocument,
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
}

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
        return task
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
