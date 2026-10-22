import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { MaximizeIcon, MinusIcon, PlusIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { fitView, panBy, wheelZoomFactor, zoomAt, zoomTo, type View } from "./pan-zoom"

interface PanZoomProps {
  /** The content's natural size, in CSS pixels. */
  width: number
  height: number
  children: ReactNode
  /** A click or tap that did not drag, at a point of the content (its own
   * pixels at scale 1). */
  onTap?: (x: number, y: number) => void
  /** Whether the content point under the pointer can be clicked, for the
   * cursor. */
  clickableAt?: (x: number, y: number) => boolean
}

// A press that moves further than this is a drag.
const TAP_SLOP_PX = 5

/**
 * A viewport over content larger than the page, as Excalidraw's own canvas
 * behaves: drag to pan, wheel or trackpad to scroll, Ctrl/⌘ + wheel or a
 * pinch to zoom about the pointer, and buttons for zoom, fit and 100%.
 * Keyboard: + / − zoom, 0 fits, 1 is 100%, arrows pan.
 */
export function PanZoom({ width, height, children, onTap, clickableAt }: PanZoomProps) {
  const viewportRef = useRef<HTMLDivElement | null>(null)
  const [view, setView] = useState<View>({ scale: 1, x: 0, y: 0 })
  const fitted = useRef(false)

  const viewportSize = () => {
    const el = viewportRef.current
    return { width: el?.clientWidth ?? 0, height: el?.clientHeight ?? 0 }
  }
  const centre = () => {
    const s = viewportSize()
    return [s.width / 2, s.height / 2] as const
  }
  const fit = useCallback(() => setView(fitView({ width, height }, viewportSize())), [width, height])

  // Fit once the viewport has a size, before the first paint.
  useLayoutEffect(() => {
    if (fitted.current) return
    fitted.current = true
    fit()
  }, [fit])

  // Wheel needs a non-passive listener to keep the page from scrolling.
  useEffect(() => {
    const el = viewportRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const rect = el.getBoundingClientRect()
      if (e.ctrlKey || e.metaKey) {
        // Ctrl + wheel, and the pinch gesture trackpads report as one.
        setView((v) => zoomAt(v, wheelZoomFactor(e.deltaY, e.deltaMode), e.clientX - rect.left, e.clientY - rect.top))
      } else {
        const unit = e.deltaMode === 1 ? 16 : 1
        const dx = e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX
        const dy = e.shiftKey && !e.deltaX ? 0 : e.deltaY
        setView((v) => panBy(v, -dx * unit, -dy * unit))
      }
    }
    el.addEventListener("wheel", onWheel, { passive: false })
    return () => el.removeEventListener("wheel", onWheel)
  }, [])

  // The content point under a client point.
  const contentAt = (clientX: number, clientY: number) => {
    const rect = viewportRef.current!.getBoundingClientRect()
    return [(clientX - rect.left - view.x) / view.scale, (clientY - rect.top - view.y) / view.scale] as const
  }

  // Pointers: one drags, two pinch.
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const press = useRef<{ id: number; x: number; y: number } | null>(null)
  const [pointing, setPointing] = useState(false)
  const onPointerDown = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest("button")) return
    e.currentTarget.setPointerCapture(e.pointerId)
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    press.current = pointers.current.size === 1 && e.button === 0 ? { id: e.pointerId, x: e.clientX, y: e.clientY } : null
  }
  const onPointerMove = (e: React.PointerEvent) => {
    const map = pointers.current
    const prev = map.get(e.pointerId)
    if (!prev) {
      if (clickableAt) setPointing(clickableAt(...contentAt(e.clientX, e.clientY)))
      return
    }
    const next = { x: e.clientX, y: e.clientY }
    if (map.size === 1) {
      setView((v) => panBy(v, next.x - prev.x, next.y - prev.y))
    } else if (map.size === 2) {
      const other = [...map.entries()].find(([id]) => id !== e.pointerId)![1]
      const before = Math.hypot(prev.x - other.x, prev.y - other.y)
      const after = Math.hypot(next.x - other.x, next.y - other.y)
      const rect = e.currentTarget.getBoundingClientRect()
      if (before > 0) {
        setView((v) => zoomAt(v, after / before, (next.x + other.x) / 2 - rect.left, (next.y + other.y) / 2 - rect.top))
      }
    }
    map.set(e.pointerId, next)
  }
  const onPointerUp = (e: React.PointerEvent) => {
    pointers.current.delete(e.pointerId)
    const p = press.current
    press.current = null
    if (onTap && p?.id === e.pointerId && e.type === "pointerup" && Math.hypot(e.clientX - p.x, e.clientY - p.y) <= TAP_SLOP_PX) {
      onTap(...contentAt(e.clientX, e.clientY))
    }
  }

  const zoomBy = (factor: number) => setView((v) => zoomAt(v, factor, ...centre()))
  const onKeyDown = (e: React.KeyboardEvent) => {
    const step = 60
    switch (e.key) {
      case "+":
      case "=":
        zoomBy(1.25)
        break
      case "-":
        zoomBy(0.8)
        break
      case "0":
        fit()
        break
      case "1":
        setView((v) => zoomTo(v, 1, ...centre()))
        break
      case "ArrowLeft":
        setView((v) => panBy(v, step, 0))
        break
      case "ArrowRight":
        setView((v) => panBy(v, -step, 0))
        break
      case "ArrowUp":
        setView((v) => panBy(v, 0, step))
        break
      case "ArrowDown":
        setView((v) => panBy(v, 0, -step))
        break
      default:
        return
    }
    e.preventDefault()
  }

  const button =
    "flex h-7 min-w-7 items-center justify-center rounded-md px-1.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"

  return (
    <div
      ref={viewportRef}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onDoubleClick={(e) => {
        if ((e.target as HTMLElement).closest("button")) return
        const rect = e.currentTarget.getBoundingClientRect()
        setView((v) => zoomAt(v, 2, e.clientX - rect.left, e.clientY - rect.top))
      }}
      onKeyDown={onKeyDown}
      className={cn(
        "wiki-viewer-panzoom relative h-full min-h-80 touch-none overflow-hidden rounded-md outline-none select-none",
        pointing ? "cursor-pointer" : "cursor-grab active:cursor-grabbing",
      )}
      aria-label="Drawing. Drag to pan; Ctrl or ⌘ with the wheel, or pinch, to zoom."
    >
      <div
        className="absolute top-0 left-0 origin-top-left"
        style={{ width, height, transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
      >
        {children}
      </div>
      <div className="absolute bottom-3 left-3 flex items-center gap-0.5 rounded-lg border bg-card p-0.5 shadow-sm">
        <button type="button" className={button} onClick={() => zoomBy(0.8)} title="Zoom out (−)" aria-label="Zoom out">
          <MinusIcon className="size-3.5" />
        </button>
        <button
          type="button"
          className={`${button} w-12 tabular-nums`}
          onClick={() => setView((v) => zoomTo(v, 1, ...centre()))}
          title="Actual size (1)"
        >
          {Math.round(view.scale * 100)}%
        </button>
        <button type="button" className={button} onClick={() => zoomBy(1.25)} title="Zoom in (+)" aria-label="Zoom in">
          <PlusIcon className="size-3.5" />
        </button>
        <button type="button" className={button} onClick={fit} title="Fit to screen (0)" aria-label="Fit to screen">
          <MaximizeIcon className="size-3.5" />
        </button>
      </div>
    </div>
  )
}
