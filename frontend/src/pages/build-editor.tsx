import { useMemo, useState } from "react"
import { Link, useNavigate, useParams, useSearchParams } from "react-router"
import { ArrowDownIcon, ArrowLeftIcon, ArrowUpIcon, HammerIcon, InfoIcon, XIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import type { BuildPresetFieldsFragment } from "@/graphql/gql/graphql"
import { useChannelEndpoints, useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import {
  useBuild,
  useBuildFiles,
  useBuildPreset,
  useCreateBuild,
  useCreateBuildPreset,
  useFactoryKinds,
  useInvalidateBuildFiles,
  useUpdateBuildPreset,
} from "@/graphql/hooks/builds"
import { uploadBuildFile } from "@/lib/build-file-upload"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { errorMessage, graphqlErrorCode } from "@/components/modules/management/api-errors"
import { FieldControl } from "@/components/modules/management/field-control"
import { groupByField, type Draft, type FieldDef } from "@/components/modules/management/field-values"
import { nameError, problemsFromError } from "@/components/endpoints/endpoint-values"
import { BuildFilesContext, type BuildFilesContextValue } from "@/components/builds/build-files-context"
import {
  connectionDraft,
  endpointUnusable,
  evaluateForm,
  factoryKindsByName,
  initialForm,
  move,
  platformLabel,
  targetFields,
  withTarget,
  type BuildForm as Form,
  type EndpointLike,
  type FactoryKind,
  type FormSource,
} from "@/components/builds/build-values"
import { PRESETS_PATH, useCanBuild } from "@/components/builds/build-access"
import { KindName } from "@/components/modules/module-icon"

type Preset = BuildPresetFieldsFragment

/** /builds/new?kind=bob[&preset=<id> | &from=<build id>] — a build in the scoped operation. */
export function NewBuildPage() {
  const scopedOperation = useScopedOperation()
  const [params] = useSearchParams()
  const presetQuery = useBuildPreset(params.get("preset") ?? "")
  const fromQuery = useBuild(params.get("from") ?? "")
  usePageMetadata({ title: "New build", icon: { kind: "lucide", component: HammerIcon } })

  if (!scopedOperation) return <GettingStartedPanel />
  if (presetQuery.isLoading || fromQuery.isLoading) return <EditorSkeleton />
  const preset = presetQuery.data?.buildPreset ?? null
  const from = fromQuery.data?.build ?? null
  const kindName = params.get("kind") ?? preset?.moduleName ?? from?.moduleName ?? ""
  let source: SourceSpec = null
  if (preset) source = { kind: "preset", preset }
  else if (from) {
    source = {
      kind: "build",
      target: from.target,
      platform: from.platform,
      values: (from.values ?? {}) as Record<string, unknown>,
      connections: (from.connections ?? []) as BuildConnectionRecord[],
    }
  }
  return <EditorLoader operationId={scopedOperation.id} kindName={kindName} mode="build" source={source} preset={null} />
}

/** /builds/presets/new?kind=bob — a preset in the scoped operation. */
export function NewPresetPage() {
  const scopedOperation = useScopedOperation()
  const [params] = useSearchParams()
  usePageMetadata({ title: "New preset", icon: { kind: "lucide", component: HammerIcon } })
  if (!scopedOperation) return <GettingStartedPanel />
  return <EditorLoader operationId={scopedOperation.id} kindName={params.get("kind") ?? ""} mode="preset" source={null} preset={null} />
}

/** /builds/presets/:id — edit a preset (read-only below operator). */
export function EditPresetPage() {
  const { id = "" } = useParams<{ id: string }>()
  const presetQuery = useBuildPreset(id)
  const preset = presetQuery.data?.buildPreset ?? null
  usePageMetadata({
    title: preset ? `${preset.name} · Presets` : "Presets",
    icon: { kind: "lucide", component: HammerIcon },
  })
  if (presetQuery.isLoading) return <EditorSkeleton />
  if (presetQuery.error) return <NotFound text={presetQuery.error.message} />
  if (!preset) return <NotFound text="This preset does not exist." />
  return (
    <EditorLoader
      operationId={preset.operationId}
      kindName={preset.moduleName}
      mode="preset"
      source={{ kind: "preset", preset }}
      preset={preset}
    />
  )
}

/** A build's copied connection, as Build.connections holds it. */
interface BuildConnectionRecord {
  endpoint?: { name?: string }
  values?: Record<string, unknown>
}

type SourceSpec =
  | null
  | { kind: "preset"; preset: Preset }
  | { kind: "build"; target: string; platform: string; values: Record<string, unknown>; connections: BuildConnectionRecord[] }

/** Loads what the form needs: the factory kind, the endpoints, the files. */
function EditorLoader({
  operationId,
  kindName,
  mode,
  source,
  preset,
}: {
  operationId: string
  kindName: string
  mode: "build" | "preset"
  source: SourceSpec
  preset: Preset | null
}) {
  const kinds = useFactoryKinds()
  const channelKinds = useChannelKinds()
  const endpointsQuery = useChannelEndpoints(operationId)
  const filesQuery = useBuildFiles(operationId)

  if (kinds.isLoading || channelKinds.isLoading || endpointsQuery.isLoading || filesQuery.isLoading) {
    return <EditorSkeleton />
  }
  const kind = factoryKindsByName(kinds.data?.factoryKinds ?? []).get(kindName)
  if (!kind) return <NotFound text={`No "${kindName}" factory declares what it builds.`} />
  const endpoints = endpointsQuery.data?.channelEndpoints ?? []
  const channelVersions = new Map((channelKinds.data?.channelKinds ?? []).map((k) => [k.moduleName, k.schema.version]))

  let formSource: FormSource | null = null
  const notes: string[] = []
  if (source?.kind === "preset") {
    formSource = {
      target: source.preset.target,
      platform: source.preset.platform,
      values: (source.preset.values ?? {}) as Record<string, unknown>,
      connections: source.preset.connections.map((c) => ({
        endpointId: c.endpointId,
        values: (c.values ?? {}) as Record<string, unknown>,
      })),
    }
  } else if (source?.kind === "build") {
    // A build copied its endpoints; take the operation's current ones by name.
    const byName = new Map(endpoints.map((e) => [e.name, e.id]))
    const connections: { endpointId: string; values: Record<string, unknown> }[] = []
    for (const c of source.connections) {
      const id = byName.get(c.endpoint?.name ?? "")
      if (id) connections.push({ endpointId: id, values: c.values ?? {} })
      else notes.push(`Endpoint "${c.endpoint?.name ?? "?"}" no longer exists in this operation.`)
    }
    formSource = { target: source.target, platform: source.platform, values: source.values, connections }
  }

  return (
    <Editor
      operationId={operationId}
      kind={kind}
      mode={mode}
      preset={preset}
      source={formSource}
      notes={notes}
      endpoints={endpoints}
      channelVersions={channelVersions}
      files={(filesQuery.data?.buildFiles ?? []).map((f) => ({ id: f.id, name: f.name, size: f.size, mediaType: f.mediaType }))}
    />
  )
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
      <HammerIcon className="size-8 opacity-50" />
      <p className="max-w-md text-sm">{text}</p>
      <Link to="/builds" className="text-sm underline underline-offset-4">
        Back to builds
      </Link>
    </div>
  )
}

function Editor({
  operationId,
  kind,
  mode,
  preset,
  source,
  notes,
  endpoints,
  channelVersions,
  files,
}: {
  operationId: string
  kind: FactoryKind
  mode: "build" | "preset"
  preset: Preset | null
  source: FormSource | null
  notes: string[]
  endpoints: readonly (EndpointLike & { description?: string | null })[]
  channelVersions: ReadonlyMap<string, number>
  files: BuildFilesContextValue["files"]
}) {
  const navigate = useNavigate()
  const { canWrite, loading: roleLoading } = useCanBuild(operationId)
  const readOnly = !roleLoading && !canWrite
  const backTo = mode === "build" ? "/builds" : PRESETS_PATH
  const endpointKinds = useMemo(() => new Map(endpoints.map((e) => [e.id, e.moduleName])), [endpoints])
  const endpointById = useMemo(() => new Map(endpoints.map((e) => [e.id, e])), [endpoints])
  const channelIcons = useChannelIcons()

  const [form, setForm] = useState<Form>(() => initialForm(kind, source, endpointKinds))
  const [name, setName] = useState(preset?.name ?? "")
  const [description, setDescription] = useState(preset?.description ?? "")
  const [submitted, setSubmitted] = useState(false)
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({})
  const [saveError, setSaveError] = useState<string | null>(null)
  const createBuild = useCreateBuild()
  const createPreset = useCreateBuildPreset()
  const updatePreset = useUpdateBuildPreset()
  const invalidateFiles = useInvalidateBuildFiles()
  const pending = createBuild.isPending || createPreset.isPending || updatePreset.isPending
  const disabled = pending || readOnly

  const target = kind.targets.find((t) => t.id === form.target)
  const fields = targetFields(kind, target)
  const { input, errors } = evaluateForm(kind, form, mode === "preset", endpointKinds)
  const nameErr = mode === "preset" ? nameError(name) : null
  const shown: Record<string, string> = { ...(submitted ? errors : {}), ...serverErrors }
  const hasErrors = nameErr !== null || Object.keys(errors).length > 0

  const filesCtx: BuildFilesContextValue = useMemo(
    () => ({
      files,
      upload: async (file, onProgress) => {
        const f = await uploadBuildFile(file, operationId, { onProgress })
        await invalidateFiles(operationId)
        return { id: f.id, name: f.name, size: f.size, mediaType: f.mediaType ?? null }
      },
    }),
    [files, operationId, invalidateFiles],
  )

  function clearServerError(key: string) {
    setServerErrors((prev) => {
      if (!(key in prev)) return prev
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  function setValueDraft(key: string, d: Draft) {
    setForm((f) => ({ ...f, drafts: { ...f.drafts, [key]: d } }))
    clearServerError(`values.${key}`)
  }

  function setConnectionDraft(i: number, key: string, d: Draft) {
    setForm((f) => ({
      ...f,
      connections: f.connections.map((c, j) => (j === i ? { ...c, drafts: { ...c.drafts, [key]: d } } : c)),
    }))
    clearServerError(`connections[${i}].values.${key}`)
  }

  async function submit() {
    setSubmitted(true)
    setSaveError(null)
    if (hasErrors || readOnly) return
    try {
      if (mode === "build") {
        const res = await createBuild.mutateAsync({
          operationId,
          input: {
            moduleName: kind.name,
            target: input.target,
            platform: input.platform,
            values: input.values,
            connections: input.connections,
            presetId: preset?.id ?? null,
          },
        })
        toast.success("Build queued")
        navigate(`/builds/${res.createBuild.id}`)
        return
      }
      const presetInput = {
        name: name.trim(),
        description: description.trim() || null,
        target: input.target,
        platform: input.platform || null,
        values: input.values,
        connections: input.connections,
      }
      if (preset) {
        await updatePreset.mutateAsync({ id: preset.id, input: presetInput, expectedRevision: preset.revision })
        toast.success("Preset saved")
      } else {
        await createPreset.mutateAsync({ operationId, moduleName: kind.name, input: presetInput })
        toast.success("Preset created")
      }
      navigate(PRESETS_PATH)
    } catch (err) {
      if (graphqlErrorCode(err) === "CONFLICT") {
        setSaveError("Someone else changed this preset meanwhile. Reload the page to see their version.")
        return
      }
      const problems = problemsFromError(err)
      setServerErrors(problems)
      setSaveError(
        problems[""] ??
          (Object.keys(problems).length > 0
            ? "Some settings were rejected; see the marked fields."
            : errorMessage(err, mode === "build" ? "Failed to start the build" : "Failed to save the preset")),
      )
    }
  }

  const groups = groupByField(fields, (f) => f)
  const unusedEndpoints = endpoints.filter((e) => !form.connections.some((c) => c.endpointId === e.id))

  return (
    <BuildFilesContext.Provider value={filesCtx}>
      <form
        className="flex flex-1 flex-col"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <header className="sticky top-0 z-10 flex flex-wrap items-center gap-x-3 gap-y-2 border-b bg-background px-4 py-3">
          <Link to={backTo} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeftIcon className="size-4" />
            {mode === "build" ? "Builds" : "Presets"}
          </Link>
          <span className="h-5 w-px bg-border" aria-hidden />
          <h1 className="text-lg font-semibold">
            {mode === "build" ? "New build" : preset ? preset.name : "New preset"}
          </h1>
          <span className="text-sm text-muted-foreground">
            <KindName type="factory" name={kind.name} icon={kind.icon} /> · schema v{kind.version}
            {mode === "build" && ` · ${kind.instances} running`}
          </span>
          <div className="flex items-center gap-2 sm:ml-auto">
            {readOnly ? (
              <span className="text-sm text-muted-foreground">Read-only — needs operator in this operation</span>
            ) : (
              <>
                {submitted && hasErrors && <span className="text-sm text-destructive">Fix the marked fields</span>}
                <Button type="button" variant="outline" onClick={() => navigate(backTo)} disabled={pending}>
                  Cancel
                </Button>
                <Button type="submit" disabled={pending}>
                  {pending ? "Saving..." : mode === "build" ? "Build" : preset ? "Save" : "Create"}
                </Button>
              </>
            )}
          </div>
        </header>

        <div className="flex min-w-0 max-w-3xl flex-1 flex-col gap-8 px-4 py-6">
          {saveError && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{saveError}</div>}
          {notes.length > 0 && (
            <div className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
              {notes.map((n) => (
                <p key={n}>{n}</p>
              ))}
            </div>
          )}

          <FormSection title="What to build">
            {mode === "preset" && (
              <>
                <Row id="preset-name" label="Name" required help="Unique in this operation: lowercase letters, digits, '.', '_' and '-'." error={submitted ? (nameErr ?? undefined) : undefined}>
                  <Input
                    id="preset-name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="eu-front-service"
                    autoComplete="off"
                    spellCheck={false}
                    disabled={disabled}
                    aria-invalid={(submitted && !!nameErr) || undefined}
                  />
                </Row>
                <Row id="preset-description" label="Description" help="What this preset is for, for the rest of the team.">
                  <Textarea
                    id="preset-description"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    rows={2}
                    maxLength={500}
                    disabled={disabled}
                  />
                </Row>
              </>
            )}
            <Row id="build-target" label="Target" required help={target?.description ?? "What the factory produces."} error={shown.target}>
              <Select
                value={form.target === "" ? null : form.target}
                onValueChange={(v) => {
                  if (typeof v === "string") setForm((f) => withTarget(kind, f, v))
                }}
                disabled={disabled}
              >
                <SelectTrigger id="build-target" className="w-full max-w-80">
                  <SelectValue placeholder="Select…" />
                </SelectTrigger>
                <SelectContent>
                  {kind.targets.map((t) => (
                    <SelectItem key={t.id} value={t.id} description={t.description ?? `${t.format}`}>
                      {t.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Row>
            <Row
              id="build-platform"
              label="Platform"
              required={mode === "build"}
              help={mode === "preset" ? "Optional in a preset: it can be left for the build." : "Where the minion runs."}
              error={shown.platform}
            >
              <Select
                value={form.platform === "" ? null : form.platform}
                onValueChange={(v) => setForm((f) => ({ ...f, platform: typeof v === "string" ? v : "" }))}
                disabled={disabled || !target}
              >
                <SelectTrigger id="build-platform" className="w-full max-w-80">
                  <SelectValue placeholder={mode === "preset" ? "Chosen when building" : "Select…"} />
                </SelectTrigger>
                <SelectContent>
                  {(target?.platforms ?? []).map((p) => (
                    <SelectItem key={p} value={p} description={p}>
                      {platformLabel(p)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Row>
          </FormSection>

          {groups.map((g) => (
            <FormSection key={g.group} title={g.group}>
              {g.items.map((f) => (
                <FieldRow
                  key={f.key}
                  idPrefix="build"
                  field={f}
                  draft={form.drafts[f.key]}
                  onChange={(d) => setValueDraft(f.key, d)}
                  error={shown[`values.${f.key}`]}
                  disabled={disabled}
                  partial={mode === "preset"}
                />
              ))}
            </FormSection>
          ))}

          <FormSection title="Endpoints">
            <div className="flex flex-col gap-3 py-3">
              <p className="text-sm text-muted-foreground">
                The minion tries them in this order. Each is copied into the build: changing an endpoint later changes
                nothing already built.
              </p>
              {shown.connections && <p className="text-sm text-destructive">{shown.connections}</p>}
              {form.connections.map((c, i) => {
                const ep = endpointById.get(c.endpointId)
                const params = kind.channelFields.get(ep?.moduleName ?? "") ?? []
                const problem = shown[`connections[${i}]`] ?? (ep ? endpointUnusable(target, ep, channelVersions) : "This endpoint no longer exists")
                return (
                  <div key={`${c.endpointId}-${i}`} className="rounded-lg border">
                    <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
                      <span className="text-xs text-muted-foreground">{i + 1}.</span>
                      <span className="font-mono text-sm font-medium">{ep?.name ?? "(deleted)"}</span>
                      {ep && (
                        <KindName type="channel" name={ep.moduleName} icon={channelIcons.get(ep.moduleName)} className="text-xs text-muted-foreground" />
                      )}
                      <div className="ml-auto flex items-center gap-1">
                        <Button type="button" variant="ghost" size="icon-sm" title="Earlier" aria-label="Earlier" disabled={disabled || i === 0}
                          onClick={() => setForm((f) => ({ ...f, connections: move(f.connections, i, -1) }))}>
                          <ArrowUpIcon />
                        </Button>
                        <Button type="button" variant="ghost" size="icon-sm" title="Later" aria-label="Later" disabled={disabled || i === form.connections.length - 1}
                          onClick={() => setForm((f) => ({ ...f, connections: move(f.connections, i, 1) }))}>
                          <ArrowDownIcon />
                        </Button>
                        <Button type="button" variant="ghost" size="icon-sm" title="Remove" aria-label="Remove" disabled={disabled}
                          onClick={() => setForm((f) => ({ ...f, connections: f.connections.filter((_, j) => j !== i) }))}>
                          <XIcon />
                        </Button>
                      </div>
                    </div>
                    {problem && <p className="px-3 pt-2 text-sm text-destructive">{problem}</p>}
                    {params.length > 0 && (
                      <div className="divide-y px-3">
                        {params.map((f) => (
                          <FieldRow
                            key={f.key}
                            idPrefix={`conn-${i}`}
                            field={f}
                            draft={c.drafts[f.key]}
                            onChange={(d) => setConnectionDraft(i, f.key, d)}
                            error={shown[`connections[${i}].values.${f.key}`]}
                            disabled={disabled}
                            partial={mode === "preset"}
                          />
                        ))}
                      </div>
                    )}
                  </div>
                )
              })}
              <Select
                value={null}
                onValueChange={(v) => {
                  if (typeof v !== "string") return
                  const ep = endpointById.get(v)
                  setForm((f) => ({ ...f, connections: [...f.connections, connectionDraft(kind, v, ep?.moduleName)] }))
                  clearServerError("connections")
                }}
                disabled={disabled || unusedEndpoints.length === 0}
              >
                <SelectTrigger className="w-full max-w-80" aria-label="Add an endpoint">
                  <SelectValue
                    placeholder={endpoints.length === 0 ? "No endpoints in this operation yet" : "Add an endpoint…"}
                  />
                </SelectTrigger>
                <SelectContent>
                  {unusedEndpoints.map((e) => {
                    const why = endpointUnusable(target, e, channelVersions)
                    return (
                      <SelectItem key={e.id} value={e.id} disabled={why !== null} description={why ?? e.moduleName}>
                        {e.name}
                      </SelectItem>
                    )
                  })}
                </SelectContent>
              </Select>
            </div>
          </FormSection>
        </div>
      </form>
    </BuildFilesContext.Provider>
  )
}

function FieldRow({
  idPrefix,
  field: f,
  draft,
  onChange,
  error,
  disabled,
  partial,
}: {
  idPrefix: string
  field: FieldDef
  draft: Draft | undefined
  onChange: (d: Draft) => void
  error?: string
  disabled: boolean
  partial: boolean
}) {
  const id = `${idPrefix}-${f.key}`
  if (!draft) return null
  return (
    <Row id={id} label={f.label} fieldKey={f.key} required={f.required && !partial} help={f.description ?? undefined} error={error}>
      <FieldControl field={f} id={id} draft={draft} onChange={onChange} disabled={disabled} invalid={!!error} defaultValue={f.default} />
    </Row>
  )
}

function FormSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h2 className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">{title}</h2>
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
              render={<button type="button" className="text-muted-foreground hover:text-foreground" aria-label={`About ${label}`} />}
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
