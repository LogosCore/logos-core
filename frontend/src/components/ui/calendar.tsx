import { useEffect, useRef, useState, type KeyboardEvent } from "react"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  CURSOR_ATTR,
  MONTHS,
  addDays,
  addMonths,
  clampDay,
  dayAllowed,
  dayLabel,
  inMonth,
  monthAllowed,
  monthGrid,
  monthOf,
  monthTitle,
  moveDay,
  moveInGrid,
  sameMonth,
  weekdays,
  yearAllowed,
  yearOf,
  yearPage,
  wheelSteps,
  type DayKey,
} from "@/lib/calendar"
import { cn } from "@/lib/utils"

// A month's calendar (lib/calendar.ts reckons it), the date pickers' heart
// (date-picker.tsx): a month's six weeks from Monday, today marked, the day
// picked filled, days past a bound dimmed and not to be picked. Its title
// zooms out to the year's months, then to twelve years, and back in as one
// is picked. The keyboard moves as a grid does: ← → ↑ ↓ by a day and a
// week, PageUp and PageDown by a month (a year with Shift), Home and End to
// the week's ends; Enter or Space picks. One element of it takes Tab: the
// day the focus is on (its cursor), which follows the day picked. The wheel
// over the days picks the day after or before, a step a notch, the page
// turning with it past a month's end.

type View = "days" | "months" | "years"

export function Calendar({
  value,
  onSelect,
  today,
  min,
  max,
  autoFocus,
}: {
  /** The day picked; null for none. */
  value: DayKey | null
  /** A day picked: by a click or a key (pick), or by the wheel (wheel), which goes on picking. */
  onSelect: (day: DayKey, how: "pick" | "wheel") => void
  /** Today: marked, and where the cursor starts without a day picked. */
  today: DayKey
  /** The first and last days that can be picked; none: no bound. */
  min?: DayKey | null
  max?: DayKey | null
  /** The cursor takes the focus as it shows (a picker's popup opening). */
  autoFocus?: boolean
}) {
  const [cursor, setCursor] = useState<DayKey>(() => clampDay(value ?? today, min, max))
  const [view, setView] = useState<View>("days")
  // A day picked from elsewhere (typed): the cursor goes to it.
  const [had, setHad] = useState(value)
  if (value !== had) {
    setHad(value)
    if (value) setCursor(clampDay(value, min, max))
  }
  const body = useRef<HTMLDivElement>(null)
  // The cursor's element takes the focus after a move by key, or a view
  // changed: a click elsewhere (a month on) leaves the focus where it is.
  const focusCursor = useRef(false)
  useEffect(() => {
    if (!focusCursor.current) return
    focusCursor.current = false
    body.current?.querySelector<HTMLElement>(`[${CURSOR_ATTR}]`)?.focus()
  }, [cursor, view])
  useEffect(() => {
    if (!autoFocus) return
    // After the popup's own focus, which comes as it opens.
    const id = requestAnimationFrame(() => {
      const el = body.current?.querySelector<HTMLElement>(`[${CURSOR_ATTR}]`)
      if (el && !body.current?.contains(document.activeElement)) el.focus()
    })
    return () => cancelAnimationFrame(id)
  }, [autoFocus])

  // The wheel over the days picks the day after or before, a step a notch,
  // the page following; the page under the popup does not scroll (a native
  // listener: React's onWheel is passive). Days past a bound are passed by
  // as the keys pass them: the wheel stops at the bound.
  const root = useRef<HTMLDivElement>(null)
  const gathered = useRef(0)
  // The day the wheel picked last, until the value shows it.
  const wheeled = useRef<DayKey | null>(null)
  useEffect(() => {
    const el = root.current
    wheeled.current = null
    if (!el || view !== "days") return
    const onWheel = (e: WheelEvent) => {
      // Pinching zooms, a sideways swipe is no day's.
      if (e.ctrlKey || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return
      e.preventDefault()
      const r = wheelSteps(e.deltaY, e.deltaMode, gathered.current)
      gathered.current = r.gathered
      if (!r.steps) return
      const from = wheeled.current ?? value ?? cursor
      const next = clampDay(addDays(from, r.steps), min, max)
      if (next === from) return
      wheeled.current = next
      setCursor(next)
      onSelect(next, "wheel")
    }
    el.addEventListener("wheel", onWheel, { passive: false })
    return () => el.removeEventListener("wheel", onWheel)
  }, [view, value, cursor, min, max, onSelect])

  const move = (k: DayKey, focus = true) => {
    setCursor(clampDay(k, min, max))
    focusCursor.current = focus
  }
  const zoom = (v: View) => {
    setView(v)
    focusCursor.current = true
  }

  const year = yearOf(cursor)
  const month = monthOf(cursor)
  const page = yearPage(year)
  const step = {
    days: { back: addMonths(cursor, -1), on: addMonths(cursor, 1), what: "month" },
    months: { back: addMonths(cursor, -12), on: addMonths(cursor, 12), what: "year" },
    years: { back: addMonths(cursor, -144), on: addMonths(cursor, 144), what: "twelve years" },
  }[view]
  // A step whose page has nothing to pick is none.
  const reachable = (k: DayKey) =>
    view === "days"
      ? monthAllowed(yearOf(k), monthOf(k), min, max)
      : view === "months"
        ? yearAllowed(yearOf(k), min, max)
        : yearPage(yearOf(k)).some((y) => yearAllowed(y, min, max))
  const title = view === "days" ? monthTitle(cursor) : view === "months" ? String(year) : `${page[0]} – ${page[11]}`

  return (
    <div ref={root} className="flex w-[16.75rem] flex-col gap-1.5 select-none">
      <div className="flex items-center justify-between gap-1">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={`The ${step.what} before`}
          title={`The ${step.what} before${view === "days" ? " (PageUp)" : ""}`}
          disabled={!reachable(step.back)}
          onClick={() => move(step.back, false)}
        >
          <ChevronLeftIcon />
        </Button>
        <button
          type="button"
          disabled={view === "years"}
          onClick={() => zoom(view === "days" ? "months" : "years")}
          title={view === "days" ? "Pick a month" : view === "months" ? "Pick a year" : undefined}
          aria-label={view === "years" ? title : `${title}: pick a ${view === "days" ? "month" : "year"}`}
          className="rounded-md px-2 py-1 text-sm font-medium tabular-nums transition-colors outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 disabled:hover:bg-transparent"
        >
          {title}
        </button>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={`The ${step.what} after`}
          title={`The ${step.what} after${view === "days" ? " (PageDown)" : ""}`}
          disabled={!reachable(step.on)}
          onClick={() => move(step.on, false)}
        >
          <ChevronRightIcon />
        </Button>
      </div>
      <div ref={body}>
        {view === "days" && (
          <Days
            cursor={cursor}
            value={value}
            today={today}
            min={min}
            max={max}
            onMove={move}
            onPick={(k) => {
              setCursor(k)
              onSelect(k, "pick")
            }}
          />
        )}
        {view === "months" && (
          <Choices
            label={`The months of ${year}`}
            columns={3}
            items={MONTHS.map((name, m) => ({
              key: m,
              text: name.slice(0, 3),
              label: `${name} ${year}`,
              cursor: m === month,
              picked: value !== null && yearOf(value) === year && monthOf(value) === m,
              now: yearOf(today) === year && monthOf(today) === m,
              allowed: monthAllowed(year, m, min, max),
            }))}
            onMove={(i) => move(inMonth(cursor, year, i))}
            onPick={(i) => {
              move(inMonth(cursor, year, i))
              zoom("days")
            }}
          />
        )}
        {view === "years" && (
          <Choices
            label={title}
            columns={3}
            items={page.map((y) => ({
              key: y,
              text: String(y),
              label: String(y),
              cursor: y === year,
              picked: value !== null && yearOf(value) === y,
              now: yearOf(today) === y,
              allowed: yearAllowed(y, min, max),
            }))}
            onMove={(i) => move(inMonth(cursor, page[i], month))}
            onPick={(i) => {
              move(inMonth(cursor, page[i], month))
              zoom("months")
            }}
          />
        )}
      </div>
    </div>
  )
}

/** A month's six weeks, from Monday. */
function Days({
  cursor,
  value,
  today,
  min,
  max,
  onMove,
  onPick,
}: {
  cursor: DayKey
  value: DayKey | null
  today: DayKey
  min?: DayKey | null
  max?: DayKey | null
  onMove: (k: DayKey) => void
  onPick: (k: DayKey) => void
}) {
  const days = monthGrid(cursor)
  const weeks = Array.from({ length: 6 }, (_, i) => days.slice(i * 7, i * 7 + 7))
  const onKeyDown = (e: KeyboardEvent) => {
    const next = moveDay(cursor, e.key, e.shiftKey)
    if (!next) return
    e.preventDefault()
    onMove(next)
  }
  return (
    <div role="grid" aria-label={monthTitle(cursor)} onKeyDown={onKeyDown} className="flex flex-col gap-0.5">
      <div role="row" className="grid grid-cols-7">
        {weekdays().map((w) => (
          <div key={w.long} role="columnheader" aria-label={w.long} title={w.long} className="py-1 text-center text-[0.7rem] font-medium text-muted-foreground">
            {w.short}
          </div>
        ))}
      </div>
      {weeks.map((week) => (
        <div key={week[0]} role="row" className="grid grid-cols-7 gap-0.5">
          {week.map((k) => {
            const picked = k === value
            const allowed = dayAllowed(k, min, max)
            const outside = !sameMonth(k, cursor)
            return (
              <button
                key={k}
                type="button"
                role="gridcell"
                tabIndex={k === cursor ? 0 : -1}
                {...(k === cursor ? { [CURSOR_ATTR]: "" } : {})}
                aria-label={dayLabel(k)}
                aria-selected={picked}
                aria-current={k === today ? "date" : undefined}
                disabled={!allowed}
                onClick={() => onPick(k)}
                className={cn(
                  "relative flex h-8 items-center justify-center rounded-md text-sm tabular-nums transition-colors outline-none",
                  "focus-visible:ring-2 focus-visible:ring-ring/60",
                  // One look each, the first that holds: past a bound, picked, the rest.
                  !allowed
                    ? "cursor-not-allowed text-muted-foreground/35"
                    : picked
                      ? "bg-primary font-semibold text-primary-foreground hover:bg-primary/90"
                      : cn("hover:bg-accent hover:text-accent-foreground", outside ? "text-muted-foreground" : "text-foreground"),
                  k === today && !picked && allowed && "font-semibold",
                  // Today's mark: a dot under its number, in its color.
                  k === today &&
                    "after:absolute after:bottom-1 after:left-1/2 after:size-1 after:-translate-x-1/2 after:rounded-full after:bg-current",
                )}
              >
                {Number(k.slice(8))}
              </button>
            )
          })}
        </div>
      ))}
    </div>
  )
}

interface Choice {
  key: number
  text: string
  label: string
  /** Where the focus is (the one Tab stop). */
  cursor: boolean
  /** The value's month or year. */
  picked: boolean
  /** Today's. */
  now: boolean
  allowed: boolean
}

/** A grid of months or years, columns wide. */
function Choices({
  label,
  columns,
  items,
  onMove,
  onPick,
}: {
  label: string
  columns: number
  items: Choice[]
  onMove: (i: number) => void
  onPick: (i: number) => void
}) {
  const at = items.findIndex((c) => c.cursor)
  const onKeyDown = (e: KeyboardEvent) => {
    const next = moveInGrid(at, e.key, columns, items.length)
    if (next === null) return
    e.preventDefault()
    onMove(next)
  }
  return (
    <div role="grid" aria-label={label} onKeyDown={onKeyDown} className="grid grid-cols-3 gap-1 pt-1">
      {items.map((c, i) => (
        <button
          key={c.key}
          type="button"
          role="gridcell"
          tabIndex={c.cursor ? 0 : -1}
          {...(c.cursor ? { [CURSOR_ATTR]: "" } : {})}
          aria-label={c.label}
          aria-selected={c.picked}
          disabled={!c.allowed}
          onClick={() => onPick(i)}
          className={cn(
            "h-[3.25rem] rounded-md text-sm tabular-nums transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
            !c.allowed
              ? "cursor-not-allowed text-muted-foreground/35"
              : c.picked
                ? "bg-primary font-semibold text-primary-foreground hover:bg-primary/90"
                : "text-foreground hover:bg-accent hover:text-accent-foreground",
            c.now && !c.picked && c.allowed && "font-semibold underline decoration-2 underline-offset-4",
          )}
        >
          {c.text}
        </button>
      ))}
    </div>
  )
}
