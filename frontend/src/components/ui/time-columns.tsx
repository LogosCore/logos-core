import { useEffect, useLayoutEffect, useRef, type KeyboardEvent } from "react"
import { wheelSteps } from "@/lib/calendar"
import { cn } from "@/lib/utils"

// A time of day in 24 hours, as columns: hours, minutes, and seconds when
// asked. Each is a drum: the value picked always in its middle, the list
// moving under it. The wheel over a column picks the one after or before,
// a step a notch (the list never scrolls by itself); a click picks one; in
// a column ↑ and ↓ pick the one before and after, PageUp and PageDown five
// on, Home and End the first and last; Tab goes to the next column. A value
// past the picker's bounds is dimmed and passed over.

const pad = (n: number) => String(n).padStart(2, "0")

export function TimeColumns({
  h,
  m,
  s,
  picked,
  seconds,
  onChange,
  hourAllowed,
  minuteAllowed,
  secondAllowed,
}: {
  h: number
  m: number
  s: number
  /** h, m and s are a time picked: filled. Else they are where the columns start, shown softly. */
  picked: boolean
  /** A column for seconds too. */
  seconds?: boolean
  onChange: (h: number, m: number, s: number) => void
  hourAllowed: (h: number) => boolean
  minuteAllowed: (m: number) => boolean
  secondAllowed: (s: number) => boolean
}) {
  return (
    <div className="flex h-full min-h-0 gap-1">
      <Column label="Hours" short="Hr" count={24} value={h} picked={picked} allowed={hourAllowed} onPick={(v) => onChange(v, m, s)} />
      <Column label="Minutes" short="Min" count={60} value={m} picked={picked} allowed={minuteAllowed} onPick={(v) => onChange(h, v, s)} />
      {seconds && (
        <Column label="Seconds" short="Sec" count={60} value={s} picked={picked} allowed={secondAllowed} onPick={(v) => onChange(h, m, v)} />
      )}
    </div>
  )
}

function Column({
  label,
  short,
  count,
  value,
  picked: chosen,
  allowed,
  onPick,
}: {
  label: string
  short: string
  count: number
  value: number
  /** value is picked (filled), or only where the column starts (soft). */
  picked: boolean
  allowed: (v: number) => boolean
  onPick: (v: number) => void
}) {
  const list = useRef<HTMLDivElement>(null)
  // The value in the column's middle, always: as it opens at once, then
  // gliding there as it changes. The column moves, not the page.
  const placed = useRef(false)
  useLayoutEffect(() => {
    const col = list.current
    const item = col?.querySelector<HTMLElement>(`[data-v="${value}"]`)
    if (!col || !item) return
    col.scrollTo({ top: item.offsetTop - (col.clientHeight - item.offsetHeight) / 2, behavior: placed.current ? "smooth" : "instant" })
    placed.current = true
    // A value picked by key or wheel keeps the focus on it, where it was in the column.
    if (col.contains(document.activeElement) && document.activeElement !== item) item.focus({ preventScroll: true })
  }, [value])
  // The nearest allowed value from from, by steps that way; from itself when there is none.
  const step = (from: number, by: number) => {
    let v = from
    for (let n = 0; n < Math.abs(by); n++) {
      let next = v + Math.sign(by)
      while (next >= 0 && next < count && !allowed(next)) next += Math.sign(by)
      if (next < 0 || next >= count) break
      v = next
    }
    return v
  }
  const pick = (from: number, by: number) => {
    // The nearest allowed one that way, kept within the column.
    for (let v = from + by; v >= 0 && v < count; v += by > 0 ? 1 : -1) {
      if (allowed(v)) return onPick(v)
    }
  }
  // The wheel picks: a step a notch (a touchpad's deltas gathered), not a
  // scroll (a native listener, as React's onWheel is passive).
  const gathered = useRef(0)
  // The value the wheel picked last, until the value shows it.
  const wheeled = useRef<number | null>(null)
  useEffect(() => {
    const col = list.current
    wheeled.current = null
    if (!col) return
    const onWheel = (e: WheelEvent) => {
      if (e.ctrlKey) return
      e.preventDefault()
      const r = wheelSteps(e.deltaY, e.deltaMode, gathered.current)
      gathered.current = r.gathered
      if (!r.steps) return
      const from = wheeled.current ?? value
      const next = step(from, r.steps)
      if (next === from) return
      wheeled.current = next
      onPick(next)
    }
    col.addEventListener("wheel", onWheel, { passive: false })
    return () => col.removeEventListener("wheel", onWheel)
  })
  const onKeyDown = (e: KeyboardEvent) => {
    const to: Record<string, [number, number]> = {
      ArrowUp: [value, -1],
      ArrowDown: [value, 1],
      PageUp: [value, -5],
      PageDown: [value, 5],
      Home: [-1, 1],
      End: [count, -1],
    }
    const move = to[e.key]
    if (!move) return
    e.preventDefault()
    pick(move[0], move[1])
  }
  return (
    <div className="flex min-h-0 w-11 flex-col items-stretch gap-1">
      <div className="py-1 text-center text-[0.7rem] font-medium text-muted-foreground" title={label}>
        {short}
      </div>
      <div
        ref={list}
        role="listbox"
        aria-label={label}
        onKeyDown={onKeyDown}
        // A drum: no scroll of its own, its ends fading; room above and under
        // so the first and the last can sit in the middle too.
        className="relative min-h-0 flex-1 overflow-hidden rounded-md [mask-image:linear-gradient(to_bottom,transparent,black_28%,black_72%,transparent)]"
      >
        <div aria-hidden style={{ height: "calc(50% - 1rem)" }} />
        {Array.from({ length: count }, (_, v) => {
          const picked = v === value
          const ok = allowed(v)
          return (
            <button
              key={v}
              type="button"
              role="option"
              data-v={v}
              aria-selected={picked && chosen}
              aria-label={`${pad(v)} ${label.toLowerCase()}`}
              tabIndex={picked ? 0 : -1}
              disabled={!ok}
              onClick={() => onPick(v)}
              className={cn(
                "flex h-8 w-full items-center justify-center rounded-md font-mono text-sm tabular-nums transition-colors outline-none",
                "focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:ring-inset",
                // One look each, the first that holds: past a bound, picked,
                // where the column starts, the rest.
                !ok
                  ? "cursor-not-allowed text-muted-foreground/35"
                  : picked && chosen
                    ? "bg-primary font-semibold text-primary-foreground hover:bg-primary/90"
                    : picked
                      ? "bg-muted text-foreground hover:bg-accent hover:text-accent-foreground"
                      : "text-foreground hover:bg-accent hover:text-accent-foreground",
              )}
            >
              {pad(v)}
            </button>
          )
        })}
        <div aria-hidden style={{ height: "calc(50% - 1rem)" }} />
      </div>
    </div>
  )
}
