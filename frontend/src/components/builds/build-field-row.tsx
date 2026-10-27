import { SettingRow } from "@/components/ui/settings-rows"
import { FieldControl } from "@/components/modules/management/field-control"
import type { Draft, FieldDef } from "@/components/modules/management/field-values"

/** One field of the build form: label | control | help. */
export function FieldRow({
  idPrefix,
  field: f,
  draft,
  onChange,
  error,
  disabled,
}: {
  idPrefix: string
  field: FieldDef
  draft: Draft | undefined
  onChange: (d: Draft) => void
  error?: string
  disabled: boolean
}) {
  const id = `${idPrefix}-${f.key}`
  if (!draft) return null
  return (
    <SettingRow id={id} label={f.label} fieldKey={f.key} required={f.required} help={f.description ?? undefined} error={error}>
      <FieldControl field={f} id={id} draft={draft} onChange={onChange} disabled={disabled} invalid={!!error} defaultValue={f.default} />
    </SettingRow>
  )
}
