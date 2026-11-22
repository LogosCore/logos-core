import { Fragment, useState } from "react"
import {
  ArrowDownIcon,
  ArrowDownUpIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  CopyIcon,
  CpuIcon,
  DownloadIcon,
  FolderOpenIcon,
  ListTreeIcon,
  MonitorCogIcon,
  OctagonXIcon,
  RefreshCwIcon,
  TagIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { SearchInput } from "@/components/ui/search-input"
import { SortableHeader } from "@/components/ui/sortable-header"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionProcessFold, MinionProcessOrder } from "@/graphql/gql/graphql"
import { PROCESS_PAGE, useMinionTaskProcesses, useResetMinionProcessMark, useSetMinionProcessMark } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { toggleSort, type DataTableSort } from "@/lib/data-table-sort"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { preloadable, useLoaded } from "@/lib/preloadable"
import { cn } from "@/lib/utils"
import { ActionItem, type MenuCommand } from "./action-menu"
import type { KillTarget } from "./kill-dialog"
import { MarkIcon } from "./mark-icon"
import { SessionFire } from "./session-fire"
import { MARK_TONE_CLASSES, isMarkColor, markColorVars, markRowClass } from "./session-mark-colors"
import type { SessionMarkTarget } from "./session-mark-dialog"
import { cpuTimeText, folderOf, processNotes, startedSeconds, toggled, treeLines, userText, type ProcessRow } from "./process-list"

// A process_list task's view (docs: contracts/task-results-and-actions.md,
// process_list): the host's processes, read from core a page at a time,
// ordered and searched there: a tree at first, each process under its
// parent; a column's header, or Sort, lists them flat by it (again: the
// other way), and Tree toggles back and forth. In the tree, with no search,
// a process's toggle folds away those under it, and the toolbar folds all,
// the operating system's own trees, or none; core folds them, nothing folded
// at first. A user marks processes by name ("Mark by name…": a color or a
// style, an icon or an emoji, as sessions are marked), theirs alone, in
// every process list. Its columns, pid, name, user and command line, keep their widths
// as it scrolls: a value too long for one is cut, whole in its title, and a
// click on a row opens it, its values whole, wrapped, and its memory, CPU
// time, threads and start under its command line. A process's actions are
// in its context menu: Kill (kill_process), and on its path List its folder
// (list_dir) and Download (download_file); Refresh lists them again
// (list_processes).

// The columns' widths, which hold as the table scrolls. The width there is
// goes to them in this order: the pid and the name, whole (the name with
// room for the tree's indent and its notes); the user, up to its most (a
// grid grows a track with a fixed most before a flexible one: Windows'
// users say their domain, integrity and session, "NT AUTHORITY\SYSTEM ·
// system · session 0", a Unix host's are short names); the command line,
// the rest. Past their least (min-w-min: the user's least and 12rem of
// command line) the table scrolls sideways.
function colsFor(sep: string): string {
  return sep === "\\"
    ? "grid-cols-[4.5rem_22rem_minmax(8rem,18rem)_minmax(12rem,1fr)] min-w-min"
    : "grid-cols-[4.5rem_22rem_minmax(6rem,10rem)_minmax(12rem,1fr)] min-w-min"
}

/** The tree: the order until a column's header (or Sort, or Tree turned off) lists them flat. */
const TREE: DataTableSort<MinionProcessOrder> = { field: "TREE", direction: "ASC" }

/** Flat, as Tree turned off lists them: by pid. */
const FLAT: DataTableSort<MinionProcessOrder> = { field: "PID", direction: "ASC" }

/** What Sort orders by, the columns' and the values an open row shows; the largest or the latest first where that is the first ask. */
const SORTS: readonly { field: Exclude<MinionProcessOrder, "TREE">; label: string; first: "ASC" | "DESC" }[] = [
  { field: "PID", label: "PID", first: "ASC" },
  { field: "NAME", label: "Name", first: "ASC" },
  { field: "USER", label: "User", first: "ASC" },
  { field: "MEMORY", label: "Memory", first: "DESC" },
  { field: "CPU_TIME", label: "CPU time", first: "DESC" },
  { field: "THREADS", label: "Threads", first: "DESC" },
  { field: "STARTED", label: "Started", first: "DESC" },
]

/**
 * How far a level of the tree indents its processes. Each column of it is
 * a level's: a process's corner in its parent's, its fold toggle in its
 * own, its name past that.
 */
const INDENT = 16

/**
 * A row's padding above and below (py-0.5): rows kept low, so a view holds
 * more of them. The name cell takes it back (-my-0.5 py-0.5) to span the
 * row's full height, and the tree's corners sit past it.
 */
const ROW_PAD = "py-0.5"

/** Where a tree's corner turns to the name: the middle of its first line (the row's padding, 2px, then half a line, 10px), in an open row too. */
const CORNER_Y = 12

/** A fold toggle's box, centred in its process's column on its first line. */
const TOGGLE = 14

/** The most rows a read after a fold takes in its first page (core's most). */
const MAX_FIRST = 2000

export function ProcessListView({
  task,
  hostArch,
  onKill,
  killCommands = [],
  onOpenPath,
  listCommands = [],
  onDownload,
  downloadCommands = [],
  onRefresh,
  refreshCommands = [],
}: {
  task: { id: string; dataParts: number; state: string }
  /** The host's architecture, as the hello said it: a process's shows only where it differs. */
  hostArch?: string | null
  /** Kills a process, with a kill_process command (after a confirmation); absent where the viewer may not give tasks. */
  onKill?: (target: KillTarget, commandId: string) => void
  killCommands?: readonly MenuCommand[]
  /** Lists a process's folder, with a list_dir command. */
  onOpenPath?: (path: string, commandId?: string) => void
  listCommands?: readonly MenuCommand[]
  /** Downloads a process's executable, with a download_file command. */
  onDownload?: (path: string, commandId: string) => void
  downloadCommands?: readonly MenuCommand[]
  /** Lists the processes again, with a list_processes command. */
  onRefresh?: (commandId: string) => void
  refreshCommands?: readonly MenuCommand[]
}) {
  const [search, setSearch] = useState("")
  const [sort, setSort] = useState(TREE)
  const [menuFor, setMenuFor] = useState<number | null>(null)
  // The rows opened by a click, by pid: their values whole.
  const [opened, setOpened] = useState<ReadonlySet<number>>(() => new Set())
  // A process name being marked, in the mark dialog until it is saved.
  const [marking, setMarking] = useState<SessionMarkTarget | null>(null)
  const markProcesses = useSetMinionProcessMark()
  const resetProcesses = useResetMinionProcessMark()
  // Whose processes are folded away, and the pids folded the other way.
  const [fold, setFold] = useState<MinionProcessFold>("NONE")
  const [flip, setFlip] = useState<ReadonlySet<number>>(() => new Set())
  // The first page's size: after a fold, as many rows as there were, so the
  // list read again stays where it was.
  const [firstSize, setFirstSize] = useState(PROCESS_PAGE)
  const q = useMinionTaskProcesses(
    task.id,
    { search, order: sort.field, desc: sort.direction === "DESC", fold, flip: [...flip].sort((a, b) => a - b) },
    `${task.dataParts}:${task.state}`,
    firstSize,
  )
  const first = q.data?.pages[0]?.minionTaskProcesses
  const rows = q.data?.pages.flatMap((p) => p.minionTaskProcesses.edges.map((e) => e.node)) ?? []
  const sep = first?.sep ?? "/"
  // The host's widths (its users' are longer on Windows), the same for every row.
  const cols = colsFor(sep)
  const total = first?.totalCount ?? 0
  const hidden = first?.hidden ?? 0
  const searching = search.trim() !== ""
  const tree = sort.field === "TREE"
  const sorted = SORTS.find((s) => s.field === sort.field)
  // Folds are the tree's, a search's aside (it shows every match).
  const folding = tree && !searching
  const keepPlace = () => setFirstSize(Math.min(Math.max(PROCESS_PAGE, rows.length + PROCESS_PAGE / 2), MAX_FIRST))
  const foldAs = (f: MinionProcessFold) => {
    keepPlace()
    setFold(f)
    setFlip(new Set())
  }
  // A click opens a row, another cuts it again; not one that ends a
  // selection of its text (to copy a part of it).
  const toggleOpen = (pid: number) => {
    if (window.getSelection()?.toString()) return
    setOpened((s) => toggled(s, pid))
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-1 text-xs">
        <span className="text-muted-foreground tabular-nums">
          {first ? `${total.toLocaleString("en-US")} ${searching ? (total === 1 ? "match" : "matches") : total === 1 ? "process" : "processes"}` : ""}
          {hidden > 0 && ` · ${hidden.toLocaleString("en-US")} folded`}
        </span>
        <SearchInput
          value={search}
          onValueChange={(v) => {
            setSearch(v)
            setFirstSize(PROCESS_PAGE)
          }}
          placeholder="Search name, pid, user, path, command line..."
          title="Matches the name, the pid, the user, the path and the command line; in the tree, each match shows under its ancestors, folds aside"
          className="relative w-72"
        />
        <Button
          variant={tree ? "secondary" : "ghost"}
          size="xs"
          aria-pressed={tree}
          onClick={() => setSort(tree ? FLAT : TREE)}
          title={
            tree
              ? "A tree: each process under its parent, siblings in the order they started. Click to list them flat, by pid; Sort or a column's header sorts them"
              : "Show each process under its parent"
          }
        >
          <ListTreeIcon />
          Tree
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant={tree ? "ghost" : "secondary"}
                size="xs"
                title={
                  sorted
                    ? `Sorted by ${sorted.label.toLowerCase()}, ${sort.direction === "DESC" ? "the largest or the latest first" : "the smallest or the earliest first"}`
                    : "List them flat, sorted by a value: memory, CPU time and more"
                }
              >
                <ArrowDownUpIcon />
                {sorted ? sorted.label : "Sort"}
                {sorted && (sort.direction === "DESC" ? <ArrowDownIcon /> : <ArrowUpIcon />)}
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="min-w-40">
            {/* A label must sit in a group, or Base UI throws. */}
            <DropdownMenuGroup>
              <DropdownMenuLabel>Sort flat by</DropdownMenuLabel>
              {SORTS.map((s) => (
                <DropdownMenuItem key={s.field} onClick={() => setSort(toggleSort(sort, s.field, s.first))}>
                  {s.label}
                  {s.field === sort.field && (
                    <span className="ml-auto text-muted-foreground">
                      {sort.direction === "DESC" ? <ArrowDownIcon className="size-3.5" /> : <ArrowUpIcon className="size-3.5" />}
                    </span>
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        {folding && (
          <>
            <Button
              variant="ghost"
              size="xs"
              disabled={fold === "ALL" && flip.size === 0}
              onClick={() => foldAs("ALL")}
              title="Fold every process: the roots alone show, each toggle unfolds one"
            >
              <ChevronsDownUpIcon />
              Fold all
            </Button>
            <Button
              variant="ghost"
              size="xs"
              disabled={!first?.systemTrees || (fold === "SYSTEM" && flip.size === 0)}
              onClick={() => foldAs("SYSTEM")}
              title={
                first?.systemTrees
                  ? "Fold the operating system's own trees: its kernel's threads on Linux (kthreadd); its kernel's processes (System) and its services (services.exe) on Windows"
                  : "This list has none of the operating system's own trees"
              }
            >
              <MonitorCogIcon />
              Fold system trees
            </Button>
            <Button
              variant="ghost"
              size="xs"
              disabled={fold === "NONE" && flip.size === 0}
              onClick={() => foldAs("NONE")}
              title="Unfold every process"
            >
              <ChevronsUpDownIcon />
              Unfold all
            </Button>
          </>
        )}
        <span className="flex-1" />
        {onRefresh && refreshCommands.length > 0 && (
          <Button
            variant="ghost"
            size="xs"
            onClick={() => onRefresh(refreshCommands[0].id)}
            title={`List the processes again, with ${refreshCommands[0].id}: a new task`}
          >
            <RefreshCwIcon />
            Refresh
          </Button>
        )}
      </div>
      {q.error ? (
        <div className="m-3 rounded-md bg-destructive/15 p-3 text-sm text-destructive">{q.error.message}</div>
      ) : (
        <VirtualizedDataTable
          bare
          scrollX
          items={rows}
          isLoading={q.isLoading}
          isFetchingNextPage={q.isFetchingNextPage}
          hasNextPage={q.hasNextPage}
          fetchNextPage={() => void q.fetchNextPage()}
          gridCols={cols}
          entityNoun="processes"
          header={
            <>
              <SortableHeader label="PID" field="PID" sort={sort} onSortChange={setSort} />
              <SortableHeader label="Name" field="NAME" sort={sort} onSortChange={setSort} />
              <SortableHeader label="User" field="USER" sort={sort} onSortChange={setSort} />
              <div>Command line</div>
            </>
          }
          emptyState={
            <>
              <CpuIcon className="size-8 opacity-50" />
              <p className="text-sm">{searching ? "No process matches." : "The list holds no process."}</p>
            </>
          }
          renderRow={(p) => {
            const open = opened.has(p.pid)
            return (
              <ContextMenu onOpenChange={(o) => setMenuFor(o ? p.pid : null)}>
                <ContextMenuTrigger
                  render={
                    <div
                      role="button"
                      tabIndex={0}
                      aria-expanded={open}
                      onClick={() => toggleOpen(p.pid)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault()
                          toggleOpen(p.pid)
                        }
                      }}
                      // The caller's mark on its name: a tint or a style, as a session's row takes it.
                      style={markColorVars(p.mark?.color)}
                      className={dataTableRowClass(
                        cols,
                        cn(
                          ROW_PAD,
                          "cursor-pointer",
                          markRowClass(p.mark?.color),
                          // Open: its values from the top, each cell's first line beside the
                          // name's; tinted, unless its mark tints it.
                          open && "items-start",
                          open && !p.mark?.color && "bg-muted/30",
                          p.context && "text-muted-foreground",
                          menuFor === p.pid && "bg-muted",
                        ),
                      )}
                    />
                  }
                >
                  <ProcessCells
                    p={p}
                    hostArch={hostArch}
                    windows={sep === "\\"}
                    tree={tree}
                    open={open}
                    onFold={
                      folding && p.children > 0
                        ? () => {
                            keepPlace()
                            setFlip((s) => toggled(s, p.pid))
                          }
                        : undefined
                    }
                  />
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ProcessMenu
                    p={p}
                    sep={sep}
                    onMark={() =>
                      setMarking({
                        title: `“${p.name}”`,
                        mark: p.mark ? { color: p.mark.color, icon: p.mark.icon, emoji: p.mark.emoji } : null,
                        description: p.mark?.builtin
                          ? `Its built-in mark: every user sees it on the processes named ${p.name} (whatever its case), in every process list, until they set their own or clear it. Only you see your marks.`
                          : `A color or a style for every process named ${p.name} (whatever its case), and an icon beside its name, in every process list of every operation. Only you see your marks.`,
                        save: async (mark) => {
                          await markProcesses.mutateAsync({ name: p.name, mark })
                        },
                        // Its name has a built-in mark your own (or your clearing) hides.
                        reset:
                          p.builtinMark && !p.mark?.builtin
                            ? {
                                label: "Use the built-in mark",
                                run: async () => {
                                  await resetProcesses.mutateAsync(p.name)
                                },
                              }
                            : undefined,
                      })
                    }
                    onKill={onKill}
                    killCommands={killCommands}
                    onOpenPath={onOpenPath}
                    listCommands={listCommands}
                    onDownload={onDownload}
                    downloadCommands={downloadCommands}
                  />
                </ContextMenuContent>
              </ContextMenu>
            )
          }}
        />
      )}
      {marking && <MarkDialog target={marking} onClose={() => setMarking(null)} />}
    </div>
  )
}

// The mark dialog's code (its icon picker and their catalogs) comes with the
// first marking, not with the list.
const markDialogCode = preloadable(() => import("./session-mark-dialog"))

function MarkDialog({ target, onClose }: { target: SessionMarkTarget; onClose: () => void }) {
  const code = useLoaded(markDialogCode)
  return code && <code.SessionMarkDialog target={target} onClose={onClose} />
}

/**
 * A process's cells: pid; name, indented and drawn to its parent in the
 * tree, its fold toggle before it, with how many a fold hides and its
 * notes; user; command line. Cut to their columns, whole in their titles;
 * in an open row whole, wrapped (the command line keeping its spaces), its
 * memory, CPU time, threads and start under its command line, the small
 * ones a little lower, on the name's first line.
 */
function ProcessCells({
  p,
  hostArch,
  windows,
  tree,
  open,
  onFold,
}: {
  p: ProcessRow
  hostArch?: string | null
  /** The host is Windows (the list's separator is "\\"). */
  windows: boolean
  tree: boolean
  open: boolean
  /** Folds or unfolds those under it; absent where it has none, or folds do not apply. */
  onFold?: () => void
}) {
  const notes = processNotes(p, hostArch, windows)
  const who = userText(p)
  const line = p.cmdline ?? p.path ?? ""
  const low = open && "pt-0.5"
  const folded = p.hidden > 0
  return (
    <>
      {/* data-cell: a mark's style draws on each cell (session-mark-styles.css);
          an On Fire row's fire burns behind them all, from the first. */}
      <span data-cell className={cn("font-mono text-xs tabular-nums", low)}>
        {p.mark?.color === "on-fire" && <SessionFire />}
        {p.pid}
      </span>
      {/* In the tree the cell takes the row's full height (its padding
          its own), so its lines join the rows above and below. */}
      <span
        data-cell
        className={cn("flex min-w-0 items-baseline gap-1.5", open && "flex-wrap", tree && "relative -my-0.5 self-stretch py-0.5")}
        style={tree ? { paddingLeft: (p.depth + 1) * INDENT } : undefined}
      >
        {tree && <TreeLines p={p} toggle={!!onFold} />}
        {onFold && (
          <button
            type="button"
            aria-expanded={!folded}
            aria-label={folded ? "Unfold" : "Fold"}
            title={folded ? `Unfold: show the ${p.hidden.toLocaleString("en-US")} under it` : "Fold: hide those under it"}
            onClick={(e) => {
              e.stopPropagation()
              onFold()
            }}
            onKeyDown={(e) => e.stopPropagation()}
            className="absolute flex cursor-pointer items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground"
            style={{ left: p.depth * INDENT + (INDENT - TOGGLE) / 2, top: CORNER_Y - TOGGLE / 2, width: TOGGLE, height: TOGGLE }}
          >
            {folded ? <ChevronRightIcon className="size-3" /> : <ChevronDownIcon className="size-3" />}
          </button>
        )}
        {(p.mark?.icon || p.mark?.emoji) && (
          <span data-mark-icon className={cn("shrink-0 self-center", isMarkColor(p.mark.color) && MARK_TONE_CLASSES)}>
            <MarkIcon icon={p.mark.icon} emoji={p.mark.emoji} size={14} />
          </span>
        )}
        <span className={open ? "[overflow-wrap:anywhere]" : "truncate"} title={p.path ?? p.name}>
          {p.name}
        </span>
        {folded && (
          <span className="shrink-0 rounded bg-muted px-1 text-xs text-muted-foreground tabular-nums" title={`${p.hidden.toLocaleString("en-US")} folded under it`}>
            +{p.hidden.toLocaleString("en-US")}
          </span>
        )}
        {p.minion && (
          <span className="shrink-0 rounded bg-sky-500/15 px-1 text-xs text-sky-700 dark:text-sky-300" title="This session's minion's own process">
            self
          </span>
        )}
        {notes.map((n) => (
          <span key={n.text} className="shrink-0 rounded bg-muted px-1 text-xs text-muted-foreground" title={n.title}>
            {n.text}
          </span>
        ))}
      </span>
      <span data-cell className={cn("text-xs", open ? "whitespace-normal [overflow-wrap:anywhere]" : "truncate", low)} title={open ? undefined : who}>
        {who}
      </span>
      <span
        data-cell
        className={cn("font-mono text-xs text-muted-foreground", open ? "whitespace-pre-wrap [overflow-wrap:anywhere]" : "truncate", low)}
        title={open ? undefined : line}
      >
        {line}
        {open && <ProcessStats p={p} />}
      </span>
    </>
  )
}

/** What an open row shows under its command line: its memory, CPU time, threads and start, each named (those the list has). */
function ProcessStats({ p }: { p: ProcessRow }) {
  const stats = [
    { name: "Memory", value: p.memory !== null && p.memory !== undefined ? formatBytes(p.memory) : "" },
    { name: "CPU time", value: cpuTimeText(p.cpuTime) },
    { name: "Threads", value: p.threads !== null && p.threads !== undefined ? String(p.threads) : "" },
    { name: "Started", value: p.started ? formatDateTime(p.started) : "" },
  ].filter((s) => s.value !== "")
  if (stats.length === 0) return null
  return (
    <span className="mt-1.5 flex flex-wrap gap-x-4 gap-y-0.5 font-sans whitespace-normal tabular-nums">
      {stats.map((s) => (
        <span key={s.name}>
          {s.name} <span className="text-foreground">{s.value}</span>
        </span>
      ))}
    </span>
  )
}

/**
 * The tree's lines in a name cell, as core says them (treeLines), each in
 * its level's column: a rail, or a corner's upright, the row's full height
 * and a pixel past its border, so it joins the next row's; an elbow's
 * ending where the corner turns, at the name's first line, to the process's
 * toggle where it has one, else to the name; a stem from below the toggle
 * down to its processes.
 */
function TreeLines({ p, toggle }: { p: ProcessRow; toggle: boolean }) {
  const below = toggle ? CORNER_Y + TOGGLE / 2 : CORNER_Y
  return treeLines(p).map((l) => {
    const x = l.level * INDENT + INDENT / 2
    // The corner's arm: across its own column to the toggle's box, or on to the name.
    const arm = toggle ? INDENT - INDENT / 2 + (INDENT - TOGGLE) / 2 : INDENT + INDENT / 2 - 3
    return (
      <Fragment key={`${l.kind}${l.level}`}>
        <span
          aria-hidden
          className="absolute w-px bg-muted-foreground/40"
          style={
            l.kind === "elbow"
              ? { left: x, top: 0, height: CORNER_Y }
              : l.kind === "stem"
                ? { left: x, top: below, bottom: -1 }
                : { left: x, top: 0, bottom: -1 }
          }
        />
        {(l.kind === "tee" || l.kind === "elbow") && (
          <span aria-hidden className="absolute h-px bg-muted-foreground/40" style={{ left: x, top: CORNER_Y, width: arm }} />
        )}
      </Fragment>
    )
  })
}

/** A process's menu: marking its name; copying what it is; Kill; on its path, List its folder and Download. */
function ProcessMenu({
  p,
  sep,
  onMark,
  onKill,
  killCommands,
  onOpenPath,
  listCommands,
  onDownload,
  downloadCommands,
}: {
  p: ProcessRow
  sep: string
  /** Opens the mark dialog for its name: the caller's own mark, as anyone may. */
  onMark: () => void
  onKill?: (target: KillTarget, commandId: string) => void
  killCommands: readonly MenuCommand[]
  onOpenPath?: (path: string, commandId?: string) => void
  listCommands: readonly MenuCommand[]
  onDownload?: (path: string, commandId: string) => void
  downloadCommands: readonly MenuCommand[]
}) {
  const folder = folderOf(p.path, sep)
  const path = p.path
  // The separator only above an item that shows: an action with no command shows none.
  const actions =
    (onKill && killCommands.length > 0) ||
    (folder && onOpenPath && listCommands.length > 0) ||
    (path && onDownload && downloadCommands.length > 0)
  return (
    <>
      <ContextMenuItem onClick={onMark} title={`Mark every process named ${p.name}, in every process list`}>
        <TagIcon />
        Mark by name…
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onClick={() => void copyToClipboard(String(p.pid), "pid")}>
        <CopyIcon />
        Copy pid
      </ContextMenuItem>
      <ContextMenuItem onClick={() => void copyToClipboard(p.name, "name")}>
        <CopyIcon />
        Copy name
      </ContextMenuItem>
      {path && (
        <ContextMenuItem onClick={() => void copyToClipboard(path, "path")}>
          <CopyIcon />
          Copy path
        </ContextMenuItem>
      )}
      {p.cmdline && (
        <ContextMenuItem onClick={() => void copyToClipboard(p.cmdline ?? "", "command line")}>
          <CopyIcon />
          Copy command line
        </ContextMenuItem>
      )}
      {actions && <ContextMenuSeparator />}
      {folder && onOpenPath && (
        <ActionItem label="List its folder" icon={<FolderOpenIcon />} commands={listCommands} onRun={(id) => onOpenPath(folder, id)} />
      )}
      {path && onDownload && (
        <ActionItem label="Download" icon={<DownloadIcon />} commands={downloadCommands} onRun={(id) => onDownload(path, id)} />
      )}
      {onKill && (
        <ActionItem
          label="Kill…"
          icon={<OctagonXIcon />}
          variant="destructive"
          commands={killCommands}
          onRun={(id) =>
            onKill({ pid: p.pid, name: p.name, started: startedSeconds(p.started), detail: p.cmdline ?? p.path ?? null, minion: p.minion }, id)
          }
        />
      )}
    </>
  )
}
