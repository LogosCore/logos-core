import { useEffect, useMemo, useRef, useState } from "react"
import { Link, useNavigate, useParams, useSearchParams } from "react-router"
import { ArrowLeftIcon, InfoIcon, RadioTowerIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useMyOperationRole } from "@/graphql/hooks/operations"
import type { ChannelEndpointFieldsFragment } from "@/graphql/gql/graphql"
import {
  useChannelEndpoint,
  useChannelKinds,
  useCreateChannelEndpoint,
  useUpdateChannelEndpoint,
} from "@/graphql/hooks/channel-endpoints"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { errorMessage, graphqlErrorCode } from "@/components/modules/management/api-errors"
import { FieldControl } from "@/components/modules/management/field-control"
import { groupByField, type Draft, type FieldDef } from "@/components/modules/management/field-values"
import {
  endpointDrafts,
  evaluateEndpoint,
  kindsByName,
  nameError,
  problemsFromError,
  type Kind,
} from "@/components/endpoints/endpoint-values"

type Endpoint = ChannelEndpointFieldsFragment

/** /endpoints/new?kind=http — a new endpoint in the scoped operation. */
export function NewEndpointPage() {
  const scopedOperation = useScopedOperation()
  const [params] = useSearchParams()
  const kindName = params.get("kind") ?? ""
  const kinds = useChannelKinds()

  usePageMetadata({ title: "New endpoint", icon: { kind: "lucide", component: RadioTowerIcon } })

  if (!scopedOperation) return <GettingStartedPanel />
  if (kinds.isLoading) return <EditorSkeleton />
  const kind = kindsByName(kinds.data?.channelKinds ?? []).get(kindName)
  if (!kind) return <NotFound text={`No channel kind "${kindName}" declares connection settings.`} />
  return <EndpointEditor operationId={scopedOperation.id} kind={kind} endpoint={null} />
}

/** /endpoints/:id — edit an endpoint (read-only below operator). */
export function EditEndpointPage() {
  const { id = "" } = useParams<{ id: string }>()
  const endpointQuery = useChannelEndpoint(id)
  const kinds = useChannelKinds()
  const endpoint = endpointQuery.data?.channelEndpoint ?? null

  usePageMetadata({
    title: endpoint ? `${endpoint.name} · Endpoints` : "Endpoints",
    icon: { kind: "lucide", component: RadioTowerIcon },
  })

  if (endpointQuery.isLoading || kinds.isLoading) return <EditorSkeleton />
  if (endpointQuery.error) return <NotFound text={endpointQuery.error.message} />
  if (!endpoint) return <NotFound text="This endpoint does not exist." />
  const kind = kindsByName(kinds.data?.channelKinds ?? []).get(endpoint.moduleName)
  if (!kind) {
    return <NotFound text={`No "${endpoint.moduleName}" channel declares connection settings any more, so this endpoint can't be edited.`} />
  }
  return <EndpointEditor operationId={endpoint.operationId} kind={kind} endpoint={endpoint} />
}

function EditorSkeleton() {
  return (
    <div className="flex flex-col gap-4 p-4">
      <Skeleton className="h-8 w-80" />
      <Skeleton className="h-96 w-full max-w-3xl" />
    </div>
  )
}

function NotFound({ text }: { text: string }) {
  return (
    <div className="flex flex-col items-center gap-3 py-16 text-center text-muted-foreground">
      <RadioTowerIcon className="size-8 opacity-50" />
      <p className="max-w-md text-sm">{text}</p>
      <Link to="/endpoints" className="text-sm underline underline-offset-4">
        Back to endpoints
      </Link>
    </div>
  )
}

const GENERAL = "General"

/** One section of the form, as the side list shows it. */
interface Section {
  id: string
  title: string
  fields: FieldDef[]
}

function sectionId(title: string): string {
  return `section-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`
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
}: {
  operationId: string
  kind: Kind
  endpoint: Endpoint | null
}) {
  const navigate = useNavigate()
  const hasPermission = useAuthStore((s) => s.hasPermission)
  const { data: roleData, isLoading: roleLoading } = useMyOperationRole(operationId)
  const role = roleData?.myOperationRole ?? null
  // App admins may write in any operation without being a member (the server
  // allows it); OPERATION_DELETE is what only they hold.
  const canWrite = role === "ADMIN" || role === "OPERATOR" || hasPermission(Permissions.OPERATION_DELETE)
  const readOnly = !roleLoading && !canWrite

  const fields = kind.fields
  const [name, setName] = useState(endpoint?.name ?? "")
  const [description, setDescription] = useState(endpoint?.description ?? "")
  const [drafts, setDrafts] = useState<Record<string, Draft>>(() =>
    endpointDrafts(fields, endpoint ? ((endpoint.values ?? {}) as Record<string, unknown>) : null),
  )
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
  const [active, goTo] = useActiveSection(sections.map((s) => s.id))

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
      navigate("/endpoints")
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
      className="flex flex-1 flex-col"
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <header className="sticky top-0 z-10 flex flex-wrap items-center gap-x-3 gap-y-2 border-b bg-background px-4 py-3">
        <Link
          to="/endpoints"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeftIcon className="size-4" />
          Endpoints
        </Link>
        <span className="h-5 w-px bg-border" aria-hidden />
        <h1 className="text-lg font-semibold">{endpoint ? endpoint.name : "New endpoint"}</h1>
        <span className="text-sm text-muted-foreground">
          <span className="font-mono">{kind.name}</span> · schema v{kind.version}
        </span>
        <div className="flex items-center gap-2 sm:ml-auto">
          {readOnly ? (
            <span className="text-sm text-muted-foreground">Read-only — needs operator in this operation</span>
          ) : (
            <>
              {submitted && hasErrors && (
                <span className="text-sm text-destructive">Fix the marked fields</span>
              )}
              <Button type="button" variant="outline" onClick={() => navigate("/endpoints")} disabled={pending}>
                Cancel
              </Button>
              <Button type="submit" disabled={pending}>
                {pending ? "Saving..." : endpoint ? "Save" : "Create"}
              </Button>
            </>
          )}
        </div>
      </header>

      <div className="flex gap-10 px-4 py-6">
        <nav className="sticky top-20 hidden w-40 shrink-0 self-start md:block" aria-label="Sections">
          <ul className="flex flex-col gap-0.5">
            {sections.map((s) => {
              const state = sectionState(s)
              return (
                <li key={s.id}>
                  <button
                    type="button"
                    onClick={() => goTo(s.id)}
                    className={cn(
                      "flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-sm",
                      active === s.id
                        ? "bg-foreground/10 font-medium text-foreground"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {s.title}
                    {state && (
                      <span
                        className={cn("size-1.5 rounded-full", state === "error" ? "bg-destructive" : "bg-foreground/50")}
                        aria-label={state === "error" ? "has errors" : "has values"}
                      />
                    )}
                  </button>
                </li>
              )
            })}
          </ul>
        </nav>

        <div className="flex min-w-0 max-w-3xl flex-1 flex-col gap-8">
          {saveError && (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{saveError}</div>
          )}

          <FormSection id={sectionId(GENERAL)} title={GENERAL}>
            <Row
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
            </Row>
            <Row id="endpoint-description" label="Description" help="What this endpoint is for, for the rest of the team.">
              <Textarea
                id="endpoint-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                maxLength={500}
                disabled={disabled}
              />
            </Row>
          </FormSection>

          {sections.slice(1).map((s) => (
            <FormSection key={s.id} id={s.id} title={s.title}>
              {s.fields.map((f) => {
                const id = `endpoint-${f.key}`
                return (
                  <Row
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
                  </Row>
                )
              })}
            </FormSection>
          ))}
        </div>
      </div>
    </form>
  )
}

function FormSection({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section id={id} className="scroll-mt-20" aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`} className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </h2>
      <div className="divide-y rounded-xl border bg-card px-4">{children}</div>
    </section>
  )
}

/** One setting: label (with help behind an info icon) | control and its error. */
function Row({
  id,
  label,
  fieldKey,
  required,
  help,
  error,
  children,
}: {
  id: string
  label: string
  fieldKey?: string
  required?: boolean
  help?: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className="grid gap-x-6 gap-y-1.5 py-3 sm:grid-cols-[12rem_minmax(0,1fr)] sm:items-start">
      <div className="flex items-center gap-1.5 sm:pt-1.5">
        <label htmlFor={id} className="text-sm font-medium">
          {label}
          {required && <span className="text-destructive">*</span>}
        </label>
        {help && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground"
                  aria-label={`About ${label}`}
                />
              }
            >
              <InfoIcon className="size-3.5" />
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">
              <div className="flex flex-col gap-1 text-left">
                <span>{help}</span>
                {fieldKey && <code className="font-mono opacity-75">{fieldKey}</code>}
              </div>
            </TooltipContent>
          </Tooltip>
        )}
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        {children}
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
    </div>
  )
}

/** How far below the viewport top a section's heading counts as reached. */
const SPY_OFFSET = 120

/**
 * The section the reader is in, and a way to go to one. Reading position: the
 * last section whose top has scrolled above SPY_OFFSET, or the last section
 * once the page is scrolled to its end (short last sections never reach the
 * top). A section picked from the list stays highlighted until the reader
 * scrolls themselves: near the end of the page it may not move at all.
 */
function useActiveSection(ids: string[]): [string | null, (id: string) => void] {
  const [active, setActive] = useState<string | null>(ids[0] ?? null)
  const idsRef = useRef(ids)
  const pickedRef = useRef(false)
  useEffect(() => {
    idsRef.current = ids
  })

  useEffect(() => {
    const update = () => {
      if (pickedRef.current) return
      const els = idsRef.current
        .map((id) => document.getElementById(id))
        .filter((el): el is HTMLElement => el !== null)
      if (els.length === 0) return
      const last = els[els.length - 1]
      // At the end: the last section has fully come into view.
      if (last.getBoundingClientRect().bottom <= window.innerHeight + 1) {
        setActive(last.id)
        return
      }
      let current = els[0].id
      for (const el of els) {
        if (el.getBoundingClientRect().top <= SPY_OFFSET) current = el.id
      }
      setActive(current)
    }
    // The reader taking over ends a pick.
    const release = () => {
      pickedRef.current = false
    }
    // Capture: the page scrolls inside the app layout, not the window.
    document.addEventListener("scroll", update, { capture: true, passive: true })
    window.addEventListener("resize", update)
    for (const ev of ["wheel", "touchstart", "keydown"] as const) {
      window.addEventListener(ev, release, { passive: true })
    }
    update()
    return () => {
      document.removeEventListener("scroll", update, { capture: true })
      window.removeEventListener("resize", update)
      for (const ev of ["wheel", "touchstart", "keydown"] as const) {
        window.removeEventListener(ev, release)
      }
    }
  }, [])

  const goTo = (id: string) => {
    pickedRef.current = true
    setActive(id)
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" })
  }
  return [active, goTo]
}
