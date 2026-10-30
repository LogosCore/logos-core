import { useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react"
import { cn } from "@/lib/utils"

// Two panes stacked, with a divider between them the user drags to share the
// height: with the pointer, with the arrow keys once it has focus (Home and
// End go to the ends), and back to the default on a double click. While it
// moves only this component re-renders; onChange gets the new share when the
// move ends, to keep.

const KEY_STEP = 0.05

export function SplitPane({
  top,
  bottom,
  split,
  defaultSplit,
  min = 0.1,
  max = 0.9,
  onChange,
  label,
  className,
}: {
  top: ReactNode
  bottom: ReactNode
  /** The share of the height the top pane takes, 0 to 1. */
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
  const current = moving ?? split
  const clamp = (v: number) => Math.min(max, Math.max(min, v))

  const at = (e: PointerEvent) => {
    const rect = box.current!.getBoundingClientRect()
    return clamp((e.clientY - rect.top) / rect.height)
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
    if (end !== split) onChange(end)
  }
  const onKeyDown = (e: KeyboardEvent) => {
    const next = {
      ArrowUp: clamp(current - KEY_STEP),
      ArrowDown: clamp(current + KEY_STEP),
      Home: min,
      End: max,
    }[e.key]
    if (next === undefined) return
    e.preventDefault()
    if (next !== split) onChange(next)
  }

  return (
    <div ref={box} className={cn("flex min-h-0 flex-1 flex-col", moving !== null && "cursor-row-resize select-none", className)}>
      <div className="flex min-h-0 flex-col" style={{ flexBasis: `${current * 100}%`, flexGrow: 0, flexShrink: 0 }}>
        {top}
      </div>
      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label={label}
        aria-valuemin={Math.round(min * 100)}
        aria-valuemax={Math.round(max * 100)}
        aria-valuenow={Math.round(current * 100)}
        tabIndex={0}
        title="Drag to resize; double-click to reset"
        className="group/divider flex h-3 shrink-0 cursor-row-resize touch-none items-center justify-center outline-none"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={() => setMoving(null)}
        onKeyDown={onKeyDown}
        onDoubleClick={() => defaultSplit !== split && onChange(defaultSplit)}
      >
        <div
          className={cn(
            "h-1 w-12 rounded-full bg-border transition-colors group-hover/divider:bg-muted-foreground/50 group-focus-visible/divider:bg-ring",
            moving !== null && "bg-muted-foreground/70",
          )}
        />
      </div>
      <div className="flex min-h-0 flex-1 flex-col">{bottom}</div>
    </div>
  )
}
