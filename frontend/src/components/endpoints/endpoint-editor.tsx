import { useEffect, useMemo, useRef, useState } from "react"
import { useLocation } from "react-router"
import { RadioTowerIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
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
import { childPointer, type JsonProblem } from "@/lib/json-edit"
import { whenIdle } from "@/lib/preloadable"
import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { useEditorModeStore } from "@/stores/editor-mode"
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
  exportFileName,
  kindsByName,
  MAX_DESCRIPTION_LENGTH,
  nameError,
  problemsFromError,
  type Kind,
} from "@/components/endpoints/endpoint-values"
import {
  ENDPOINT_JSON_LOCKED,
  endpointJson,
  endpointJsonHints,
  formValues,
  readEndpointJson,
} from "@/components/endpoints/endpoint-json"

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
  // The JSON tab edits the same endpoint as text. While it is open the text
  // leads: each version that reads as an endpoint is copied into the form's
  // state, so Save, the close guard and the Form tab all see it. The modal
  // opens on the tab picked last, in any record's editor (stores/editor-mode).
  const preferred = useEditorModeStore((s) => s.mode)
  const setPreferred = useEditorModeStore((s) => s.setMode)
  const [tab, setTab] = useState(preferred)
  const [jsonText, setJsonText] = useState(() =>
    preferred === "json" ? endpointJson(kind, { name, description, values: formValues(fields, drafts) }) : "",
  )
  const [leavingJson, setLeavingJson] = useState(false)
  const read = tab === "json" ? readEndpointJson(jsonText, kind) : null
  const hints = useMemo(() => endpointJsonHints(kind), [kind])
  // Fetched once the modal has settled, so the JSON tab opens at once.
  useEffect(() => whenIdle(preloadJsonEditor), [])

  // Changed from how it opened, or JSON that no longer reads: closing then
  // asks before throwing it away.
  const snapshot = JSON.stringify({ name, description, drafts })
  const [initial] = useState(snapshot)
  const saved = useCloseGuard(snapshot !== initial || (read !== null && !read.ok))
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
  const hasErrors = read
    ? !read.ok || read.problems.length > 0
    : nameErr !== null || Object.keys(errors).length > 0
  // The JSON tab marks problems as they appear, as code editors do, and the
  // server's from the last save at their settings.
  const jsonProblems: JsonProblem[] = read?.ok
    ? [
        ...read.problems,
        ...Object.entries(serverErrors)
          .filter(([key]) => key !== "")
          .map(([key, message]) => ({ path: childPointer("/params", key), message })),
      ]
    : []

  function showJson() {
    setJsonText(endpointJson(kind, { name, description, values: formValues(fields, drafts) }))
    setTab("json")
    setPreferred("json")
  }

  // Text that does not read as an endpoint cannot be carried over: ask first.
  function showForm() {
    if (read && !read.ok) setLeavingJson(true)
    else leaveJson()
  }

  function leaveJson() {
    setTab("form")
    setPreferred("form")
  }

  function editJson(text: string) {
    setJsonText(text)
    setServerErrors({})
    const next = readEndpointJson(text, kind)
    if (next.ok) {
      setName(next.doc.name)
      setDescription(next.doc.description)
      setDrafts(endpointDrafts(fields, next.doc.values))
    }
  }

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
    if (read && !read.ok) setSaveError(`The JSON is not an endpoint: ${read.message}`)
    if (hasErrors || readOnly) return
    // From the JSON tab, what it holds as it reads, without a trip through
    // the form's fields.
    const input = read?.ok
      ? { name: read.doc.name.trim(), description: read.doc.description.trim() || null, values: read.doc.values }
      : { name: name.trim(), description: description.trim() || null, values }
    try {
      if (endpoint) {
        await update.mutateAsync({ id: endpoint.id, input, expectedRevision: endpoint.revision })
        toast.success("Endpoint saved")
      } else {
        await create.mutateAsync({ operationId, moduleName: kind.name, input })
        toast.success("Endpoint created")
      }
      saved()
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
      <Tabs
        value={tab}
        onValueChange={(v) => (v === "json" ? showJson() : showForm())}
        className="min-h-0 flex-1 gap-0"
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
          <TabsList>
            <TabsTrigger value="form">Form</TabsTrigger>
            <TabsTrigger value="json">JSON</TabsTrigger>
          </TabsList>
          {readOnly ? (
            <span className="text-sm text-muted-foreground">Read-only — needs operator in this operation</span>
          ) : (
            <>
              {submitted && hasErrors && (
                <span className="text-sm text-destructive">
                  {read ? "Fix the marked problems" : "Fix the marked fields"}
                </span>
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

      <TabsContent value="form" className="flex min-h-0 flex-col">
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
                maxLength={MAX_DESCRIPTION_LENGTH}
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
      </TabsContent>

      <TabsContent value="json" className="flex min-h-0 flex-col gap-3 px-4 py-4">
        {saveError && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{saveError}</div>}
        {source && source.notes.length > 0 && (
          <div className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
            {source.notes.map((n) => (
              <p key={n}>{n}</p>
            ))}
          </div>
        )}
        <LazyJsonEditor
          value={jsonText}
          onChange={editJson}
          locked={ENDPOINT_JSON_LOCKED}
          problems={jsonProblems}
          hints={hints}
          readOnly={disabled}
          fileName={exportFileName(name.trim() || "endpoint")}
          aria-label="The endpoint as JSON"
          className="flex-1 rounded-md border"
        />
        <p className="text-xs text-muted-foreground">
          <code className="font-mono">null</code> leaves a setting at the channel&apos;s default; a list or map
          without one is <code className="font-mono">[]</code> or <code className="font-mono">{"{}"}</code> when empty.{" "}
          <code className="font-mono">format</code> and <code className="font-mono">channel</code> are locked.
          Settings and their values are suggested as you type; Ctrl+Space (⌥I on a Mac) asks for them.
        </p>
      </TabsContent>
      </Tabs>

      <LeaveJsonDialog
        open={leavingJson}
        problem={read && !read.ok ? read.message : ""}
        onStay={() => setLeavingJson(false)}
        onLeave={() => {
          setLeavingJson(false)
          leaveJson()
        }}
      />
    </form>
  )
}

/** Leaving JSON that does not read: the form shows the last version that did. */
function LeaveJsonDialog({
  open,
  problem,
  onStay,
  onLeave,
}: {
  open: boolean
  problem: string
  onStay: () => void
  onLeave: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onStay()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>The JSON does not read as an endpoint</DialogTitle>
          <DialogDescription>
            {problem} The form shows the last version that did; what you changed since is dropped.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onStay}>
            Keep editing
          </Button>
          <Button type="button" onClick={onLeave}>
            Show the form
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}


