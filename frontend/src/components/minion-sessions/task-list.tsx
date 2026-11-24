import { Fragment, useEffect, useLayoutEffect, useRef, type KeyboardEvent, type ReactNode } from "react"
import {
  ArrowDownIcon,
  CopyIcon,
  CornerLeftUpIcon,
  ListTreeIcon,
  PlayIcon,
  RotateCcwIcon,
  SearchIcon,
  SquareIcon,
  StarIcon,
  StarOffIcon,
  TerminalIcon,
  WorkflowIcon,
  XIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import type { MinionTaskFilter } from "@/graphql/hooks/minion-tasks"
import { useNow } from "@/hooks/use-now"
import { useSteadyTitle } from "@/hooks/use-steady-title"
import { dayGroup, formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { fromWithin } from "@/lib/portal-events"
import { lineOf, type CommandDef } from "./command-line"
import { clock, markedBy, useTaskActions, useToggleImportant, type Task } from "./task-actions"
import { emptyListText } from "./task-filter"
import { ImportantIcon, StateIcon } from "./task-state"
import { FeedbackLevelIcon } from "./feedback-parts"
import { feedbackSummary } from "./feedback-values"
import { useTaskWaiting } from "@/components/targets/use-task-waiting"
import { TaskAs } from "./task-as"
import { subtaskText } from "./composite-values"
import { dayHeadings, type TaskTreePlace, type TaskTreeRow } from "./task-tree"

// The left of a session's console: its tasks, a row each, oldest at the
// top and the newest at the bottom by the command line, under a heading
// for each day; a composite task's subtasks right under it, a tree's lines
// joining them as a process list's (task-tree.ts). A click or ↑ and ↓
// pick the one shown beside it; a row's context menu stops it, runs it
// again, copies its command or output, or marks it important (S does too):
// a star then stands for its state: yellow when it went well, else in its
// state's color. Older tasks load
// at the top. Above them, a search of their lines and a switch to the
// important ones alone, both core's (Ctrl+F in the list goes to the
// search). A composite command's task can be shown with its subtasks alone
// (a chip says so, and goes back to every task); a subtask's row says what
// its composite's factory named it.

export function TaskList({
  rows,
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
  compositeLine,
  onAllTasks,
  onShowComposite,
  filter,
  stale,
  canMark,
}: {
  /** Oldest first, each composite task's subtasks under it (taskTree). */
  rows: readonly TaskTreeRow<Task>[]
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
  /** The composite task the list is narrowed to, with its subtasks, as its line says it; null for every task. */
  compositeLine: string | null
  /** Goes back to every task. */
  onAllTasks: () => void
  /** Shows a composite task with its subtasks, pick picked there. */
  onShowComposite?: (id: string, pick?: string) => void
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

  // Following, the list stays at the bottom as tasks come, the newest in
  // view (a subtask, under its composite task, may be above later ones);
  // otherwise the picked one is kept in view.
  useLayoutEffect(() => {
    const el = list.current
    if (!el || !following) return
    el.scrollTop = el.scrollHeight
    keepInView(el, selectedId)
  }, [following, rows.length, selectedId])
  useEffect(() => {
    const el = list.current
    if (el && !following) keepInView(el, selectedId)
  }, [following, selectedId])

  const selected = selectedId ? rows.find((r) => r.task.id === selectedId)?.task : undefined
  const headings = dayHeadings(rows, (t) => dayGroup(t.queuedAt, now))
  // The list's keys: not those of a row's menu, a popup opened from it
  // (its arrows move in the menu, S is no star there).
  const onKeyDown = (e: KeyboardEvent) => {
    if (!fromWithin(e)) return
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
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "f" && fromWithin(e)) {
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
          <StarIcon />
          {importantCount > 0 && <span className="tabular-nums">{importantCount}</span>}
        </Button>
      </div>
      {compositeLine !== null && (
        <div className="flex items-baseline gap-1.5 border-b bg-muted/40 px-3 py-1 text-xs">
          <WorkflowIcon className="size-3.5 shrink-0 self-center text-violet-600 dark:text-violet-400" aria-hidden />
          <span className="shrink-0 text-muted-foreground">Composite</span>
          <code className="min-w-0 flex-1 truncate font-mono" title={compositeLine}>
            {compositeLine}
          </code>
          <span className="shrink-0 text-muted-foreground">and its subtasks</span>
          <button
            type="button"
            onClick={onAllTasks}
            title="Show every task again"
            aria-label="Show every task again"
            className="shrink-0 self-center rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
          >
            <XIcon className="size-3.5" />
          </button>
        </div>
      )}
      <div
        ref={list}
        role="listbox"
        aria-label="Tasks"
        tabIndex={0}
        onKeyDown={onKeyDown}
        aria-busy={stale || undefined}
        className={cn(
          "min-h-0 flex-1 overflow-auto pb-1 outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
          stale && "opacity-60",
        )}
      >
        {/* As wide as the widest line, every row with it: long lines scroll
            sideways, not cut. What is centered stays in view (InView). */}
        <div className="w-max min-w-full">
        {hasOlder && (
          <div className="py-1">
            <InView>
              <Button variant="ghost" size="xs" disabled={loadingOlder} onClick={onLoadOlder}>
                {loadingOlder ? "Loading…" : "Older tasks"}
              </Button>
            </InView>
          </div>
        )}
        {rows.length === 0 && !loading && <p className="sticky left-0 px-3 py-2 text-sm text-muted-foreground">{emptyListText(filter)}</p>}
        {rows.map((r, i) => {
          const t = r.task
          return (
            <Fragment key={t.id}>
              {headings[i] && <DaySeparator label={headings[i]} />}
              <TaskRow
                task={t}
                place={r}
                command={commands.get(t.command)}
                selected={t.id === selectedId}
                onSelect={() => onSelect(t.id)}
                onQueued={onQueued}
                canWrite={canWrite}
                canMark={canMark}
                onUnmark={() => marks.toggle(t)}
                onShowComposite={onShowComposite}
              />
            </Fragment>
          )
        })}
        </div>
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
    <div role="presentation" className="sticky top-0 z-10 bg-background/90 py-1 backdrop-blur-sm">
      <InView>
        <span className="text-xs font-medium text-muted-foreground tabular-nums">{label}</span>
      </InView>
    </div>
  )
}

/** Centered in the list's view, wherever it is scrolled sideways. */
function InView({ children }: { children: ReactNode }) {
  return <span className="sticky left-1/2 inline-block -translate-x-1/2">{children}</span>
}

/**
 * Scrolls list el as little as it takes to show the row of task id, below
 * the sticky day heading over its top. The list alone scrolls:
 * scrollIntoView would move the page too.
 */
function keepInView(el: HTMLElement, id: string | null) {
  const row = id ? el.querySelector<HTMLElement>(`[data-task-id="${CSS.escape(id)}"]`) : null
  if (!row) return
  const top = row.getBoundingClientRect().top - el.getBoundingClientRect().top + el.scrollTop
  const heading = 24 // the sticky day heading over the list's top
  if (top - heading < el.scrollTop) el.scrollTop = top - heading
  else if (top + row.offsetHeight > el.scrollTop + el.clientHeight) el.scrollTop = top + row.offsetHeight - el.clientHeight
}

/** How far a subtask's line is in from its composite task's. */
const INDENT = 16

/** The tree's upright: under the first character of its composite task's line. */
const LINE_X = 3

/** A row's padding above and below (py-1): the lines cross it to join the next row's. */
const ROW_PAD = 4

/** A line of the rows' text (text-xs): the stem starts under it, a corner turns at its middle. */
const LINE_H = 16

/**
 * A task's lines in the tree (its place, task-tree.ts), drawn as a process
 * list's are: a composite task's stem from under its line down to its
 * subtasks; a subtask's corner, its upright going on down to the next one
 * (├), or ending at it, the last (└), its arm stopping short of the line.
 */
function TaskTreeLines({ place }: { place: TaskTreePlace }) {
  const line = "pointer-events-none absolute bg-muted-foreground/40"
  if (place.stem) return <span aria-hidden className={cn(line, "w-px")} style={{ left: LINE_X, top: LINE_H, bottom: -ROW_PAD }} />
  if (!place.sub) return null
  // A hook's upright starts at its own line, with nothing above to join.
  const top = place.hook ? 0 : -ROW_PAD
  const corner = LINE_H / 2
  return (
    <>
      <span
        aria-hidden
        className={cn(line, "w-px")}
        style={place.last ? { left: LINE_X, top, height: corner - top } : { left: LINE_X, top, bottom: -ROW_PAD }}
      />
      <span aria-hidden className={cn(line, "h-px")} style={{ left: LINE_X, top: corner, width: INDENT - 3 - LINE_X }} />
    </>
  )
}

function TaskRow({
  task: t,
  place,
  command,
  selected,
  onSelect,
  onQueued,
  canWrite,
  canMark,
  onUnmark,
  onShowComposite,
}: {
  task: Task
  /** Its place in the tree: its lines, a subtask's indent. */
  place: TaskTreePlace
  command?: CommandDef
  selected: boolean
  onSelect: () => void
  onQueued: (t: Task) => void
  canWrite: boolean
  canMark: boolean
  onUnmark: () => void
  onShowComposite?: (id: string, pick?: string) => void
}) {
  const held = useTaskWaiting(t.sessionId)
  const line = lineOf(t.command, t.args as Record<string, unknown> | null, command)
  // A subtask's line as its composite's factory named it; a composite
  // task's status line, on hover.
  const sub = subtaskText(t)
  const status = t.composite?.status
  // The status changes as the factory's code goes: held while hovered (useSteadyTitle).
  const hover = useSteadyTitle(
    `${line}${t.parentId ? `\nA subtask of a composite task${sub ? `: ${sub}` : ""}` : ""}${status ? `\n${status}` : ""}\n${t.issuedBy === "00000000-0000-0000-0000-000000000000" ? "core" : (t.issuedByName ?? "someone gone")} · queued ${formatAbsolute(t.queuedAt)}${t.state === "SCHEDULED" && t.scheduledAt ? `\nScheduled for ${formatAbsolute(t.scheduledAt)}` : ""}${t.importantAt ? `\nMarked important by ${markedBy(t)}` : ""}`,
  )
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
            {...hover}
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
        <span className="relative flex-1" style={place.sub ? { paddingLeft: INDENT } : undefined}>
          <TaskTreeLines place={place} />
          {t.parentId && <span className="sr-only">A subtask: </span>}
          <code className="font-mono whitespace-nowrap">{line}</code>
        </span>
        {sub && <span className="shrink-0 whitespace-nowrap text-muted-foreground">{sub}</span>}
        <TaskAs as={t.as} />
        {t.feedbackErrors + t.feedbackWarnings > 0 && (
          <span className="self-center" title={`What the minion said of it: ${feedbackSummary(t.feedbackErrors, t.feedbackWarnings)}`}>
            <FeedbackLevelIcon level={t.feedbackErrors > 0 ? "error" : "warning"} />
          </span>
        )}
      </ContextMenuTrigger>
      <ContextMenuContent>
        <TaskMenu task={t} line={line} canWrite={canWrite} canMark={canMark} onQueued={onQueued} onShowComposite={onShowComposite} />
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
  onShowComposite,
}: {
  task: Task
  line: string
  canWrite: boolean
  canMark: boolean
  onQueued: (t: Task) => void
  onShowComposite?: (id: string, pick?: string) => void
}) {
  const actions = useTaskActions(t, line, { canWrite, canMark, onQueued })
  // A composite task's subtasks, and a subtask's composite task, in the list.
  const parent = t.parentId
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
        {actions.stopLabel}
      </ContextMenuItem>
      {actions.canRunNow && (
        <ContextMenuItem disabled={actions.runningNow} onClick={actions.runNow}>
          <PlayIcon />
          Run now
        </ContextMenuItem>
      )}
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
      {onShowComposite && (t.composite || parent) && <ContextMenuSeparator />}
      {onShowComposite && t.composite && (
        <ContextMenuItem onClick={() => onShowComposite(t.id, t.id)}>
          <ListTreeIcon />
          Show it with its subtasks
        </ContextMenuItem>
      )}
      {onShowComposite && parent && (
        <ContextMenuItem onClick={() => onShowComposite(parent, parent)}>
          <CornerLeftUpIcon />
          Show its composite task
        </ContextMenuItem>
      )}
    </>
  )
}
