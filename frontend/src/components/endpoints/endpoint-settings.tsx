import type { ReactNode } from "react"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { FieldControl } from "@/components/modules/management/field-control"
import { groupByField, initialDraft, type Draft } from "@/components/modules/management/field-values"
import { ConnectionKindPicker } from "./connection-kind-picker"
import { connectionKind, type Kind } from "./endpoint-values"

// A connection's settings as one form, the same wherever they are set: the
// endpoint modal, and each connection of a build. The kind first (a switch,
// its flow played under it), then the caller's own rows (children: a name,
// a description), then the channel's settings for that kind, a section per
// group. Values live with the caller, by field key.

export function EndpointSettings({
  channel,
  kind,
  onKind,
  usable,
  why,
  kindError,
  drafts,
  onDraft,
  errorOf,
  disabled,
  idPrefix,
  sectionIdOf,
  children,
}: {
  channel: Kind
  /** The connection kind whose settings these are. */
  kind: string
  onKind: (kind: string) => void
  /** The kinds that may be picked; the channel's when absent. */
  usable?: readonly string[]
  /** Why a kind the channel carries cannot be picked here. */
  why?: (kind: string) => string | undefined
  kindError?: string
  drafts: Record<string, Draft>
  onDraft: (key: string, draft: Draft) => void
  /** A field's error, by its key. */
  errorOf: (key: string) => string | undefined
  disabled: boolean
  /** Prefixes the controls' ids, unique on the page. */
  idPrefix: string
  /** A group's section id, for a side list to jump to; none without. */
  sectionIdOf?: (group: string) => string
  /** The caller's own rows, between the kind and the settings. */
  children?: ReactNode
}) {
  const fields = connectionKind(channel, kind)?.fields ?? []
  return (
    <>
      <ConnectionKindPicker
        channel={channel}
        value={kind}
        onChange={onKind}
        disabled={disabled}
        error={kindError}
        usable={usable}
        why={why}
        idPrefix={idPrefix}
      />
      {children}
      {groupByField(fields, (f) => f).map((g) => (
        <SettingsSection key={g.group} id={sectionIdOf?.(g.group)} title={g.group}>
          {g.items.map((f) => {
            const id = `${idPrefix}-${f.key}`
            const error = errorOf(f.key)
            // A field the drafts lack (the kind just changed): its default.
            const draft = drafts[f.key] ?? initialDraft(f, f.default ?? null, { secretAsText: true })
            return (
              <SettingRow
                key={f.key}
                id={id}
                label={f.label}
                fieldKey={f.key}
                required={f.required}
                help={f.description ?? undefined}
                error={error}
              >
                <FieldControl
                  field={f}
                  id={id}
                  draft={draft}
                  onChange={(d) => onDraft(f.key, d)}
                  disabled={disabled}
                  invalid={!!error}
                  defaultValue={f.default}
                />
              </SettingRow>
            )
          })}
        </SettingsSection>
      ))}
    </>
  )
}
