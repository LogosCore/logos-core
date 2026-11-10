import { useRef, useState, type KeyboardEvent } from "react"
import { ChevronRightIcon } from "lucide-react"
import { SettingRow } from "@/components/ui/settings-rows"
import { cn } from "@/lib/utils"
import { ConnectionKindDiagram } from "./connection-kind-diagram"
import { KIND_FLOWS } from "./connection-kind-flows"
import { kindLabels, kindNames, type Kind } from "./endpoint-values"

// A connection's kind, as a settings row (the rest of its settings follow
// from it): a segmented switch of the kinds the channel carries, those that
// cannot be picked here disabled with the reason; under it one sentence on
// the picked kind and "How it works", which opens its flow, played, its steps
// lit in time with it, at a size that does not grow with the form. The
// endpoint form and a build's connection both use it (EndpointSettings).

export function ConnectionKindPicker({
  channel,
  value,
  onChange,
  disabled,
  error,
  usable: allowed,
  why,
  idPrefix = "endpoint",
}: {
  channel: Kind
  value: string
  onChange: (kind: string) => void
  disabled: boolean
  error?: string
  /** The kinds that may be picked; the channel's when absent. */
  usable?: readonly string[]
  /** Why a kind the channel carries cannot be picked (a build's target lacks it). */
  why?: (kind: string) => string | undefined
  idPrefix?: string
}) {
  const carried = kindNames(channel)
  // The channel's kinds, and the one picked when the channel no longer carries it.
  const options = value && !carried.includes(value) ? [value, ...carried] : carried
  const usable = carried.filter((k) => !allowed || allowed.includes(k))
  const reason = (k: string) =>
    !carried.includes(k) ? `The ${channel.name} channel no longer carries it` : usable.includes(k) ? undefined : (why?.(k) ?? "Not usable here")
  const id = `${idPrefix}-kind`
  const buttons = useRef(new Map<string, HTMLButtonElement>())
  const [explained, setExplained] = useState(false)
  const [step, setStep] = useState<number | null>(null)
  const flow = KIND_FLOWS[value]

  const onKeyDown = (e: KeyboardEvent) => {
    if (!["ArrowRight", "ArrowLeft", "ArrowDown", "ArrowUp"].includes(e.key) || usable.length === 0) return
    e.preventDefault()
    const i = usable.indexOf(value)
    const next = usable[(i + (e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : usable.length - 1)) % usable.length]
    if (next !== value) onChange(next)
    buttons.current.get(next)?.focus()
  }

  return (
    <SettingRow
      id={id}
      label="Kind"
      error={error}
      hint={
        flow && (
          <div className="flex flex-col gap-2">
            <p className="text-xs text-muted-foreground">
              {flow.summary}{" "}
              <button
                type="button"
                className="inline-flex items-center gap-0.5 font-medium text-foreground/80 hover:text-foreground"
                aria-expanded={explained}
                aria-controls={`${id}-flow`}
                onClick={() => setExplained((on) => !on)}
              >
                How it works
                <ChevronRightIcon className={cn("size-3 transition-transform", explained && "rotate-90")} />
              </button>
            </p>
            {explained && (
              <div id={`${id}-flow`} className="flex flex-col gap-3 rounded-lg bg-muted/40 p-3">
                <ConnectionKindDiagram key={value} flow={flow} kind={value} onStep={setStep} className="max-w-sm" />
                <ol className="flex list-decimal flex-col gap-1 pl-4 text-xs text-muted-foreground">
                  {flow.steps.map((s, i) => (
                    <li key={s} className={cn("transition-colors", i === step && "text-foreground")}>
                      {s}
                    </li>
                  ))}
                </ol>
              </div>
            )}
          </div>
        )
      }
    >
      <div
        id={id}
        role="radiogroup"
        aria-label="Kind"
        className="flex flex-wrap gap-0.5 self-start rounded-lg border bg-muted/40 p-0.5"
        onKeyDown={onKeyDown}
      >
        {options.map((k) => {
          const on = k === value
          const off = reason(k) !== undefined
          return (
            <button
              key={k}
              ref={(el) => {
                if (el) buttons.current.set(k, el)
                else buttons.current.delete(k)
              }}
              type="button"
              role="radio"
              aria-checked={on}
              tabIndex={on ? 0 : -1}
              disabled={disabled || off}
              title={reason(k) ?? `${kindLabels[k] ?? k} (${k})`}
              onClick={() => !on && onChange(k)}
              className={cn(
                "h-6.5 rounded-md px-3 text-sm transition-colors disabled:cursor-not-allowed",
                on ? "bg-card shadow-sm ring-1 ring-border" : "text-muted-foreground hover:bg-card/60 hover:text-foreground",
                off && "opacity-40",
              )}
            >
              {KIND_FLOWS[k]?.label ?? k}
            </button>
          )
        })}
      </div>
    </SettingRow>
  )
}
