import { Fragment, useEffect, useLayoutEffect, useRef, type KeyboardEvent } from "react"
import { ArrowDownIcon, CopyIcon, RotateCcwIcon, SearchIcon, SquareIcon, StarIcon, StarOffIcon, TerminalIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import type { MinionTaskFilter } from "@/graphql/hooks/minion-tasks"
import { useNow } from "@/hooks/use-now"
import { dayGroup, formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { lineOf, type CommandDef } from "./command-line"
import { clock, markedBy, useTaskActions, useToggleImportant, type Task } from "./task-actions"
import { emptyListText } from "./task-filter"
import { ImportantIcon, StateIcon } from "./task-state"
import { FeedbackLevelIcon } from "./feedback-parts"
import { feedbackSummary } from "./feedback-values"
import { useTaskWaiting } from "@/components/targets/use-task-waiting"
import { useSessionConnectionChoices } from "./use-connection-choices"
import { TaskAs } from "./task-as"

// The left of a session's console: its tasks, a row each, oldest at the
// top and the newest at the bottom by the command line, under a heading
// for each day. A click or ↑ and ↓
// pick the one shown beside it; a row's context menu stops it, runs it
// again, copies its command or output, or marks it important (S does too):
// a star then stands for its state, in its state's color. Older tasks load
// at the top. Above them, a search of their lines and a switch to the
// important ones alone, both core's (Ctrl+F in the list goes to the
// search).

export function TaskList({
  tasks,
  loading,
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
  searchText,
  onSearchText,
  important,
  onImportant,
  importantCount,
  filter,
  stale,
  canMark,
}: {
  /** Oldest first. */
  tasks: readonly Task[]
  /** The first page is still coming. */
  loading: boolean
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
  /** The search as typed; filter has it once core is asked. */
  searchText: string
  onSearchText: (text: string) => void
  /** The important tasks alone. */
  important: boolean
  onImportant: (on: boolean) => void
  /** How many of the session's tasks are marked important. */
  importantCount: number
  /** What the tasks shown were read with. */
  filter: MinionTaskFilter
  /** The tasks shown are another filter's, until this one's come. */
  stale: boolean
  /** Whether the caller marks tasks important: an operator, whatever the session's state. */
  canMark: boolean
}) {
  const list = useRef<HTMLDivElement>(null)
  const search = useRef<HTMLInputElement>(null)
  const marks = useToggleImportant()
  // Today and Yesterday move on at midnight.
  const now = new Date(useNow(60_000))

  // Following, the list stays at the bottom as tasks come; otherwise the
  // picked one is kept in view.
  useLayoutEffect(() => {
    const el = list.current
    if (el && following) el.scrollTop = el.scrollHeight
  }, [following, tasks.length])
  useEffect(() => {
    const el = list.current
    const row = selectedId ? el?.querySelector<HTMLElement>(`[data-task-id="${CSS.escape(selectedId)}"]`) : null
    if (!el || !row || following) return
    // The list alone scrolls: scrollIntoView would move the page too.
    const top = row.getBoundingClientRect().top - el.getBoundingClientRect().top + el.scrollTop
    const heading = 24 // the sticky day heading over the list's top
    if (top - heading < el.scrollTop) el.scrollTop = top - heading
    else if (top + row.offsetHeight > el.scrollTop + el.clientHeight) el.scrollTop = top + row.offsetHeight - el.clientHeight
  }, [following, selectedId])

  const selected = selectedId ? tasks.find((t) => t.id === selectedId) : undefined
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault()
      onStep(e.key === "ArrowUp" ? -1 : 1)
    } else if (e.key === "End") {
      e.preventDefault()
      onFollow()
    } else if (e.key.toLowerCase() === "s" && !e.ctrlKey && !e.metaKey && !e.altKey && canMark && selected) {
      e.preventDefault()
      marks.toggle(selected)
    }
  }
  // Ctrl+F in the list goes to its search, not the output's find.
  const onPaneKeyDown = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "f") {
      e.preventDefault()
      e.stopPropagation()
      search.current?.focus()
      search.current?.select()
    }
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col" onKeyDown={onPaneKeyDown}>
      <div className="flex items-center gap-1 border-b px-2 py-1">
        <div className="relative min-w-0 flex-1">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={search}
            value={searchText}
            onChange={(e) => onSearchText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape" && searchText !== "") {
                // Clears it; another Escape goes on (a dialog closes).
                e.preventDefault()
                e.stopPropagation()
                onSearchText("")
              } else if (e.key === "ArrowDown") {
                e.preventDefault()
                list.current?.focus()
              }
            }}
            placeholder="Search commands"
            aria-label="Search the tasks' command lines"
            title={'What a task\'s line holds, case aside; "quoted" for whole words'}
            className="h-7 px-7 text-xs md:text-xs"
          />
          {searchText !== "" && (
            <button
              type="button"
              aria-label="Clear the search"
              onClick={() => {
                onSearchText("")
                search.current?.focus()
              }}
              className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
            >
              <XIcon className="size-3.5" />
            </button>
          )}
        </div>
        <Button
          variant={important ? "secondary" : "ghost"}
          size="xs"
          aria-pressed={important}
          aria-label="The important tasks alone"
          title={important ? "The important tasks alone: show them all" : "Show the important tasks alone"}
          onClick={() => onImportant(!important)}
        >
          <StarIcon className={cn(important && "fill-current")} />
          {importantCount > 0 && <span className="tabular-nums">{importantCount}</span>}
        </Button>
      </div>
      <div
        ref={list}
        role="listbox"
        aria-label="Tasks"
        tabIndex={0}
        onKeyDown={onKeyDown}
        aria-busy={stale || undefined}
        className={cn(
          "min-h-0 flex-1 overflow-y-auto pb-1 outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
          stale && "opacity-60",
        )}
      >
        {hasOlder && (
          <div className="flex justify-center py-1">
            <Button variant="ghost" size="xs" disabled={loadingOlder} onClick={onLoadOlder}>
              {loadingOlder ? "Loading…" : "Older tasks"}
            </Button>
          </div>
        )}
        {tasks.length === 0 && !loading && <p className="px-3 py-2 text-sm text-muted-foreground">{emptyListText(filter)}</p>}
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
                canMark={canMark}
                onUnmark={() => marks.toggle(t)}
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
      <span className="text-xs font-medium text-muted-foreground tabular-nums">{label}</span>
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
  canMark,
  onUnmark,
}: {
  task: Task
  command?: CommandDef
  selected: boolean
  onSelect: () => void
  onQueued: (t: Task) => void
  canWrite: boolean
  canMark: boolean
  onUnmark: () => void
}) {
  const connections = useSessionConnectionChoices(t.sessionId)
  const held = useTaskWaiting(t.sessionId)
  const line = lineOf(t.command, t.args as Record<string, unknown> | null, command, connections)
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
            title={`${line}\n${t.issuedBy === "00000000-0000-0000-0000-000000000000" ? "core" : (t.issuedByName ?? "someone gone")} · queued ${formatAbsolute(t.queuedAt)}${t.importantAt ? `\nMarked important by ${markedBy(t)}` : ""}`}
            className={cn(
              "relative flex cursor-pointer items-baseline gap-2 px-3 py-1 text-xs select-none",
              selected ? "bg-muted before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-foreground" : "hover:bg-muted/50",
            )}
          />
        }
      >
        {t.importantAt && canMark ? (
          // The star unmarks it, and leaves the row as picked as it was.
          <button
            type="button"
            tabIndex={-1}
            aria-label="Unmark the task as important"
            onClick={(e) => {
              e.stopPropagation()
              onUnmark()
            }}
            className="self-center rounded-sm"
          >
            <ImportantIcon t={t} held={held} />
          </button>
        ) : t.importantAt ? (
          <ImportantIcon t={t} held={held} className="self-center" />
        ) : (
          <StateIcon t={t} held={held} className="self-center" />
        )}
        <span className="shrink-0 font-mono text-muted-foreground tabular-nums">{clock(t.queuedAt)}</span>
        <code className="min-w-0 flex-1 truncate font-mono">{line}</code>
        <TaskAs as={t.as} />
        {t.feedbackErrors + t.feedbackWarnings > 0 && (
          <span className="self-center" title={`What the minion said of it: ${feedbackSummary(t.feedbackErrors, t.feedbackWarnings)}`}>
            <FeedbackLevelIcon level={t.feedbackErrors > 0 ? "error" : "warning"} />
          </span>
        )}
      </ContextMenuTrigger>
      <ContextMenuContent>
        <TaskMenu task={t} line={line} canWrite={canWrite} canMark={canMark} onQueued={onQueued} />
      </ContextMenuContent>
    </ContextMenu>
  )
}

/** A row's menu: mounted while it is open, so a row holds no mutations of its own. */
function TaskMenu({
  task: t,
  line,
  canWrite,
  canMark,
  onQueued,
}: {
  task: Task
  line: string
  canWrite: boolean
  canMark: boolean
  onQueued: (t: Task) => void
}) {
  const actions = useTaskActions(t, line, { canWrite, canMark, onQueued })
  return (
    <>
      <ContextMenuItem disabled={!actions.canMark || actions.marking} onClick={actions.toggleImportant}>
        {t.importantAt ? <StarOffIcon /> : <StarIcon />}
        {t.importantAt ? "Unmark important" : "Mark important"}
        <span className="ml-auto pl-4 text-xs text-muted-foreground">S</span>
      </ContextMenuItem>
      <ContextMenuSeparator />
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
    </>
  )
}
