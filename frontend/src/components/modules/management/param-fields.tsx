import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { FieldControl } from "./field-control"
import { compareFields, emptyDraft, type Draft, type FieldDef } from "./field-values"

/** Form body for an action's params: label, description, control and error per field. */
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
  const sorted = [...params].sort(compareFields)
  return (
    <FieldGroup className="gap-4">
      {sorted.map((f) => {
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
      })}
    </FieldGroup>
  )
}
