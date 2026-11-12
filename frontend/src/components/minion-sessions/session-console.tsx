import { useMemo, useState, type KeyboardEvent } from "react"
import { SquareTerminalIcon } from "lucide-react"
import { SplitPane } from "@/components/ui/split-pane"
import { useCanBuild } from "@/components/builds/build-access"
import { useMinionSession, useMinionSessionsLayout } from "@/graphql/hooks/minion-sessions"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import {
  narrows,
  useMinionTaskImportantCount,
  useMinionTaskSubscription,
  useMinionTasks,
  useQueueMinionTask,
} from "@/graphql/hooks/minion-tasks"
import { useDebounced } from "@/hooks/use-debounced"
import { CommandBar } from "./command-bar"
import { useConsoleCommands } from "./console-commands"
import { commandHistory, lineOf } from "./command-line"
import { carriedSelection, FOLLOWING, newestMoved, selectTask, selectedTask, stepTask, type ConsoleSelection } from "./console-selection"
import { DEFAULT_CONSOLE_SPLIT, MAX_SPLIT, MIN_SPLIT } from "./sessions-layout"
import { TaskDetail } from "./task-detail"
import { TaskList } from "./task-list"
import { emptyListText } from "./task-filter"
import { UploadDialog } from "./upload-dialog"
import { DeleteDialog, type DeleteTarget } from "./delete-dialog"
import { startStreamUpload } from "@/lib/stream-upload"
import type { UploadTarget } from "./upload-target"
import { commandsFor } from "./action-commands"
import type { Task } from "./task-actions"
import { useWorkspaceActions } from "./workspace-tabs"
import { useSessionConnectionChoices } from "./use-connection-choices"
import { PromptFxLayer } from "./prompt-fx-layer"

// A session's console, in its work pane tab: the tasks given its minion on
// the left, the one picked on the right with what came back, and the
// command line under both. Live: the tasks change as the minion takes them
// and answers. The console follows the newest task until another is picked
// (console-selection.ts); what is picked stays per tab, as the tab's panel
// stays mounted. Where the session stands (live, late, lost) is its row's,
// which the table scrolls to with the tab. The operator's prompt effect
// (/fx) plays over the whole console (PromptFxLayer). The list can be
// narrowed, by core, to a search of the tasks' lines and to the important
// ones; running a command clears both, so the new task shows and is
// followed. The command line's ↑ and ↓ go through every task all the same.

export function SessionConsole({ sessionId }: { sessionId: string }) {
  const sessionQ = useMinionSession(sessionId)
  const session = sessionQ.data?.minionSession ?? null
  const access = useCanBuild(session?.operationId ?? "")
  const canWrite = access.canWrite
  useMinionTaskSubscription(sessionId)
  // What the list is narrowed to: the search as typed goes to core a
  // moment after the typing stops (a cleared one at once).
  const [searchText, setSearchText] = useState("")
  const [important, setImportant] = useState(false)
  const search = useDebounced(searchText, searchText.trim() === "" ? 0 : 250)
  const filter = useMemo(() => ({ search, important }), [search, important])
  const filterKey = `${important}:${search.trim()}`
  const allQ = useMinionTasks(sessionId)
  const tasksQ = useMinionTasks(sessionId, filter)
  const importantCount = useMinionTaskImportantCount(sessionId).data ?? 0
  const commandsQ = useConsoleCommands(sessionId)
  // The divider is the operator's, kept with their sessions layout.
  const { layout, setLayout } = useMinionSessionsLayout()

  const commands = commandsQ.commands
  const connections = useSessionConnectionChoices(sessionId)
  const byId = useMemo(() => new Map(commands.map((c) => [c.id, c])), [commands])
  // Oldest first: the newest at the bottom, by the command line.
  const tasks = useMemo(() => (tasksQ.data?.pages ?? []).flatMap((p) => p.minionTasks.tasks).toReversed(), [tasksQ.data])

  // What ↑ and ↓ go through on the command line: every task as typed,
  // whatever the list is narrowed to.
  const history = useMemo(
    () =>
      commandHistory(
        (allQ.data?.pages ?? [])
          .flatMap((p) => p.minionTasks.tasks)
          .toReversed()
          .map((t) => lineOf(t.command, t.args as Record<string, unknown> | null, byId.get(t.command), connections)),
      ),
    [allQ.data, byId, connections],
  )

  // The selection is the list's it was made in: another filter's carries
  // the task picked over while the list holds it (carriedSelection).
  const [picked, setPicked] = useState<{ key: string; sel: ConsoleSelection }>(() => ({ key: filterKey, sel: FOLLOWING }))
  const selection = picked.key === filterKey ? picked.sel : carriedSelection(tasks, picked.sel)
  const setSelection = (sel: ConsoleSelection) => setPicked({ key: filterKey, sel })
  const selected = selectedTask(tasks, selection)
  const moved = newestMoved(tasks, selection)
  // A task the operator runs is the newest: shown, and followed, in the
  // whole list.
  const follow = () => setSelection(FOLLOWING)
  const ran = () => {
    setSearchText("")
    setImportant(false)
    follow()
  }
  // A folder opened in a listing, with a list_dir command: the one picked
  // (an item's menu), else the listing's own when it is one (Up, a crumb),
  // else the session's first. The listing's own keeps its other args;
  // another runs with path alone, the rest its defaults.
  const queue = useQueueMinionTask()
  // An action on a listing's file (Download): the command picked, with the
  // path alone; the console follows the new task.
  const runAction = (commandId: string, args: Record<string, unknown>) =>
    queue.mutate(
      { sessionId, command: commandId, args },
      { onSuccess: ran, onError: (err) => toast.error(errorMessage(err, "Could not run it")) },
    )
  // A listing's Upload here: the folder, and the upload_file command picked,
  // in the upload dialog until it runs.
  const [uploadTo, setUploadTo] = useState<{ target: UploadTarget; commandId: string } | null>(null)
  // Read here, not in the dialog's handlers: the compiler reads what a
  // handler uses at render, where there may be no upload.
  const uploadCommand = uploadTo ? byId.get(uploadTo.commandId) : undefined
  // A listing's Delete: the entry, and the delete_path command picked, in
  // the confirmation until it runs.
  const [deleting, setDeleting] = useState<{ target: DeleteTarget; commandId: string } | null>(null)
  const deleteCommandId = deleting?.commandId ?? null
  const openPath = (t: Task, path: string, picked?: string) => {
    const commandId = picked ?? commandsFor(commands, "list_dir", t.command)[0]?.id
    if (!commandId) return
    queue.mutate(
      {
        sessionId,
        command: commandId,
        args: commandId === t.command ? { ...((t.args as Record<string, unknown> | null) ?? {}), path } : { path },
      },
      { onSuccess: ran, onError: (err) => toast.error(errorMessage(err, "Could not list the folder")) },
    )
  }
  const workspace = useWorkspaceActions()
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
    <div className="relative flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown}>
      <PromptFxLayer effect={layout.promptFx}>
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
              onQueued={ran}
              commands={byId}
              canWrite={writable}
              hasOlder={!!tasksQ.hasNextPage}
              loadingOlder={tasksQ.isFetchingNextPage}
              onLoadOlder={() => void tasksQ.fetchNextPage()}
              moved={moved}
              onFollow={() => follow()}
              searchText={searchText}
              onSearchText={setSearchText}
              important={important}
              onImportant={setImportant}
              importantCount={importantCount}
              filter={filter}
              stale={tasksQ.isPlaceholderData || searchText.trim() !== search.trim()}
              canMark={canWrite}
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
                  canMark={canWrite}
                  onQueued={ran}
                  // No list_dir command: a listing is read, not browsed.
                  onOpenPath={
                    writable && commandsFor(commands, "list_dir").length > 0
                      ? (path, commandId) => openPath(selected, path, commandId)
                      : undefined
                  }
                  listCommands={commandsFor(commands, "list_dir", selected.command)}
                  onDownloadFile={writable ? (path, commandId) => runAction(commandId, { path }) : undefined}
                  downloadCommands={commandsFor(commands, "download_file")}
                  onUploadHere={writable ? (target, commandId) => setUploadTo({ target, commandId }) : undefined}
                  uploadCommands={[...commandsFor(commands, "upload_file"), ...commandsFor(commands, "upload_stream")]}
                  onStreamFile={writable ? (path, commandId) => runAction(commandId, { path }) : undefined}
                  streamCommands={commandsFor(commands, "stream_file")}
                  onDeletePath={writable ? (target, commandId) => setDeleting({ target, commandId }) : undefined}
                  deleteCommands={commandsFor(commands, "delete_path")}
                  onWalkPath={writable ? (path, commandId) => runAction(commandId, { path }) : undefined}
                  walkCommands={commandsFor(commands, "walk_dir")}
                  onOpenInFileBrowser={workspace ? (path) => workspace.openFileBrowser(sessionId, path) : undefined}
                  findSignal={findSignal}
                />
              ) : (
                <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
                  <SquareTerminalIcon className="size-6 opacity-50" />
                  <p className="text-sm">
                    {tasksQ.isLoading
                      ? ""
                      : narrows(filter)
                        ? emptyListText(filter)
                        : "No tasks yet. Type a command below; it goes out when the minion next checks in."}
                  </p>
                </div>
              )}
            </div>
          }
        />
        <DeleteDialog
          target={deleting?.target ?? null}
          commandId={deleteCommandId}
          onOpenChange={(open) => !open && setDeleting(null)}
          onDelete={async (args) => {
            if (!deleteCommandId) return
            await queue.mutateAsync({ sessionId, command: deleteCommandId, args })
            ran()
          }}
        />
        <UploadDialog
          target={uploadTo?.target ?? null}
          command={uploadCommand}
          operationId={session?.operationId ?? ""}
          onOpenChange={(open) => !open && setUploadTo(null)}
          onSubmit={async (args, stream) => {
            if (!uploadCommand) return
            const data = await queue.mutateAsync({ sessionId, command: uploadCommand.id, args })
            if (stream) startStreamUpload(data.queueMinionTask.id, stream)
            ran()
          }}
        />
        <CommandBar
          sessionId={sessionId}
          operationId={session?.operationId ?? ""}
          commands={commands}
          connKind={session?.connKind}
          disabled={disabledReason !== undefined}
          disabledReason={disabledReason}
          history={history}
          onQueued={ran}
          effect={layout.promptFx}
          onEffect={(promptFx) => setLayout({ ...layout, promptFx })}
        />
      </PromptFxLayer>
    </div>
  )
}
