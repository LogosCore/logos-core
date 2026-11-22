import { useState } from "react"
import { CopyIcon, CpuIcon, DownloadIcon, FolderOpenIcon, ListTreeIcon, OctagonXIcon, RefreshCwIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { SearchInput } from "@/components/ui/search-input"
import { SortableHeader } from "@/components/ui/sortable-header"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionProcessOrder } from "@/graphql/gql/graphql"
import { useMinionTaskProcesses } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import type { DataTableSort } from "@/lib/data-table-sort"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { ActionItem, type MenuCommand } from "./action-menu"
import type { KillTarget } from "./kill-dialog"
import { cpuTimeText, folderOf, processNotes, startedSeconds, userText, type ProcessRow } from "./process-list"

// A process_list task's view (docs: contracts/task-results-and-actions.md,
// process_list): the host's processes, read from core a page at a time,
// ordered and searched there: a tree at first, each process under its
// parent; a column's header sorts them flat by it (again: the other way),
// and Tree goes back. A process's actions are in its context menu: Kill
// (kill_process), and on its path List its folder (list_dir) and Download
// (download_file); Refresh lists them again (list_processes).

const COLS = "grid-cols-[4.5rem_minmax(0,1.5fr)_minmax(0,1fr)_5.5rem_4.5rem_4rem_9.5rem_minmax(0,2fr)]"

/** The tree: the order until a column's header sorts the list flat. */
const TREE: DataTableSort<MinionProcessOrder> = { field: "TREE", direction: "ASC" }

/** How far a level of the tree indents its processes. */
const INDENT = 14

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
  const q = useMinionTaskProcesses(task.id, { search, order: sort.field, desc: sort.direction === "DESC" }, `${task.dataParts}:${task.state}`)
  const first = q.data?.pages[0]?.minionTaskProcesses
  const rows = q.data?.pages.flatMap((p) => p.minionTaskProcesses.edges.map((e) => e.node)) ?? []
  const sep = first?.sep ?? "/"
  const total = first?.totalCount ?? 0
  const searching = search.trim() !== ""
  const tree = sort.field === "TREE"

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-1 text-xs">
        <span className="text-muted-foreground tabular-nums">
          {first ? `${total.toLocaleString("en-US")} ${searching ? (total === 1 ? "match" : "matches") : total === 1 ? "process" : "processes"}` : ""}
        </span>
        <SearchInput
          value={search}
          onValueChange={setSearch}
          placeholder="Search name, pid, user, path, command line..."
          title="Matches the name, the pid, the user, the path and the command line; in the tree, each match shows under its ancestors"
          className="w-72"
        />
        <Button
          variant={tree ? "secondary" : "ghost"}
          size="xs"
          aria-pressed={tree}
          onClick={() => setSort(TREE)}
          title={
            tree
              ? "Each process under its parent, siblings in the order they started: a column's header sorts them flat by it"
              : "Show each process under its parent again"
          }
        >
          <ListTreeIcon />
          Tree
        </Button>
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
        <div className="flex min-h-0 flex-1 flex-col p-3">
          <VirtualizedDataTable
            items={rows}
            isLoading={q.isLoading}
            isFetchingNextPage={q.isFetchingNextPage}
            hasNextPage={q.hasNextPage}
            fetchNextPage={() => void q.fetchNextPage()}
            gridCols={COLS}
            entityNoun="processes"
            header={
              <>
                <SortableHeader label="PID" field="PID" sort={sort} onSortChange={setSort} />
                <SortableHeader label="Name" field="NAME" sort={sort} onSortChange={setSort} />
                <SortableHeader label="User" field="USER" sort={sort} onSortChange={setSort} />
                {/* The largest first, as the latest started: the first ask of each. */}
                <SortableHeader label="Memory" field="MEMORY" sort={sort} onSortChange={setSort} initialDirection="DESC" className="justify-self-end" />
                <SortableHeader label="CPU" field="CPU_TIME" sort={sort} onSortChange={setSort} initialDirection="DESC" className="justify-self-end" />
                <SortableHeader label="Threads" field="THREADS" sort={sort} onSortChange={setSort} initialDirection="DESC" className="justify-self-end" />
                <SortableHeader label="Started" field="STARTED" sort={sort} onSortChange={setSort} initialDirection="DESC" />
                <div>Command line</div>
              </>
            }
            emptyState={
              <>
                <CpuIcon className="size-8 opacity-50" />
                <p className="text-sm">{searching ? "No process matches." : "The list holds no process."}</p>
              </>
            }
            renderRow={(p) => (
              <ContextMenu onOpenChange={(open) => setMenuFor(open ? p.pid : null)}>
                <ContextMenuTrigger
                  render={<div className={dataTableRowClass(COLS, cn(p.context && "text-muted-foreground", menuFor === p.pid && "bg-muted"))} />}
                >
                  <ProcessCells p={p} hostArch={hostArch} tree={tree} />
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ProcessMenu
                    p={p}
                    sep={sep}
                    onKill={onKill}
                    killCommands={killCommands}
                    onOpenPath={onOpenPath}
                    listCommands={listCommands}
                    onDownload={onDownload}
                    downloadCommands={downloadCommands}
                  />
                </ContextMenuContent>
              </ContextMenu>
            )}
          />
        </div>
      )}
    </div>
  )
}

/** A process's cells: pid; name, indented by depth in the tree, with its notes; user; memory; CPU time; threads; started; command line. */
function ProcessCells({ p, hostArch, tree }: { p: ProcessRow; hostArch?: string | null; tree: boolean }) {
  const notes = processNotes(p, hostArch)
  const who = userText(p)
  const line = p.cmdline ?? p.path ?? ""
  return (
    <>
      <span className="font-mono text-xs tabular-nums">{p.pid}</span>
      <span className="flex min-w-0 items-baseline gap-1.5" style={tree ? { paddingLeft: p.depth * INDENT } : undefined}>
        <span className="truncate" title={p.path ?? p.name}>
          {p.name}
        </span>
        {p.minion && (
          <span className="shrink-0 rounded bg-sky-500/15 px-1 text-xs text-sky-700 dark:text-sky-300" title="This session's minion's own process">
            this minion
          </span>
        )}
        {notes.map((n) => (
          <span key={n} className="shrink-0 rounded bg-muted px-1 text-xs text-muted-foreground">
            {n}
          </span>
        ))}
      </span>
      <span className="truncate text-xs" title={who}>
        {who}
      </span>
      <span className="text-right text-xs tabular-nums">{p.memory !== null && p.memory !== undefined ? formatBytes(p.memory) : ""}</span>
      <span className="text-right text-xs tabular-nums">{cpuTimeText(p.cpuTime)}</span>
      <span className="text-right text-xs tabular-nums">{p.threads ?? ""}</span>
      <span className="truncate text-xs tabular-nums">{p.started ? formatDateTime(p.started) : ""}</span>
      <span className="truncate font-mono text-xs text-muted-foreground" title={line}>
        {line}
      </span>
    </>
  )
}

/** A process's menu: copying what it is; Kill; on its path, List its folder and Download. */
function ProcessMenu({
  p,
  sep,
  onKill,
  killCommands,
  onOpenPath,
  listCommands,
  onDownload,
  downloadCommands,
}: {
  p: ProcessRow
  sep: string
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
