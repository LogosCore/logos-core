import { useEffect, useRef, type KeyboardEvent, type ReactNode } from "react"
import { XIcon } from "lucide-react"
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core"
import { SortableContext, horizontalListSortingStrategy, useSortable } from "@dnd-kit/sortable"
import { CSS as DndCSS } from "@dnd-kit/utilities"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { cn } from "@/lib/utils"
import { activateTab, closeTabs, moveTab, tabsToClose, type WorkPaneTabs } from "./work-pane-tabs"

// A work pane: tabs over panels, for working with several things at once
// (the Sessions page's sessions). It knows nothing of what is in them: its
// owner keeps the tabs (work-pane-tabs.ts), says how each is labelled, and
// renders each panel.
//
// The tab strip scrolls sideways when the tabs do not fit, and brings the
// one in front into view. A tab closes by its ✕ or a middle click; its
// context menu closes it, the others, those to its right or all; a drag
// reorders them; the arrow keys, Home and End move between them. Every
// panel stays mounted, the ones behind hidden, so what is going on in a
// tab (a console, later) carries on behind the others.

export interface WorkPaneTab {
  id: string
  /** What the tab says: short, an icon and a name. */
  label: ReactNode
  /** The tab's tooltip, its whole name. */
  title?: string
}

export function WorkPane({
  label,
  tabs,
  state,
  onChange,
  panel,
  empty,
  className,
}: {
  /** The pane's accessible name. */
  label: string
  /** The open tabs, in state's order. */
  tabs: readonly WorkPaneTab[]
  state: WorkPaneTabs
  onChange: (next: WorkPaneTabs) => void
  /** A tab's panel. */
  panel: (id: string) => ReactNode
  /** What the pane shows with no tab open. */
  empty: ReactNode
  className?: string
}) {
  const strip = useRef<HTMLDivElement>(null)
  // A drag only past 6px: a click brings the tab to the front.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }))

  // The tab in front, in view (opened at the end, or reached by the keys).
  useEffect(() => {
    if (state.active === null) return
    strip.current
      ?.querySelector<HTMLElement>(`[data-tab-id="${CSS.escape(state.active)}"]`)
      ?.scrollIntoView({ block: "nearest", inline: "nearest" })
  }, [state.active])

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (over && active.id !== over.id) onChange(moveTab(state, String(active.id), String(over.id)))
  }

  const onKeyDown = (e: KeyboardEvent) => {
    const at = state.active === null ? -1 : state.ids.indexOf(state.active)
    const to = { ArrowLeft: at - 1, ArrowRight: at + 1, Home: 0, End: state.ids.length - 1 }[e.key]
    if (to === undefined || to < 0 || to >= state.ids.length) return
    e.preventDefault()
    const id = state.ids[to]
    onChange(activateTab(state, id))
    strip.current?.querySelector<HTMLElement>(`[data-tab-id="${CSS.escape(id)}"]`)?.focus()
  }

  if (tabs.length === 0) return <div className={cn("flex min-h-0 flex-1 flex-col", className)}>{empty}</div>

  return (
    <div className={cn("flex min-h-0 flex-1 flex-col", className)}>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={state.ids} strategy={horizontalListSortingStrategy}>
          <div
            ref={strip}
            role="tablist"
            aria-label={label}
            onKeyDown={onKeyDown}
            className="flex shrink-0 overflow-x-auto border-b bg-muted/40 [scrollbar-width:thin]"
          >
            {tabs.map((tab) => (
              <Tab
                key={tab.id}
                tab={tab}
                active={tab.id === state.active}
                onActivate={() => onChange(activateTab(state, tab.id))}
                onClose={(which) => onChange(closeTabs(state, tabsToClose(state, tab.id, which)))}
                last={tab.id === state.ids[state.ids.length - 1]}
                alone={state.ids.length === 1}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>
      {tabs.map((tab) => (
        <div
          key={tab.id}
          role="tabpanel"
          id={panelId(tab.id)}
          aria-labelledby={tabId(tab.id)}
          // hidden wins over flex (Tailwind's preflight makes it !important).
          hidden={tab.id !== state.active}
          className="flex min-h-0 flex-1 flex-col"
        >
          {panel(tab.id)}
        </div>
      ))}
    </div>
  )
}

const tabId = (id: string) => `work-pane-tab-${id}`
const panelId = (id: string) => `work-pane-panel-${id}`

function Tab({
  tab,
  active,
  onActivate,
  onClose,
  last,
  alone,
}: {
  tab: WorkPaneTab
  active: boolean
  onActivate: () => void
  onClose: (which: "this" | "others" | "right" | "all") => void
  last: boolean
  alone: boolean
}) {
  const { listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: tab.id })
  return (
    <div
      ref={setNodeRef}
      style={{ transform: DndCSS.Translate.toString(transform), transition }}
      className={cn("shrink-0", isDragging && "z-10")}
      {...listeners}
    >
      <ContextMenu>
        <ContextMenuTrigger
          render={
            <div
              role="tab"
              id={tabId(tab.id)}
              data-tab-id={tab.id}
              aria-selected={active}
              aria-controls={panelId(tab.id)}
              tabIndex={active ? 0 : -1}
              title={tab.title}
              onClick={onActivate}
              // A middle click closes it, as in a browser (and does not scroll).
              onMouseDown={(e) => {
                if (e.button === 1) e.preventDefault()
              }}
              onAuxClick={(e) => {
                if (e.button === 1) onClose("this")
              }}
              className={cn(
                "group/tab flex h-9 max-w-56 cursor-pointer items-center gap-2 border-r border-b-2 border-b-transparent pr-1.5 pl-3 text-sm outline-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
                active ? "border-b-foreground bg-card text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground",
                isDragging && "bg-muted shadow-sm",
              )}
            />
          }
        >
          <span className="flex min-w-0 items-center gap-1.5 truncate">{tab.label}</span>
          <button
            type="button"
            aria-label="Close the tab"
            title="Close (middle click)"
            tabIndex={-1}
            // The tab under it is not activated by closing it.
            onClick={(e) => {
              e.stopPropagation()
              onClose("this")
            }}
            onPointerDown={(e) => e.stopPropagation()}
            className={cn(
              "flex size-5 shrink-0 cursor-pointer items-center justify-center rounded opacity-0 group-hover/tab:opacity-100 hover:bg-foreground/10 focus-visible:opacity-100",
              active && "opacity-100",
            )}
          >
            <XIcon className="size-3.5" />
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={() => onClose("this")}>Close</ContextMenuItem>
          <ContextMenuItem disabled={alone} onClick={() => onClose("others")}>
            Close others
          </ContextMenuItem>
          <ContextMenuItem disabled={last} onClick={() => onClose("right")}>
            Close to the right
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onClick={() => onClose("all")}>Close all</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    </div>
  )
}
