import { useState, type ReactNode } from "react"
import { ArrowDownIcon, ArrowUpIcon, BookmarkPlusIcon, ChevronRightIcon, CopyIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { SettingRow } from "@/components/ui/settings-rows"
import { KindName } from "@/components/modules/module-icon"
import { groupByField, type Draft } from "@/components/modules/management/field-values"
import { useChannelEndpointCount } from "@/graphql/hooks/channel-endpoints"
import { cn } from "@/lib/utils"
import { FieldRow } from "./build-field-row"
import { defaultConnectionName, settingsSummary, type ChannelKind, type ConnectionDraft, type FactoryKind } from "./build-values"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"

/** An operation's endpoint, as a connection is filled from it. */
export interface EndpointOption {
  id: string
  name: string
  slug: string
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
  operationId,
  index,
  count,
  connection: c,
  kind,
  channel,
  channelIcon,
  settings,
  errors,
  disabled,
  onName,
  onDescription,
  onSetting,
  onParam,
  onFill,
  onSave,
  onMove,
  onRemove,
}: {
  /** The operation whose endpoints the connection can be filled from. */
  operationId: string
  index: number
  count: number
  connection: ConnectionDraft
  kind: FactoryKind
  /** The channel kind; undefined when it declares no settings now. */
  channel: ChannelKind | undefined
  channelIcon?: ModuleIconSvgs
  /** The settings as the form submits them, for the folded line. */
  settings: Record<string, unknown>
  /** The form's errors, keyed like the server's problems. */
  errors: Record<string, string>
  disabled: boolean
  onName: (name: string) => void
  onDescription: (description: string) => void
  onSetting: (key: string, d: Draft) => void
  onParam: (key: string, d: Draft) => void
  /** Asks for an endpoint to fill the settings from. */
  onFill: () => void
  /** Asks to save the settings as a new endpoint. */
  onSave: () => void
  onMove: (by: -1 | 1) => void
  onRemove: () => void
}) {
  // How many of the operation's endpoints the connection can be filled from.
  const endpointCount = useChannelEndpointCount(operationId, [c.moduleName])
  const [folded, setFolded] = useState(false)
  const at = `connections[${index}]`
  const hasError = Object.keys(errors).some((k) => k === at || k.startsWith(`${at}.`))
  const open = !folded || hasError
  const defaultName = defaultConnectionName(c.moduleName, index)
  const params = kind.channelFields.get(c.moduleName) ?? []
  const summary = settingsSummary(channel, settings)
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
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled || !channel || endpointCount === 0}
            title={endpointCount === 0 ? `No ${c.moduleName} endpoints in this operation` : "Replace the settings with an endpoint's"}
            onClick={onFill}
          >
            <CopyIcon />
            Fill from endpoint…
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled || !channel}
            title="Keep these settings as an endpoint of the operation, to fill connections from later"
            onClick={onSave}
          >
            <BookmarkPlusIcon />
            Save as endpoint…
          </Button>
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
          <SettingRow
            id={`${idPrefix}-description`}
            label="Description"
            help="A note for your team on what this connection is for. Core keeps it with the build; the factory and the minion never get it."
            error={errors[`${at}.description`]}
          >
            <Textarea
              id={`${idPrefix}-description`}
              value={c.description}
              onChange={(e) => onDescription(e.target.value)}
              rows={2}
              maxLength={500}
              disabled={disabled}
              aria-invalid={!!errors[`${at}.description`] || undefined}
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
