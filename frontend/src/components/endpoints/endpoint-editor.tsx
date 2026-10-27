import { useMemo, useRef, useState } from "react"
import { useLocation } from "react-router"
import { RadioTowerIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import {
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
  useCloseGuard,
  useRequestClose,
} from "@/components/ui/record-modal"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { SettingsNav } from "@/components/ui/settings-nav"
import { sectionId, useActiveSection } from "@/hooks/use-active-section"
import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { useRecordModals } from "@/hooks/use-record-modals"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useMyOperationRole } from "@/graphql/hooks/operations"
import type { ChannelEndpointFieldsFragment } from "@/graphql/gql/graphql"
import {
  useChannelEndpoint,
  useChannelKinds,
  useCreateChannelEndpoint,
  useFreeChannelEndpointName,
  useUpdateChannelEndpoint,
} from "@/graphql/hooks/channel-endpoints"
import { KindName } from "@/components/modules/module-icon"
import { errorMessage, graphqlErrorCode } from "@/components/modules/management/api-errors"
import { FieldControl } from "@/components/modules/management/field-control"
import { groupByField, type Draft, type FieldDef } from "@/components/modules/management/field-values"
import {
  endpointDrafts,
  endpointImportFrom,
  evaluateEndpoint,
  kindsByName,
  nameError,
  problemsFromError,
  type Kind,
} from "@/components/endpoints/endpoint-values"

type Endpoint = ChannelEndpointFieldsFragment

/** What a new endpoint starts from: a duplicate, or an import. */
interface EditorSource {
  /** Shown after the kind in the header, e.g. "duplicate of edge". */
  label: React.ReactNode
  name: string
  description: string
  values: Record<string, unknown>
  /** Shown above the form, e.g. what an import left out. */
  notes: string[]
}

/**
 * A new endpoint in the scoped operation (the endpoint modal,
 * ?newEndpoint=<channel>); with from, a duplicate of that endpoint: its values
 * and description, and a free name with a -dup suffix. An import (Endpoints
 * → Import) arrives in the history entry's state instead.
 */
export function NewEndpoint({ channel, from: fromId }: { channel: string; from: string | null }) {
  const scopedOperation = useScopedOperation()
  const imported = endpointImportFrom(useLocation().state)
  const kinds = useChannelKinds()
  const fromQuery = useChannelEndpoint(fromId ?? "")
  const from = fromQuery.data?.channelEndpoint ?? null
  const kindName = channel || (from?.moduleName ?? "")
  // A copy or an import starts with a name the operation does not use yet.
  const wanted = from ? from.name : (imported?.name ?? "")
  const freeName = useFreeChannelEndpointName(scopedOperation?.id ?? "", wanted, from ? "-dup" : "", wanted !== "")

  if (!scopedOperation) {
    return <NotFound title="New endpoint" text="Endpoints belong to an operation: pick one first, in the sidebar." />
  }
  // Empty while loading, as the tables are.
  if (kinds.isLoading || (fromId && fromQuery.isLoading) || (wanted !== "" && freeName === null)) return null
  if (fromId && !from) return <NotFound title="New endpoint" text="The endpoint to duplicate does not exist." />
  const kind = kindsByName(kinds.data?.channelKinds ?? []).get(kindName)
  if (!kind) return <NotFound title="New endpoint" text={`No channel kind "${kindName}" declares connection settings.`} />
  const source: EditorSource | null = from
    ? {
        label: (
          <>
            duplicate of <span className="font-mono">{from.name}</span>
          </>
        ),
        name: freeName ?? from.name,
        description: from.description ?? "",
        values: (from.values ?? {}) as Record<string, unknown>,
        notes: [],
      }
    : imported && {
        label: "imported from JSON",
        name: freeName ?? imported.name,
        description: "",
        values: imported.values,
        notes: imported.notes,
      }
  return <EndpointEditor operationId={scopedOperation.id} kind={kind} endpoint={null} source={source} />
}

/** An endpoint (the endpoint modal, ?endpoint=<id>): its editor, read-only below operator. */
export function EditEndpoint({ id }: { id: string }) {
  const endpointQuery = useChannelEndpoint(id)
  const kinds = useChannelKinds()
  const endpoint = endpointQuery.data?.channelEndpoint ?? null

  // Empty while loading, as the tables are.
  if (endpointQuery.isLoading || kinds.isLoading) return null
  if (endpointQuery.error) return <NotFound title="Endpoint" text={endpointQuery.error.message} />
  if (!endpoint) return <NotFound title="Endpoint" text="This endpoint does not exist." />
  const kind = kindsByName(kinds.data?.channelKinds ?? []).get(endpoint.moduleName)
  if (!kind) {
    return (
      <NotFound
        title={endpoint.name}
        text={`No "${endpoint.moduleName}" channel declares connection settings any more, so this endpoint can't be edited.`}
      />
    )
  }
  return <EndpointEditor operationId={endpoint.operationId} kind={kind} endpoint={endpoint} />
}

function NotFound({ title, text }: { title: string; text: string }) {
  return <RecordModalMessage icon={<RadioTowerIcon className="size-8 opacity-50" />} title={title} text={text} />
}

const GENERAL = "General"

/** One section of the form, as the side list shows it. */
interface Section {
  id: string
  title: string
  fields: FieldDef[]
}

/**
 * The editor: a section list on the left, one column of label | control rows
 * on the right, actions in a header that stays in view. Descriptions sit
 * behind an info icon so the rows read as a list of settings.
 */
function EndpointEditor({
  operationId,
  kind,
  endpoint,
  source = null,
}: {
  operationId: string
  kind: Kind
  endpoint: Endpoint | null
  /** What a new endpoint starts from when it duplicates another. */
  source?: EditorSource | null
}) {
  const { close } = useRecordModals()
  const requestClose = useRequestClose()
  const hasPermission = useAuthStore((s) => s.hasPermission)
  const { data: roleData, isLoading: roleLoading } = useMyOperationRole(operationId)
  const role = roleData?.myOperationRole ?? null
  // App admins may write in any operation without being a member (the server
  // allows it); OPERATION_DELETE is what only they hold.
  const canWrite = role === "ADMIN" || role === "OPERATOR" || hasPermission(Permissions.OPERATION_DELETE)
  const readOnly = !roleLoading && !canWrite

  const fields = kind.fields
  const [name, setName] = useState(endpoint?.name ?? source?.name ?? "")
  const [description, setDescription] = useState(endpoint?.description ?? source?.description ?? "")
  const [drafts, setDrafts] = useState<Record<string, Draft>>(() =>
    endpointDrafts(fields, endpoint ? ((endpoint.values ?? {}) as Record<string, unknown>) : (source?.values ?? null)),
  )
  // Changed from how it opened: closing then asks before throwing it away.
  const snapshot = JSON.stringify({ name, description, drafts })
  const [initial] = useState(snapshot)
  useCloseGuard(snapshot !== initial)
  const body = useRef<HTMLDivElement>(null)
  const [submitted, setSubmitted] = useState(false)
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({})
  const [saveError, setSaveError] = useState<string | null>(null)
  const create = useCreateChannelEndpoint()
  const update = useUpdateChannelEndpoint()
  const pending = create.isPending || update.isPending
  const disabled = pending || readOnly

  const { values, errors } = evaluateEndpoint(fields, drafts)
  const nameErr = nameError(name)
  const shownErrors: Record<string, string> = { ...(submitted ? errors : {}), ...serverErrors }
  const hasErrors = nameErr !== null || Object.keys(errors).length > 0

  const sections: Section[] = useMemo(
    () => [
      { id: sectionId(GENERAL), title: GENERAL, fields: [] },
      ...groupByField(fields, (f) => f).map((g) => ({ id: sectionId(g.group), title: g.group, fields: g.items })),
    ],
    [fields],
  )
  const [active, goTo] = useActiveSection(
    sections.map((s) => s.id),
    body,
  )

  function setDraft(key: string, d: Draft) {
    setDrafts((prev) => ({ ...prev, [key]: d }))
    setServerErrors((prev) => {
      if (!(key in prev)) return prev
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  async function save() {
    setSubmitted(true)
    setSaveError(null)
    if (hasErrors || readOnly) return
    const input = { name: name.trim(), description: description.trim() || null, values }
    try {
      if (endpoint) {
        await update.mutateAsync({ id: endpoint.id, input, expectedRevision: endpoint.revision })
        toast.success("Endpoint saved")
      } else {
        await create.mutateAsync({ operationId, moduleName: kind.name, input })
        toast.success("Endpoint created")
      }
      close()
    } catch (err) {
      if (graphqlErrorCode(err) === "CONFLICT") {
        setSaveError("Someone else changed this endpoint meanwhile. Reload the page to see their version.")
        return
      }
      const problems = problemsFromError(err)
      setServerErrors(problems)
      setSaveError(
        problems[""] ??
          (Object.keys(problems).length > 0
            ? "Some values were rejected; see the marked fields."
            : errorMessage(err, "Failed to save the endpoint")),
      )
    }
  }

  // Side list markers: a section with an error, or with values set.
  const sectionState = (s: Section): "error" | "set" | null => {
    if (s.title === GENERAL) return submitted && nameErr ? "error" : name.trim() ? "set" : null
    if (s.fields.some((f) => shownErrors[f.key])) return "error"
    if (s.fields.some((f) => values[f.key] !== undefined)) return "set"
    return null
  }

  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <RecordModalHeader>
        {/* The title and the channel kind share a baseline. */}
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
          <RecordModalTitle>{endpoint ? endpoint.name : "New endpoint"}</RecordModalTitle>
          <span className="text-sm text-muted-foreground">
            <KindName type="channel" name={kind.name} icon={kind.icon} /> · schema v{kind.version}
            {source && <> · {source.label}</>}
          </span>
        </div>
        <div className="flex items-center gap-2 sm:ml-auto">
          {readOnly ? (
            <span className="text-sm text-muted-foreground">Read-only — needs operator in this operation</span>
          ) : (
            <>
              {submitted && hasErrors && (
                <span className="text-sm text-destructive">Fix the marked fields</span>
              )}
              <Button type="button" variant="outline" onClick={requestClose} disabled={pending}>
                Cancel
              </Button>
              <Button type="submit" disabled={pending}>
                {pending ? "Saving..." : endpoint ? "Save" : "Create"}
              </Button>
            </>
          )}
        </div>
      </RecordModalHeader>

      <RecordModalBody ref={body}>
      <div className="flex gap-10 px-4 py-6">
        <SettingsNav
          sections={sections.map((s) => ({ id: s.id, title: s.title, state: sectionState(s) }))}
          active={active}
          onPick={goTo}
        />

        <div className="@container/settings min-w-0 flex-1">
        <div className="flex max-w-3xl flex-col gap-8 @6xl/settings:max-w-[80rem]">
          {saveError && (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{saveError}</div>
          )}
          {source && source.notes.length > 0 && (
            <div className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
              {source.notes.map((n) => (
                <p key={n}>{n}</p>
              ))}
            </div>
          )}

          <SettingsSection id={sectionId(GENERAL)} title={GENERAL}>
            <SettingRow
              id="endpoint-name"
              label="Name"
              required
              help="Unique in this operation: lowercase letters, digits, '.', '_' and '-'."
              error={submitted ? (nameErr ?? undefined) : undefined}
            >
              <Input
                id="endpoint-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="eu-cdn"
                autoComplete="off"
                spellCheck={false}
                disabled={disabled}
                aria-invalid={(submitted && !!nameErr) || undefined}
              />
            </SettingRow>
            <SettingRow id="endpoint-description" label="Description" help="What this endpoint is for, for the rest of the team.">
              <Textarea
                id="endpoint-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                maxLength={500}
                disabled={disabled}
              />
            </SettingRow>
          </SettingsSection>

          {sections.slice(1).map((s) => (
            <SettingsSection key={s.id} id={s.id} title={s.title}>
              {s.fields.map((f) => {
                const id = `endpoint-${f.key}`
                return (
                  <SettingRow
                    key={f.key}
                    id={id}
                    label={f.label}
                    fieldKey={f.key}
                    required={f.required}
                    help={f.description ?? undefined}
                    error={shownErrors[f.key]}
                  >
                    <FieldControl
                      field={f}
                      id={id}
                      draft={drafts[f.key]}
                      onChange={(d) => setDraft(f.key, d)}
                      disabled={disabled}
                      invalid={!!shownErrors[f.key]}
                      defaultValue={f.default}
                    />
                  </SettingRow>
                )
              })}
            </SettingsSection>
          ))}
        </div>
        </div>
      </div>
      </RecordModalBody>
    </form>
  )
}


