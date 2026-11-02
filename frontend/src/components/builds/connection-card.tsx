import { useState } from "react"
import { ArrowDownIcon, ArrowUpIcon, BookmarkPlusIcon, ChevronRightIcon, CopyIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { KindName } from "@/components/modules/module-icon"
import type { Draft } from "@/components/modules/management/field-values"
import { EndpointSettings } from "@/components/endpoints/endpoint-settings"
import { useChannelEndpointCount } from "@/graphql/hooks/channel-endpoints"
import { cn } from "@/lib/utils"
import { FieldRow } from "./build-field-row"
import { ConnectionFailoverSettings } from "./failover-editor"
import type { ConnectionFailover } from "./failover-values"
import {
  defaultConnectionName,
  kindLabels,
  paramFields,
  settingsSummary,
  type ChannelKind,
  type ConnectionDraft,
  type FactoryKind,
} from "./build-values"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"

/** An operation's endpoint, as a connection is filled from it. */
export interface EndpointOption {
  id: string
  name: string
  slug: string
  description?: string | null
  moduleName: string
  /** The connection kind its settings are for. */
  kind: string
  values?: unknown
  problems: readonly unknown[]
}

/**
 * One connection of the build form: its kind, its channel's settings for
 * that kind, filled in or from an endpoint, and the factory's parameters for
 * it. It folds to a line; one with errors stays open.
 */
export function ConnectionCard({
  operationId,
  index,
  count,
  connection: c,
  kind,
  kinds,
  channel,
  channelIcon,
  settings,
  errors,
  disabled,
  onKind,
  onName,
  onDescription,
  onSetting,
  onParam,
  onFill,
  onSave,
  onMove,
  onRemove,
  buildFailAfter,
  onFailover,
}: {
  /** The operation whose endpoints the connection can be filled from. */
  operationId: string
  index: number
  count: number
  connection: ConnectionDraft
  kind: FactoryKind
  /** The connection kinds the target and the channel both support, in the factory's order. */
  kinds: readonly string[]
  /** The channel kind; undefined when it declares no settings now. */
  channel: ChannelKind | undefined
  channelIcon?: ModuleIconSvgs
  /** The settings as the form submits them, for the folded line. */
  settings: Record<string, unknown>
  /** The form's errors, keyed like the server's problems. */
  errors: Record<string, string>
  disabled: boolean
  onKind: (kind: string) => void
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
  /** The build's failures-in-a-row, what the connection's own replaces. */
  buildFailAfter: number
  onFailover: (f: ConnectionFailover | undefined) => void
}) {
  // How many of the operation's endpoints the connection can be filled from:
  // the channel's, of a kind it can use.
  const endpointCount = useChannelEndpointCount(operationId, [c.moduleName], kinds)
  const [folded, setFolded] = useState(false)
  const at = `connections[${index}]`
  const hasError = Object.keys(errors).some((k) => k === at || k.startsWith(`${at}.`))
  const open = !folded || hasError
  const defaultName = defaultConnectionName(c.moduleName, index)
  const idPrefix = `connection-${c.key}`
  // The kind the build gets: the one picked, or the first both support.
  const effectiveKind = c.kind || kinds[0] || ""
  const params = paramFields(kind, c.moduleName, effectiveKind)
  const summary = settingsSummary(channel, effectiveKind, settings)
  // The folded line names the kind when there is a choice, or when the one
  // it has is no longer usable (another target, a channel that dropped it).
  const kindOptions = effectiveKind && !kinds.includes(effectiveKind) ? [effectiveKind, ...kinds] : kinds
  const askKind = kindOptions.length > 1

  // The connection's own rows: what the minion calls it, the team's note.
  const general = (
    <SettingsSection title="General">
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
    </SettingsSection>
  )

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
          {askKind && effectiveKind && (
            <span className="shrink-0 rounded border px-1.5 font-mono text-xs text-muted-foreground" title={kindLabels[effectiveKind]}>
              {effectiveKind}
            </span>
          )}
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
            title={
              endpointCount === 0
                ? `No ${c.moduleName} endpoints of a kind this connection can use in this operation`
                : "Replace the kind and settings with an endpoint's"
            }
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
        <div className="flex flex-col gap-6 border-t p-4">
          {channel ? (
            // The connection's settings, as the endpoint form has them.
            <EndpointSettings
              channel={channel}
              kind={effectiveKind}
              onKind={onKind}
              usable={kinds}
              why={() => "This target's minions do not use it"}
              kindError={errors[`${at}.kind`]}
              drafts={c.settings}
              onDraft={onSetting}
              errorOf={(key) => errors[`${at}.settings.${key}`]}
              disabled={disabled}
              idPrefix={`${idPrefix}-setting`}
            >
              {general}
            </EndpointSettings>
          ) : (
            general
          )}
          {params.length > 0 && (
            <SettingsSection title={`${kind.name} parameters`}>
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
            </SettingsSection>
          )}
          {count > 1 && (
            <ConnectionFailoverSettings
              idPrefix={`${idPrefix}-failover`}
              connKind={effectiveKind}
              value={c.failover}
              onChange={onFailover}
              error={errors[`${at}.failover.fail_after`]}
              disabled={disabled}
              buildFailAfter={buildFailAfter}
            />
          )}
        </div>
      )}
    </div>
  )
}
