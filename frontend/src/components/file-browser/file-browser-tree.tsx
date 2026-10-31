import { ChevronRightIcon, FolderIcon, HardDriveIcon, LoaderIcon } from "lucide-react"
import { samePath, useFileBrowserEntries, type FsEntriesFilter } from "@/graphql/hooks/file-browser"
import type { FsEntryFieldsFragment } from "@/graphql/gql/graphql"
import { cn } from "@/lib/utils"

// The file browser's folder tree: the machine's roots, and each folder's
// sub-folders as it is opened, read from core (foldersOnly) a page at a
// time. A deleted folder is struck through while Show deleted is on.

type Entry = FsEntryFieldsFragment

export function FolderTree({
  sessionId,
  showDeleted,
  current,
  expanded,
  onToggle,
  onOpen,
}: {
  sessionId: string
  showDeleted: boolean
  /** The folder shown beside it: marked. */
  current: string | null
  expanded: readonly string[]
  onToggle: (path: string, open: boolean) => void
  onOpen: (path: string) => void
}) {
  return (
    <nav aria-label="Folders" className="min-h-0 flex-1 overflow-auto py-1 text-xs [scrollbar-width:thin]">
      {/* As wide as its widest row: a deep or long name scrolls sideways
          rather than being cut. */}
      <div className="w-max min-w-full">
        <Folders
          sessionId={sessionId}
          path={null}
          depth={0}
          showDeleted={showDeleted}
          current={current}
          expanded={expanded}
          onToggle={onToggle}
          onOpen={onOpen}
        />
      </div>
    </nav>
  )
}

function Folders({
  sessionId,
  path,
  depth,
  showDeleted,
  current,
  expanded,
  onToggle,
  onOpen,
}: {
  sessionId: string
  path: string | null
  depth: number
  showDeleted: boolean
  current: string | null
  expanded: readonly string[]
  onToggle: (path: string, open: boolean) => void
  onOpen: (path: string) => void
}) {
  const filter: FsEntriesFilter = { sort: "name", desc: false, search: "", showDeleted }
  const q = useFileBrowserEntries(sessionId, path, filter, { foldersOnly: true })
  const folders = (q.data?.pages ?? []).flatMap((p) => p.fileBrowserEntries.entries)
  const pad = { paddingLeft: 6 + depth * 12 }
  if (q.isLoading)
    return (
      <div className="py-0.5 text-muted-foreground" style={pad}>
        <LoaderIcon className="inline size-3 animate-spin" />
      </div>
    )
  if (q.isError)
    return (
      <div className="py-0.5 text-red-600 dark:text-red-400" style={pad}>
        {q.error instanceof Error ? q.error.message : "Couldn't read the folders."}
      </div>
    )
  if (path === null && folders.length === 0)
    return <p className="max-w-xs px-3 py-2 text-muted-foreground">Nothing seen yet: list a folder (ls) in the session&apos;s console, or Refresh here.</p>
  return (
    <>
      {folders.map((f) => (
        <FolderNode
          key={f.path}
          folder={f}
          sessionId={sessionId}
          depth={depth}
          showDeleted={showDeleted}
          current={current}
          expanded={expanded}
          onToggle={onToggle}
          onOpen={onOpen}
        />
      ))}
      {q.hasNextPage && (
        <button
          type="button"
          className="w-full cursor-pointer py-0.5 text-left text-muted-foreground hover:text-foreground"
          style={{ paddingLeft: 6 + depth * 12 + 16 }}
          disabled={q.isFetchingNextPage}
          onClick={() => void q.fetchNextPage()}
        >
          {q.isFetchingNextPage ? "…" : "more folders"}
        </button>
      )}
    </>
  )
}

function FolderNode({
  folder: f,
  sessionId,
  depth,
  showDeleted,
  current,
  expanded,
  onToggle,
  onOpen,
}: {
  folder: Entry
  sessionId: string
  depth: number
  showDeleted: boolean
  current: string | null
  expanded: readonly string[]
  onToggle: (path: string, open: boolean) => void
  onOpen: (path: string) => void
}) {
  const open = expanded.some((p) => samePath(p, f.path))
  const here = current !== null && samePath(current, f.path)
  const Icon = f.parent ? FolderIcon : HardDriveIcon
  return (
    <>
      <div
        className={cn(
          "flex cursor-pointer items-center gap-1 py-0.5 pr-2 hover:bg-muted/60",
          here && "bg-muted font-medium",
          f.goneAt && "text-muted-foreground line-through",
        )}
        style={{ paddingLeft: 6 + depth * 12 }}
        title={f.path}
        onClick={() => onOpen(f.path)}
      >
        <button
          type="button"
          aria-label={open ? `Close ${f.name}` : `Open ${f.name}`}
          className="cursor-pointer rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
          onClick={(e) => {
            e.stopPropagation()
            onToggle(f.path, !open)
          }}
        >
          <ChevronRightIcon className={cn("size-3 transition-transform", open && "rotate-90")} />
        </button>
        <Icon className={cn("size-3.5 shrink-0", f.parent ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground")} />
        <span className="font-mono whitespace-nowrap">{f.name}</span>
      </div>
      {open && (
        <Folders
          sessionId={sessionId}
          path={f.path}
          depth={depth + 1}
          showDeleted={showDeleted}
          current={current}
          expanded={expanded}
          onToggle={onToggle}
          onOpen={onOpen}
        />
      )}
    </>
  )
}
