import { useState } from "react"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { CollapsibleSection } from "@/components/ui/collapsible-section"
import { FieldControl } from "./field-control"
import { compareFields, emptyDraft, fieldApplies, type Draft, type FieldDef } from "./field-values"

/**
 * Form body for an action's params: label, description, control and error
 * per field. A param that does not apply as the form stands (its `when`: a
 * tunnel's host, a forward's alone) is not shown; one seldom changed
 * (`advanced`) is under More options, folded until opened, or until one of
 * them has a problem to show.
 */
export function ParamFields({
  params,
  drafts,
  onChange,
  errors,
  idPrefix,
  disabled,
}: {
  params: readonly FieldDef[]
  drafts: Record<string, Draft>
  onChange: (key: string, draft: Draft) => void
  errors: Record<string, string>
  idPrefix: string
  disabled?: boolean
}) {
  const [moreOpen, setMoreOpen] = useState(false)
  const shown = [...params].sort(compareFields).filter((f) => fieldApplies(f, params, drafts))
  const main = shown.filter((f) => !f.advanced)
  const more = shown.filter((f) => f.advanced)
  const open = moreOpen || more.some((f) => errors[f.key])

  const field = (f: FieldDef) => {
    const id = `${idPrefix}-${f.key}`
    const error = errors[f.key]
    return (
      <Field key={f.key} data-invalid={error ? true : undefined}>
        <FieldLabel htmlFor={id}>
          {f.label}
          {f.required && <span className="text-destructive">*</span>}
        </FieldLabel>
        {f.description && <FieldDescription>{f.description}</FieldDescription>}
        <FieldControl
          field={f}
          id={id}
          draft={drafts[f.key] ?? emptyDraft(f, { secretAsText: true })}
          onChange={(d) => onChange(f.key, d)}
          disabled={disabled}
          invalid={!!error}
        />
        {error && <FieldError>{error}</FieldError>}
      </Field>
    )
  }

  return (
    <FieldGroup className="gap-4">
      {main.map(field)}
      {more.length > 0 && (
        <CollapsibleSection title="More options" count={more.length} collapsible open={open} onOpenChange={setMoreOpen}>
          <FieldGroup className="gap-4">{more.map(field)}</FieldGroup>
        </CollapsibleSection>
      )}
    </FieldGroup>
  )
}
