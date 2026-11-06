import { FolderIcon, LayersIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core"
import { SortableContext, arrayMove, horizontalListSortingStrategy, useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DocumentIcon } from "@/components/wiki/document-icon"
import type { MinionSessionFolderFieldsFragment } from "@/graphql/gql/graphql"
import { cn } from "@/lib/utils"

// The Sessions table's folders, as tabs above it: All first (every session),
// then the operation's folders in their order. A click shows one; an
// operator makes one (+), changes or deletes one (its context menu) and
// reorders them by dragging.

type Folder = MinionSessionFolderFieldsFragment

/** A folder's icon: its own (icon or emoji, in its color), else a folder. */
export function FolderGlyph({ folder, size = 14 }: { folder: Pick<Folder, "icon" | "emoji" | "color">; size?: number }) {
  if (!folder.icon && !folder.emoji) {
    return <FolderIcon className="shrink-0" style={{ width: size, height: size, ...(folder.color ? { color: folder.color } : {}) }} aria-hidden />
  }
  return <DocumentIcon emoji={folder.emoji ?? ""} icon={folder.icon ?? ""} color={folder.color ?? ""} size={size} />
}

export function SessionFolderTabs({
  folders,
  current,
  onSelect,
  canWrite,
  onNew,
  onEdit,
  onDelete,
  onMove,
  allCount,
}: {
  /** All's count: the operation's sessions, not archived. */
  allCount?: number
  folders: readonly Folder[]
  /** The folder shown; null: All. */
  current: string | null
  onSelect: (folderId: string | null) => void
  canWrite: boolean
  onNew: () => void
  onEdit: (folder: Folder) => void
  onDelete: (folder: Folder) => void
  /** A folder dragged: before another, or last (null). */
  onMove: (id: string, before: string | null) => void
}) {
  // A drag only past 6px: a click shows the folder.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }))
  const ids = folders.map((f) => f.id)
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return
    const next = arrayMove(ids, ids.indexOf(String(active.id)), ids.indexOf(String(over.id)))
    const at = next.indexOf(String(active.id))
    onMove(String(active.id), next[at + 1] ?? null)
  }
  return (
    // Its bottom line drawn inside it (an inset shadow), under the tabs'
    // underlines: a tab hanging 1px over a border made it overflow down, and
    // where scrollbars take room a 1px vertical one showed.
    <div
      role="tablist"
      aria-label="Folders"
      className="flex shrink-0 items-stretch gap-0.5 overflow-x-auto overflow-y-hidden px-2 shadow-[inset_0_-1px_0_var(--color-border)] [scrollbar-width:thin]"
    >
      <FolderTab active={current === null} onClick={() => onSelect(null)} title="Every session of the operation">
        <LayersIcon className="size-3.5 shrink-0" aria-hidden />
        All
        {allCount !== undefined && <Count n={allCount} />}
      </FolderTab>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={ids} strategy={horizontalListSortingStrategy}>
          {folders.map((f) => (
            <SortableFolder
              key={f.id}
              folder={f}
              active={current === f.id}
              canWrite={canWrite}
              onClick={() => onSelect(f.id)}
              onEdit={() => onEdit(f)}
              onDelete={() => onDelete(f)}
            />
          ))}
        </SortableContext>
      </DndContext>
      {canWrite && (
        <Button variant="ghost" size="icon-xs" className="my-auto ml-0.5 shrink-0" aria-label="New folder" title="New folder" onClick={onNew}>
          <PlusIcon />
        </Button>
      )}
    </div>
  )
}

function FolderTab({
  active,
  onClick,
  title,
  children,
  className,
}: {
  active: boolean
  onClick: () => void
  title?: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      title={title}
      onClick={onClick}
      className={cn(
        "flex shrink-0 cursor-pointer items-center gap-1.5 border-b-2 px-2.5 py-1.5 text-sm whitespace-nowrap transition-colors",
        active ? "border-primary font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
        className,
      )}
    >
      {children}
    </button>
  )
}

function SortableFolder({
  folder,
  active,
  canWrite,
  onClick,
  onEdit,
  onDelete,
}: {
  folder: Folder
  active: boolean
  canWrite: boolean
  onClick: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: folder.id, disabled: !canWrite })
  return (
    <ContextMenu>
      <ContextMenuTrigger
        render={
          <div
            ref={setNodeRef}
            style={{ transform: CSS.Translate.toString(transform), transition }}
            className={cn("flex", isDragging && "z-10 opacity-80")}
            {...attributes}
            {...listeners}
          />
        }
      >
        <FolderTab active={active} onClick={onClick} title={`${folder.name}: ${folder.sessions} ${folder.sessions === 1 ? "session" : "sessions"}, not archived`}>
          <FolderGlyph folder={folder} />
          <span className="max-w-[16ch] truncate">{folder.name}</span>
          <Count n={folder.sessions} />
        </FolderTab>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem disabled={!canWrite} onClick={onEdit}>
          <PencilIcon />
          Edit folder…
        </ContextMenuItem>
        <ContextMenuItem disabled={!canWrite} variant="destructive" onClick={onDelete}>
          <Trash2Icon />
          Delete folder…
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

/** A tab's count of sessions, not archived. */
function Count({ n }: { n: number }) {
  return <span className="rounded-full bg-muted px-1.5 text-xs leading-4 font-normal text-muted-foreground tabular-nums">{n}</span>
}
