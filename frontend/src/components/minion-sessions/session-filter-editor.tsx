import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { MinionSessionFilterOp } from "@/graphql/gql/graphql"
import { useMinionSessionValues } from "@/graphql/hooks/minion-sessions"
import { cn } from "@/lib/utils"
import {
  FILTER_OPS,
  enumLabel,
  formatSeconds,
  isComplete,
  type SessionColumnDef,
  type SessionFilter,
} from "./sessions-layout"

// The filter of one Sessions column, edited in a popover from its header or
// its chip: the form follows what the column holds. Apply sets it, Clear
// removes it.

const OP_LABEL: Record<MinionSessionFilterOp, string> = {
  CONTAINS: "contains",
  NOT_CONTAINS: "does not contain",
  EMPTY: "is empty",
  NOT_EMPTY: "is not empty",
  IN: "is any of",
  NOT_IN: "is none of",
  RANGE: "between",
  WITHIN: "within the last",
  OLDER_THAN: "longer ago than",
  NEVER: "never",
  EQUALS: "is",
  LIVE: "is on time",
  LATE: "is late",
}

const WITHIN_PRESETS = [60, 300, 900, 3600, 86400]
const OLDER_PRESETS = [300, 3600, 86400, 7 * 86400]
const UNITS = [
  { label: "minutes", seconds: 60 },
  { label: "hours", seconds: 3600 },
  { label: "days", seconds: 86400 },
] as const

export function SessionFilterEditor({
  column,
  operationId,
  initial,
  onApply,
  onClear,
}: {
  column: SessionColumnDef
  operationId: string
  initial: SessionFilter | undefined
  onApply: (filter: SessionFilter) => void
  onClear: () => void
}) {
  const ops = FILTER_OPS[column.kind]
  const [draft, setDraft] = useState<SessionFilter>(initial ?? { column: column.id, op: ops[0] })
  const set = (patch: Partial<SessionFilter>) => setDraft((d) => ({ ...d, ...patch }))
  // Never, on time and late are Last seen's alone.
  const ops2 = column.id === "lastSeen" ? ops : ops.filter((o) => o !== "NEVER" && o !== "LIVE" && o !== "LATE")
  const ready = isComplete(draft, column)

  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault()
        if (ready) onApply(draft)
      }}
    >
      <div className="text-sm font-medium">
        {column.label} {ops2.length === 1 && <span className="font-normal text-muted-foreground">{OP_LABEL[ops2[0]]}</span>}
      </div>
      {ops2.length > 1 && (
        <Select value={draft.op} onValueChange={(op) => set({ op: op as MinionSessionFilterOp })}>
          <SelectTrigger className="w-full" aria-label="Condition">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ops2.map((op) => (
              <SelectItem key={op} value={op}>
                {OP_LABEL[op]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      {(draft.op === "CONTAINS" || draft.op === "NOT_CONTAINS") && (
        <Input
          autoFocus
          value={draft.text ?? ""}
          onChange={(e) => set({ text: e.target.value })}
          placeholder={column.kind === "list" ? "e.g. 10.0." : "Text, case aside"}
        />
      )}
      {draft.op === "EQUALS" && (
        <Input
          autoFocus
          className="font-mono text-xs"
          value={draft.text ?? ""}
          onChange={(e) => set({ text: e.target.value.trim() })}
          placeholder="A whole UUID"
        />
      )}
      {(draft.op === "IN" || draft.op === "NOT_IN") && (
        <ValueList column={column} operationId={operationId} values={draft.values ?? []} onChange={(values) => set({ values })} />
      )}
      {draft.op === "RANGE" && column.kind === "number" && (
        <div className="flex items-center gap-2">
          <NumberInput value={draft.min} onChange={(min) => set({ min })} placeholder="From" />
          <span className="text-muted-foreground">–</span>
          <NumberInput value={draft.max} onChange={(max) => set({ max })} placeholder="To" />
        </div>
      )}
      {draft.op === "RANGE" && column.kind === "time" && (
        <div className="flex flex-col gap-2">
          <DateTimeInput label="From" value={draft.from} onChange={(from) => set({ from })} />
          <DateTimeInput label="To" value={draft.to} onChange={(to) => set({ to })} />
        </div>
      )}
      {(draft.op === "WITHIN" || draft.op === "OLDER_THAN") && (
        <SecondsInput
          presets={draft.op === "WITHIN" ? WITHIN_PRESETS : OLDER_PRESETS}
          value={draft.seconds ?? undefined}
          onChange={(seconds) => set({ seconds })}
        />
      )}

      <div className="flex justify-between gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onClear} disabled={!initial}>
          Clear
        </Button>
        <Button type="submit" size="sm" disabled={!ready}>
          Apply
        </Button>
      </div>
    </form>
  )
}

/** The values to pick from: the operation's own, from core; yes / no for a flag. */
function ValueList({
  column,
  operationId,
  values,
  onChange,
}: {
  column: SessionColumnDef
  operationId: string
  values: string[]
  onChange: (values: string[]) => void
}) {
  const query = useMinionSessionValues(operationId, column.id, column.kind === "enum")
  const options = column.kind === "bool" ? ["true", "false"] : (query.data?.minionSessionValues ?? [])
  // A value picked earlier that no session has any more stays listed, so it
  // can be unpicked.
  const all = [...options, ...values.filter((v) => !options.includes(v))]
  if (query.isLoading) return <p className="text-xs text-muted-foreground">Loading values…</p>
  if (all.length === 0) return <p className="text-xs text-muted-foreground">No session has a value here yet.</p>
  return (
    <div className="flex max-h-56 flex-col gap-1 overflow-y-auto">
      {all.map((v) => (
        <label key={v} className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 hover:bg-muted">
          <Checkbox
            checked={values.includes(v)}
            onCheckedChange={(on) => onChange(on ? [...values, v] : values.filter((x) => x !== v))}
          />
          <span className={cn("truncate", column.kind !== "bool" && column.id !== "os" && "font-mono text-xs")}>
            {enumLabel(column.id, v)}
          </span>
        </label>
      ))}
    </div>
  )
}

function NumberInput({
  value,
  onChange,
  placeholder,
}: {
  value: number | null | undefined
  onChange: (v: number | undefined) => void
  placeholder: string
}) {
  return (
    <Input
      type="number"
      inputMode="numeric"
      value={value ?? ""}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
    />
  )
}

/** A local date and time, kept as RFC 3339 (UTC). */
function DateTimeInput({
  label,
  value,
  onChange,
}: {
  label: string
  value: string | null | undefined
  onChange: (v: string | undefined) => void
}) {
  const local = value ? toLocalInput(new Date(value)) : ""
  return (
    <label className="flex items-center gap-2 text-xs text-muted-foreground">
      <span className="w-8">{label}</span>
      <Input
        type="datetime-local"
        value={local}
        onChange={(e) => onChange(e.target.value ? new Date(e.target.value).toISOString().replace(/\.\d{3}Z$/, "Z") : undefined)}
      />
    </label>
  )
}

function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A span of time: a preset, or a number of minutes, hours or days. */
function SecondsInput({
  presets,
  value,
  onChange,
}: {
  presets: number[]
  value: number | undefined
  onChange: (seconds: number | undefined) => void
}) {
  const fitting = [...UNITS].reverse().find((u) => value && value % u.seconds === 0) ?? UNITS[0]
  const [unit, setUnit] = useState<number>(fitting.seconds)
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-1">
        {presets.map((p) => (
          <Button
            key={p}
            type="button"
            size="xs"
            variant={value === p ? "default" : "outline"}
            onClick={() => onChange(p)}
          >
            {formatSeconds(p)}
          </Button>
        ))}
      </div>
      <div className="flex items-center gap-2">
        <Input
          type="number"
          min={1}
          className="w-24"
          value={value ? value / unit : ""}
          onChange={(e) => onChange(e.target.value === "" ? undefined : Math.round(Number(e.target.value) * unit))}
        />
        <Select
          value={String(unit)}
          onValueChange={(u) => {
            const next = Number(u)
            setUnit(next)
            if (value) onChange(Math.round((value / unit) * next))
          }}
        >
          <SelectTrigger className="flex-1" aria-label="Unit">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {UNITS.map((u) => (
              <SelectItem key={u.seconds} value={String(u.seconds)}>
                {u.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  )
}
