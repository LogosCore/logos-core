import { useState } from "react"
import {
  ArrowUpIcon,
  ChevronRightIcon,
  CircleHelpIcon,
  CopyIcon,
  DownloadIcon,
  FileIcon,
  FolderIcon,
  FolderOpenIcon,
  Link2Icon,
  UploadIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatBytes } from "@/lib/format-bytes"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { childPath, crumbs, sortEntries, type DirEntry, type DirListing } from "./dir-listing"
import { ActionItem, type MenuCommand } from "./action-menu"
import type { UploadTarget } from "./upload-target"

// A dir_listing task's data as a file browser (docs:
// contracts/task-results-and-actions.md, View): the path as crumbs, up, and
// the entries, folders first. Up and a crumb list that folder with the
// listing's own command; an entry's actions are in its context menu (a
// right click): List directory on a folder or a link, Download on a file,
// Upload here on a folder (and for the folder listed, by the crumbs), with
// a submenu of commands when the session has more than one, and copying
// its path or name. onOpen is absent where the viewer may not
// give tasks, who then only reads what is listed.

const SHOWN = 500

const ICONS = { dir: FolderIcon, file: FileIcon, link: Link2Icon, other: CircleHelpIcon } as const

export function DirListingView({
  listing,
  onOpen,
  listCommands = [],
  onDownload,
  downloadCommands = [],
  onUploadHere,
  uploadCommands = [],
}: {
  listing: DirListing
  /** Lists a folder: with commandId, or the listing's own command. */
  onOpen?: (path: string, commandId?: string) => void
  /** The session's list_dir commands, the listing's own first. */
  listCommands?: readonly MenuCommand[]
  /** Downloads a file with a download_file command. */
  onDownload?: (path: string, commandId: string) => void
  /** The session's download_file commands. */
  downloadCommands?: readonly MenuCommand[]
  /** Uploads a file to a folder with an upload_file command. */
  onUploadHere?: (target: UploadTarget, commandId: string) => void
  /** The session's upload_file commands. */
  uploadCommands?: readonly MenuCommand[]
}) {
  const [all, setAll] = useState(false)
  // The entry whose menu is open, marked while it is.
  const [menuFor, setMenuFor] = useState<string | null>(null)
  const sorted = sortEntries(listing.entries)
  const shown = all ? sorted : sorted.slice(0, SHOWN)
  const path = crumbs(listing.path, listing.sep)
  const lists = (e: DirEntry) => !!onOpen && (e.type === "dir" || e.type === "link")
  const uploads = !!onUploadHere && uploadCommands.length > 0

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b px-2 py-1">
        <Button
          variant="ghost"
          size="icon-xs"
          title={listing.parent ? `Up: ${listing.parent}` : "At the root"}
          aria-label="Up"
          disabled={!onOpen || !listing.parent}
          onClick={() => listing.parent && onOpen?.(listing.parent)}
        >
          <ArrowUpIcon />
        </Button>
        <nav aria-label="Path" className="flex min-w-0 flex-1 items-center overflow-x-auto font-mono text-xs [scrollbar-width:thin]">
          {path.map((c, i) => (
            <span key={c.path} className="flex shrink-0 items-center">
              {i > 1 && <ChevronRightIcon className="size-3 text-muted-foreground" />}
              {i === path.length - 1 || !onOpen ? (
                <span className={cn("px-1 py-0.5", i === path.length - 1 && "font-semibold")}>{c.label}</span>
              ) : (
                <button type="button" className="cursor-pointer rounded px-1 py-0.5 hover:bg-muted" title={`Open ${c.path}`} onClick={() => onOpen(c.path)}>
                  {c.label}
                </button>
              )}
            </span>
          ))}
        </nav>
        <span className="shrink-0 text-xs text-muted-foreground">
          {listing.entries.length} {listing.entries.length === 1 ? "entry" : "entries"}
        </span>
        {uploads && (
          <Button
            variant="ghost"
            size="icon-xs"
            title={`Upload a file to ${listing.path} (${uploadCommands[0].id})`}
            aria-label="Upload here"
            onClick={() =>
              onUploadHere!({ folder: listing.path, sep: listing.sep, names: listing.entries.map((e) => e.name) }, uploadCommands[0].id)
            }
          >
            <UploadIcon />
          </Button>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <table className="w-full text-xs">
          <tbody>
            {shown.map((e) => {
              const Icon = ICONS[e.type]
              const path = childPath(listing, e.name)
              return (
                <ContextMenu key={e.name} onOpenChange={(open) => setMenuFor(open ? e.name : null)}>
                  <ContextMenuTrigger
                    render={
                      <tr
                        className={cn(
                          "border-b last:border-b-0 hover:bg-muted/40",
                          e.hidden && "text-muted-foreground",
                          menuFor === e.name && "bg-muted",
                        )}
                      />
                    }
                  >
                    <td className="w-full max-w-0 truncate px-3 py-1 font-mono">
                      <span className="inline-flex max-w-full items-baseline gap-1.5">
                        <Icon
                          className={cn(
                            "size-3.5 shrink-0 self-center",
                            e.type === "dir" ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground",
                          )}
                        />
                        <span className="truncate" title={e.name}>
                          {e.name}
                        </span>
                        {e.target && <span className="truncate text-muted-foreground" title={e.target}>→ {e.target}</span>}
                      </span>
                    </td>
                    <td className="px-3 py-1 text-right whitespace-nowrap text-muted-foreground tabular-nums">
                      {e.type === "file" && e.size !== undefined ? formatBytes(e.size) : ""}
                    </td>
                    <td className="px-3 py-1 whitespace-nowrap text-muted-foreground tabular-nums">
                      {e.modified !== undefined ? formatDateTime(e.modified * 1000) : ""}
                    </td>
                    <td className="px-3 py-1 font-mono whitespace-nowrap text-muted-foreground">{e.mode}</td>
                    <td className="px-3 py-1 whitespace-nowrap text-muted-foreground">{[e.owner, e.group].filter(Boolean).join(":")}</td>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    {lists(e) && (
                      <>
                        <ActionItem
                          label="List directory"
                          icon={<FolderOpenIcon />}
                          commands={listCommands}
                          onRun={(id) => onOpen!(path, id)}
                        />
                        {listCommands.length > 0 && <ContextMenuSeparator />}
                      </>
                    )}
                    {uploads && e.type === "dir" && (
                      <>
                        <ActionItem
                          label="Upload here…"
                          icon={<UploadIcon />}
                          commands={uploadCommands}
                          onRun={(id) => onUploadHere!({ folder: path, sep: listing.sep }, id)}
                        />
                        <ContextMenuSeparator />
                      </>
                    )}
                    {onDownload && e.type === "file" && downloadCommands.length > 0 && (
                      <>
                        <ActionItem label="Download" icon={<DownloadIcon />} commands={downloadCommands} onRun={(id) => onDownload(path, id)} />
                        <ContextMenuSeparator />
                      </>
                    )}
                    <ContextMenuItem onClick={() => void copyToClipboard(path, "path")}>
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
            })}
          </tbody>
        </table>
        {listing.entries.length === 0 && <p className="px-3 py-2 text-xs text-muted-foreground">The directory is empty.</p>}
        {!all && sorted.length > shown.length && (
          <button type="button" className="w-full cursor-pointer border-t px-3 py-1 text-xs text-muted-foreground hover:bg-muted/40" onClick={() => setAll(true)}>
            Show all {sorted.length}
          </button>
        )}
      </div>
    </div>
  )
}
