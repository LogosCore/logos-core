import { Fragment, useEffect, useLayoutEffect, useRef, type KeyboardEvent } from "react"
import { ArrowDownIcon, CopyIcon, RotateCcwIcon, SquareIcon, TerminalIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { dayGroup, formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { lineOf, type CommandDef } from "./command-line"
import { clock, useTaskActions, type Task } from "./task-actions"
import { StateIcon } from "./task-state"

// The left of a session's console: its tasks, a row each, oldest at the
// top and the newest at the bottom by the command line, under a heading
// for each day. A click or ↑ and ↓
// pick the one shown beside it; a row's context menu stops it, runs it
// again, or copies its command or output. Older tasks load at the top.

export function TaskList({
  tasks,
  selectedId,
  following,
  onSelect,
  onStep,
  onQueued,
  commands,
  canWrite,
  hasOlder,
  loadingOlder,
  onLoadOlder,
  moved,
  onFollow,
}: {
  /** Oldest first. */
  tasks: readonly Task[]
  selectedId: string | null
  /** Whether the newest is followed: the list then stays at the bottom. */
  following: boolean
  onSelect: (id: string) => void
  onStep: (by: -1 | 1) => void
  /** A task run again from a row's menu. */
  onQueued: (t: Task) => void
  commands: ReadonlyMap<string, CommandDef>
  canWrite: boolean
  hasOlder: boolean
  loadingOlder: boolean
  onLoadOlder: () => void
  /** The newest, when it moved on while another is picked. */
  moved: { task: Task; isNew: boolean } | null
  onFollow: () => void
}) {
  const list = useRef<HTMLDivElement>(null)
  // Today and Yesterday are as of this render.
  const now = new Date()

  // Following, the list stays at the bottom as tasks come; otherwise the
  // picked one is kept in view.
  useLayoutEffect(() => {
    const el = list.current
    if (el && following) el.scrollTop = el.scrollHeight
  }, [following, tasks.length])
  useEffect(() => {
    if (!following && selectedId) {
      list.current?.querySelector(`[data-task-id="${CSS.escape(selectedId)}"]`)?.scrollIntoView({ block: "nearest" })
    }
  }, [following, selectedId])

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault()
      onStep(e.key === "ArrowUp" ? -1 : 1)
    } else if (e.key === "End") {
      e.preventDefault()
      onFollow()
    }
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={list}
        role="listbox"
        aria-label="Tasks"
        tabIndex={0}
        onKeyDown={onKeyDown}
        className="min-h-0 flex-1 overflow-y-auto pb-1 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
      >
        {hasOlder && (
          <div className="flex justify-center py-1">
            <Button variant="ghost" size="xs" disabled={loadingOlder} onClick={onLoadOlder}>
              {loadingOlder ? "Loading…" : "Older tasks"}
            </Button>
          </div>
        )}
        {tasks.length === 0 && <p className="px-3 py-2 text-xs text-muted-foreground">No tasks yet.</p>}
        {tasks.map((t, i) => {
          const day = dayGroup(t.queuedAt, now)
          const newDay = i === 0 || dayGroup(tasks[i - 1].queuedAt, now).key !== day.key
          return (
            <Fragment key={t.id}>
              {newDay && <DaySeparator label={day.label} />}
              <TaskRow
                task={t}
                command={commands.get(t.command)}
                selected={t.id === selectedId}
                onSelect={() => onSelect(t.id)}
                onQueued={onQueued}
                canWrite={canWrite}
              />
            </Fragment>
          )
        })}
      </div>
      {moved && (
        <button
          type="button"
          onClick={onFollow}
          title="Show the newest task, and follow it"
          className="absolute inset-x-2 bottom-2 flex cursor-pointer items-center gap-1.5 rounded-md border bg-popover px-2 py-1 text-left text-xs shadow-md hover:bg-muted"
        >
          <ArrowDownIcon className="size-3.5 shrink-0 text-sky-600 dark:text-sky-400" />
          <span className="shrink-0 text-muted-foreground">{moved.isNew ? "New task:" : "New output:"}</span>
          <code className="min-w-0 truncate font-mono">{lineOf(moved.task.command, moved.task.args as Record<string, unknown> | null)}</code>
        </button>
      )}
    </div>
  )
}

/** Where a day's tasks start, as in a messenger: it stays at the top while its day scrolls by. */
function DaySeparator({ label }: { label: string }) {
  return (
    <div role="presentation" className="sticky top-0 z-10 flex justify-center bg-background/90 py-1 backdrop-blur-sm">
      <span className="rounded-full border bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground tabular-nums">{label}</span>
    </div>
  )
}

function TaskRow({
  task: t,
  command,
  selected,
  onSelect,
  onQueued,
  canWrite,
}: {
  task: Task
  command?: CommandDef
  selected: boolean
  onSelect: () => void
  onQueued: (t: Task) => void
  canWrite: boolean
}) {
  const line = lineOf(t.command, t.args as Record<string, unknown> | null, command)
  const actions = useTaskActions(t, line, { canWrite, onQueued })
  return (
    <ContextMenu>
      <ContextMenuTrigger
        render={
          <div
            role="option"
            aria-selected={selected}
            data-task-id={t.id}
            onClick={onSelect}
            // The menu shows the row it is for.
            onContextMenu={onSelect}
            title={`${line}\n${t.issuedByName ?? "someone gone"} · queued ${formatAbsolute(t.queuedAt)}`}
            className={cn(
              "relative flex cursor-pointer items-baseline gap-2 px-3 py-1 text-xs select-none",
              selected ? "bg-muted before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-foreground" : "hover:bg-muted/50",
            )}
          />
        }
      >
        <StateIcon t={t} className="self-center" />
        <span className="shrink-0 font-mono text-muted-foreground tabular-nums">{clock(t.queuedAt)}</span>
        <code className="min-w-0 flex-1 truncate font-mono">{line}</code>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem disabled={!actions.canStop || actions.stopping} onClick={actions.stop}>
          <SquareIcon />
          Stop
        </ContextMenuItem>
        <ContextMenuItem disabled={!actions.canRunAgain || actions.runningAgain} onClick={actions.runAgain}>
          <RotateCcwIcon />
          Run again
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onClick={actions.copyCommand}>
          <TerminalIcon />
          Copy command
        </ContextMenuItem>
        <ContextMenuItem disabled={!actions.hasOutput} onClick={() => void actions.copyOutput()}>
          <CopyIcon />
          Copy output
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}
