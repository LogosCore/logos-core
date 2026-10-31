import { useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react"
import { cn } from "@/lib/utils"

// Two panes, stacked (vertical) or side by side (horizontal), with a divider
// between them the user drags to share the space: with the pointer, with the
// arrow keys once it has focus (Home and End go to the ends), and back to the
// default on a double click. While it moves only this component re-renders;
// onChange gets the new share when the move ends, to keep. The new share is
// held until split comes back with it: an owner keeping it in a query cache
// hears of the change a tick later, and the panes would snap back to the old
// share for that frame (the work pane flickered on every release).

const KEY_STEP = 0.05

export function SplitPane({
  first,
  second,
  orientation = "vertical",
  split,
  defaultSplit,
  min = 0.1,
  max = 0.9,
  onChange,
  label,
  className,
}: {
  /** The top pane, or the left one. */
  first: ReactNode
  /** The bottom pane, or the right one. */
  second: ReactNode
  /** vertical: one above the other; horizontal: side by side. */
  orientation?: "vertical" | "horizontal"
  /** The share of the height (or width) the first pane takes, 0 to 1. */
  split: number
  /** Where a double click on the divider puts it. */
  defaultSplit: number
  min?: number
  max?: number
  /** The share when a move ends. */
  onChange: (split: number) => void
  /** What the divider separates, for screen readers. */
  label: string
  className?: string
}) {
  const box = useRef<HTMLDivElement>(null)
  // The share while the divider moves; null at rest, when split rules.
  const [moving, setMoving] = useState<number | null>(null)
  // The share given to onChange, until split changes.
  const [held, setHeld] = useState<number | null>(null)
  const [heldFor, setHeldFor] = useState(split)
  if (heldFor !== split) {
    setHeldFor(split)
    setHeld(null)
  }
  const current = moving ?? held ?? split
  const clamp = (v: number) => Math.min(max, Math.max(min, v))
  const commit = (v: number) => {
    if (v === current && moving === null) return
    setHeld(v)
    onChange(v)
  }
  const across = orientation === "horizontal"

  const at = (e: PointerEvent) => {
    const rect = box.current!.getBoundingClientRect()
    return clamp(across ? (e.clientX - rect.left) / rect.width : (e.clientY - rect.top) / rect.height)
  }
  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    setMoving(at(e))
  }
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (moving !== null) setMoving(at(e))
  }
  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    if (moving === null) return
    e.currentTarget.releasePointerCapture(e.pointerId)
    const end = at(e)
    setMoving(null)
    commit(end)
  }
  const onKeyDown = (e: KeyboardEvent) => {
    const next = {
      [across ? "ArrowLeft" : "ArrowUp"]: clamp(current - KEY_STEP),
      [across ? "ArrowRight" : "ArrowDown"]: clamp(current + KEY_STEP),
      Home: min,
      End: max,
    }[e.key]
    if (next === undefined) return
    e.preventDefault()
    commit(next)
  }

  return (
    <div
      ref={box}
      className={cn(
        "flex min-h-0 min-w-0 flex-1",
        across ? "flex-row" : "flex-col",
        moving !== null && (across ? "cursor-col-resize select-none" : "cursor-row-resize select-none"),
        className,
      )}
    >
      <div className="flex min-h-0 min-w-0 flex-col" style={{ flexBasis: `${current * 100}%`, flexGrow: 0, flexShrink: 0 }}>
        {first}
      </div>
      <div
        role="separator"
        aria-orientation={across ? "vertical" : "horizontal"}
        aria-label={label}
        aria-valuemin={Math.round(min * 100)}
        aria-valuemax={Math.round(max * 100)}
        aria-valuenow={Math.round(current * 100)}
        tabIndex={0}
        title="Drag to resize; double-click to reset"
        className={cn(
          "group/divider flex shrink-0 touch-none items-center justify-center outline-none",
          across ? "w-3 cursor-col-resize" : "h-3 cursor-row-resize",
        )}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={() => setMoving(null)}
        onKeyDown={onKeyDown}
        onDoubleClick={() => commit(defaultSplit)}
      >
        <div
          className={cn(
            "rounded-full bg-border transition-colors group-hover/divider:bg-muted-foreground/50 group-focus-visible/divider:bg-ring",
            across ? "h-12 w-1" : "h-1 w-12",
            moving !== null && "bg-muted-foreground/70",
          )}
        />
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{second}</div>
    </div>
  )
}
