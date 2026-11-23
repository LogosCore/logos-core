import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { TimeColumns } from "@/components/ui/time-columns"
import { useNow } from "@/hooks/use-now"
import {
  boundOf,
  clampDay,
  clampTime,
  dayAllowed,
  dayDate,
  dayDistanceText,
  dayKey,
  dayTimeOf,
  distanceText,
  instantOf,
  joinDateTime,
  spanAllowed,
  splitDateTime,
  zoneText,
  type DayKey,
  type DayTime,
} from "@/lib/calendar"
import { DATE_TIME, formatDateTime } from "@/lib/date-format"
import { dateText } from "@/lib/date-time-text"

// The date pickers' popups (date-picker.tsx loads them as they first open,
// so the calendar costs nothing until then): a month's calendar, with a
// time's columns beside it for a date and time, and under them what is
// picked, how far from now, the browser's time zone, and the shortcuts.

/** A bound as the picker's own value (YYYY-MM-DD) or as an instant (date-picker.tsx's min and max). */
type Bound = Date | string | null | undefined

/** A date's popup: picking a day picks it and closes. */
export function DatePanel({
  value,
  onChange,
  min,
  max,
  required,
  onDone,
}: {
  /** YYYY-MM-DD, "" for none; other text (typed, no date) as none. */
  value: string
  onChange: (value: string) => void
  min?: Bound
  max?: Bound
  required?: boolean
  onDone: () => void
}) {
  const now = new Date(useNow(60_000))
  const today = dayKey(now)
  const day = dayDate(value) ? value : null
  const lo = dayOf(min)
  const hi = dayOf(max)
  const pick = (k: string) => {
    onChange(k)
    onDone()
  }
  return (
    <div className="flex flex-col gap-2">
      {/* A click or a key picks and closes; the wheel picks on, open. */}
      <Calendar value={day} today={today} min={lo} max={hi} autoFocus onSelect={(k, how) => (how === "wheel" ? onChange(k) : pick(k))} />
      <div className="flex items-center justify-between gap-2 border-t pt-2">
        <div className="min-w-0 text-xs leading-snug">
          {day ? (
            <>
              <div className="font-mono text-foreground tabular-nums">{dateText(day)}</div>
              <div className="text-muted-foreground">{dayDistanceText(day, today)}</div>
            </>
          ) : (
            <div className="text-muted-foreground">No date picked</div>
          )}
        </div>
        <div className="flex shrink-0 gap-1">
          {!required && value !== "" && (
            <Button type="button" variant="ghost" size="sm" onClick={() => pick("")}>
              Clear
            </Button>
          )}
          {dayAllowed(today, lo, hi) && (
            <Button type="button" variant="outline" size="sm" onClick={() => pick(today)}>
              Today
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

/** A date and time's popup: a day and a time picked keep it open until Done. */
export function DateTimePanel({
  value,
  onChange,
  min,
  max,
  required,
  seconds,
  onDone,
}: {
  /** YYYY-MM-DDTHH:MM[:SS], "" for none; other text (typed, no time) as none. */
  value: string
  onChange: (value: string) => void
  min?: Bound
  max?: Bound
  required?: boolean
  /** Seconds are picked too: else a value's own are kept, and a bound is met to the minute. */
  seconds?: boolean
  onDone: () => void
}) {
  const now = new Date(useNow(15_000))
  const today = dayKey(now)
  const picked = splitDateTime(value)
  // To the minute without seconds: a time picked never falls short of min.
  const lo = upToMinute(boundOf(min), seconds)
  const hi = boundOf(max)
  const loDay = lo ? dayKey(lo) : null
  const hiDay = hi ? dayKey(hi) : null
  // The time the columns show: the value's, else now's to the minute on
  // the day a time picked first goes to, within the bounds (today, or the
  // first time allowed).
  const shown: DayTime =
    picked ?? clampTime({ day: clampDay(today, loDay, hiDay), h: now.getHours(), m: now.getMinutes(), s: 0 }, lo, hi)
  const set = (t: DayTime) => onChange(joinDateTime(clampTime(t, lo, hi)))
  const at = picked ? instantOf(picked) : null
  const nowAllowed = (!lo || now >= lo) && (!hi || now <= hi)
  const { day, h, m } = shown
  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-3">
        <Calendar value={picked?.day ?? null} today={today} min={loDay} max={hiDay} autoFocus onSelect={(k) => set({ ...shown, day: k })} />
        {/* As tall as the calendar: the columns scroll within it. */}
        <div className={`relative border-l ${seconds ? "w-[9.5rem]" : "w-[6.5rem]"}`}>
          <div className="absolute inset-0 left-3">
            <TimeColumns
              h={h}
              m={m}
              s={shown.s}
              picked={picked !== null}
              seconds={seconds}
              onChange={(nh, nm, ns) => set({ day, h: nh, m: nm, s: ns })}
              hourAllowed={(v) => spanAllowed(day, [v, 0, 0], [v, 59, 59], lo, hi)}
              minuteAllowed={(v) => spanAllowed(day, [h, v, 0], [h, v, 59], lo, hi)}
              secondAllowed={(v) => spanAllowed(day, [h, m, v], [h, m, v], lo, hi)}
            />
          </div>
        </div>
      </div>
      <div className="flex items-center justify-between gap-3 border-t pt-2">
        <div className="min-w-0 text-xs leading-snug">
          {at ? (
            <div className="font-mono text-foreground tabular-nums">
              {formatDateTime(at, seconds || picked?.s ? DATE_TIME : "yyyy.mm.dd hh:MM")}
              <span className="ml-1.5 font-sans text-muted-foreground">{distanceText(at, now)}</span>
            </div>
          ) : (
            <div className="text-muted-foreground">No date and time picked</div>
          )}
          <div className="truncate text-muted-foreground" title="Times are in this browser's time zone">
            {zoneText(now)}
          </div>
        </div>
        <div className="flex shrink-0 gap-1">
          {!required && value !== "" && (
            <Button type="button" variant="ghost" size="sm" onClick={() => onChange("")}>
              Clear
            </Button>
          )}
          {nowAllowed && (
            <Button type="button" variant="ghost" size="sm" onClick={() => set(seconds ? dayTimeOf(now) : { ...dayTimeOf(now), s: 0 })}>
              Now
            </Button>
          )}
          <Button type="button" size="sm" onClick={onDone}>
            Done
          </Button>
        </div>
      </div>
    </div>
  )
}

/** A bound's day, for a date's picker: a day key as it is, else an instant's day. */
function dayOf(b: Bound): DayKey | null {
  if (typeof b === "string" && dayDate(b)) return b
  const at = boundOf(b)
  return at ? dayKey(at) : null
}

/** An instant to the minute, up (a bound met without seconds), unless seconds are kept. */
function upToMinute(at: Date | null, seconds?: boolean): Date | null {
  if (!at || seconds || (at.getSeconds() === 0 && at.getMilliseconds() === 0)) return at
  return new Date(Math.ceil(at.getTime() / 60_000) * 60_000)
}
