import { useLayoutEffect, useMemo, useRef } from "react"
import { SquareTerminalIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useCanBuild } from "@/components/builds/build-access"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { useMinionSessionCommands, useMinionTaskSubscription, useMinionTasks } from "@/graphql/hooks/minion-tasks"
import { CommandBar, type ConsoleCommand } from "./command-bar"
import { TaskEntry } from "./task-entry"

// A session's console, in its work pane tab: the tasks given its minion and
// what came back, oldest at the top, and the command line. Live: the tasks
// change as the minion takes them and answers. Where the session stands
// (live, late, lost) is its row's, which the table scrolls to with the tab.

export function SessionConsole({ sessionId }: { sessionId: string }) {
  const { data } = useMinionSession(sessionId)
  const session = data?.minionSession ?? null
  const { canWrite } = useCanBuild(session?.operationId ?? "")
  useMinionTaskSubscription(sessionId)
  const tasksQ = useMinionTasks(sessionId)
  const commandsQ = useMinionSessionCommands(sessionId)

  const commands = useMemo<ConsoleCommand[]>(
    () =>
      (commandsQ.data?.minionSessionCommands ?? []).map((c) => ({
        id: c.id,
        label: c.label,
        description: c.description,
        fields: c.params,
        params: c.params.map((p) => ({ key: p.key, type: p.type, label: p.label, required: p.required, default: p.default })),
      })),
    [commandsQ.data],
  )
  const byId = useMemo(() => new Map(commands.map((c) => [c.id, c])), [commands])
  // Oldest first: the newest at the bottom, by the command line.
  const tasks = useMemo(() => (tasksQ.data?.pages ?? []).flatMap((p) => p.minionTasks.tasks).toReversed(), [tasksQ.data])

  // Stays at the bottom as tasks and output come, unless the operator
  // scrolled up to read.
  const log = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const lastSize = tasks.reduce((n, t) => n + t.outputSize + (t.state.length << 20), tasks.length)
  useLayoutEffect(() => {
    const el = log.current
    if (el && atBottom.current) el.scrollTop = el.scrollHeight
  }, [lastSize])

  const closed = session?.state === "CLOSED"
  const disabledReason = !session
    ? "This session does not exist any more."
    : closed
      ? "The session is closed: its minion takes no tasks."
      : !canWrite
        ? "Viewers see the tasks; operators give them."
        : commandsQ.error
          ? (commandsQ.error as Error).message
          : undefined

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div
        ref={log}
        className="min-h-0 flex-1 overflow-y-auto"
        onScroll={(e) => {
          const el = e.currentTarget
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}
      >
        {tasksQ.hasNextPage && (
          <div className="flex justify-center py-2">
            <Button variant="ghost" size="xs" disabled={tasksQ.isFetchingNextPage} onClick={() => void tasksQ.fetchNextPage()}>
              {tasksQ.isFetchingNextPage ? "Loading…" : "Older tasks"}
            </Button>
          </div>
        )}
        {tasks.length === 0 && !tasksQ.isLoading && (
          <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
            <SquareTerminalIcon className="size-6 opacity-50" />
            <p className="text-sm">No tasks yet. Type a command below; it goes out when the minion next checks in.</p>
          </div>
        )}
        {tasks.map((t) => (
          <TaskEntry key={t.id} task={t} command={byId.get(t.command)} canWrite={canWrite && !closed} />
        ))}
      </div>
      <CommandBar sessionId={sessionId} commands={commands} disabled={disabledReason !== undefined} disabledReason={disabledReason} />
    </div>
  )
}
