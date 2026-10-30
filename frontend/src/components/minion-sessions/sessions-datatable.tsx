import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { Virtuoso } from "react-virtuoso"
import {
  DndContext,
  PointerSensor,
  KeyboardSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core"
import {
  SortableContext,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ArrowDownIcon,
  ArrowLeftIcon,
  ArrowRightIcon,
  ArrowUpIcon,
  Columns3Icon,
  EyeOffIcon,
  FilterIcon,
  FilterXIcon,
  CopyIcon,
  ListRestartIcon,
  ListChecksIcon,
  LoaderIcon,
  PencilIcon,
  PinIcon,
  TagIcon,
  XIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  ContextMenu,
  ContextMenuCheckboxItem,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ContextMenuRow } from "@/components/ui/context-menu-row"
import { Popover, PopoverContent } from "@/components/ui/popover"
import { Skeleton } from "@/components/ui/skeleton"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { PLACEHOLDER_DELAY_MS, useDelayedFlag } from "@/hooks/use-delayed-flag"
import { useNow } from "@/hooks/use-now"
import { cn } from "@/lib/utils"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { useRecordModals } from "@/hooks/use-record-modals"
import { useFactoryKinds } from "@/graphql/hooks/builds"
import { factoryKindsByName } from "@/components/builds/build-values"
import {
  useSetMinionSessionAddress,
  useSetMinionSessionArchived,
  useSetMinionSessionsArchived,
  useSetMinionSessionMark,
  useSetMinionSessionsMark,
} from "@/graphql/hooks/minion-sessions"
import { Checkbox } from "@/components/ui/checkbox"
import { errorMessage } from "@/components/modules/management/api-errors"
import { toast } from "sonner"
import {
  emptySelection,
  selectLoaded,
  selectedIds,
  selectionIn,
  toggleSelected,
  type SessionSelection,
} from "./session-selection"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { SessionCell } from "./session-cells"
import { SessionFilterEditor } from "./session-filter-editor"
import { SessionDescriptionDialog } from "./session-description-dialog"
import { SessionMarkDialog, type SessionMarkTarget } from "./session-mark-dialog"
import { MARKED_ROW_CLASSES, markColorVars } from "./session-mark-colors"
import {
  DEFAULT_SESSIONS_LAYOUT,
  addressMatcher,
  SESSION_COLUMNS,
  SESSION_COLUMN_BY_ID,
  cycleSort,
  describeFilter,
  filterOf,
  moveColumn,
  setFilter,
  setSort,
  shiftColumn,
  toggleColumn,
  type SessionColumnDef,
  type SessionColumnId,
  type SessionsLayout,
} from "./sessions-layout"

// The Sessions table: the operators' main view. Unlike the app's other
// tables, its columns are the operator's: any property of a session, in any
// order (drag a header, or Move left / right), filtered and sorted on any
// column, all from the header: a click sorts, the funnel filters, and a right
// click (or the columns button at the end) opens the rest. Core filters and
// sorts; the layout is saved on the operator (useMinionSessionsLayout).

const SKELETON_ROWS = 5
const MENU_TRACK = "2.25rem"
const CHECK_TRACK = "1.25rem"

/** Where a column's filter editor is open: the column, and the element it hangs from. */
interface FilterTarget {
  column: SessionColumnId
  anchor: HTMLElement
}

export function SessionsDatatable({
  operationId,
  canWrite,
  archived,
  onArchivedChange,
  layout,
  onLayoutChange,
  sessions,
  icons,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  total,
  emptyState,
}: {
  operationId: string
  /** How many sessions the list matches, every page: for "select all matching". */
  total: number
  /** Whether the viewer may change sessions (pick an address, archive). */
  canWrite: boolean
  /** The archived sessions are listed instead of the others. */
  archived: boolean
  onArchivedChange: (archived: boolean) => void
  layout: SessionsLayout
  onLayoutChange: (layout: SessionsLayout | null) => void
  sessions: MinionSessionFieldsFragment[]
  icons: ReadonlyMap<string, ModuleIconSvgs>
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  emptyState: ReactNode
}) {
  // Ticks the relative times every second: "Last seen" counts a silent
  // minion's seconds, and turns late on time.
  const now = useNow(1_000)
  const [filterTarget, setFilterTarget] = useState<FilterTarget | null>(null)
  // The session whose description is being edited.
  const [describing, setDescribing] = useState<MinionSessionFieldsFragment | null>(null)
  // What the mark dialog marks: a session, or the selection.
  const [marking, setMarking] = useState<SessionMarkTarget | null>(null)
  const { mutateAsync: markOne } = useSetMinionSessionMark()
  const { mutateAsync: markMany } = useSetMinionSessionsMark()
  // The column a right click landed on; null: the header past the columns.
  const [menuColumn, setMenuColumn] = useState<SessionColumnId | null>(null)
  const cells = useRef(new Map<SessionColumnId, HTMLElement>())
  const setAddress = useSetMinionSessionAddress()
  // Whose minions can claim a persisted session: the factory says.
  const kindsQuery = useFactoryKinds()
  const persistable = useMemo(() => {
    const kinds = factoryKindsByName(kindsQuery.data?.factoryKinds ?? [])
    return (s: MinionSessionFieldsFragment) => kinds.get(s.moduleName)?.persistedSessions === true
  }, [kindsQuery.data])
  const matchAddress = useMemo(() => addressMatcher(layout.filters, null), [layout.filters])
  const { mutate: pick } = setAddress
  const ctx = useMemo(
    () => ({
      icons,
      now,
      matchAddress,
      pickAddress: canWrite
        ? (s: MinionSessionFieldsFragment, address: string) => pick({ id: s.id, address })
        : undefined,
    }),
    [icons, now, matchAddress, canWrite, pick],
  )

  // The selection for bulk actions, made under this view and these filters
  // (session-selection.ts): a change to either empties it.
  const scope = JSON.stringify([archived, layout.filters])
  const [selectionState, setSelection] = useState<SessionSelection>(() => emptySelection(scope))
  const selection = selectionIn(selectionState, scope)
  const order = useMemo(() => sessions.map((s) => s.id), [sessions])
  const picked = selectedIds(selection, order)
  const pickedSet = new Set(picked)
  const selecting = selection.all || picked.length > 0
  const clearSelection = () => setSelection(emptySelection(scope))
  // Esc leaves selecting.
  useEffect(() => {
    if (!selecting) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setSelection(emptySelection(scope))
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [selecting, scope])

  const columns = useMemo(() => layout.columns.map((id) => SESSION_COLUMN_BY_ID.get(id)!), [layout.columns])
  const template = [...(selecting ? [CHECK_TRACK] : []), ...columns.map((c) => c.width), MENU_TRACK].join(" ")
  const grid = { gridTemplateColumns: template }

  const sensors = useSensors(
    // A click sorts; only a drag past 6px moves the column.
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (over && active.id !== over.id) {
      onLayoutChange(moveColumn(layout, active.id as SessionColumnId, over.id as SessionColumnId))
    }
  }

  const openFilter = (column: SessionColumnId, anchor?: HTMLElement | null) => {
    const el = anchor ?? cells.current.get(column)
    if (el) setFilterTarget({ column, anchor: el })
  }

  const slowLoad = useDelayedFlag(isLoading, PLACEHOLDER_DELAY_MS)
  const showEmpty = !isLoading && sessions.length === 0
  const showList = !isLoading && sessions.length > 0

  const footer = useMemo(
    () => ({
      Footer: () => {
        if (isFetchingNextPage) {
          return (
            <div className="flex items-center justify-center py-4">
              <LoaderIcon className="size-4 animate-spin" />
            </div>
          )
        }
        if (!hasNextPage && sessions.length > 0) {
          return (
            <div className="flex items-center justify-center py-4 text-sm text-muted-foreground">
              No more sessions to load
            </div>
          )
        }
        return null
      },
    }),
    [isFetchingNextPage, hasNextPage, sessions.length],
  )

  const filterColumn = filterTarget ? SESSION_COLUMN_BY_ID.get(filterTarget.column)! : null

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <FilterChips
        selectionBar={
          selecting ? (
            <SelectionBar
              operationId={operationId}
              archived={archived}
              canWrite={canWrite}
              selection={selection}
              picked={picked}
              loaded={order.length}
              total={total}
              filters={layout.filters}
              onSelectAll={() => setSelection({ ...selection, all: true, anchor: null })}
              onMark={() => {
                const n = selection.all ? total : picked.length
                setMarking({
                  title: `${n} ${n === 1 ? "session" : "sessions"}`,
                  mark: null,
                  many: true,
                  save: async (mark) => {
                    await markMany({
                      operationId,
                      mark,
                      ...(selection.all ? { matching: { archived, filters: layout.filters } } : { ids: picked }),
                    })
                    clearSelection()
                  },
                })
              }}
              onDone={clearSelection}
            />
          ) : null
        }
        archived={archived}
        onShowLive={() => onArchivedChange(false)}
        layout={layout}
        onEdit={(column, anchor) => openFilter(column, anchor)}
        onRemove={(column) => onLayoutChange(setFilter(layout, column, null))}
        onClearAll={() => onLayoutChange({ ...layout, filters: [] })}
      />
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border bg-card">
        <ContextMenu>
          <ContextMenuTrigger
            // A right click past the cells is about the table, not a column.
            onContextMenu={(e) => {
              if (e.target === e.currentTarget) setMenuColumn(null)
            }}
            className="shrink-0 border-b bg-muted/50"
            render={<div />}
          >
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
              <SortableContext items={layout.columns} strategy={horizontalListSortingStrategy}>
                <div
                  className="grid gap-3 px-4 py-1.5 text-xs font-medium tracking-wide text-muted-foreground uppercase"
                  style={grid}
                  onContextMenu={(e) => {
                    if (e.target === e.currentTarget) setMenuColumn(null)
                  }}
                >
                  {selecting && (
                    <Checkbox
                      aria-label="Select every loaded session"
                      className="self-center"
                      checked={selection.all || (order.length > 0 && picked.length === order.length)}
                      indeterminate={!selection.all && picked.length > 0 && picked.length < order.length}
                      onCheckedChange={(on) => setSelection(selectLoaded(selection, order, on))}
                    />
                  )}
                  {columns.map((col) => (
                    <HeaderCell
                      key={col.id}
                      column={col}
                      layout={layout}
                      cellRef={(el) => {
                        if (el) cells.current.set(col.id, el)
                        else cells.current.delete(col.id)
                      }}
                      onSort={() => onLayoutChange(cycleSort(layout, col.id))}
                      onFilter={() => openFilter(col.id)}
                      onContextMenu={() => setMenuColumn(col.id)}
                    />
                  ))}
                  <ColumnsButton
                    layout={layout}
                    onLayoutChange={onLayoutChange}
                    archived={archived}
                    onArchivedChange={onArchivedChange}
                  />
                </div>
              </SortableContext>
            </DndContext>
          </ContextMenuTrigger>
          <HeaderMenu
            column={menuColumn}
            archived={archived}
            onArchivedChange={onArchivedChange}
            layout={layout}
            onLayoutChange={onLayoutChange}
            onFilter={(column) => openFilter(column)}
          />
        </ContextMenu>

        {slowLoad && (
          <div className="space-y-3 p-4 animate-in duration-200 fade-in-0">
            {Array.from({ length: SKELETON_ROWS }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        )}
        {showEmpty && (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 py-16 text-muted-foreground">
            {emptyState}
          </div>
        )}
        {showList && (
          <div className="flex min-h-0 flex-1 flex-col">
            <Virtuoso
              data={sessions}
              endReached={() => {
                if (hasNextPage && !isFetchingNextPage) fetchNextPage()
              }}
              overscan={200}
              style={{ height: "100%" }}
              className="min-h-0 flex-1"
              components={footer}
              itemContent={(_, s) => (
                <ContextMenuRow
                  gridCols=""
                  // The caller's mark tints the row; a picked row is outlined,
                  // so it shows marked and picked at once.
                  style={{ ...grid, ...markColorVars(s.mark?.color) }}
                  // While selecting, a drag or shift-click does not select text.
                  className={cn(
                    selecting && "select-none",
                    s.mark?.color && MARKED_ROW_CLASSES,
                    pickedSet.has(s.id) && "ring-2 ring-primary/60 ring-inset",
                  )}
                  // While selecting, a click picks the row (shift: a range).
                  onOpen={selecting ? (e) => setSelection(toggleSelected(selection, order, s.id, e.shiftKey)) : undefined}
                  menuLabel="Session actions"
                  menuButtonSize="icon-xs"
                  menu={
                    <SessionMenu
                      session={s}
                      canWrite={canWrite}
                      persistable={persistable(s)}
                      onSelect={() => setSelection(toggleSelected(selection, order, s.id, false))}
                      onEditDescription={() => setDescribing(s)}
                      onMark={() =>
                        setMarking({
                          title: `session #${s.number}`,
                          mark: s.mark ?? null,
                          save: async (mark) => {
                            await markOne({ id: s.id, mark })
                          },
                        })
                      }
                      selected={pickedSet.has(s.id)}
                    />
                  }
                >
                  {(menuButton) => (
                    <>
                      {selecting && (
                        <Checkbox
                          aria-label={`Select session ${s.number}`}
                          className="pointer-events-none self-center"
                          checked={pickedSet.has(s.id)}
                          tabIndex={-1}
                        />
                      )}
                      {columns.map((col) => (
                        <div key={col.id} className="min-w-0">
                          <SessionCell id={col.id} session={s} ctx={ctx} />
                        </div>
                      ))}
                      <div className="justify-self-end">{menuButton}</div>
                    </>
                  )}
                </ContextMenuRow>
              )}
            />
          </div>
        )}
      </div>

      <SessionDescriptionDialog session={describing} onClose={() => setDescribing(null)} />
      <SessionMarkDialog target={marking} onClose={() => setMarking(null)} />
      <Popover open={filterTarget !== null} onOpenChange={(open) => !open && setFilterTarget(null)}>
        <PopoverContent anchor={filterTarget?.anchor} align="start" className="w-72">
          {filterTarget && filterColumn && (
            <SessionFilterEditor
              key={filterTarget.column}
              column={filterColumn}
              operationId={operationId}
              initial={filterOf(layout, filterTarget.column)}
              onApply={(f) => {
                onLayoutChange(setFilter(layout, filterTarget.column, f))
                setFilterTarget(null)
              }}
              onClear={() => {
                onLayoutChange(setFilter(layout, filterTarget.column, null))
                setFilterTarget(null)
              }}
            />
          )}
        </PopoverContent>
      </Popover>
    </div>
  )
}

/**
 * One header cell: drag it to move the column, click its label to sort, the
 * funnel to filter (shown on hover, and always while the column is filtered).
 */
function HeaderCell({
  column,
  layout,
  cellRef,
  onSort,
  onFilter,
  onContextMenu,
}: {
  column: SessionColumnDef
  layout: SessionsLayout
  cellRef: (el: HTMLElement | null) => void
  onSort: () => void
  onFilter: () => void
  onContextMenu: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: column.id })
  const sorted = layout.sort?.column === column.id ? layout.sort.direction : null
  const filtered = filterOf(layout, column.id) !== undefined
  return (
    <div
      ref={(el) => {
        setNodeRef(el)
        cellRef(el)
      }}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        "group/header -mx-1 flex min-w-0 cursor-grab items-center gap-0.5 rounded px-1 py-0.5 select-none hover:bg-muted active:cursor-grabbing",
        isDragging && "z-10 bg-muted shadow-sm",
        (sorted || filtered) && "text-foreground",
      )}
      onContextMenu={onContextMenu}
      {...attributes}
      {...listeners}
    >
      <button
        type="button"
        className={cn("flex min-w-0 items-center gap-1 uppercase", column.sortable ? "cursor-pointer" : "cursor-grab")}
        onClick={column.sortable ? onSort : undefined}
        title={column.sortable ? `Sort by ${column.label}` : column.label}
      >
        <span className="truncate">{column.label}</span>
        {sorted === "ASC" && <ArrowUpIcon className="size-3 shrink-0" />}
        {sorted === "DESC" && <ArrowDownIcon className="size-3 shrink-0" />}
      </button>
      <button
        type="button"
        aria-label={`Filter ${column.label}`}
        title={filtered ? `Filtered: ${describeFilter(filterOf(layout, column.id)!)}` : `Filter ${column.label}`}
        className={cn(
          "ml-auto shrink-0 cursor-pointer rounded p-0.5 hover:bg-background",
          filtered ? "text-foreground" : "opacity-0 group-hover/header:opacity-60",
        )}
        // The funnel opens the filter; it must not start a drag.
        onPointerDown={(e) => e.stopPropagation()}
        onClick={onFilter}
      >
        <FilterIcon className={cn("size-3", filtered && "fill-current")} />
      </button>
    </div>
  )
}

/** The header's right-click menu: about the column clicked, then the table. */
function HeaderMenu({
  column,
  archived,
  onArchivedChange,
  layout,
  onLayoutChange,
  onFilter,
}: {
  column: SessionColumnId | null
  archived: boolean
  onArchivedChange: (archived: boolean) => void
  layout: SessionsLayout
  onLayoutChange: (layout: SessionsLayout | null) => void
  onFilter: (column: SessionColumnId) => void
}) {
  const col = column ? SESSION_COLUMN_BY_ID.get(column) : undefined
  const i = column ? layout.columns.indexOf(column) : -1
  const sorted = col && layout.sort?.column === col.id ? layout.sort.direction : null
  const filtered = col ? filterOf(layout, col.id) !== undefined : false
  return (
    <ContextMenuContent className="min-w-52">
      {col && (
        <>
          {col.sortable && (
            <>
              <ContextMenuItem
                disabled={sorted === "ASC"}
                onClick={() => onLayoutChange(setSort(layout, { column: col.id, direction: "ASC" }))}
              >
                <ArrowUpIcon />
                Sort ascending
              </ContextMenuItem>
              <ContextMenuItem
                disabled={sorted === "DESC"}
                onClick={() => onLayoutChange(setSort(layout, { column: col.id, direction: "DESC" }))}
              >
                <ArrowDownIcon />
                Sort descending
              </ContextMenuItem>
              {sorted && (
                <ContextMenuItem onClick={() => onLayoutChange(setSort(layout, null))}>
                  <ListRestartIcon />
                  Clear sort (newest first)
                </ContextMenuItem>
              )}
              <ContextMenuSeparator />
            </>
          )}
          <ContextMenuItem onClick={() => onFilter(col.id)}>
            <FilterIcon />
            {filtered ? "Edit filter…" : "Filter…"}
          </ContextMenuItem>
          {filtered && (
            <ContextMenuItem onClick={() => onLayoutChange(setFilter(layout, col.id, null))}>
              <FilterXIcon />
              Clear filter
            </ContextMenuItem>
          )}
          <ContextMenuSeparator />
          <ContextMenuItem disabled={i <= 0} onClick={() => onLayoutChange(shiftColumn(layout, col.id, -1))}>
            <ArrowLeftIcon />
            Move left
          </ContextMenuItem>
          <ContextMenuItem
            disabled={i < 0 || i >= layout.columns.length - 1}
            onClick={() => onLayoutChange(shiftColumn(layout, col.id, 1))}
          >
            <ArrowRightIcon />
            Move right
          </ContextMenuItem>
          <ContextMenuItem
            disabled={layout.columns.length === 1}
            onClick={() => onLayoutChange(toggleColumn(layout, col.id))}
          >
            <EyeOffIcon />
            Hide column
          </ContextMenuItem>
          <ContextMenuSeparator />
        </>
      )}
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <Columns3Icon />
          Columns
        </ContextMenuSubTrigger>
        <ContextMenuSubContent className="max-h-96 overflow-y-auto">
          {SESSION_COLUMNS.map((c) => (
            <ContextMenuCheckboxItem
              key={c.id}
              checked={layout.columns.includes(c.id)}
              disabled={layout.columns.length === 1 && layout.columns[0] === c.id}
              // Added next to the column the menu is about.
              onCheckedChange={() => onLayoutChange(toggleColumn(layout, c.id, column))}
            >
              {c.id === "number" ? "Number" : c.label}
            </ContextMenuCheckboxItem>
          ))}
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuCheckboxItem checked={archived} onCheckedChange={(on) => onArchivedChange(on)}>
        Show archived sessions
      </ContextMenuCheckboxItem>
      {layout.filters.length > 0 && (
        <ContextMenuItem onClick={() => onLayoutChange({ ...layout, filters: [] })}>
          <FilterXIcon />
          Clear all filters
        </ContextMenuItem>
      )}
      <ContextMenuItem onClick={() => onLayoutChange(null)}>
        <ListRestartIcon />
        Reset to default
      </ContextMenuItem>
    </ContextMenuContent>
  )
}

/** The button at the header's end: which columns show, and the reset. */
function ColumnsButton({
  layout,
  onLayoutChange,
  archived,
  onArchivedChange,
}: {
  layout: SessionsLayout
  onLayoutChange: (layout: SessionsLayout | null) => void
  archived: boolean
  onArchivedChange: (archived: boolean) => void
}) {
  const isDefault = JSON.stringify(layout) === JSON.stringify(DEFAULT_SESSIONS_LAYOUT)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon-xs" aria-label="Columns" title="Columns" className="justify-self-end">
            <Columns3Icon />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="max-h-96 min-w-52 overflow-y-auto">
        {/* A label must sit in a group, or Base UI throws. */}
        <DropdownMenuGroup>
          <DropdownMenuLabel>Columns</DropdownMenuLabel>
          {SESSION_COLUMNS.map((c) => (
            <DropdownMenuCheckboxItem
              key={c.id}
              checked={layout.columns.includes(c.id)}
              disabled={layout.columns.length === 1 && layout.columns[0] === c.id}
              onCheckedChange={() => onLayoutChange(toggleColumn(layout, c.id))}
            >
              {c.id === "number" ? "Number" : c.label}
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuCheckboxItem checked={archived} onCheckedChange={(on) => onArchivedChange(on)}>
          Show archived sessions
        </DropdownMenuCheckboxItem>
        <DropdownMenuItem disabled={isDefault} onClick={() => onLayoutChange(null)}>
          <ListRestartIcon />
          Reset to default
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The filters in force, above the table: a click edits one, × removes it. */
function FilterChips({
  selectionBar,
  archived,
  onShowLive,
  layout,
  onEdit,
  onRemove,
  onClearAll,
}: {
  /** The bulk-action bar while rows are selected; it leads the row. */
  selectionBar: ReactNode
  archived: boolean
  onShowLive: () => void
  layout: SessionsLayout
  onEdit: (column: SessionColumnId, anchor: HTMLElement) => void
  onRemove: (column: SessionColumnId) => void
  onClearAll: () => void
}) {
  if (layout.filters.length === 0 && !archived && !selectionBar) return null
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {selectionBar}
      {archived && (
        <span className="inline-flex items-center rounded-md border border-foreground/30 bg-muted text-xs">
          <span className="flex items-center gap-1 py-1 pr-1 pl-2">
            <ArchiveIcon className="size-3 shrink-0" />
            <span className="font-medium">Archived sessions</span>
          </span>
          <button
            type="button"
            aria-label="Back to the sessions"
            title="Back to the sessions"
            className="cursor-pointer rounded p-1 text-muted-foreground hover:text-foreground"
            onClick={onShowLive}
          >
            <XIcon className="size-3" />
          </button>
        </span>
      )}
      {layout.filters.map((f) => {
        const col = SESSION_COLUMN_BY_ID.get(f.column)!
        const hidden = !layout.columns.includes(f.column)
        return (
          <span
            key={f.column}
            className="inline-flex max-w-full items-center rounded-md border bg-card text-xs"
            title={hidden ? "This column is hidden; its filter still applies" : undefined}
          >
            <button
              type="button"
              className="flex min-w-0 cursor-pointer items-center gap-1 py-1 pr-1 pl-2 hover:text-foreground"
              onClick={(e) => onEdit(f.column, e.currentTarget.parentElement as HTMLElement)}
            >
              <FilterIcon className="size-3 shrink-0" />
              <span className={cn("font-medium", hidden && "italic")}>{col.id === "number" ? "Number" : col.label}</span>
              <span className="truncate text-muted-foreground">{describeFilter(f)}</span>
            </button>
            <button
              type="button"
              aria-label={`Remove the ${col.label} filter`}
              className="cursor-pointer rounded p-1 text-muted-foreground hover:text-foreground"
              onClick={() => onRemove(f.column)}
            >
              <XIcon className="size-3" />
            </button>
          </span>
        )
      })}
      {layout.filters.length > 1 && (
        <Button variant="ghost" size="xs" onClick={onClearAll}>
          Clear all
        </Button>
      )}
    </div>
  )
}

/** A session row's menu: build a minion that comes back to this session, copy its id. */
function SessionMenu({
  session: s,
  canWrite,
  persistable,
  onSelect,
  selected,
  onEditDescription,
  onMark,
}: {
  session: MinionSessionFieldsFragment
  canWrite: boolean
  persistable: boolean
  /** Opens the description's editor. */
  onEditDescription: () => void
  /** Opens the caller's mark for the session. */
  onMark: () => void
  /** Picks the row for bulk actions (or unpicks it). */
  onSelect: () => void
  selected: boolean
}) {
  const modals = useRecordModals()
  const archive = useSetMinionSessionArchived()
  const why = !canWrite
    ? "Needs operator in this operation"
    : !persistable
      ? `The ${s.moduleName} factory's minions do not claim persisted sessions`
      : undefined
  return (
    <>
      {canWrite && (
        <>
          <ContextMenuItem onClick={onSelect}>
            <ListChecksIcon />
            {selected ? "Unselect" : "Select"}
          </ContextMenuItem>
          <ContextMenuItem onClick={onEditDescription}>
            <PencilIcon />
            Edit description…
          </ContextMenuItem>
          <ContextMenuSeparator />
        </>
      )}
      {/* A mark is the caller's own: any member marks. */}
      <ContextMenuItem onClick={onMark}>
        <TagIcon />
        Mark…
      </ContextMenuItem>
      <ContextMenuItem
        disabled={why !== undefined}
        title={why}
        onClick={() => modals.open({ kind: "new-build", factory: s.moduleName, from: s.buildId, session: s.id })}
      >
        <PinIcon />
        Build with persisted session
      </ContextMenuItem>
      <ContextMenuItem
        disabled={!canWrite}
        title={canWrite ? undefined : "Needs operator in this operation"}
        onClick={() => archive.mutate({ id: s.id, archived: !s.archived })}
      >
        {s.archived ? <ArchiveRestoreIcon /> : <ArchiveIcon />}
        {s.archived ? "Restore" : "Archive"}
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onClick={() => void copyToClipboard(s.id, "session id")}>
        <CopyIcon />
        Copy session id
      </ContextMenuItem>
    </>
  )
}

/**
 * The bar while sessions are selected: how many, the bulk action of the view
 * (Archive, or Restore among the archived), "select all matching" once every
 * loaded row is picked and more match, and Done. An action on all matching
 * asks first: it reaches rows not on screen.
 */
function SelectionBar({
  operationId,
  archived,
  canWrite,
  selection,
  picked,
  loaded,
  total,
  filters,
  onSelectAll,
  onMark,
  onDone,
}: {
  operationId: string
  archived: boolean
  canWrite: boolean
  selection: SessionSelection
  picked: string[]
  loaded: number
  total: number
  filters: SessionsLayout["filters"]
  onSelectAll: () => void
  /** Opens the mark for every selected session. */
  onMark: () => void
  onDone: () => void
}) {
  const bulk = useSetMinionSessionsArchived()
  const [confirming, setConfirming] = useState(false)
  const count = selection.all ? total : picked.length
  const verb = archived ? "Restore" : "Archive"
  const run = () => {
    setConfirming(false)
    bulk.mutate(
      {
        operationId,
        archived: !archived,
        ...(selection.all ? { matching: { archived, filters } } : { ids: picked }),
      },
      {
        onSuccess: (data) => {
          const n = data.setMinionSessionsArchived
          toast.success(`${archived ? "Restored" : "Archived"} ${n} ${n === 1 ? "session" : "sessions"}`)
          onDone()
        },
        onError: (err) => toast.error(errorMessage(err, `Failed to ${verb.toLowerCase()} the sessions`)),
      },
    )
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5 rounded-md border border-foreground/30 bg-muted px-2 py-0.5 text-xs">
      <ListChecksIcon className="size-3 shrink-0" />
      <span className="font-medium">
        {selection.all ? `All ${total} matching selected` : `${count} selected`}
      </span>
      {!selection.all && picked.length === loaded && total > loaded && (
        <Button variant="link" size="xs" className="h-auto px-0 text-xs" onClick={onSelectAll}>
          Select all {total} matching
        </Button>
      )}
      {canWrite &&
        (confirming ? (
          <>
            <span>
              {verb} {count} sessions?
            </span>
            <Button size="xs" onClick={run} disabled={bulk.isPending}>
              {verb}
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
          </>
        ) : (
          <Button
            size="xs"
            variant="outline"
            disabled={bulk.isPending || count === 0}
            onClick={() => (selection.all ? setConfirming(true) : run())}
          >
            {archived ? <ArchiveRestoreIcon /> : <ArchiveIcon />}
            {verb}
          </Button>
        ))}
      <Button size="xs" variant="outline" disabled={count === 0} onClick={onMark}>
        <TagIcon />
        Mark…
      </Button>
      <Button size="xs" variant="ghost" onClick={onDone} title="Done (Esc)">
        Done
      </Button>
    </span>
  )
}
