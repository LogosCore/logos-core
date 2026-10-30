import { useLayoutEffect, useMemo, useRef } from "react"
import { InfoIcon, SquareTerminalIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useCanBuild } from "@/components/builds/build-access"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { useMinionSessionCommands, useMinionTaskSubscription, useMinionTasks } from "@/graphql/hooks/minion-tasks"
import { useNow } from "@/hooks/use-now"
import { useRecordModals } from "@/hooks/use-record-modals"
import { cn } from "@/lib/utils"
import { CommandBar, type ConsoleCommand } from "./command-bar"
import { formatInterval, lateness } from "./sessions-layout"
import { TaskEntry } from "./task-entry"

// A session's console, in its work pane tab: when its minion is next due
// (tasks go out only when it checks in), the tasks given it and what came
// back, oldest at the top, and the command line. Live: the tasks change as
// the minion takes them and answers.

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
  const waiting = tasks.filter((t) => t.state === "QUEUED" || (t.state === "SENT" && !t.ackedAt)).length

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
      {session && <StatusStrip session={session} waiting={waiting} />}
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

/** Where the minion stands: live, late or lost; its schedule; when it is next due; what waits for it. */
function StatusStrip({ session: s, waiting }: { session: MinionSessionFieldsFragment; waiting: number }) {
  const now = useNow(1_000)
  const { open } = useRecordModals()
  const state = lateness(s, now)
  const due = s.lastSeenAt && s.sleepInterval != null ? Date.parse(s.lastSeenAt) + s.sleepInterval - now : null
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-1.5 text-xs text-muted-foreground">
      <span className="inline-flex items-center gap-1.5">
        <span
          className={cn(
            "size-2 rounded-full bg-muted-foreground/40",
            state === "live" && "bg-emerald-500",
            state === "late" && "bg-amber-500",
            state === "lost" && "bg-red-500",
          )}
        />
        <span className="text-foreground">{state ?? (s.lastSeenAt ? "no schedule said" : "not seen yet")}</span>
      </span>
      {s.sleepInterval != null && (
        <span>
          checks in every {formatInterval(s.sleepInterval)}
          {s.sleepJitter ? ` ± ${s.sleepJitter}%` : ""}
        </span>
      )}
      {due !== null && <span>{due > 0 ? `next check-in in ~${formatInterval(due)}` : `check-in due ${formatInterval(-due)} ago`}</span>}
      {waiting > 0 && (
        <span className="text-foreground">
          {waiting} {waiting === 1 ? "task waits" : "tasks wait"} for it
        </span>
      )}
      <Button variant="ghost" size="xs" className="ml-auto" onClick={() => open({ kind: "session", id: s.id })}>
        <InfoIcon />
        Details
      </Button>
    </div>
  )
}
