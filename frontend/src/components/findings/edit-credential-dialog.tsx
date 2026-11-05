import { type FormEvent, useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { useCredentialStore } from "@/stores/credentials"
import {
  useCredential,
  useCredentialTags,
  useUpdateCredential,
} from "@/graphql/hooks/credentials"
import {
  CredentialFormFields,
  type CredentialFormValues,
} from "@/components/findings/credential-form-fields"
import { CredentialValiditySelect } from "@/components/findings/credential-validity-utils"
import { keyDraftsFromWire } from "@/components/findings/credential-key-drafts"
import {
  propertyDraftsFromWire,
  propertyDraftsToInputs,
} from "@/components/findings/credential-property-drafts"
import {
  carryCredentialFieldsOver,
  credentialFieldDraftsFrom,
  credentialUpdateFields,
} from "@/components/findings/credential-form-values"
import {
  useCredentialTypeSpecs,
  type CredentialTypeSpecs,
} from "@/components/findings/credential-type-utils"
import type {
  CredentialFieldsFragment,
  CredentialType,
} from "@/graphql/gql/graphql"

export function EditCredentialDialog() {
  const editDialogOpen = useCredentialStore((s) => s.editDialogOpen)
  const selected = useCredentialStore((s) => s.selected)
  const closeEditDialog = useCredentialStore((s) => s.closeEditDialog)
  const convertTo = useCredentialStore((s) => s.editConvertTo)
  const { data, isLoading } = useCredential(selected?.id ?? "")
  const credential = data?.credential
  const specs = useCredentialTypeSpecs()

  return (
    <Dialog
      open={editDialogOpen}
      onOpenChange={(open) => {
        if (!open) closeEditDialog()
      }}
    >
      <DialogContent className="grid-rows-[auto_minmax(0,1fr)] max-h-[calc(100dvh-2rem)] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {convertTo ? "Convert credential" : "Edit credential"}
          </DialogTitle>
          <DialogDescription>
            {convertTo
              ? "Check its fields under the new type, then save."
              : "Update credential details, tags, and validity."}
          </DialogDescription>
        </DialogHeader>
        {isLoading || !credential || specs.size === 0 ? (
          <div className="py-8 text-center text-sm text-muted-foreground">
            Loading...
          </div>
        ) : (
          // Remount the form on credential id change so its initial state
          // re-seeds from the freshly loaded entity. Avoids setState-in-effect.
          <EditCredentialForm
            key={`${credential.id}:${convertTo ?? ""}`}
            credential={credential}
            specs={specs}
            convertTo={convertTo}
            onSaved={closeEditDialog}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

interface EditCredentialFormProps {
  credential: CredentialFieldsFragment
  specs: CredentialTypeSpecs
  /** Opened as "Convert to…": the type to start from, its fields seeded
   * from what the old shape holds. */
  convertTo: CredentialType | null
  onSaved: () => void
}

function EditCredentialForm({
  credential,
  specs,
  convertTo,
  onSaved,
}: EditCredentialFormProps) {
  const updateCredential = useUpdateCredential()
  // Tag suggestions are scoped to this credential's operation. React Query
  // dedupes against the toolbar's tag query, so opening the dialog while the
  // tab is mounted is a cache hit.
  const { data: tagsData, isLoading: tagsLoading } = useCredentialTags(
    credential.operationId,
  )
  const [initialFields] = useState(() => credentialFieldDraftsFrom(credential))
  const [values, setValues] = useState<CredentialFormValues>(() => {
    const initial: CredentialFormValues = {
      name: credential.name,
      type: credential.type,
      fields: initialFields,
      keys: keyDraftsFromWire(credential.keys),
      properties: propertyDraftsFromWire(credential.properties),
      validity: credential.validity,
      tags: credential.tags,
    }
    return convertTo && convertTo !== credential.type
      ? carryCredentialFieldsOver({ ...initial, type: convertTo }, specs.get(convertTo))
      : initial
  })
  const was = {
    type: credential.type,
    spec: specs.get(credential.type),
    fields: initialFields,
  }
  const spec = specs.get(values.type)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setError(null)
    try {
      await updateCredential.mutateAsync({
        id: credential.id,
        input: {
          name: values.name,
          ...credentialUpdateFields(values, spec, was),
          properties: propertyDraftsToInputs(values.properties),
          validity: values.validity,
          tags: values.tags,
        },
      })
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update credential")
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      autoComplete="off"
      className="flex min-h-0 flex-col"
    >
      {/* Body scrolls; header (parent) and footer stay pinned. The negative
          inset + padding keeps focus rings from being clipped by overflow. */}
      <div className="-mx-1 min-h-0 flex-1 overflow-y-auto px-1">
        {error && (
          <div className="mb-3 rounded-md bg-destructive/15 p-3 text-sm text-destructive">
            {error}
          </div>
        )}
        <CredentialFormFields
          idPrefix="edit-cred"
          values={values}
          onChange={setValues}
          specs={specs}
          was={{ spec: was.spec, legacyType: credential.legacyType }}
          tagSuggestions={tagsData?.credentialTags ?? []}
          tagSuggestionsLoading={tagsLoading}
        />
      </div>
      <DialogFooter className="mt-4 flex-row items-center justify-between sm:justify-between">
        <CredentialValiditySelect
          id="edit-cred-validity"
          value={values.validity}
          onValueChange={(validity) => setValues((v) => ({ ...v, validity }))}
        />
        <Button
          type="submit"
          disabled={updateCredential.isPending || !values.name.trim() || !spec}
        >
          {updateCredential.isPending ? "Saving..." : "Save changes"}
        </Button>
      </DialogFooter>
    </form>
  )
}
