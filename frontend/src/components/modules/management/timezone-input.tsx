import { useMemo } from "react"
import { SuggestionInput, type SuggestionOption } from "@/components/ui/suggestion-input"
import { browserTimezone, matchTimezones, timezoneNames, timezoneOffset, type Timezone } from "@/lib/timezones"

// A field that holds a time zone (protocol.FormatTimezone): typed as text,
// the browser's time zones suggested as it is (by any part of the name, or
// an offset: "kyiv", "new y", "utc+2"), each with its offset now, the
// browser's own first. A name typed whole is taken as it is: the module
// checks it.

export function TimezoneInput({
  id,
  value,
  onChange,
  disabled,
  invalid,
  placeholder,
  maxLength,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  invalid?: boolean
  placeholder?: string
  maxLength?: number
}) {
  const own = useMemo(() => browserTimezone(), [])
  // Their offsets as the form opens: a form is not open across a change.
  const zones = useMemo<Timezone[]>(() => {
    const now = new Date()
    return timezoneNames().map((name) => ({ name, offset: timezoneOffset(name, now) }))
  }, [])
  const options: SuggestionOption[] = matchTimezones(zones, value, own).map((z) => ({
    value: z.name,
    label: z.name,
    hint: z.name === own ? `${z.offset} · yours` : z.offset,
  }))
  return (
    <SuggestionInput
      id={id}
      freeText
      openOnFocus
      search={value}
      onSearchChange={onChange}
      selected={null}
      onSelect={() => {}}
      options={options}
      placeholder={placeholder}
      emptyMessage="No such time zone"
      disabled={disabled}
      invalid={invalid}
      maxLength={maxLength}
    />
  )
}
