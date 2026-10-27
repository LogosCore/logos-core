import { useState, type ReactNode } from "react"
import { ArrowDownIcon, ArrowUpIcon, ChevronRightIcon, PlusIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SettingRow } from "@/components/ui/settings-rows"
import { KindName, ModuleIcon } from "@/components/modules/module-icon"
import { groupByField, type Draft } from "@/components/modules/management/field-values"
import { settingRows } from "@/components/endpoints/endpoint-values"
import { cn } from "@/lib/utils"
import { FieldRow } from "./build-field-row"
import {
  channelUnusable,
  defaultConnectionName,
  type ChannelKind,
  type ConnectionDraft,
  type FactoryKind,
  type Target,
} from "./build-values"

/** An operation's endpoint, as a connection is filled from it. */
export interface EndpointOption {
  id: string
  name: string
  description?: string | null
  moduleName: string
  values?: unknown
  problems: readonly unknown[]
}

/**
 * One connection of the build form: its channel's settings, filled in or from
 * an endpoint, and the factory's parameters for it. It folds to a line; one
 * with errors stays open.
 */
export function ConnectionCard({
  index,
  count,
  connection: c,
  kind,
  channel,
  channelIcon,
  endpoints,
  settings,
  errors,
  disabled,
  partial,
  onName,
  onSetting,
  onParam,
  onFill,
  onMove,
  onRemove,
}: {
  index: number
  count: number
  connection: ConnectionDraft
  kind: FactoryKind
  /** The channel kind; undefined when it declares no settings now. */
  channel: ChannelKind | undefined
  channelIcon?: string
  /** The operation's endpoints of the connection's channel kind. */
  endpoints: readonly EndpointOption[]
  /** The settings as the form submits them, for the folded line. */
  settings: Record<string, unknown>
  /** The form's errors, keyed like the server's problems. */
  errors: Record<string, string>
  disabled: boolean
  partial: boolean
  onName: (name: string) => void
  onSetting: (key: string, d: Draft) => void
  onParam: (key: string, d: Draft) => void
  onFill: (endpoint: EndpointOption) => void
  onMove: (by: -1 | 1) => void
  onRemove: () => void
}) {
  const [folded, setFolded] = useState(false)
  const at = `connections[${index}]`
  const hasError = Object.keys(errors).some((k) => k === at || k.startsWith(`${at}.`))
  const open = !folded || hasError
  const defaultName = defaultConnectionName(c.moduleName, index)
  const params = kind.channelFields.get(c.moduleName) ?? []
  const summary = settingRows(channel ?? null, settings)
    .slice(0, 2)
    .map((r) => r.text)
    .join(" · ")
  const idPrefix = `connection-${c.key}`

  return (
    <div className="rounded-xl border bg-card" aria-label={`Connection ${index + 1}`} role="group">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-4 py-2">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-2 text-left disabled:cursor-default"
          onClick={() => setFolded(open)}
          disabled={hasError}
          aria-expanded={open}
          title={hasError ? "Fix the marked settings first" : open ? "Fold" : "Unfold"}
        >
          <ChevronRightIcon className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
          <span className="text-xs text-muted-foreground tabular-nums">{index + 1}.</span>
          <KindName type="channel" name={c.moduleName} icon={channelIcon} className="shrink-0 text-sm" />
          <span className="text-muted-foreground">·</span>
          <span className={cn("truncate font-mono text-sm font-medium", !c.name.trim() && "text-muted-foreground")}>
            {c.name.trim() || defaultName}
          </span>
          {open
            ? c.filledFrom && <span className="truncate text-xs text-muted-foreground">filled from {c.filledFrom}</span>
            : summary && <span className="truncate font-mono text-xs text-muted-foreground">{summary}</span>}
        </button>
        <div className="flex items-center gap-1">
          <Select
            value={null}
            onValueChange={(v) => {
              const ep = endpoints.find((e) => e.id === v)
              if (ep) onFill(ep)
            }}
            disabled={disabled || !channel || endpoints.length === 0}
          >
            <SelectTrigger size="sm" aria-label="Fill the settings from an endpoint">
              <SelectValue placeholder={endpoints.length === 0 ? `No ${c.moduleName} endpoints` : "Fill from endpoint…"} />
            </SelectTrigger>
            <SelectContent>
              {endpoints.map((e) => (
                <SelectItem
                  key={e.id}
                  value={e.id}
                  description={
                    e.problems.length > 0 ? "Doesn't fit the channel's current settings: check them after filling" : (e.description ?? undefined)
                  }
                >
                  {e.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="button" variant="ghost" size="icon-sm" title="Earlier" aria-label="Earlier" disabled={disabled || index === 0} onClick={() => onMove(-1)}>
            <ArrowUpIcon />
          </Button>
          <Button type="button" variant="ghost" size="icon-sm" title="Later" aria-label="Later" disabled={disabled || index === count - 1} onClick={() => onMove(1)}>
            <ArrowDownIcon />
          </Button>
          <Button type="button" variant="ghost" size="icon-sm" title="Remove" aria-label="Remove" disabled={disabled} onClick={onRemove}>
            <XIcon />
          </Button>
        </div>
      </div>
      {errors[at] && <p className="border-t px-4 py-2 text-sm text-destructive">{errors[at]}</p>}
      {c.notes.length > 0 && (
        <div className="border-t bg-amber-500/10 px-4 py-2 text-sm text-amber-800 dark:text-amber-300">
          {c.notes.map((n) => (
            <p key={n}>{n}</p>
          ))}
        </div>
      )}
      {open && (
        <div className="divide-y border-t px-4">
          <SettingRow
            id={`${idPrefix}-name`}
            label="Name"
            help={`What the minion calls this connection. Left empty, it is ${defaultName}.`}
            error={errors[`${at}.name`]}
          >
            <Input
              id={`${idPrefix}-name`}
              value={c.name}
              onChange={(e) => onName(e.target.value)}
              placeholder={defaultName}
              autoComplete="off"
              spellCheck={false}
              disabled={disabled}
              aria-invalid={!!errors[`${at}.name`] || undefined}
            />
          </SettingRow>
          {channel &&
            groupByField(channel.fields, (f) => f).map((g) => (
              <Group key={g.group} title={g.group}>
                {g.items.map((f) => (
                  <FieldRow
                    key={f.key}
                    idPrefix={`${idPrefix}-setting`}
                    field={f}
                    draft={c.settings[f.key]}
                    onChange={(d) => onSetting(f.key, d)}
                    error={errors[`${at}.settings.${f.key}`]}
                    disabled={disabled}
                    partial={partial}
                  />
                ))}
              </Group>
            ))}
          {params.length > 0 && (
            <Group title={`${kind.name} parameters`}>
              {params.map((f) => (
                <FieldRow
                  key={f.key}
                  idPrefix={`${idPrefix}-param`}
                  field={f}
                  draft={c.drafts[f.key]}
                  onChange={(d) => onParam(f.key, d)}
                  error={errors[`${at}.values.${f.key}`]}
                  disabled={disabled}
                  partial={partial}
                />
              ))}
            </Group>
          )}
        </div>
      )}
    </div>
  )
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="pt-3">
      <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{title}</h3>
      <div className="divide-y">{children}</div>
    </div>
  )
}

/**
 * Adds a connection: blank, through a channel the target speaks, or filled
 * from one of the operation's endpoints.
 */
export function AddConnectionMenu({
  target,
  channels,
  channelIcons,
  endpoints,
  disabled,
  onAdd,
}: {
  target: Target | undefined
  channels: ReadonlyMap<string, ChannelKind>
  channelIcons: ReadonlyMap<string, string>
  endpoints: readonly EndpointOption[]
  disabled: boolean
  onAdd: (moduleName: string, endpoint: EndpointOption | null) => void
}) {
  const speaks = (target?.channels ?? []).map((c) => ({ name: c.moduleName, why: channelUnusable(target, c.moduleName, channels) }))
  const usable = new Set(speaks.filter((c) => c.why === null).map((c) => c.name))
  const fillable = endpoints.filter((e) => usable.has(e.moduleName))
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button type="button" variant="outline" className="self-start" disabled={disabled || speaks.length === 0}>
            <PlusIcon />
            Add connection
          </Button>
        }
      />
      <DropdownMenuContent align="start" className="min-w-64">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Blank</DropdownMenuLabel>
          {speaks.map((c) => (
            <DropdownMenuItem key={c.name} disabled={c.why !== null} onClick={() => onAdd(c.name, null)}>
              <ModuleIcon type="channel" icon={channelIcons.get(c.name)} />
              <span className="flex min-w-0 flex-col">
                <span className="font-mono">{c.name}</span>
                {c.why && <span className="text-xs whitespace-normal text-muted-foreground">{c.why}</span>}
              </span>
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        {fillable.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>From an endpoint</DropdownMenuLabel>
              {fillable.map((e) => (
                <DropdownMenuItem key={e.id} onClick={() => onAdd(e.moduleName, e)}>
                  <ModuleIcon type="channel" icon={channelIcons.get(e.moduleName)} />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{e.name}</span>
                    <span className="truncate text-xs text-muted-foreground">
                      {e.moduleName}
                      {e.problems.length > 0 && " · doesn't fit the current settings"}
                    </span>
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
