import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { PlusIcon, XIcon } from "lucide-react"
import { UserAgentGenerator } from "./user-agent-generator"
import {
  BYTE_UNITS,
  DURATION_HINT,
  byteUnitFactor,
  formatValue,
  humanizeBytes,
  type ByteUnit,
  type Draft,
  type FieldDef,
  type MapEntry,
} from "./field-values"

interface FieldControlProps {
  field: FieldDef
  draft: Draft
  onChange: (draft: Draft) => void
  id: string
  disabled?: boolean
  invalid?: boolean
  /**
   * The module's default, used when nothing is set — shown as the placeholder
   * (or the switch's resting state) so an empty control still says what
   * applies.
   */
  defaultValue?: unknown
  /** Secrets only: whether a value is stored. */
  secretSet?: boolean
}

/** One control per field type; the label, badges and errors live around it. */
export function FieldControl({
  field,
  draft,
  onChange,
  id,
  disabled,
  invalid,
  defaultValue,
  secretSet,
}: FieldControlProps) {
  const defaultText =
    defaultValue === null || defaultValue === undefined
      ? undefined
      : formatValue(field.type, defaultValue, field.unit)
  const placeholder = defaultText ?? field.placeholder ?? undefined

  switch (draft.kind) {
    case "bool":
      return (
        <div className="flex items-center gap-2">
          <Switch
            id={id}
            checked={draft.value ?? defaultValue === true}
            onCheckedChange={(checked) => onChange({ kind: "bool", value: checked })}
            disabled={disabled}
            aria-invalid={invalid || undefined}
          />
          <span className="text-sm text-muted-foreground">
            {(draft.value ?? defaultValue === true) ? "On" : "Off"}
            {draft.value === null && defaultText !== undefined && " (default)"}
          </span>
        </div>
      )

    case "secret":
      return (
        <SecretControl
          id={id}
          draft={draft}
          onChange={onChange}
          disabled={disabled}
          invalid={invalid}
          isSet={!!secretSet}
          placeholder={field.placeholder ?? undefined}
        />
      )

    case "bytes":
      return (
        <BytesControl
          id={id}
          draft={draft}
          onChange={onChange}
          disabled={disabled}
          invalid={invalid}
          placeholder={placeholder}
        />
      )

    case "map":
      return (
        <MapControl
          id={id}
          field={field}
          draft={draft}
          onChange={onChange}
          disabled={disabled}
          invalid={invalid}
          defaultText={defaultText}
        />
      )

    case "text":
      break
  }

  const text = draft.text
  const setText = (t: string) => onChange({ kind: "text", text: t })

  switch (field.type) {
    case "enum": {
      const options = field.options ?? []
      const defaultOption =
        typeof defaultValue === "string"
          ? options.find((o) => o.value === defaultValue)
          : undefined
      return (
        <Select
          value={text === "" ? null : text}
          onValueChange={(v) => setText(typeof v === "string" ? v : "")}
          disabled={disabled}
        >
          <SelectTrigger id={id} className="w-full max-w-72" aria-invalid={invalid || undefined}>
            <SelectValue
              placeholder={
                defaultOption
                  ? `${defaultOption.label || defaultOption.value} (default)`
                  : "Select…"
              }
            />
          </SelectTrigger>
          <SelectContent>
            {options.map((o) => (
              <SelectItem key={o.value} value={o.value} description={o.description}>
                {o.label || o.value}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
    }

    case "string_list":
      return (
        <Textarea
          id={id}
          value={text}
          onChange={(e) => setText(e.target.value)}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          rows={Math.min(8, Math.max(3, text.split("\n").length + 1))}
          placeholder={
            Array.isArray(defaultValue) && defaultValue.length > 0
              ? defaultValue.join("\n")
              : (field.placeholder ?? "One per line")
          }
          spellCheck={false}
          className="font-mono text-sm"
        />
      )

    case "duration":
      return (
        <div className="flex flex-col gap-1">
          <Input
            id={id}
            value={text}
            onChange={(e) => setText(e.target.value)}
            disabled={disabled}
            aria-invalid={invalid || undefined}
            placeholder={placeholder ?? "30s"}
            spellCheck={false}
            className="w-full max-w-72"
          />
          <span className="text-xs text-muted-foreground">{DURATION_HINT}</span>
        </div>
      )

    case "int":
    case "number":
      return (
        <div className="flex items-center gap-2">
          <Input
            id={id}
            value={text}
            onChange={(e) => setText(e.target.value)}
            disabled={disabled}
            aria-invalid={invalid || undefined}
            placeholder={placeholder}
            inputMode={field.type === "int" ? "numeric" : "decimal"}
            className="w-full max-w-48"
          />
          {field.unit && (
            <span className="text-sm text-muted-foreground">{field.unit}</span>
          )}
        </div>
      )

    default: {
      // string, url, secret-as-text (action params), and unknown types.
      const input = (
        <Input
          id={id}
          type={field.type === "secret" ? "password" : "text"}
          inputMode={field.type === "url" ? "url" : undefined}
          value={text}
          onChange={(e) => setText(e.target.value)}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          placeholder={field.type === "secret" ? (field.placeholder ?? undefined) : placeholder}
          maxLength={field.maxLength ?? undefined}
          autoComplete={field.type === "secret" ? "new-password" : "off"}
          spellCheck={false}
        />
      )
      // A helper for what the field holds, when the module says so.
      if (field.type === "string" && field.format === "user_agent") {
        return (
          <div className="flex items-center gap-2">
            <div className="min-w-0 flex-1">{input}</div>
            <UserAgentGenerator onUse={setText} disabled={disabled} />
          </div>
        )
      }
      return input
    }
  }
}

function BytesControl({
  id,
  draft,
  onChange,
  disabled,
  invalid,
  placeholder,
}: {
  id: string
  draft: Extract<Draft, { kind: "bytes" }>
  onChange: (d: Draft) => void
  disabled?: boolean
  invalid?: boolean
  placeholder?: string
}) {
  const amount = parseFloat(draft.text)
  const hint =
    draft.text.trim() !== "" && Number.isFinite(amount)
      ? `${(amount * byteUnitFactor(draft.unit)).toLocaleString()} bytes`
      : null
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        id={id}
        value={draft.text}
        onChange={(e) => onChange({ ...draft, text: e.target.value })}
        disabled={disabled}
        aria-invalid={invalid || undefined}
        placeholder={placeholder}
        inputMode="decimal"
        className="w-40"
      />
      <Select
        value={draft.unit}
        onValueChange={(v) => {
          if (typeof v === "string") onChange({ ...draft, unit: v as ByteUnit })
        }}
        disabled={disabled}
      >
        <SelectTrigger className="w-24" aria-label="Unit">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {BYTE_UNITS.map((u) => (
            <SelectItem key={u} value={u}>
              {u}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {hint && draft.unit !== "B" && (
        <span className="text-xs text-muted-foreground">= {hint}</span>
      )}
      {hint && draft.unit === "B" && amount >= 1024 && (
        <span className="text-xs text-muted-foreground">≈ {humanizeBytes(amount)}</span>
      )}
    </div>
  )
}

/**
 * string_map: one row per entry (key, value, remove), and "Add". Rows keep
 * their order while editing; a blank row is ignored on save.
 */
function MapControl({
  id,
  field,
  draft,
  onChange,
  disabled,
  invalid,
  defaultText,
}: {
  id: string
  field: FieldDef
  draft: Extract<Draft, { kind: "map" }>
  onChange: (d: Draft) => void
  disabled?: boolean
  invalid?: boolean
  defaultText?: string
}) {
  const entries = draft.entries
  const set = (next: MapEntry[]) => onChange({ kind: "map", entries: next })
  const update = (i: number, patch: Partial<MapEntry>) =>
    set(entries.map((e, j) => (j === i ? { ...e, ...patch } : e)))
  const full = field.maxItems != null && entries.length >= field.maxItems

  return (
    <div className="flex flex-col gap-2">
      {entries.length === 0 && (
        <span className="text-sm text-muted-foreground">
          {defaultText !== undefined && defaultText !== "(empty)" ? `Default: ${defaultText}` : "None"}
        </span>
      )}
      {entries.map((e, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input
            id={i === 0 ? id : undefined}
            value={e.key}
            onChange={(ev) => update(i, { key: ev.target.value })}
            placeholder="Key"
            aria-label="Key"
            disabled={disabled}
            aria-invalid={invalid || undefined}
            autoComplete="off"
            spellCheck={false}
            className="w-2/5 font-mono text-sm"
          />
          <Input
            value={e.value}
            onChange={(ev) => update(i, { value: ev.target.value })}
            placeholder="Value"
            aria-label={e.key ? `Value of ${e.key}` : "Value"}
            disabled={disabled}
            aria-invalid={invalid || undefined}
            autoComplete="off"
            spellCheck={false}
            className="min-w-0 flex-1 font-mono text-sm"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => set(entries.filter((_, j) => j !== i))}
            disabled={disabled}
            title="Remove"
            aria-label="Remove"
          >
            <XIcon />
          </Button>
        </div>
      ))}
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => set([...entries, { key: "", value: "" }])}
          disabled={disabled || full}
          title={full ? `At most ${field.maxItems}` : undefined}
        >
          <PlusIcon />
          Add
        </Button>
      </div>
    </div>
  )
}

function SecretControl({
  id,
  draft,
  onChange,
  disabled,
  invalid,
  isSet,
  placeholder,
}: {
  id: string
  draft: Extract<Draft, { kind: "secret" }>
  onChange: (d: Draft) => void
  disabled?: boolean
  invalid?: boolean
  isSet: boolean
  placeholder?: string
}) {
  const keep = () => onChange({ kind: "secret", mode: "keep", text: "" })

  if (draft.mode === "replace") {
    return (
      <div className="flex items-center gap-2">
        <Input
          id={id}
          type="password"
          value={draft.text}
          onChange={(e) => onChange({ ...draft, text: e.target.value })}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          placeholder={placeholder ?? "New value"}
          autoComplete="new-password"
          autoFocus
        />
        <Button type="button" variant="ghost" size="sm" onClick={keep}>
          Cancel
        </Button>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-2">
      {draft.mode === "clear" ? (
        <Badge variant="destructive">Will be cleared</Badge>
      ) : isSet ? (
        <Badge variant="secondary">Set</Badge>
      ) : (
        <Badge variant="outline">Not set</Badge>
      )}
      {draft.mode === "clear" ? (
        <Button type="button" variant="ghost" size="sm" onClick={keep} disabled={disabled}>
          Undo
        </Button>
      ) : (
        <Button
          id={id}
          type="button"
          variant="outline"
          size="sm"
          onClick={() => onChange({ kind: "secret", mode: "replace", text: "" })}
          disabled={disabled}
        >
          {isSet ? "Replace…" : "Set…"}
        </Button>
      )}
    </div>
  )
}
