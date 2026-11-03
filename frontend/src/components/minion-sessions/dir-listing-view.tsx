import { useMemo, useState } from "react";
import { TableVirtuoso, type TableComponents } from "react-virtuoso";
import {
  ArrowUpIcon,
  ChevronRightIcon,
  CircleHelpIcon,
  CopyIcon,
  DownloadIcon,
  FileIcon,
  FolderIcon,
  FolderOpenIcon,
  FolderTreeIcon,
  ListTreeIcon,
  Link2Icon,
  RadioTowerIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { copyToClipboard } from "@/lib/copy-to-clipboard";
import { formatBytes } from "@/lib/format-bytes";
import { formatDateTime } from "@/lib/date-format";
import { cn } from "@/lib/utils";
import {
  childPath,
  crumbs,
  sortEntries,
  type DirEntry,
  type DirListing,
} from "./dir-listing";
import { FileNameIcon } from "@/components/file-preview/file-type-icon";
import { ActionItem, type MenuCommand } from "./action-menu";
import { UploadHereButton } from "./upload-here-button";
import type { UploadTarget } from "./upload-target";
import type { DeleteTarget } from "./delete-dialog";

// A dir_listing task's data as a file browser (docs:
// contracts/task-results-and-actions.md, View): the path as crumbs, up, and
// the entries, folders first. Up and a crumb list that folder with the
// listing's own command; an entry's actions are in its context menu (a
// right click): List directory on a folder or a link, Walk from here on a
// folder, Download and Stream on a file, Upload here on a folder (and for the folder listed, by the
// crumbs), Delete on any (after a confirmation), with a submenu of commands
// when the session has more than one, and copying its path or name. onOpen is absent where the viewer may not
// give tasks, who then only reads what is listed. Open in File Browser shows
// the folder in the machine's file browser (docs: contracts/file-browser.md).

const ICONS = {
  dir: FolderIcon,
  file: FileIcon,
  link: Link2Icon,
  other: CircleHelpIcon,
} as const;

export function DirListingView({
  listing,
  onOpen,
  listCommands = [],
  onDownload,
  downloadCommands = [],
  onUploadHere,
  uploadCommands = [],
  onStream,
  streamCommands = [],
  onDelete,
  deleteCommands = [],
  onWalk,
  walkCommands = [],
  onOpenInFileBrowser,
}: {
  listing: DirListing;
  /** Lists a folder: with commandId, or the listing's own command. */
  onOpen?: (path: string, commandId?: string) => void;
  /** The session's list_dir commands, the listing's own first. */
  listCommands?: readonly MenuCommand[];
  /** Downloads a file with a download_file command. */
  onDownload?: (path: string, commandId: string) => void;
  /** The session's download_file commands. */
  downloadCommands?: readonly MenuCommand[];
  /** Uploads a file to a folder with an upload_file command. */
  onUploadHere?: (target: UploadTarget, commandId: string) => void;
  /** The session's upload_file commands. */
  uploadCommands?: readonly MenuCommand[];
  /** Streams a file with a stream_file command. */
  onStream?: (path: string, commandId: string) => void;
  /** The session's stream_file commands. */
  streamCommands?: readonly MenuCommand[];
  /** Deletes an entry with a delete_path command (after a confirmation). */
  onDelete?: (target: DeleteTarget, commandId: string) => void;
  /** The session's delete_path commands. */
  deleteCommands?: readonly MenuCommand[];
  /** Walks a folder and those under it with a walk_dir command. */
  onWalk?: (path: string, commandId: string) => void;
  /** The session's walk_dir commands. */
  walkCommands?: readonly MenuCommand[];
  /** Opens the folder in the machine's file browser. */
  onOpenInFileBrowser?: (path: string) => void;
}) {
  // The entry whose menu is open, marked while it is.
  const [menuFor, setMenuFor] = useState<string | null>(null);
  // Sorted once per listing, not per render.
  const sorted = useMemo(() => sortEntries(listing.entries), [listing.entries]);
  const path = crumbs(listing.path, listing.sep);
  const uploads = !!onUploadHere && uploadCommands.length > 0;
  // What a row and its menu need, through the table's context: the row
  // components stay the same from render to render.
  const context: RowContext = {
    listing,
    menuFor,
    setMenuFor,
    onOpen,
    listCommands,
    onDownload,
    downloadCommands,
    onUploadHere,
    uploadCommands,
    uploads,
    onStream,
    streamCommands,
    onDelete,
    deleteCommands,
    onWalk,
    walkCommands,
  };

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
        <nav
          aria-label="Path"
          className="flex min-w-0 flex-1 items-center overflow-x-auto font-mono text-xs [scrollbar-width:thin]"
        >
          {path.map((c, i) => (
            <span key={c.path} className="flex shrink-0 items-center">
              {i > 1 && (
                <ChevronRightIcon className="size-3 text-muted-foreground" />
              )}
              {i === path.length - 1 || !onOpen ? (
                <span
                  className={cn(
                    "px-1 py-0.5",
                    i === path.length - 1 && "font-semibold",
                  )}
                >
                  {c.label}
                </span>
              ) : (
                <button
                  type="button"
                  className="cursor-pointer rounded px-1 py-0.5 hover:bg-muted"
                  title={`Open ${c.path}`}
                  onClick={() => onOpen(c.path)}
                >
                  {c.label}
                </button>
              )}
            </span>
          ))}
        </nav>
        <span className="shrink-0 text-xs text-muted-foreground">
          {listing.entries.length}{" "}
          {listing.entries.length === 1 ? "entry" : "entries"}
        </span>
        {onOpenInFileBrowser && (
          <Button
            variant="ghost"
            size="icon-xs"
            title="Open in File Browser: the machine's files, as its sessions have seen them"
            aria-label="Open in File Browser"
            onClick={() => onOpenInFileBrowser(listing.path)}
          >
            <FolderTreeIcon />
          </Button>
        )}
        {onUploadHere && (
          <UploadHereButton
            folder={listing.path}
            commands={uploadCommands}
            onRun={(id) =>
              onUploadHere(
                {
                  folder: listing.path,
                  sep: listing.sep,
                  names: listing.entries.map((e) => e.name),
                },
                id,
              )
            }
          />
        )}
      </div>
      {listing.entries.length === 0 ? (
        <p className="px-3 py-2 text-sm text-muted-foreground">
          The directory is empty.
        </p>
      ) : (
        // Only the rows in view render: a listing of thousands (C:\Windows)
        // is as quick as one of ten.
        <TableVirtuoso
          className="min-h-0 flex-1"
          style={{ height: "100%" }}
          data={sorted}
          context={context}
          components={LISTING_TABLE}
          computeItemKey={(_, e) => e.name}
          itemContent={(_, e) => <EntryCells e={e} />}
        />
      )}
    </div>
  );
}

/** What a listing's rows and their menus read: the listing, its handlers, the entry whose menu is open. */
interface RowContext {
  listing: DirListing;
  menuFor: string | null;
  setMenuFor: (name: string | null) => void;
  onOpen?: (path: string, commandId?: string) => void;
  listCommands: readonly MenuCommand[];
  onDownload?: (path: string, commandId: string) => void;
  downloadCommands: readonly MenuCommand[];
  onUploadHere?: (target: UploadTarget, commandId: string) => void;
  uploadCommands: readonly MenuCommand[];
  uploads: boolean;
  onStream?: (path: string, commandId: string) => void;
  streamCommands: readonly MenuCommand[];
  onDelete?: (target: DeleteTarget, commandId: string) => void;
  deleteCommands: readonly MenuCommand[];
  onWalk?: (path: string, commandId: string) => void;
  walkCommands: readonly MenuCommand[];
}

// The table's parts, made once: a row is its entry's context menu trigger.
const LISTING_TABLE: TableComponents<DirEntry, RowContext> = {
  Table: (props) => <table {...props} className="w-full text-sm" />,
  TableRow: ({ item: e, context, children, ...props }) => (
    <ContextMenu
      onOpenChange={(open) => context.setMenuFor(open ? e.name : null)}
    >
      <ContextMenuTrigger
        render={
          <tr
            {...props}
            className={cn(
              "border-b last:border-b-0 hover:bg-muted/40",
              e.hidden && "text-muted-foreground",
              context.menuFor === e.name && "bg-muted",
            )}
          />
        }
      >
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent>
        <EntryMenu e={e} c={context} />
      </ContextMenuContent>
    </ContextMenu>
  ),
};

/** An entry's cells: its name (with its kind's icon, a link's target), size, modified, mode, owner. */
function EntryCells({ e }: { e: DirEntry }) {
  const Icon = ICONS[e.type];
  return (
    <>
      <td className="w-full max-w-0 truncate px-3 py-1">
        <span className="inline-flex max-w-full items-baseline gap-1.5">
          {/* A file's icon says its kind, by its name. */}
          {e.type === "file" ? (
            <FileNameIcon
              name={e.name}
              className="size-3.5 shrink-0 self-center text-muted-foreground"
            />
          ) : (
            <Icon
              className={cn(
                "size-3.5 shrink-0 self-center",
                e.type === "dir"
                  ? "text-amber-600 dark:text-amber-400"
                  : "text-muted-foreground",
              )}
            />
          )}
          <span className="truncate" title={e.name}>
            {e.name}
          </span>
          {e.target && (
            <span className="truncate text-muted-foreground" title={e.target}>
              → {e.target}
            </span>
          )}
        </span>
      </td>
      <td className="px-3 py-1 text-right whitespace-nowrap text-muted-foreground tabular-nums">
        {e.type === "file" && e.size !== undefined ? formatBytes(e.size) : ""}
      </td>
      <td className="px-3 py-1 whitespace-nowrap text-muted-foreground tabular-nums">
        {e.modified !== undefined ? formatDateTime(e.modified * 1000) : ""}
      </td>
      <td className="px-3 py-1 font-mono text-xs whitespace-nowrap text-muted-foreground">
        {e.mode}
      </td>
      <td className="px-3 py-1 whitespace-nowrap text-muted-foreground">
        {[e.owner, e.group].filter(Boolean).join(":")}
      </td>
    </>
  );
}

/** An entry's menu: what the session's commands can do with it, and copying its path or name. */
function EntryMenu({ e, c }: { e: DirEntry; c: RowContext }) {
  const {
    listing,
    onOpen,
    listCommands,
    onDownload,
    downloadCommands,
    onUploadHere,
    uploadCommands,
    uploads,
    onStream,
    streamCommands,
    onDelete,
    deleteCommands,
    onWalk,
    walkCommands,
  } = c;
  const path = childPath(listing, e.name);
  const lists = !!onOpen && (e.type === "dir" || e.type === "link");
  return (
    <>
      {lists && (
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
      {onWalk && e.type === "dir" && walkCommands.length > 0 && (
        <>
          <ActionItem
            label="Walk from here"
            icon={<ListTreeIcon />}
            commands={walkCommands}
            onRun={(id) => onWalk(path, id)}
          />
          <ContextMenuSeparator />
        </>
      )}
      {uploads && e.type === "dir" && (
        <>
          <ActionItem
            label="Upload here…"
            icon={<UploadIcon />}
            commands={uploadCommands}
            onRun={(id) =>
              onUploadHere!({ folder: path, sep: listing.sep }, id)
            }
          />
          <ContextMenuSeparator />
        </>
      )}
      {onDownload && e.type === "file" && downloadCommands.length > 0 && (
        <>
          <ActionItem
            label="Download"
            icon={<DownloadIcon />}
            commands={downloadCommands}
            onRun={(id) => onDownload(path, id)}
          />
          {!(onStream && streamCommands.length > 0) && <ContextMenuSeparator />}
        </>
      )}
      {onStream && e.type === "file" && streamCommands.length > 0 && (
        <>
          <ActionItem
            label="Stream"
            icon={<RadioTowerIcon />}
            commands={streamCommands}
            onRun={(id) => onStream(path, id)}
          />
          <ContextMenuSeparator />
        </>
      )}
      {onDelete && deleteCommands.length > 0 && (
        <>
          <ActionItem
            label="Delete…"
            icon={<Trash2Icon />}
            variant="destructive"
            commands={deleteCommands}
            onRun={(id) => onDelete({ path, name: e.name, type: e.type }, id)}
          />
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
    </>
  );
}
