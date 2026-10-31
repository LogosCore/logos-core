import { useMemo, useState, type KeyboardEvent } from "react"
import { SquareTerminalIcon } from "lucide-react"
import { SplitPane } from "@/components/ui/split-pane"
import { useCanBuild } from "@/components/builds/build-access"
import { useMinionSession, useMinionSessionsLayout } from "@/graphql/hooks/minion-sessions"
import { useMinionSessionCommands, useMinionTaskSubscription, useMinionTasks } from "@/graphql/hooks/minion-tasks"
import { CommandBar, type ConsoleCommand } from "./command-bar"
import { FOLLOWING, newestMoved, selectTask, selectedTask, stepTask, type ConsoleSelection } from "./console-selection"
import { DEFAULT_CONSOLE_SPLIT, MAX_SPLIT, MIN_SPLIT } from "./sessions-layout"
import { TaskDetail } from "./task-detail"
import { TaskList } from "./task-list"

// A session's console, in its work pane tab: the tasks given its minion on
// the left, the one picked on the right with what came back, and the
// command line under both. Live: the tasks change as the minion takes them
// and answers. The console follows the newest task until another is picked
// (console-selection.ts); what is picked stays per tab, as the tab's panel
// stays mounted. Where the session stands (live, late, lost) is its row's,
// which the table scrolls to with the tab.

export function SessionConsole({ sessionId }: { sessionId: string }) {
  const sessionQ = useMinionSession(sessionId)
  const session = sessionQ.data?.minionSession ?? null
  const access = useCanBuild(session?.operationId ?? "")
  const canWrite = access.canWrite
  useMinionTaskSubscription(sessionId)
  const tasksQ = useMinionTasks(sessionId)
  const commandsQ = useMinionSessionCommands(sessionId)
  // The divider is the operator's, kept with their sessions layout.
  const { layout, setLayout } = useMinionSessionsLayout()

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

  const [selection, setSelection] = useState<ConsoleSelection>(FOLLOWING)
  const selected = selectedTask(tasks, selection)
  const moved = newestMoved(tasks, selection)
  // A task the operator runs is the newest: shown, and followed.
  const follow = () => setSelection(FOLLOWING)
  // Ctrl+F anywhere in the console finds in the output shown.
  const [findSignal, setFindSignal] = useState(0)

  const closed = session?.state === "CLOSED"
  const writable = canWrite && !closed
  // Until the session and the caller's role are in, the line waits without
  // a word: a reason said then would be a wrong one, gone a moment later.
  const pending = sessionQ.isLoading || access.loading
  const disabledReason = pending
    ? ""
    : !session
    ? "This session does not exist any more."
    : closed
      ? "The session is closed: its minion takes no tasks."
      : !canWrite
        ? "Viewers see the tasks; operators give them."
        : commandsQ.error
          ? (commandsQ.error as Error).message
          : undefined

  const onKeyDown = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "f" && selected) {
      e.preventDefault()
      setFindSignal((n) => n + 1)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown}>
      <SplitPane
        orientation="horizontal"
        label="Resize the task list and the task beside it"
        split={layout.consoleSplit ?? DEFAULT_CONSOLE_SPLIT}
        defaultSplit={DEFAULT_CONSOLE_SPLIT}
        min={MIN_SPLIT}
        max={MAX_SPLIT}
        onChange={(consoleSplit) => setLayout({ ...layout, consoleSplit })}
        className="border-t"
        first={
          <TaskList
            tasks={tasks}
            loading={tasksQ.isLoading}
            selectedId={selected?.id ?? null}
            following={selection.pinned === null}
            onSelect={(id) => setSelection(selectTask(tasks, id))}
            onStep={(by) => setSelection(stepTask(tasks, selection, by))}
            onQueued={follow}
            commands={byId}
            canWrite={writable}
            hasOlder={!!tasksQ.hasNextPage}
            loadingOlder={tasksQ.isFetchingNextPage}
            onLoadOlder={() => void tasksQ.fetchNextPage()}
            moved={moved}
            onFollow={() => follow()}
          />
        }
        second={
          <div className="flex min-h-0 flex-1 flex-col border-l">
            {selected ? (
              <TaskDetail
                key={selected.id}
                task={selected}
                command={byId.get(selected.command)}
                canWrite={writable}
                onQueued={follow}
                findSignal={findSignal}
              />
            ) : (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
                <SquareTerminalIcon className="size-6 opacity-50" />
                <p className="text-sm">
                  {tasksQ.isLoading ? "" : "No tasks yet. Type a command below; it goes out when the minion next checks in."}
                </p>
              </div>
            )}
          </div>
        }
      />
      <CommandBar
        sessionId={sessionId}
        commands={commands}
        disabled={disabledReason !== undefined}
        disabledReason={disabledReason}
        onQueued={follow}
      />
    </div>
  )
}
