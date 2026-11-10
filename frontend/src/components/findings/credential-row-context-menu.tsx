import type { ReactElement, ReactNode } from "react"
import { toast } from "sonner"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import {
  ArrowRightLeftIcon,
  CopyIcon,
  KeyIcon,
  LinkIcon,
  PencilIcon,
  TagIcon,
  TrashIcon,
} from "lucide-react"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { useCredentialStore } from "@/stores/credentials"
import { CredentialValidityIcon } from "@/components/findings/credential-validity-utils"
import { CREDENTIAL_VALIDITIES, credentialValidity } from "@/components/findings/credential-validity-values"
import { useUpdateCredential } from "@/graphql/hooks/credentials"
import {
  CREDENTIAL_TYPES,
  credentialTypeLabel,
  legacyCredentialTypeSuccessor,
  useCredentialTypeSpecs,
} from "@/components/findings/credential-type-utils"
import { filledCredentialFields } from "@/lib/credential-fields"
import { buildCredentialShareUrl } from "@/components/findings/credential-share-link"
import type {
  CredentialChipFieldsFragment,
  CredentialValidity,
} from "@/graphql/gql/graphql"

interface CredentialRowContextMenuProps {
  credential: CredentialChipFieldsFragment
  children: ReactNode
  // Optional override for the trigger's host element. Base UI's
  // ContextMenu.Trigger defaults to a <div>, which is fine for full-width row
  // buttons but breaks inline placement (e.g. the wiki credential chip sitting
  // between prose tokens). Pass `<span />` from inline call sites.
  triggerRender?: ReactElement
}

// Right-click context menu wrapper for a single credentials row. Holds all
// per-row actions: toggle validity, copy each field the type holds, convert
// an OTHER, edit, delete. The trigger is rendered as the row passed via
// children.
export function CredentialRowContextMenu({
  credential,
  children,
  triggerRender,
}: CredentialRowContextMenuProps) {
  const openEdit = useCredentialStore((s) => s.openEditDialog)
  const openConvert = useCredentialStore((s) => s.openConvertDialog)
  const openDelete = useCredentialStore((s) => s.openDeleteDialog)
  const updateCredential = useUpdateCredential()
  const specs = useCredentialTypeSpecs()
  const filled = filledCredentialFields(
    credential,
    specs.get(credential.type)?.fields ?? [],
  )
  const suggested = legacyCredentialTypeSuccessor(credential.legacyType)
  const convertTargets = CREDENTIAL_TYPES.filter((t) => t !== "OTHER")
  if (suggested) {
    convertTargets.sort((a, b) => Number(b === suggested) - Number(a === suggested))
  }


  async function copyShareLink() {
    try {
      await navigator.clipboard.writeText(buildCredentialShareUrl(credential.id))
      toast.success("Link copied")
    } catch {
      toast.error("Failed to copy link")
    }
  }

  async function setValidity(validity: CredentialValidity) {
    try {
      await updateCredential.mutateAsync({
        id: credential.id,
        input: { validity },
      })
      toast.success(`Marked as ${credentialValidity(validity).label.toLowerCase()}`)
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to update credential",
      )
    }
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger render={triggerRender}>{children}</ContextMenuTrigger>
      <ContextMenuContent>
        {/* One item per state the credential is not already in. A toggle
            would do here when there were two states; with three, offering
            both alternatives is the only way to reach either in one click. */}
        {CREDENTIAL_VALIDITIES.filter((v) => v !== credential.validity).map(
          (v) => (
            <ContextMenuItem key={v} onClick={() => setValidity(v)}>
              <CredentialValidityIcon validity={v} />
              Mark as {credentialValidity(v).label.toLowerCase()}
            </ContextMenuItem>
          ),
        )}

        <ContextMenuSeparator />

        {filled.length === 0 && (
          <ContextMenuItem disabled>
            <CopyIcon className="size-4" />
            Nothing to copy
          </ContextMenuItem>
        )}
        {filled.map((f) => (
          <ContextMenuItem
            key={f.field}
            onClick={() => copyToClipboard(f.value, f.label.toLowerCase())}
          >
            {f.kind === "BLOB" ? (
              <KeyIcon className="size-4" />
            ) : (
              <CopyIcon className="size-4" />
            )}
            Copy {f.label.toLowerCase()}
          </ContextMenuItem>
        ))}

        {credential.keys.length > 0 && (
          <>
            <ContextMenuSeparator />
            <ContextMenuGroup>
              <ContextMenuLabel>Keys</ContextMenuLabel>
              {credential.keys.map((k, i) => {
                const label = k.name?.trim() || `Key ${i + 1}`
                return (
                  <ContextMenuItem
                    key={`${i}-${k.name}`}
                    disabled={!k.content}
                    onClick={() => copyToClipboard(k.content, label)}
                  >
                    <KeyIcon className="size-4" />
                    <span className="truncate">Copy {label}</span>
                  </ContextMenuItem>
                )
              })}
            </ContextMenuGroup>
          </>
        )}

        {credential.properties.length > 0 && (
          <>
            <ContextMenuSeparator />
            <ContextMenuSub>
              <ContextMenuSubTrigger>
                <TagIcon className="size-4" />
                Copy property
              </ContextMenuSubTrigger>
              <ContextMenuSubContent>
                {credential.properties.map((p) => (
                  <ContextMenuItem
                    key={p.name}
                    disabled={!p.value}
                    onClick={() => copyToClipboard(p.value, p.name)}
                  >
                    <CopyIcon className="size-4" />
                    <span className="truncate">{p.name}</span>
                  </ContextMenuItem>
                ))}
              </ContextMenuSubContent>
            </ContextMenuSub>
          </>
        )}

        <ContextMenuSeparator />

        <ContextMenuItem onClick={copyShareLink}>
          <LinkIcon className="size-4" />
          Copy link
        </ContextMenuItem>

        <ContextMenuSeparator />

        <ContextMenuItem
          onClick={() => openEdit({ id: credential.id, name: credential.name })}
        >
          <PencilIcon className="size-4" />
          Edit
        </ContextMenuItem>
        {credential.type === "OTHER" && (
          <ContextMenuSub>
            <ContextMenuSubTrigger>
              <ArrowRightLeftIcon className="size-4" />
              Convert to…
            </ContextMenuSubTrigger>
            <ContextMenuSubContent>
              {convertTargets.map((t) => (
                <ContextMenuItem
                  key={t}
                  onClick={() =>
                    openConvert({ id: credential.id, name: credential.name }, t)
                  }
                >
                  {credentialTypeLabel(t)}
                </ContextMenuItem>
              ))}
            </ContextMenuSubContent>
          </ContextMenuSub>
        )}
        <ContextMenuItem
          variant="destructive"
          onClick={() =>
            openDelete({ id: credential.id, name: credential.name })
          }
        >
          <TrashIcon className="size-4" />
          Delete
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}
