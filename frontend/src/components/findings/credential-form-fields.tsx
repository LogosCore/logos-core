import { TriangleAlertIcon } from "lucide-react"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type {
  CredentialFieldSpec,
  CredentialType,
  CredentialTypeSpec,
} from "@/graphql/gql/graphql"
import {
  CREDENTIAL_TYPES,
  credentialTypeLabel,
  legacyCredentialTypeLabel,
  type CredentialTypeSpecs,
} from "@/components/findings/credential-type-utils"
import {
  carryCredentialFieldsOver,
  credentialTypeChangeDrops,
  type CredentialFormValues,
} from "@/components/findings/credential-form-values"
import type { CredentialValueField } from "@/lib/credential-fields"
import type { KeyDraft } from "@/components/findings/credential-key-drafts"
import { CredentialKeysEditor } from "@/components/findings/credential-keys-editor"
import type { PropertyDraft } from "@/components/findings/credential-property-drafts"
import { CredentialPropertiesEditor } from "@/components/findings/credential-properties-editor"
import { TagComboboxInput } from "@/components/findings/tag-combobox-input"

export type { CredentialFormValues } from "@/components/findings/credential-form-values"

interface CredentialFormFieldsProps {
  values: CredentialFormValues
  onChange: (next: CredentialFormValues) => void
  idPrefix: string
  /** The server's type table: which fields the selected type shows. */
  specs: CredentialTypeSpecs
  /** Editing: the type the record has now, so a change of type can say
   * what it would drop, and an OTHER the migration made its old type. */
  was?: { spec: CredentialTypeSpec | undefined; legacyType?: string | null }
  /** Existing tag pool used to populate the tag input dropdown. */
  tagSuggestions: string[]
  /** Suggestions query in-flight — drives the dropdown loading state. */
  tagSuggestionsLoading?: boolean
}

export function CredentialFormFields({
  values,
  onChange,
  idPrefix,
  specs,
  was,
  tagSuggestions,
  tagSuggestionsLoading = false,
}: CredentialFormFieldsProps) {
  const spec = specs.get(values.type)
  const drops = credentialTypeChangeDrops(values, spec, was?.spec)
  const legacyType =
    was?.legacyType && values.type === "OTHER" ? was.legacyType : null

  function patch(p: Partial<CredentialFormValues>) {
    onChange({ ...values, ...p })
  }
  function setField(field: CredentialValueField, value: string) {
    onChange({ ...values, fields: { ...values.fields, [field]: value } })
  }
  function setType(type: CredentialType) {
    onChange(carryCredentialFieldsOver({ ...values, type }, specs.get(type)))
  }

  // One-line fields pair up two to a row; blobs and the key list take the
  // whole width, in the type's order.
  const lineFields = spec?.fields.filter(
    (f) => f.kind === "TEXT" || f.kind === "SECRET" || f.kind === "DATETIME",
  )
  const wideFields = spec?.fields.filter(
    (f) => f.kind === "BLOB" || f.kind === "KEY_LIST",
  )

  return (
    <FieldGroup>
      {/* Row 1: Name takes 2/3, Type takes 1/3 — collapses to single column under sm. */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Field className="sm:col-span-2">
          <FieldLabel htmlFor={`${idPrefix}-name`}>Name</FieldLabel>
          <Input
            id={`${idPrefix}-name`}
            name="name"
            type="text"
            required
            value={values.name}
            onChange={(e) => patch({ name: e.target.value })}
            autoFocus
          />
        </Field>

        <Field>
          <FieldLabel htmlFor={`${idPrefix}-type`}>Type</FieldLabel>
          <Select
            value={values.type}
            onValueChange={(v) => setType(v as CredentialType)}
          >
            <SelectTrigger id={`${idPrefix}-type`} className="w-full">
              <SelectValue>
                {(v) => credentialTypeLabel(v as CredentialType)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {CREDENTIAL_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {credentialTypeLabel(t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>

      {spec && (
        <p className="-mt-2 text-sm text-muted-foreground">{spec.description}</p>
      )}
      {legacyType && (
        <p className="-mt-2 text-sm text-muted-foreground">
          Recorded as {legacyCredentialTypeLabel(legacyType)} before the types
          had fields of their own. Pick its type to convert it.
        </p>
      )}
      {drops.length > 0 && (
        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400">
          <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
          <span>
            A {credentialTypeLabel(values.type)} credential has no room for{" "}
            {drops.join(", ")}: saving drops {drops.length === 1 ? "it" : "them"}.
          </span>
        </div>
      )}

      {lineFields && lineFields.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {lineFields.map((f) => (
            <LineField
              key={f.field}
              spec={f}
              id={`${idPrefix}-${f.field.toLowerCase()}`}
              value={values.fields[f.field as CredentialValueField]}
              onChange={(v) => setField(f.field as CredentialValueField, v)}
            />
          ))}
        </div>
      )}

      {wideFields?.map((f) =>
        f.kind === "KEY_LIST" ? (
          <Field key={f.field}>
            <FieldLabel>{f.label}</FieldLabel>
            <CredentialKeysEditor
              keys={values.keys}
              onChange={(keys: KeyDraft[]) => onChange({ ...values, keys })}
            />
          </Field>
        ) : (
          <Field key={f.field}>
            <FieldLabel htmlFor={`${idPrefix}-${f.field.toLowerCase()}`}>
              {f.label}
            </FieldLabel>
            <Textarea
              id={`${idPrefix}-${f.field.toLowerCase()}`}
              rows={4}
              required={f.required}
              value={values.fields[f.field as CredentialValueField]}
              onChange={(e) =>
                setField(f.field as CredentialValueField, e.target.value)
              }
              placeholder={f.hint || undefined}
              spellCheck={false}
              className="font-mono text-xs"
            />
          </Field>
        ),
      )}

      <Field>
        <FieldLabel>Properties</FieldLabel>
        <CredentialPropertiesEditor
          properties={values.properties}
          onChange={(properties: PropertyDraft[]) =>
            onChange({ ...values, properties })
          }
        />
      </Field>

      <Field>
        <FieldLabel htmlFor={`${idPrefix}-tags-input`}>Tags</FieldLabel>
        <TagComboboxInput
          value={values.tags}
          onChange={(tags) => onChange({ ...values, tags })}
          suggestions={tagSuggestions}
          loading={tagSuggestionsLoading}
          inputId={`${idPrefix}-tags-input`}
        />
      </Field>
    </FieldGroup>
  )
}

function LineField({
  spec,
  id,
  value,
  onChange,
}: {
  spec: CredentialFieldSpec
  id: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{spec.label}</FieldLabel>
      <Input
        id={id}
        type={spec.kind === "DATETIME" ? "datetime-local" : "text"}
        required={spec.required}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={spec.hint || undefined}
        autoComplete="off"
        spellCheck={false}
        className={spec.kind === "SECRET" ? "font-mono" : undefined}
      />
    </Field>
  )
}
