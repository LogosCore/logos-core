import { useRef, useState, type KeyboardEvent } from "react"
import { cn } from "@/lib/utils"
import { ConnectionKindDiagram } from "./connection-kind-diagram"
import { KIND_FLOWS, KNOWN_KINDS } from "./connection-kind-flows"
import { kindLabels, kindNames, type Kind } from "./endpoint-values"

// The endpoint's connection kind, above the rest of the form (it decides
// which settings the form has): a segmented switch of the kinds, those the
// channel does not carry shown and disabled; under it the picked kind's
// flow, played, its steps lit in time with it.

export function ConnectionKindPicker({
  channel,
  value,
  onChange,
  disabled,
  error,
}: {
  channel: Kind
  value: string
  onChange: (kind: string) => void
  disabled: boolean
  error?: string
}) {
  const carried = kindNames(channel)
  // The protocol's kinds, then any other the channel names.
  const options = [...KNOWN_KINDS, ...carried.filter((k) => !KNOWN_KINDS.includes(k))]
  const usable = options.filter((k) => carried.includes(k))
  const buttons = useRef(new Map<string, HTMLButtonElement>())
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
    <section aria-labelledby="endpoint-kind-title" className="flex flex-col gap-3">
      <h2 id="endpoint-kind-title" className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Kind
      </h2>
      <div
        role="radiogroup"
        aria-labelledby="endpoint-kind-title"
        className="flex flex-wrap gap-1 self-start rounded-lg border bg-muted/40 p-1"
        onKeyDown={onKeyDown}
      >
        {options.map((k) => {
          const on = k === value
          const off = !carried.includes(k)
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
              title={off ? `The ${channel.name} channel does not carry it` : kindLabels[k]}
              onClick={() => !on && onChange(k)}
              className={cn(
                "flex flex-col items-start rounded-md px-3 py-1.5 text-left transition-colors disabled:cursor-not-allowed",
                on ? "bg-card shadow-sm ring-1 ring-border" : "hover:bg-card/60",
                off && "opacity-40",
              )}
            >
              <span className="text-sm">{KIND_FLOWS[k]?.label ?? k}</span>
              <span className="font-mono text-xs text-muted-foreground">{k}</span>
            </button>
          )
        })}
      </div>
      {flow && (
        <div className="grid items-center gap-4 rounded-xl border bg-card p-4 md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
          <ConnectionKindDiagram key={value} flow={flow} kind={value} onStep={setStep} />
          <div className="flex flex-col gap-1.5">
            <h3 className="text-sm font-medium">
              {flow.label}: {flow.sub}
            </h3>
            <ol className="flex list-decimal flex-col gap-1 pl-4 text-sm text-muted-foreground">
              {flow.steps.map((s, i) => (
                <li key={s} className={cn("transition-colors", i === step && "text-foreground")}>
                  {s}
                </li>
              ))}
            </ol>
          </div>
        </div>
      )}
      {error && <p className="text-sm text-destructive">{error}</p>}
    </section>
  )
}
