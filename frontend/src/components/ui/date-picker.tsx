import { lazy, Suspense, useRef, useState } from "react"
import { CalendarIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { CURSOR_ATTR } from "@/lib/calendar"
import { dateText, dateTimeText, dateTimeValue, dateValue } from "@/lib/date-time-text"
import { cn } from "@/lib/utils"

// The app's date and date-and-time fields: what a date or a time is typed
// in, as the app writes them (2026.11.24, 2026.11.24 22:30: 24 hours, this
// browser's time zone), and a calendar to pick it from (its button, or
// Alt+↓ in the field). Values are what the browser's date and
// datetime-local inputs carry (YYYY-MM-DD, YYYY-MM-DDTHH:MM[:SS]; "" for
// none), so a form reads them as before; text that is no date goes as
// typed, for the form's own check to say so. The calendar's popup loads
// the first time one opens (date-picker-panel.tsx), fetched as the pointer
// nears its button.

const loadPanels = () => import("./date-picker-panel")
const DatePanel = lazy(() => loadPanels().then((m) => ({ default: m.DatePanel })))
const DateTimePanel = lazy(() => loadPanels().then((m) => ({ default: m.DateTimePanel })))

interface PickerProps {
  id?: string
  /** A hidden input carries the value in its form's data under name. */
  name?: string
  /** YYYY-MM-DD for a date, YYYY-MM-DDTHH:MM[:SS] for a date and time; "" for none. */
  value: string
  onChange: (value: string) => void
  /**
   * The first and last that can be picked: an RFC 3339 time, a value of
   * the picker's own form, or a Date; none: no bound. A day or a time past
   * one is dimmed and not to be picked; one typed is the form's to check.
   */
  min?: Date | string | null
  max?: Date | string | null
  disabled?: boolean
  invalid?: boolean
  /** No Clear: a value must be there. */
  required?: boolean
  placeholder?: string
  className?: string
  "aria-label"?: string
}

/** A date: 2026.11.24, typed or picked from a month's calendar. */
export function DatePicker(props: PickerProps) {
  return <Picker {...props} kind="date" />
}

/** A date and time: 2026.11.24 22:30, typed or picked from a calendar and a time's columns. */
export function DateTimePicker(props: PickerProps & { /** Seconds picked too. */ seconds?: boolean }) {
  return <Picker {...props} kind="datetime" />
}

function Picker({
  kind,
  id,
  name,
  value,
  onChange,
  min,
  max,
  disabled,
  invalid,
  required,
  placeholder,
  className,
  seconds,
  "aria-label": ariaLabel,
}: PickerProps & { kind: "date" | "datetime"; seconds?: boolean }) {
  const toText = kind === "date" ? dateText : dateTimeText
  const toValue = kind === "date" ? dateValue : dateTimeValue
  const [text, setText] = useState(() => toText(value))
  // A value from elsewhere (picked, reset, a form filled in) is shown,
  // unless it is what the text typed stands for already: typing is never
  // rewritten under the caret.
  const [had, setHad] = useState(value)
  if (value !== had) {
    setHad(value)
    if (toValue(text) !== value && text !== value) setText(toText(value))
  }
  const [open, setOpen] = useState(false)
  const popup = useRef<HTMLDivElement>(null)
  const panel = {
    value,
    onChange,
    min,
    max,
    required,
    onDone: () => setOpen(false),
  }
  return (
    <div className={cn("flex items-center gap-1", className)}>
      <Input
        id={id}
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          onChange(toValue(e.target.value) ?? e.target.value)
        }}
        // The calendar fetched while the field is in use: Alt+↓ opens it at once.
        onFocus={() => void loadPanels()}
        // Typed another way (2026-11-24 9:05): written the app's way once left.
        onBlur={() => {
          const v = toValue(text)
          if (v) setText(toText(v))
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && (e.altKey || e.metaKey)) {
            e.preventDefault()
            setOpen(true)
          }
        }}
        placeholder={placeholder ?? (kind === "date" ? "yyyy.mm.dd" : "yyyy.mm.dd hh:mm")}
        disabled={disabled}
        required={required}
        aria-invalid={invalid || undefined}
        aria-label={ariaLabel}
        autoComplete="off"
        spellCheck={false}
        className="min-w-0 flex-1 font-mono tabular-nums"
      />
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="outline"
              size="icon"
              disabled={disabled}
              aria-label={kind === "date" ? "Pick a date" : "Pick a date and time"}
              title={`${kind === "date" ? "Pick a date" : "Pick a date and time"} (Alt+↓ in the field)`}
              onPointerEnter={() => void loadPanels()}
              onFocus={() => void loadPanels()}
            >
              <CalendarIcon />
            </Button>
          }
        />
        <PopoverContent
          ref={popup}
          align="end"
          className="w-auto p-3"
          // The calendar's day (picked, else today) takes the focus as it opens.
          initialFocus={() => popup.current?.querySelector<HTMLElement>(`[${CURSOR_ATTR}]`) ?? true}
        >
          <Suspense fallback={<div className="flex h-72 w-72 items-center justify-center text-xs text-muted-foreground">Loading the calendar…</div>}>
            {kind === "date" ? <DatePanel {...panel} /> : <DateTimePanel {...panel} seconds={seconds} />}
          </Suspense>
        </PopoverContent>
      </Popover>
      {name && <input type="hidden" name={name} value={value} />}
    </div>
  )
}
