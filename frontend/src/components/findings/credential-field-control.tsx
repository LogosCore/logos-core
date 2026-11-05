import { useContext, useState } from "react"
import { ArrowDownIcon, ArrowUpIcon, KeyIcon, PlusIcon, XIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CredentialPickerDialog } from "@/components/findings/credential-picker-dialog"
import {
  credentialTypeLabel,
  useCredentialTypeSpecs,
} from "@/components/findings/credential-type-utils"
import { useCredentialChip } from "@/graphql/hooks/credentials"
import { useScopedOperationId } from "@/hooks/use-scoped-operation"
import { credentialAccount, filledCredentialFields } from "@/lib/credential-fields"
import { cn } from "@/lib/utils"
import {
  CredentialOperationContext,
  acceptedCredentialTypes,
} from "@/components/findings/credential-field-context"
import type { Draft, FieldDef } from "@/components/modules/management/field-values"

type CredentialsDraft = Extract<Draft, { kind: "credentials" }>

/**
 * A credential or credential_list field: the operation's credentials picked
 * by name, of the types the field takes. A list keeps the order they are
 * tried in, which the arrows change.
 */
export function CredentialFieldControl({
  id,
  field,
  draft,
  onChange,
  disabled,
  invalid,
}: {
  id: string
  field: FieldDef
  draft: CredentialsDraft
  onChange: (draft: Draft) => void
  disabled?: boolean
  invalid?: boolean
}) {
  const scoped = useScopedOperationId()
  const operationId = useContext(CredentialOperationContext) ?? scoped
  const [picking, setPicking] = useState(false)
  const types = acceptedCredentialTypes(field)
  const max = draft.multiple ? (field.maxItems ?? 16) : 1
  const ids = draft.ids

  function set(next: string[]) {
    onChange({ ...draft, ids: next })
  }

  return (
    <div id={id} className="flex flex-col gap-1.5" aria-invalid={invalid || undefined}>
      {ids.map((cid, i) => (
        <PickedCredential
          key={cid}
          id={cid}
          disabled={disabled}
          onRemove={() => set(ids.filter((x) => x !== cid))}
          onUp={draft.multiple && i > 0 ? () => set(swap(ids, i, i - 1)) : undefined}
          onDown={draft.multiple && i < ids.length - 1 ? () => set(swap(ids, i, i + 1)) : undefined}
        />
      ))}
      {(ids.length < max || !draft.multiple) && (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled || !operationId}
            onClick={() => setPicking(true)}
          >
            <PlusIcon className="size-3.5" />
            {draft.multiple ? "Add credential" : ids.length > 0 ? "Change credential" : "Pick credential"}
          </Button>
          {types.length > 0 && (
            <span className="text-xs text-muted-foreground">
              {types.map((t) => credentialTypeLabel(t)).join(", ")}
            </span>
          )}
        </div>
      )}
      {operationId && (
        <CredentialPickerDialog
          open={picking}
          onOpenChange={setPicking}
          operationId={operationId}
          types={types}
          excludeIds={new Set(ids)}
          title={field.label || "Pick a credential"}
          description={
            types.length > 0
              ? `The operation's credentials this field takes: ${types.map((t) => credentialTypeLabel(t)).join(", ")}.`
              : "The operation's credentials."
          }
          createDescription="Record the credential in the operation's findings, then pick it here."
          createSubmitLabel="Create & pick"
          createIdPrefix={`${id}-new-credential`}
          onPick={(c) => {
            set(draft.multiple ? [...ids, c.id] : [c.id])
            setPicking(false)
          }}
        />
      )}
    </div>
  )
}

function swap(ids: string[], a: number, b: number): string[] {
  const next = [...ids]
  ;[next[a], next[b]] = [next[b], next[a]]
  return next
}

/** One picked credential: its name, type and account, or that it is gone. */
function PickedCredential({
  id,
  disabled,
  onRemove,
  onUp,
  onDown,
}: {
  id: string
  disabled?: boolean
  onRemove: () => void
  onUp?: () => void
  onDown?: () => void
}) {
  const { data, isLoading } = useCredentialChip(id)
  const specs = useCredentialTypeSpecs()
  const c = data?.credential
  const account = c ? credentialAccount(filledCredentialFields(c, specs.get(c.type)?.fields ?? [])) : ""
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-md border bg-muted/30 px-2 py-1 text-sm">
      <KeyIcon className="size-3.5 shrink-0 text-muted-foreground" />
      <span className={cn("min-w-0 flex-1 truncate", !c && !isLoading && "text-destructive")}>
        {c ? c.name : isLoading ? "…" : "Credential not found: deleted, or another operation's"}
      </span>
      {c && (
        <>
          {account && <span className="hidden max-w-[16ch] truncate text-xs text-muted-foreground sm:inline">{account}</span>}
          <Badge variant="outline">{credentialTypeLabel(c.type)}</Badge>
        </>
      )}
      {onUp && (
        <Button type="button" variant="ghost" size="icon-xs" onClick={onUp} disabled={disabled} aria-label="Try earlier">
          <ArrowUpIcon className="size-3.5" />
        </Button>
      )}
      {onDown && (
        <Button type="button" variant="ghost" size="icon-xs" onClick={onDown} disabled={disabled} aria-label="Try later">
          <ArrowDownIcon className="size-3.5" />
        </Button>
      )}
      <Button type="button" variant="ghost" size="icon-xs" onClick={onRemove} disabled={disabled} aria-label="Remove credential">
        <XIcon className="size-3.5" />
      </Button>
    </div>
  )
}

/** Read-only: the credentials a value names, by name. */
export function CredentialValueNames({ value }: { value: unknown }) {
  const ids = Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : typeof value === "string" ? [value] : []
  if (ids.length === 0) return <>not set</>
  return (
    <span className="inline-flex flex-wrap gap-1">
      {ids.map((id) => (
        <CredentialName key={id} id={id} />
      ))}
    </span>
  )
}

function CredentialName({ id }: { id: string }) {
  const { data, isLoading } = useCredentialChip(id)
  const c = data?.credential
  return (
    <Badge variant="outline" className="gap-1">
      <KeyIcon className="size-3" />
      {c ? c.name : isLoading ? "…" : "deleted credential"}
    </Badge>
  )
}
