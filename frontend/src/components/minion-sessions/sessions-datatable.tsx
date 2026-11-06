import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso"
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
  ArchiveXIcon,
  ArrowDownIcon,
  ArrowLeftIcon,
  ArrowRightIcon,
  ArrowUpIcon,
  Columns3Icon,
  EyeOffIcon,
  FilterIcon,
  FilterXIcon,
  HammerIcon,
  InfoIcon,
  MonitorDotIcon,
  CopyIcon,
  ListRestartIcon,
  MoveHorizontalIcon,
  ListChecksIcon,
  LoaderIcon,
  PencilIcon,
  PinIcon,
  SquareTerminalIcon,
  FolderTreeIcon,
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
import { SessionFire } from "./session-fire"
import { SessionFilterEditor } from "./session-filter-editor"
import { SessionDescriptionDialog } from "./session-description-dialog"
import type { SessionMarkTarget } from "./session-mark-dialog"
import { preloadable, useLoaded } from "@/lib/preloadable"
import { markColorVars, markRowClass } from "./session-mark-colors"
import {
  DEFAULT_SESSIONS_LAYOUT,
  addressMatcher,
  SESSION_COLUMNS,
  SESSION_COLUMN_BY_ID,
  cycleSort,
  describeFilter,
  filterOf,
  moveColumn,
  columnTrack,
  rowMinWidth,
  setFilter,
  setSort,
  shiftColumn,
  sizeColumn,
  toggleColumn,
  type SessionColumnDef,
  type SessionColumnId,
  type SessionsLayout,
} from "./sessions-layout"
import { SwitchConnectionMenu } from "./switch-connection-menu"
import { ConnectTargetMenu } from "./connect-target-menu"

// The Sessions table: the operators' main view. Unlike the app's other
// tables, its columns are the operator's: any property of a session, in any
// order (drag a header, or Move left / right), filtered and sorted on any
// column, all from the header: a click sorts, the funnel filters, and a right
// click (or the columns button at the end) opens the rest. Core filters and
// sorts; the layout is saved on the operator (useMinionSessionsLayout).

const SKELETON_ROWS = 5
const MENU_PX = 36
const CHECK_PX = 20
const MENU_TRACK = `${MENU_PX}px`
const CHECK_TRACK = `${CHECK_PX}px`
// A pinned cell ([data-pinned]) covers what scrolls under it, under any mark
// (session-mark-styles.css).
// Pinned at the scroller's edge, over the row's px-4: the cell takes the
// padding in (margin out, padding in), so it keeps its space and covers what
// scrolls under it; the row's py-2 too, so it is the row's height.
// The bar down the left edge of the row whose tab is in front.
const ACTIVE_BAR = "before:pointer-events-none before:absolute before:inset-y-0 before:left-0 before:w-1 before:bg-primary"
const PIN_LEFT = "left-0 -ml-4 pl-4 -my-2 py-2 grid items-center self-stretch"
const PIN_RIGHT = "right-0 -mr-4 pr-4 -my-2 py-2 flex items-center self-stretch"
const PINNED_HEADER = "sticky z-[2] bg-[color-mix(in_oklab,var(--color-muted)_50%,var(--color-card))]"

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
  onInteract,
  onFileBrowser,
  activeSessionId = null,
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
  /** Opens the session in the work pane below. */
  onInteract: (session: MinionSessionFieldsFragment) => void
  /** Opens the session's machine's file browser in the work pane below. */
  onFileBrowser: (session: MinionSessionFieldsFragment) => void
  /** The session whose tab is in front in the work pane: marked, and
   *  scrolled to when the tab changes. */
  activeSessionId?: string | null
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

  // The work pane's tab in front: its row comes into view when the tab
  // changes (only then: scrolling away from it is the operator's to do).
  const list = useRef<VirtuosoHandle>(null)
  const loaded = useRef(sessions)
  useEffect(() => {
    loaded.current = sessions
  }, [sessions])
  useEffect(() => {
    if (!activeSessionId) return
    const index = loaded.current.findIndex((s) => s.id === activeSessionId)
    if (index >= 0) list.current?.scrollIntoView({ index, behavior: "smooth" })
  }, [activeSessionId])

  const columns = useMemo(() => layout.columns.map((id) => SESSION_COLUMN_BY_ID.get(id)!), [layout.columns])
  // A column being sized: its width as dragged, kept in the layout on release.
  const [sizing, setSizing] = useState<{ id: SessionColumnId; px: number; dragging?: boolean } | null>(null)
  const widths = sizing ? { ...layout.widths, [sizing.id]: sizing.px } : layout.widths
  // The layout caught up with a released size: the width is its own now
  // (state adjusted while rendering, as React has it for state from props).
  if (sizing !== null && !sizing.dragging && layout.widths[sizing.id] === sizing.px) setSizing(null)
  const template = [...(selecting ? [CHECK_TRACK] : []), ...columns.map((c) => columnTrack(c, widths)), MENU_TRACK].join(" ")
  // Past the room the window has, the table scrolls sideways, the header with
  // the rows; # and the menu stay in view.
  const minWidth = rowMinWidth(columns, widths, [...(selecting ? [CHECK_PX] : []), MENU_PX])
  const grid = { gridTemplateColumns: template, minWidth }
  // The cell a right click came from: its value to copy, from the row's menu.
  const [copyCell, setCopyCell] = useState<{ label: string; text: string; column: SessionColumnId } | null>(null)
  const copyColumn = copyCell?.column ?? null
  const pinned = (col: SessionColumnDef, i: number) => col.id === "number" && i === 0
  const pinnedFirst = columns.length > 0 && pinned(columns[0], 0)

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

  const components = useMemo(
    () => ({
      Header: ListHeader,
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

  // The header: in the list, at its top (it scrolls sideways with the rows,
  // the browser's own scroll); above the list's place when there is none.
  const headerBlock = (
    <ContextMenu>
      <ContextMenuTrigger
        // A right click past the cells is about the table, not a column.
        onContextMenu={(e) => {
          if (e.target === e.currentTarget) setMenuColumn(null)
        }}
        // Opaque: the rows scroll under it. As wide as they are, to the end.
        className="w-fit min-w-full shrink-0 border-b bg-[color-mix(in_oklab,var(--color-muted)_50%,var(--color-card))]"
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
              {columns.map((col, i) => (
                <HeaderCell
                  key={col.id}
                  column={col}
                  pinned={pinned(col, i)}
                  onSize={(px) => setSizing({ id: col.id, px, dragging: true })}
                  onSized={(px) => {
                    const next = sizeColumn(layout, col.id, px)
                    // Held until the layout has it (it comes a moment later):
                    // dropped now, the old width showed for a frame.
                    setSizing(px === null ? null : { id: col.id, px: next.widths[col.id]! })
                    onLayoutChange(next)
                  }}
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
              <div className={cn(PINNED_HEADER, PIN_RIGHT, "-my-1.5 py-1.5 justify-self-end")}>
                <ColumnsButton
                  layout={layout}
                  onLayoutChange={onLayoutChange}
                  archived={archived}
                  onArchivedChange={onArchivedChange}
                />
              </div>
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
  )
  const listContext = { header: headerBlock }


  return (
    <div className="flex min-h-0 flex-1 flex-col">
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
                    const { setMinionSessionsMark: marked } = await markMany({
                      operationId,
                      mark,
                      ...(selection.all ? { matching: { archived, filters: layout.filters } } : { ids: picked }),
                    })
                    toast.success(`${mark ? "Marked" : "Cleared the marks of"} ${marked} ${marked === 1 ? "session" : "sessions"}`)
                    clearSelection()
                  },
                })
              }}
              onDone={clearSelection}
            />
          ) : null
        }
        layout={layout}
        onEdit={(column, anchor) => openFilter(column, anchor)}
        onRemove={(column) => onLayoutChange(setFilter(layout, column, null))}
        onClearAll={() => onLayoutChange({ ...layout, filters: [] })}
      />
      {/* Edge to edge: no card around it, a line where the divider below starts. */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden border-b bg-card">
        {!showList && headerBlock}

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
              ref={list}
              data={sessions}
              endReached={() => {
                if (hasNextPage && !isFetchingNextPage) fetchNextPage()
              }}
              overscan={200}
              style={{ height: "100%" }}
              className="min-h-0 flex-1"
              components={components}
              context={listContext}
              itemContent={(_, s) => (
                <ContextMenuRow
                  gridCols=""
                  // The caller's mark tints (or styles) the row; a picked row is outlined,
                  // so it shows marked and picked at once.
                  style={{ ...grid, ...markColorVars(s.mark?.color) }}
                  // While selecting, a drag or shift-click does not select text.
                  className={cn(
                    "group/row",
                    selecting && "select-none",
                    markRowClass(s.mark?.color),
                    pickedSet.has(s.id) && "ring-2 ring-primary/60 ring-inset",
                    // Its tab is in front: a bar down its left edge.
                    // (On the pinned # cell when there is one: the row's edge scrolls away under it.)
                    s.id === activeSessionId && !pinnedFirst && cn("relative", ACTIVE_BAR),
                  )}
                  // While selecting, a click picks the row (shift: a range).
                  onOpen={selecting ? (e) => setSelection(toggleSelected(selection, order, s.id, e.shiftKey)) : undefined}
                  menuLabel="Session actions"
                  menuButtonSize="icon-xs"
                  menu={
                    <SessionMenu
                      copyCell={copyCell}
                      table={{ column: copyColumn, archived, onArchivedChange, layout, onLayoutChange }}
                      session={s}
                      canWrite={canWrite}
                      persistable={persistable(s)}
                      onSelect={() => setSelection(toggleSelected(selection, order, s.id, false))}
                      onEditDescription={() => setDescribing(s)}
                      onInteract={() => onInteract(s)}
                      onFileBrowser={() => onFileBrowser(s)}
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
                          // Opaque: on a tinted or styled row its light border alone vanished.
                          className="pointer-events-none self-center bg-background"
                          checked={pickedSet.has(s.id)}
                          tabIndex={-1}
                        />
                      )}
                      {columns.map((col, i) => {
                        const cell = (
                          <div
                            key={col.id}
                            data-cell
                            data-column={col.id}
                            className="min-w-0"
                            // Its value, for the menu's Copy, and in a title when it is cut.
                            onContextMenu={(e) => setCopyCell({ label: col.label, text: cellText(e.currentTarget), column: col.id })}
                            onPointerOver={(e) => titleIfCut(e.currentTarget)}
                          >
                            <SessionCell id={col.id} session={s} ctx={ctx} />
                          </div>
                        )
                        // Pinned: in a wrapper that covers what scrolls under it.
                        return pinned(col, i) ? (
                          <div key={col.id} data-pinned className={cn(PIN_LEFT, s.id === activeSessionId && ACTIVE_BAR)}>
                            {s.mark?.color === "on-fire" && <SessionFire />}
                            {cell}
                          </div>
                        ) : (
                          cell
                        )
                      })}
                      <div
                        className={cn("justify-self-end", PIN_RIGHT)}
                        data-pinned
                        onPointerDownCapture={() => setCopyCell(null)}
                      >
                        {s.mark?.color === "on-fire" && <SessionFire />}
                        {menuButton}
                      </div>
                      {/* Positioned behind the cells, it takes no place in the grid. */}
                      {s.mark?.color === "on-fire" && <SessionFire />}
                    </>
                  )}
                </ContextMenuRow>
              )}
            />
          </div>
        )}
      </div>

      <SessionDescriptionDialog session={describing} onClose={() => setDescribing(null)} />
      {/* Its code (the icon picker and the icon catalogs) the first time a
          mark is set, then kept mounted so it closes as it opened. */}
      {(marking !== null || markDialogCode.current()) && <MarkDialog target={marking} onClose={() => setMarking(null)} />}
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
  pinned,
  onSize,
  onSized,
  layout,
  cellRef,
  onSort,
  onFilter,
  onContextMenu,
}: {
  column: SessionColumnDef
  /** Stays in view as the table scrolls sideways. */
  pinned: boolean
  /** Its width as dragged; onSized on release (null: back to its own). */
  onSize: (px: number) => void
  onSized: (px: number | null) => void
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
        "group/header relative -mx-1 flex min-w-0 cursor-grab items-center gap-0.5 rounded px-1 py-0.5 select-none hover:bg-muted active:cursor-grabbing",
        // Its -mx-1 and the row's padding, as the cells' PIN_LEFT.
        pinned && cn(PINNED_HEADER, "left-0 -ml-5 pl-5"),
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
      <ResizeHandle column={column} onSize={onSize} onSized={onSized} />
    </div>
  )
}

/**
 * A header's right edge: dragged, it sizes the column; a double click fits
 * it to the widest value on screen (and its label).
 */
function ResizeHandle({
  column,
  onSize,
  onSized,
}: {
  column: SessionColumnDef
  onSize: (px: number) => void
  onSized: (px: number | null) => void
}) {
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={`Resize ${column.label}`}
      title="Drag to resize; double-click to fit"
      className="absolute inset-y-0 -right-2 z-[3] w-3 cursor-col-resize after:absolute after:inset-y-1 after:left-1/2 after:w-px after:bg-border after:opacity-0 group-hover/header:after:opacity-100 hover:after:bg-primary"
      // Its own drag, not the column's move nor a sort.
      onPointerDown={(e) => {
        e.stopPropagation()
        e.preventDefault()
        const cell = e.currentTarget.parentElement!
        const start = e.clientX
        // The column's width: the header cell overhangs it by its -mx-1, 4px a side.
        const from = cell.getBoundingClientRect().width - 8
        let px: number | null = null
        const move = (ev: PointerEvent) => {
          // A click (or a double click's) is no drag: nothing changes.
          if (px === null && Math.abs(ev.clientX - start) < 3) return
          px = Math.max(column.min / 2, from + ev.clientX - start)
          onSize(px)
        }
        const up = () => {
          window.removeEventListener("pointermove", move)
          window.removeEventListener("pointerup", up)
          if (px !== null) onSized(px)
        }
        window.addEventListener("pointermove", move)
        window.addEventListener("pointerup", up)
      }}
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => {
        e.stopPropagation()
        // The header's own (its label, the sort arrow, the funnel), then
        // each row on screen's; the header cell overhangs its column by 8px.
        let widest = naturalWidth(e.currentTarget.parentElement!) - 8
        for (const el of document.querySelectorAll<HTMLElement>(`[data-cell][data-column="${column.id}"]`)) {
          widest = Math.max(widest, naturalWidth(el.firstElementChild as HTMLElement | null))
        }
        onSized(Math.ceil(widest) + 2)
      }}
    />
  )
}

/**
 * How wide an element would be uncut: laid out at max-content for a moment
 * (its parts side by side, none truncated), then put back.
 */
function naturalWidth(el: HTMLElement | null): number {
  if (!el) return 0
  const { width, minWidth, maxWidth, flex } = el.style
  el.style.width = "max-content"
  el.style.minWidth = "max-content"
  el.style.maxWidth = "none"
  el.style.flex = "none"
  const w = el.getBoundingClientRect().width
  Object.assign(el.style, { width, minWidth, maxWidth, flex })
  return w
}

/** A cell's value as text, for Copy. */
function cellText(cell: HTMLElement): string {
  const t = (cell.title || cell.querySelector<HTMLElement>("[title]")?.title || cell.innerText).trim()
  return t === "—" ? "" : t
}

/** Gives a cell whose text is cut its whole text as a title, unless it has one. */
function titleIfCut(cell: HTMLElement) {
  if (cell.querySelector("[title]") || cell.title) return
  const cut = [...cell.querySelectorAll<HTMLElement>("*")].some((el) => el.scrollWidth > el.clientWidth + 1)
  if (cut) cell.title = cell.innerText.trim()
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
          {layout.widths[col.id] && (
            <ContextMenuItem onClick={() => onLayoutChange(sizeColumn(layout, col.id, null))}>
              <MoveHorizontalIcon />
              Reset width
            </ContextMenuItem>
          )}
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
      <TableMenuItems
        column={column}
        archived={archived}
        onArchivedChange={onArchivedChange}
        layout={layout}
        onLayoutChange={onLayoutChange}
      />
    </ContextMenuContent>
  )
}

/**
 * The table's own items, the same in the header's menu and a row's, last in
 * both: which columns show (one added beside the column the menu is about),
 * archived sessions, the filters, the layout's reset.
 */
function TableMenuItems({
  column,
  archived,
  onArchivedChange,
  layout,
  onLayoutChange,
}: {
  column: SessionColumnId | null
  archived: boolean
  onArchivedChange: (archived: boolean) => void
  layout: SessionsLayout
  onLayoutChange: (layout: SessionsLayout | null) => void
}) {
  return (
    <>
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
      {/* Says what it does: the other view. */}
      <ContextMenuItem onClick={() => onArchivedChange(!archived)}>
        {archived ? <ArchiveXIcon /> : <ArchiveIcon />}
        {archived ? "Hide archived sessions" : "Show archived sessions"}
      </ContextMenuItem>
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
    </>
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
        <DropdownMenuItem onClick={() => onArchivedChange(!archived)}>
          {archived ? <ArchiveXIcon /> : <ArchiveIcon />}
          {archived ? "Hide archived sessions" : "Show archived sessions"}
        </DropdownMenuItem>
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
  layout,
  onEdit,
  onRemove,
  onClearAll,
}: {
  /** The bulk-action bar while rows are selected; it leads the row. */
  selectionBar: ReactNode
  layout: SessionsLayout
  onEdit: (column: SessionColumnId, anchor: HTMLElement) => void
  onRemove: (column: SessionColumnId) => void
  onClearAll: () => void
}) {
  if (layout.filters.length === 0 && !selectionBar) return null
  return (
    // A strip across the top of the table, as wide as it.
    <div className="flex flex-wrap items-center gap-1.5 border-b px-2 py-1.5">
      {selectionBar}
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
  onInteract,
  onFileBrowser,
  copyCell,
  table,
}: {
  /** The table's items, after the session's (TableMenuItems). */
  table: { column: SessionColumnId | null; archived: boolean; onArchivedChange: (archived: boolean) => void; layout: SessionsLayout; onLayoutChange: (layout: SessionsLayout | null) => void }
  /** The cell a right click opened the menu from: its value to copy. */
  copyCell?: { label: string; text: string } | null
  session: MinionSessionFieldsFragment
  canWrite: boolean
  persistable: boolean
  /** Opens the description's editor. */
  onEditDescription: () => void
  /** Opens the caller's mark for the session. */
  onMark: () => void
  /** Opens the session in the work pane. */
  onInteract: () => void
  /** Opens its machine's file browser in the work pane. */
  onFileBrowser: () => void
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
      {copyCell?.text && (
        <>
          <ContextMenuItem onClick={() => void copyToClipboard(copyCell.text, copyCell.label.toLowerCase())}>
            <CopyIcon />
            Copy {copyCell.label.toLowerCase()}
          </ContextMenuItem>
          <ContextMenuSeparator />
        </>
      )}
      <ContextMenuItem onClick={onInteract}>
        <SquareTerminalIcon />
        Interact
      </ContextMenuItem>
      <ContextMenuItem onClick={onFileBrowser}>
        <FolderTreeIcon />
        File Browser
      </ContextMenuItem>
      <ConnectTargetMenu s={s} canWrite={canWrite} onInteract={onInteract} />
      <SwitchConnectionMenu s={s} canWrite={canWrite} />
      {/* The session's own record, and the build its minion came from. */}
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <InfoIcon />
          Details
        </ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuItem onClick={() => modals.open({ kind: "session", id: s.id })}>
            <MonitorDotIcon />
            Session details
          </ContextMenuItem>
          <ContextMenuItem onClick={() => modals.open({ kind: "build", id: s.buildId })}>
            <HammerIcon />
            Build details
          </ContextMenuItem>
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuSeparator />
      {/* Any member selects: marking many is theirs too (archiving, in the
          bar, stays an operator's). */}
      <ContextMenuItem onClick={onSelect}>
        <ListChecksIcon />
        {selected ? "Unselect" : "Select"}
      </ContextMenuItem>
      {canWrite && (
        <ContextMenuItem onClick={onEditDescription}>
          <PencilIcon />
          Edit description…
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
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
      <ContextMenuSeparator />
      <TableMenuItems {...table} />
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

const markDialogCode = preloadable(() => import("./session-mark-dialog"))

function MarkDialog({ target, onClose }: { target: SessionMarkTarget | null; onClose: () => void }) {
  const code = useLoaded(markDialogCode)
  return code && <code.SessionMarkDialog target={target} onClose={onClose} />
}

/** The list's header, pinned to its top as the rows scroll under it (Virtuoso's Header, its context's). */
function ListHeader({ context }: { context?: { header: ReactNode } }) {
  return <div className="sticky top-0 z-[5]">{context?.header}</div>
}
