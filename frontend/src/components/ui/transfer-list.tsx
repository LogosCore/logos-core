import { useEffect, useId, useState, type KeyboardEvent, type ReactNode } from "react"
import { ChevronLeftIcon, ChevronRightIcon, ChevronsLeftIcon, ChevronsRightIcon, LockIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { SearchInput } from "@/components/ui/search-input"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  after,
  clicked,
  extended,
  leaving,
  movable,
  moved,
  NO_MARKS,
  normalized,
  sideItems,
  stepped,
  type Marks,
  type Side,
  type TransferItem,
} from "./transfer-list-state"

export type { TransferItem } from "./transfer-list-state"

/**
 * Two lists side by side, the items to pick from and the ones picked, with
 * buttons between them that move items across: the marked ones, or all
 * those shown. A click marks an item (Ctrl or ⌘ adds one, Shift a range), a
 * double click moves it. In a list, the arrow keys move focus (with Shift,
 * marking as they go), Space marks, Enter moves, Ctrl+A marks all. A locked
 * item stays selected. Both lists keep the items' order, and one search
 * narrows both. Narrow, the lists stack and the arrows turn.
 */
export function TransferList({
  items,
  selected,
  onChange,
  defaults,
  titles = { available: "Available", selected: "Selected" },
  lockedNote = "Always selected",
  searchPlaceholder = "Search…",
  empty = { available: "Nothing left to add.", selected: "Nothing selected." },
  invalid,
  disabled,
  className,
}: {
  /** Every item, in the order both lists show them. */
  items: readonly TransferItem[]
  /** The ids selected; the locked items are, whatever this says. */
  selected: readonly string[]
  /** Gets the ids selected, the locked ones among them, in the items' order. */
  onChange: (selected: string[]) => void
  /** What "Reset to defaults" selects; no such link without it. */
  defaults?: readonly string[]
  titles?: Record<Side, string>
  /** What a locked item's tooltip says of it. */
  lockedNote?: string
  searchPlaceholder?: string
  /** What an empty list says. */
  empty?: Record<Side, string>
  /** Marks the selected list, for a problem with the selection. */
  invalid?: boolean
  disabled?: boolean
  className?: string
}) {
  const id = useId()
  const [query, setQuery] = useState("")
  const [marks, setMarks] = useState<Record<Side, Marks>>({ available: NO_MARKS, selected: NO_MARKS })
  const [active, setActive] = useState<Record<Side, string | null>>({ available: null, selected: null })

  const picked = normalized(items, selected)
  const shown: Record<Side, TransferItem[]> = {
    available: sideItems(items, selected, "available", query),
    selected: sideItems(items, selected, "selected", query),
  }
  const atDefaults = defaults !== undefined && normalized(items, defaults).join() === picked.join()
  const searching = query.trim() !== ""

  function move(from: Side, ids: string[]) {
    if (ids.length === 0 || disabled) return
    const to: Side = from === "available" ? "selected" : "available"
    onChange(moved(items, selected, ids, to))
    // Focus stays near where the items left; they land marked, so an
    // arrow back undoes the move.
    const next = after(
      shown[from].map((i) => i.id),
      ids,
    )
    setActive((a) => ({ ...a, [from]: next, [to]: ids[0] }))
    setMarks((m) => ({ ...m, [from]: NO_MARKS, [to]: { marked: new Set(ids), anchor: ids[0] } }))
  }

  const marked = (side: Side) => leaving(shown[side], side, marks[side], null)
  const all = (side: Side) => shown[side].filter((i) => movable(i, side)).map((i) => i.id)

  function onKeyDown(side: Side, e: KeyboardEvent<HTMLDivElement>) {
    if (disabled) return
    const order = shown[side].map((i) => i.id)
    const steps: Record<string, "next" | "previous" | "first" | "last"> = {
      ArrowDown: "next",
      ArrowUp: "previous",
      Home: "first",
      End: "last",
    }
    const step = steps[e.key]
    if (step) {
      e.preventDefault()
      const from = active[side]
      const to = stepped(order, from, step)
      if (to === null) return
      setActive((a) => ({ ...a, [side]: to }))
      setMarks((m) => ({ ...m, [side]: extended(order, m[side], from, to, e.shiftKey) }))
    } else if (e.key === " ") {
      e.preventDefault()
      const at = active[side] ?? order[0]
      if (at === undefined) return
      setActive((a) => ({ ...a, [side]: at }))
      setMarks((m) => ({ ...m, [side]: clicked(order, m[side], at, { shift: false, toggle: true }) }))
    } else if (e.key === "Enter") {
      e.preventDefault()
      move(side, leaving(shown[side], side, marks[side], active[side]))
    } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "a") {
      e.preventDefault()
      setMarks((m) => ({ ...m, [side]: { marked: new Set(order), anchor: order[0] ?? null } }))
    }
  }

  const list = (side: Side) => (
    <ItemList
      id={`${id}-${side}`}
      side={side}
      title={titles[side]}
      count={side === "selected" ? `${picked.length} of ${items.length}` : `${items.length - picked.length}`}
      items={shown[side]}
      marks={marks[side]}
      active={active[side]}
      lockedNote={lockedNote}
      empty={searching ? "Nothing matches the search." : empty[side]}
      invalid={invalid && side === "selected"}
      disabled={disabled}
      onClick={(itemId, mods) => {
        const order = shown[side].map((i) => i.id)
        setActive((a) => ({ ...a, [side]: itemId }))
        setMarks((m) => ({ ...m, [side]: clicked(order, m[side], itemId, mods) }))
      }}
      onDoubleClick={(item) => movable(item, side) && move(side, [item.id])}
      onKeyDown={(e) => onKeyDown(side, e)}
    />
  )

  return (
    <div className={cn("@container/transfer flex min-w-0 flex-col gap-2", className)}>
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          value={query}
          onValueChange={setQuery}
          placeholder={searchPlaceholder}
          debounceMs={0}
          title=""
          className="relative w-full max-w-72"
        />
        {defaults && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="ml-auto"
            disabled={disabled || atDefaults}
            onClick={() => {
              onChange(normalized(items, defaults))
              setMarks({ available: NO_MARKS, selected: NO_MARKS })
            }}
          >
            Reset to defaults
          </Button>
        )}
      </div>
      <div className="grid gap-2 @xl/transfer:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
        {list("available")}
        <div className="flex items-center justify-center gap-1 @xl/transfer:flex-col">
          <Move label="Add the marked" onClick={() => move("available", marked("available"))} disabled={disabled || marked("available").length === 0}>
            <ChevronRightIcon />
          </Move>
          <Move
            label={searching ? "Add all shown" : "Add all"}
            onClick={() => move("available", all("available"))}
            disabled={disabled || all("available").length === 0}
          >
            <ChevronsRightIcon />
          </Move>
          <Move label="Remove the marked" onClick={() => move("selected", marked("selected"))} disabled={disabled || marked("selected").length === 0}>
            <ChevronLeftIcon />
          </Move>
          <Move
            label={`${searching ? "Remove all shown" : "Remove all"}${items.some((i) => i.locked) ? "; the locked ones stay" : ""}`}
            onClick={() => move("selected", all("selected"))}
            disabled={disabled || all("selected").length === 0}
          >
            <ChevronsLeftIcon />
          </Move>
        </div>
        {list("selected")}
      </div>
    </div>
  )
}

/** A button between the lists. Its arrow points across; down or up when the lists stack. */
function Move({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            aria-label={label}
            disabled={disabled}
            onClick={onClick}
            className="[&_svg]:rotate-90 @xl/transfer:[&_svg]:rotate-0"
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

/** A row's content: an icon, the label and the hint on one baseline. */
const ROW = "flex min-w-0 flex-1 items-baseline gap-2"

/** The DOM id of a list's option, for the list's active descendant. */
const optionId = (list: string, item: string) => `${list}-${item}`

/** One of the two lists: a listbox the keyboard works in. */
function ItemList({
  id,
  side,
  title,
  count,
  items,
  marks,
  active,
  lockedNote,
  empty,
  invalid,
  disabled,
  onClick,
  onDoubleClick,
  onKeyDown,
}: {
  id: string
  side: Side
  title: string
  count: string
  items: readonly TransferItem[]
  marks: Marks
  active: string | null
  lockedNote: string
  empty: string
  invalid?: boolean
  disabled?: boolean
  onClick: (id: string, mods: { shift: boolean; toggle: boolean }) => void
  onDoubleClick: (item: TransferItem) => void
  onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => void
}) {
  const focused = items.some((i) => i.id === active) ? active : null

  // The focused item stays in view as the keys move it.
  useEffect(() => {
    if (focused) document.getElementById(optionId(id, focused))?.scrollIntoView({ block: "nearest" })
  }, [id, focused])

  return (
    <div
      className={cn(
        "flex min-w-0 flex-col overflow-hidden rounded-lg border bg-background",
        invalid && "border-destructive",
        disabled && "opacity-60",
      )}
    >
      <div className="flex items-baseline gap-2 border-b bg-muted/40 px-3 py-1.5 text-xs">
        <span id={`${id}-title`} className="font-medium">
          {title}
        </span>
        <span className="ml-auto text-muted-foreground tabular-nums">{count}</span>
      </div>
      <div
        role="listbox"
        aria-multiselectable
        aria-labelledby={`${id}-title`}
        aria-activedescendant={focused ? optionId(id, focused) : undefined}
        aria-disabled={disabled || undefined}
        tabIndex={disabled ? -1 : 0}
        onKeyDown={onKeyDown}
        className="group/list h-64 overflow-y-auto py-1 outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
      >
        {items.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted-foreground">{empty}</p>}
        {items.map((item) => {
          const canMove = movable(item, side)
          const isMarked = canMove && marks.marked.has(item.id)
          const note = [item.locked ? lockedNote : "", item.description ?? ""].filter(Boolean).join("\n")
          const content = (
            <>
              {item.locked && <LockIcon aria-hidden className="size-3 shrink-0 self-center" />}
              <span className="min-w-0 truncate">{item.label}</span>
              {item.hint && <code className="ml-auto shrink-0 font-mono text-xs text-muted-foreground">{item.hint}</code>}
            </>
          )
          return (
            <div
              key={item.id}
              id={optionId(id, item.id)}
              role="option"
              aria-selected={isMarked}
              aria-disabled={!canMove || undefined}
              aria-description={note || undefined}
              data-active={item.id === focused || undefined}
              onClick={(e) => !disabled && onClick(item.id, { shift: e.shiftKey, toggle: e.ctrlKey || e.metaKey })}
              onDoubleClick={() => !disabled && onDoubleClick(item)}
              className={cn(
                "mx-1 flex cursor-default rounded-md px-2 py-1 text-sm select-none",
                isMarked ? "bg-accent text-accent-foreground" : "hover:bg-muted/60",
                !canMove && "text-muted-foreground",
                "group-focus-visible/list:data-active:ring-1 group-focus-visible/list:data-active:ring-ring",
              )}
            >
              {/* The tooltip's trigger fills the row: the trigger gives what it
                  renders an id of its own, and the option's must stay. */}
              {note ? (
                <Tooltip>
                  <TooltipTrigger delay={500} render={<span className={ROW} />}>
                    {content}
                  </TooltipTrigger>
                  <TooltipContent side={side === "available" ? "right" : "left"} className="whitespace-pre-line">
                    {note}
                  </TooltipContent>
                </Tooltip>
              ) : (
                <span className={ROW}>{content}</span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
