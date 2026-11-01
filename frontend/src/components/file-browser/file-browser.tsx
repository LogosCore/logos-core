import { useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { toast } from "sonner"
import { Virtuoso } from "react-virtuoso"
import {
  ArrowUpIcon,
  ChevronRightIcon,
  CircleHelpIcon,
  CopyIcon,
  DownloadIcon,
  FileIcon,
  FolderIcon,
  FolderLockIcon,
  FolderOpenIcon,
  HardDriveIcon,
  Link2Icon,
  LoaderIcon,
  RadioTowerIcon,
  RefreshCwIcon,
  SearchIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useCanBuild } from "@/components/builds/build-access"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SortableHeader } from "@/components/ui/sortable-header"
import { SplitPane } from "@/components/ui/split-pane"
import { Switch } from "@/components/ui/switch"
import type { FsEntryFieldsFragment } from "@/graphql/gql/graphql"
import {
  samePath,
  useFileBrowser,
  useFileBrowserChanged,
  useFileBrowserEntries,
  useFileBrowserSearch,
  type FsEntriesFilter,
} from "@/graphql/hooks/file-browser"
import { useMinionSession, useMinionSessionsLayout } from "@/graphql/hooks/minion-sessions"
import { DEFAULT_FILES_SPLIT, MIN_SPLIT } from "@/components/minion-sessions/sessions-layout"
import { useMinionTaskSubscription, useMinionTasks, useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { startStreamUpload } from "@/lib/stream-upload"
import { ActionItem, type MenuCommand } from "@/components/minion-sessions/action-menu"
import { chooseActionCommand, commandsFor, readPicked } from "@/components/minion-sessions/action-commands"
import { useConsoleCommands } from "@/components/minion-sessions/console-commands"
import { crumbs } from "@/components/minion-sessions/dir-listing"
import { DeleteDialog, type DeleteTarget } from "@/components/minion-sessions/delete-dialog"
import { UploadDialog } from "@/components/minion-sessions/upload-dialog"
import type { UploadTarget } from "@/components/minion-sessions/upload-target"
import { UploadHereButton } from "@/components/minion-sessions/upload-here-button"
import { useWorkspaceActions } from "@/components/minion-sessions/workspace-tabs"
import { usePaneRow } from "@/components/minion-sessions/pane-text"
import { FileNameIcon } from "@/components/file-preview/file-type-icon"
import { DownloadedFileDialog } from "./downloaded-file-dialog"
import { FolderTree } from "./file-browser-tree"
import {
  byWhom,
  inFlight,
  inFlightAt,
  LISTED_FOLDER,
  listedSays,
  parentOf,
  pathsTo,
  sepOf,
  UNLISTED_FOLDER,
  UNREADABLE_FOLDER,
  when,
  type InFlight,
} from "./file-browser-model"
import { useFileBrowserState } from "./file-browser-state"

// The file browser of a session's machine (docs: contracts/file-browser.md):
// what core's minions have seen of its files, read from core's snapshot, a
// folder at a time, with the folder tree beside it and a search of the
// whole machine above. Its actions are tasks of a session of the machine:
// the tab's own, or another the operator picks; each changes the snapshot
// as it ends, and the folders it changed are read again.

type Entry = FsEntryFieldsFragment

const ICONS = { dir: FolderIcon, file: FileIcon, link: Link2Icon, other: CircleHelpIcon } as const
// Columns as wide as the rows' text makes them (text-sm, pane-text.ts):
// 84, 154, 84 and 112px at 14px.
const GRID =
  "grid-cols-[minmax(0,1fr)_calc(var(--text-sm)*6)_calc(var(--text-sm)*11)_calc(var(--text-sm)*6)_minmax(0,calc(var(--text-sm)*8))]"

export function FileBrowser({ tabId, sessionId }: { tabId: string; sessionId: string }) {
  const [state, set] = useFileBrowserState(tabId)
  const { layout, setLayout } = useMinionSessionsLayout()
  const machineQ = useFileBrowser(sessionId)
  useFileBrowserChanged(sessionId)
  const own = useMinionSession(sessionId).data?.minionSession ?? null
  const access = useCanBuild(own?.operationId ?? "")
  const workspace = useWorkspaceActions()

  // The session actions run through: the tab's. Act as another turns the
  // tab into that session's (its label and mark), as it is.
  const sessions = machineQ.data?.fileBrowser.sessions ?? []
  const actingId = sessionId
  const acting = sessions.find((s) => s.id === actingId)
  const actingNumber = acting?.number ?? own?.number
  const closed = (acting?.state ?? own?.state) === "CLOSED"
  const writable = access.canWrite && !closed
  const why = access.loading
    ? undefined
    : !access.canWrite
      ? "Viewers see the files; operators act on them."
      : closed
        ? `Session #${actingNumber} is closed: pick an open one of the machine.`
        : undefined

  const { commands } = useConsoleCommands(actingId)
  const byId = useMemo(() => new Map(commands.map((c) => [c.id, c])), [commands])
  useMinionTaskSubscription(actingId)
  const tasksQ = useMinionTasks(actingId)
  const running = useMemo(
    () => inFlight((tasksQ.data?.pages ?? []).flatMap((p) => p.minionTasks.tasks), (c) => byId.get(c)?.action),
    [tasksQ.data, byId],
  )

  const listCommands = writable ? commandsFor(commands, "list_dir") : []
  const downloadCommands = writable ? commandsFor(commands, "download_file") : []
  const streamCommands = writable ? commandsFor(commands, "stream_file") : []
  const uploadCommands = writable ? [...commandsFor(commands, "upload_file"), ...commandsFor(commands, "upload_stream")] : []
  const deleteCommands = writable ? commandsFor(commands, "delete_path") : []
  const refreshCommand = chooseActionCommand(listCommands, readPicked(actingId, "list_dir"))

  const queue = useQueueMinionTask()
  // quiet: a task the browser runs by itself (Auto-list) says nothing
  // unless it fails; its row and the folder's line show it going.
  const run = (commandId: string, args: Record<string, unknown>, what: string, quiet = false) =>
    queue.mutate(
      { sessionId: actingId, command: commandId, args },
      {
        onSuccess: () =>
          !quiet &&
          toast.success(`${what}: queued on #${actingNumber}`, {
            action: workspace ? { label: "Console", onClick: () => workspace.openConsole(actingId) } : undefined,
          }),
        onError: (err) => toast.error(errorMessage(err, `Could not queue it`)),
      },
    )

  const [uploadTo, setUploadTo] = useState<{ target: UploadTarget; commandId: string } | null>(null)
  // Read at render: the compiler reads what a handler uses there.
  const uploadCommand = uploadTo ? byId.get(uploadTo.commandId) : undefined
  const [deleting, setDeleting] = useState<{ target: DeleteTarget; commandId: string } | null>(null)
  const deleteCommandId = deleting?.commandId ?? null
  const [viewing, setViewing] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  // The entry a search opened its folder at: marked.
  const [marked, setMarked] = useState<string | null>(null)

  const open = (path: string | null, mark: string | null = null) => {
    setMarked(mark)
    if (path === null) return set({ path: null })
    // The tree opens on the way to it.
    const way = pathsTo(path).slice(0, -1)
    const expanded = [...state.expanded, ...way.filter((p) => !state.expanded.some((e) => samePath(e, p)))]
    set({ path, expanded })
  }

  const actions: EntryActions = {
    open: (e) => open(e.path),
    list: listCommands.length > 0 ? (e, id) => run(id, { path: e.path }, `List ${e.name}`) : undefined,
    listCommands,
    download: (e, id) => run(id, { path: e.path }, `Download ${e.name}`),
    downloadCommands,
    stream: (e, id) => run(id, { path: e.path }, `Stream ${e.name}`),
    streamCommands,
    upload: (folder, id, names) => setUploadTo({ target: { folder, sep: sepOf(folder), names }, commandId: id }),
    uploadCommands,
    remove: (e, id) => setDeleting({ target: { path: e.path, name: e.name, type: e.type }, commandId: id }),
    deleteCommands,
    view: (fileTaskId) => setViewing(fileTaskId),
  }

  const [toolsSlot, setToolsSlot] = useState<HTMLDivElement | null>(null)
  const searching = query.trim().length >= 2
  const machineKey = machineQ.data?.fileBrowser.key ?? ""

  return (
    <div className="flex min-h-0 flex-1 flex-col border-t">
      <div className="flex flex-wrap items-center gap-2 border-b px-2 py-1 text-xs">
        <span className="text-muted-foreground" title={machineKey.startsWith("session:") ? "The minion gave no lasting machine id: this session's own files" : `Machine ${machineKey}`}>
          Act as
        </span>
        <Select value={actingId} disabled={!workspace} onValueChange={(v) => v && v !== sessionId && workspace?.switchFileBrowser(sessionId, v)}>
          <SelectTrigger size="sm" className="h-6 min-w-40 text-xs" aria-label="The session actions run through">
            <SelectValue>
              {(v: string) => {
                const s = sessions.find((x) => x.id === v)
                return s ? sessionLabel(s) : actingNumber !== undefined ? `#${actingNumber}` : "…"
              }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {sessions
              .filter((s) => s.state !== "CLOSED" || s.id === sessionId)
              .map((s) => (
                <SelectItem key={s.id} value={s.id}>
                  {sessionLabel(s)}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
        <SearchInput
          value={query}
          onValueChange={setQuery}
          placeholder="Search the machine"
          title="Every entry the snapshot has, by a part of the name or a glob (*.kdbx, id_rsa*), any case"
          className="relative w-56"
          inputClassName={cn("h-6 pl-8 text-xs", query && "pr-7")}
        />
        <label className="flex cursor-pointer items-center gap-1.5 text-muted-foreground">
          <Switch size="sm" checked={state.showDeleted} onCheckedChange={(v) => set({ showDeleted: v })} />
          Show deleted
        </label>
        <label
          className="flex cursor-pointer items-center gap-1.5 text-muted-foreground"
          title="Entering a folder never listed lists it (ls), through this tab's session"
        >
          <Switch size="sm" checked={state.autoList} onCheckedChange={(v) => set({ autoList: v })} />
          Auto-list
        </label>
        {/* The folder's status and tools, from the folder view (a portal):
            in this row, not one of their own. */}
        <div className="ml-auto flex min-w-0 items-center gap-2">
          {why && <span className="truncate text-muted-foreground">{why}</span>}
          <div ref={setToolsSlot} className="contents" />
        </div>
      </div>
      <SplitPane
        orientation="horizontal"
        label="Resize the folder tree and the folder beside it"
        // The divider is the operator's, kept with their sessions layout:
        // every file browser opens with it where they left it.
        split={layout.filesSplit ?? DEFAULT_FILES_SPLIT}
        defaultSplit={DEFAULT_FILES_SPLIT}
        min={MIN_SPLIT}
        max={0.6}
        onChange={(filesSplit) => setLayout({ ...layout, filesSplit })}
        first={
          <FolderTree
            sessionId={sessionId}
            showDeleted={state.showDeleted}
            current={searching ? null : state.path}
            expanded={state.expanded}
            onToggle={(p, isOpen) =>
              set({ expanded: isOpen ? [...state.expanded, p] : state.expanded.filter((e) => !samePath(e, p)) })
            }
            onOpen={(p) => {
              setQuery("")
              open(p)
            }}
          />
        }
        second={
          <div className="flex min-h-0 flex-1 flex-col border-l">
            {searching ? (
              <SearchResults
                sessionId={sessionId}
                query={query.trim()}
                showDeleted={state.showDeleted}
                running={running}
                actions={actions}
                onOpenAt={(e) => {
                  setQuery("")
                  if (e.type === "dir") open(e.path)
                  else open(e.parent ?? null, e.path)
                }}
              />
            ) : (
              <FolderView
                sessionId={sessionId}
                path={state.path}
                filter={{ sort: state.sort, desc: state.desc, search: "", showDeleted: state.showDeleted }}
                onSort={(sort, desc) => set({ sort, desc })}
                marked={marked}
                running={running}
                actions={actions}
                onOpen={(p) => open(p)}
                onRefresh={
                  refreshCommand && state.path ? (quiet) => run(refreshCommand.id, { path: state.path }, "Refresh", quiet) : undefined
                }
                autoList={state.autoList}
                toolsSlot={toolsSlot}
              />
            )}
          </div>
        }
      />
      <DeleteDialog
        target={deleting?.target ?? null}
        commandId={deleteCommandId}
        onOpenChange={(isOpen) => !isOpen && setDeleting(null)}
        onDelete={async (args) => {
          if (!deleteCommandId) return
          await queue.mutateAsync({ sessionId: actingId, command: deleteCommandId, args })
          toast.success(`Delete: queued on #${actingNumber}`)
        }}
      />
      <UploadDialog
        target={uploadTo?.target ?? null}
        command={uploadCommand}
        operationId={own?.operationId ?? ""}
        onOpenChange={(isOpen) => !isOpen && setUploadTo(null)}
        onSubmit={async (args, stream) => {
          if (!uploadCommand) return
          const data = await queue.mutateAsync({ sessionId: actingId, command: uploadCommand.id, args })
          if (stream) startStreamUpload(data.queueMinionTask.id, stream)
          toast.success(`Upload: queued on #${actingNumber}`)
        }}
      />
      <DownloadedFileDialog taskId={viewing} onClose={() => setViewing(null)} />
    </div>
  )
}

function sessionLabel(s: { number: number; host?: string | null; user?: string | null; state: string }): string {
  const who = [s.user, s.host].filter(Boolean).join("@")
  return `#${s.number}${who ? ` ${who}` : ""}${s.state === "CLOSED" ? " (closed)" : ""}`
}

/** What an entry's menu can do; a missing one, or no commands, offers nothing. */
interface EntryActions {
  open: (e: Entry) => void
  list?: (e: Entry, commandId: string) => void
  listCommands: readonly MenuCommand[]
  download: (e: Entry, commandId: string) => void
  downloadCommands: readonly MenuCommand[]
  stream: (e: Entry, commandId: string) => void
  streamCommands: readonly MenuCommand[]
  upload: (folder: string, commandId: string, names?: string[]) => void
  uploadCommands: readonly MenuCommand[]
  remove: (e: Entry, commandId: string) => void
  deleteCommands: readonly MenuCommand[]
  /** Opens the kept file of a download task. */
  view: (taskId: string) => void
}

function FolderView({
  sessionId,
  path,
  filter,
  onSort,
  marked,
  running,
  actions,
  onOpen,
  onRefresh,
  autoList,
  toolsSlot,
}: {
  sessionId: string
  path: string | null
  filter: FsEntriesFilter
  onSort: (sort: FsEntriesFilter["sort"], desc: boolean) => void
  marked: string | null
  running: readonly InFlight[]
  actions: EntryActions
  onOpen: (path: string | null) => void
  /** Lists the folder; quiet, when the browser does it by itself. */
  onRefresh?: (quiet?: boolean) => void
  /** Lists a folder never listed as it is entered. */
  autoList: boolean
  /** Where the status and tools go: the browser's header. */
  toolsSlot: HTMLElement | null
}) {
  const [search, setSearch] = useState("")
  const q = useFileBrowserEntries(sessionId, path, { ...filter, search })
  const pages = q.data?.pages ?? []
  const entries = pages.flatMap((p) => p.fileBrowserEntries.entries)
  const folder = pages[0]?.fileBrowserEntries.folder ?? null
  const sep = path ? sepOf(path) : "/"
  const way = path ? crumbs(path, sep) : []
  const here = path ? inFlightAt(running, path) : undefined
  // Auto-list: a folder entered that was never listed (or is not in the
  // snapshot at all) is listed, once while the tab shows it: a listing
  // that fails is not run again and again. Its own entries, read, decide:
  // not the folder before's, still shown while they come.
  const autoListed = useRef(new Set<string>())
  const unlisted = path !== null && !q.isLoading && !q.isPlaceholderData && !q.isError && !folder?.listedAt
  useEffect(() => {
    if (!autoList || !unlisted || !onRefresh || here || autoListed.current.has(path)) return
    autoListed.current.add(path)
    onRefresh(true)
  }, [autoList, unlisted, onRefresh, here, path])
  // Uploads on their way into this folder: their files may not be in it yet.
  const arriving = path ? running.filter((f) => f.action.startsWith("upload") && parentOf(f.path) !== null && samePath(parentOf(f.path)!, path)) : []
  const sort = { field: filter.sort, direction: filter.desc ? "DESC" : "ASC" } as const
  const names = entries.map((e) => e.name)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b px-2 py-1">
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Up"
          title={path ? `Up: ${parentOf(path) ?? "the machine's roots"}` : "At the machine's roots"}
          disabled={!path}
          onClick={() => onOpen(path ? parentOf(path) : null)}
        >
          <ArrowUpIcon />
        </Button>
        <nav aria-label="Path" className="flex min-w-0 flex-1 items-center overflow-x-auto font-mono text-xs [scrollbar-width:thin]">
          <button type="button" className="shrink-0 cursor-pointer rounded px-1 py-0.5 hover:bg-muted" title="The machine's roots" onClick={() => onOpen(null)}>
            <HardDriveIcon className="size-3.5 text-muted-foreground" />
          </button>
          {way.map((c, i) => (
            <span key={c.path} className="flex shrink-0 items-center">
              {i > 1 && <ChevronRightIcon className="size-3 text-muted-foreground" />}
              {i === way.length - 1 ? (
                <span className="px-1 py-0.5 font-semibold">{c.label}</span>
              ) : (
                <button type="button" className="cursor-pointer rounded px-1 py-0.5 hover:bg-muted" title={`Open ${c.path}`} onClick={() => onOpen(c.path)}>
                  {c.label}
                </button>
              )}
            </span>
          ))}
        </nav>
        {toolsSlot &&
          createPortal(
            <>
              <span className="flex min-w-0 shrink items-center gap-3 px-1 text-xs whitespace-nowrap text-muted-foreground">
                {path !== null &&
                  (folder?.listedAt ? (
                    <span className="truncate" title={formatDateTime(folder.listedAt)}>
                      Listed {when(folder.listedAt)} by {byWhom(folder.listedBy)}
                    </span>
                  ) : (
                    <span className="truncate" title="Only what other tasks put here: Refresh lists it">
                      Never listed
                    </span>
                  ))}
                {folder?.listErr && (
                  <span className="truncate text-red-600 dark:text-red-400" title={listedSays(folder)}>
                    Could not be read{folder.listErrAt ? ` ${when(folder.listErrAt)}` : ""}: {folder.listErr}
                  </span>
                )}
                {folder?.goneAt && <span className="truncate text-red-600 dark:text-red-400">{goneSays(folder)}</span>}
                {here && (
                  <span className="flex shrink-0 items-center gap-1">
                    <LoaderIcon className="size-3 animate-spin" />
                    {here.label}
                  </span>
                )}
                {arriving.length > 0 && (
                  <span className="shrink-0">{arriving.length === 1 ? "1 upload" : `${arriving.length} uploads`} on the way</span>
                )}
                <span className="shrink-0 tabular-nums">
                  {entries.length.toLocaleString("en-US")}
                  {q.hasNextPage ? "+" : ""} {entries.length === 1 ? "entry" : "entries"}
                </span>
              </span>
              <SearchInput
                value={search}
                onValueChange={setSearch}
                placeholder="Filter"
                title="This folder's entries whose names hold it, any case"
                className="relative w-36"
                inputClassName={cn("h-6 pl-8 text-xs", search && "pr-7")}
              />
              {path && <UploadHereButton folder={path} commands={actions.uploadCommands} onRun={(id) => actions.upload(path, id, names)} />}
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="Refresh"
                title={onRefresh ? `List ${path} again on the host` : path ? "No session that can list it" : "Open a folder to list it"}
                disabled={!onRefresh || !!here}
                onClick={() => onRefresh?.()}
              >
                <RefreshCwIcon className={cn(here?.action === "list_dir" && "animate-spin")} />
              </Button>
            </>,
            toolsSlot,
          )}
      </div>
      <div className={`grid ${GRID} gap-3 border-b bg-muted/50 px-3 py-1.5 text-xs font-medium tracking-wide text-muted-foreground uppercase`}>
        <SortableHeader label="Name" field="name" sort={sort} onSortChange={(s) => onSort(s.field, s.direction === "DESC")} />
        <SortableHeader
          label="Size"
          field="size"
          sort={sort}
          initialDirection="DESC"
          className="justify-end"
          onSortChange={(s) => onSort(s.field, s.direction === "DESC")}
        />
        <SortableHeader
          label="Modified"
          field="modified"
          sort={sort}
          initialDirection="DESC"
          onSortChange={(s) => onSort(s.field, s.direction === "DESC")}
        />
        <div>Mode</div>
        <div>Owner</div>
      </div>
      <Rows
        entries={entries}
        loading={q.isLoading}
        error={q.isError ? (q.error instanceof Error ? q.error.message : "Couldn't read the folder.") : null}
        hasNextPage={!!q.hasNextPage}
        fetching={q.isFetchingNextPage}
        fetchNextPage={() => void q.fetchNextPage()}
        empty={
          search
            ? "No entry here holds that."
            : path === null
              ? "Nothing seen yet: list a folder (ls) in the session's console."
              : folder?.listedAt
                ? "The folder was empty when it was listed."
                : "Nothing known here yet: Refresh lists it."
        }
        marked={marked}
        running={running}
        actions={actions}
      />
    </div>
  )
}

function SearchResults({
  sessionId,
  query,
  showDeleted,
  running,
  actions,
  onOpenAt,
}: {
  sessionId: string
  query: string
  showDeleted: boolean
  running: readonly InFlight[]
  actions: EntryActions
  onOpenAt: (e: Entry) => void
}) {
  const q = useFileBrowserSearch(sessionId, query, showDeleted)
  const entries = (q.data?.pages ?? []).flatMap((p) => p.fileBrowserSearch.entries)
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-6 items-center gap-2 border-b px-3 py-1 text-xs text-muted-foreground">
        <SearchIcon className="size-3.5" />
        <span>
          Entries of the machine matching <span className="font-mono text-foreground">{query}</span>: a click opens its folder at it.
        </span>
        <span className="ml-auto tabular-nums">
          {entries.length.toLocaleString("en-US")}
          {q.hasNextPage ? "+" : ""} found
        </span>
      </div>
      <div className={`grid ${GRID} gap-3 border-b bg-muted/50 px-3 py-1.5 text-xs font-medium tracking-wide text-muted-foreground uppercase`}>
        <div>Path</div>
        <div className="text-right">Size</div>
        <div>Modified</div>
        <div>Mode</div>
        <div>Owner</div>
      </div>
      <Rows
        entries={entries}
        loading={q.isLoading}
        error={q.isError ? (q.error instanceof Error ? q.error.message : "Couldn't search.") : null}
        hasNextPage={!!q.hasNextPage}
        fetching={q.isFetchingNextPage}
        fetchNextPage={() => void q.fetchNextPage()}
        empty="Nothing the machine was seen to have matches."
        marked={null}
        running={running}
        actions={actions}
        fullPaths
        onClick={onOpenAt}
      />
    </div>
  )
}

function Rows({
  entries,
  loading,
  error,
  hasNextPage,
  fetching,
  fetchNextPage,
  empty,
  marked,
  running,
  actions,
  fullPaths = false,
  onClick,
}: {
  entries: Entry[]
  loading: boolean
  error: string | null
  hasNextPage: boolean
  fetching: boolean
  fetchNextPage: () => void
  empty: string
  marked: string | null
  running: readonly InFlight[]
  actions: EntryActions
  fullPaths?: boolean
  onClick?: (e: Entry) => void
}) {
  // The entry whose menu is open, marked while it is.
  const [menuFor, setMenuFor] = useState<string | null>(null)
  // 30px at the app's size (14px rows): as tall as the pane's text makes it.
  const rowH = usePaneRow(30)
  if (error) return <p className="p-4 text-sm text-red-600 dark:text-red-400">{error}</p>
  if (loading)
    return (
      <div className="flex flex-1 items-center justify-center p-4 text-muted-foreground">
        <LoaderIcon className="size-4 animate-spin" />
      </div>
    )
  if (entries.length === 0) return <p className="flex flex-1 items-center justify-center p-4 text-sm text-muted-foreground">{empty}</p>
  return (
    <Virtuoso
      data={entries}
      className="min-h-0 flex-1"
      style={{ height: "100%" }}
      fixedItemHeight={rowH}
      overscan={400}
      // A marked entry (a search opened its folder at it) is scrolled to.
      initialTopMostItemIndex={marked ? Math.max(0, entries.findIndex((e) => samePath(e.path, marked))) : 0}
      endReached={() => hasNextPage && !fetching && fetchNextPage()}
      computeItemKey={(_, e) => e.path}
      itemContent={(_, e) => (
        <EntryRow
          entry={e}
          fullPath={fullPaths}
          marked={(marked !== null && samePath(marked, e.path)) || menuFor === e.path}
          running={inFlightAt(running, e.path)}
          actions={actions}
          onMenu={(isOpen) => setMenuFor(isOpen ? e.path : null)}
          onClick={onClick ? () => onClick(e) : clickOf(e, actions)}
          height={rowH}
        />
      )}
      components={{
        Footer: () =>
          fetching ? (
            <div className="flex justify-center py-2">
              <LoaderIcon className="size-4 animate-spin text-muted-foreground" />
            </div>
          ) : null,
      }}
    />
  )
}

/** What a click on a folder's entry does: a folder opens; a file core keeps a download of opens that file. */
function clickOf(e: Entry, actions: EntryActions): (() => void) | undefined {
  if (e.type === "dir") return () => actions.open(e)
  const kept = e.downloaded.findLast((d) => !!d.fileId)
  return kept ? () => actions.view(kept.taskId) : undefined
}

/** Why an entry is gone, and when. */
function goneSays(e: Entry): string {
  if (!e.goneAt) return ""
  const how = e.goneHow === "deleted" ? "Deleted" : "Not in the folder's listing"
  return `${how} ${when(e.goneAt)}${e.goneBy ? ` (${byWhom(e.goneBy)})` : ""}`
}

function EntryRow({
  entry: e,
  fullPath,
  marked,
  running,
  actions,
  onMenu,
  onClick,
  height,
}: {
  entry: Entry
  /** The row's height: the list lays rows out by it. */
  height: number
  fullPath: boolean
  marked: boolean
  running?: InFlight
  actions: EntryActions
  onMenu: (open: boolean) => void
  onClick?: () => void
}) {
  const Icon = !e.parent ? HardDriveIcon : e.type === "dir" && e.listErr ? FolderLockIcon : (ICONS[e.type as keyof typeof ICONS] ?? CircleHelpIcon)
  const gone = !!e.goneAt
  const kept = e.downloaded.findLast((d) => !!d.fileId)
  const lastDownload = e.downloaded.at(-1)
  const lastUpload = e.uploaded.at(-1)
  const isDir = e.type === "dir"
  const isFile = e.type === "file"
  const title = [
    e.path,
    gone ? goneSays(e) : null,
    !e.known ? "Known only as a folder on the way to another entry" : null,
    isDir ? listedSays(e) : null,
    e.seenAt ? `Seen ${formatDateTime(e.seenAt)} by ${byWhom(e.seenBy)}` : null,
  ]
    .filter(Boolean)
    .join("\n")
  return (
    <ContextMenu onOpenChange={onMenu}>
      <ContextMenuTrigger
        render={
          <div
            className={cn(
              `grid ${GRID} cursor-default items-center gap-3 border-b px-3 text-sm hover:bg-muted/40`,
              (e.hidden || !e.known) && "text-muted-foreground",
              gone && "text-muted-foreground",
              marked && "bg-muted",
              onClick && "cursor-pointer",
            )}
            style={{ height }}
            onClick={onClick}
          />
        }
      >
        <span className="flex min-w-0 items-baseline gap-1.5" title={title}>
          {/* A file's icon says its kind, by its name: the snapshot has no content types. */}
          {isFile && e.parent ? (
            <FileNameIcon name={e.name} className="size-3.5 shrink-0 self-center text-muted-foreground" />
          ) : (
            <Icon
              className={cn(
                "size-3.5 shrink-0 self-center",
                isDir && !gone ? (e.listErr ? UNREADABLE_FOLDER : e.listedAt ? LISTED_FOLDER : UNLISTED_FOLDER) : "text-muted-foreground",
              )}
            />
          )}
          {/* A name as a name (the sessions table's hosts); a search's whole path as a path. */}
          <span className={cn("truncate", fullPath && "font-mono text-xs", gone && "line-through", !e.known && "italic")}>
            {fullPath ? e.path : e.name}
          </span>
          {e.target && <span className="truncate text-muted-foreground">→ {e.target}</span>}
          {lastDownload && (
            <button
              type="button"
              className={cn("shrink-0 self-center text-sky-600 dark:text-sky-400", kept && "cursor-pointer hover:text-foreground")}
              title={`Downloaded ${formatDateTime(lastDownload.at)}${lastDownload.stream ? " (streamed: core keeps no copy)" : ""}${e.downloaded.length > 1 ? `, ${e.downloaded.length} times` : ""}${kept ? ": open the file" : ""}`}
              aria-label="Downloaded"
              onClick={(ev) => {
                ev.stopPropagation()
                if (kept) actions.view(kept.taskId)
              }}
            >
              <DownloadIcon className="size-3" />
            </button>
          )}
          {lastUpload && (
            <span
              className="shrink-0 self-center text-emerald-600 dark:text-emerald-400"
              title={`Uploaded ${formatDateTime(lastUpload.at)}${lastUpload.sha256 ? `\nSHA-256 ${lastUpload.sha256}` : ""}`}
            >
              <UploadIcon className="size-3" />
            </span>
          )}
          {running && (
            <span className="flex shrink-0 items-center gap-1 self-center text-xs text-muted-foreground">
              <LoaderIcon className="size-3 animate-spin" />
              {running.label}
            </span>
          )}
        </span>
        <span className="text-right whitespace-nowrap text-muted-foreground tabular-nums">{isFile && e.size != null ? formatBytes(e.size) : ""}</span>
        <span className="whitespace-nowrap text-muted-foreground tabular-nums">{e.modified != null ? formatDateTime(e.modified * 1000) : ""}</span>
        <span className="truncate font-mono text-xs text-muted-foreground">{e.mode}</span>
        <span className="truncate text-muted-foreground">{[e.owner, e.group].filter(Boolean).join(":")}</span>
      </ContextMenuTrigger>
      <ContextMenuContent>
        {(isDir || e.type === "link") && (
          <ContextMenuItem onClick={() => actions.open(e)}>
            <FolderOpenIcon />
            Open
          </ContextMenuItem>
        )}
        {(isDir || e.type === "link" || !e.parent) && actions.list && (
          <ActionItem label="List directory" icon={<RefreshCwIcon />} commands={actions.listCommands} onRun={(id) => actions.list!(e, id)} />
        )}
        {(isDir || e.type === "link" || !e.parent) && <ContextMenuSeparator />}
        {kept && (
          <ContextMenuItem onClick={() => actions.view(kept.taskId)}>
            <FileIcon />
            Open downloaded file
          </ContextMenuItem>
        )}
        {isFile && !gone && (
          <>
            <ActionItem label="Download" icon={<DownloadIcon />} commands={actions.downloadCommands} onRun={(id) => actions.download(e, id)} />
            <ActionItem label="Stream" icon={<RadioTowerIcon />} commands={actions.streamCommands} onRun={(id) => actions.stream(e, id)} />
          </>
        )}
        {isDir && !gone && (
          <ActionItem label="Upload here…" icon={<UploadIcon />} commands={actions.uploadCommands} onRun={(id) => actions.upload(e.path, id)} />
        )}
        {!gone && e.parent && (
          <ActionItem
            label="Delete…"
            icon={<Trash2Icon />}
            variant="destructive"
            commands={actions.deleteCommands}
            onRun={(id) => actions.remove(e, id)}
          />
        )}
        <ContextMenuSeparator />
        <ContextMenuItem onClick={() => void copyToClipboard(e.path, "path")}>
          <CopyIcon />
          Copy path
        </ContextMenuItem>
        <ContextMenuItem onClick={() => void copyToClipboard(e.name, "name")}>
          <CopyIcon />
          Copy name
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}
