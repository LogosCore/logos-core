import { useMemo, useState, type ComponentProps } from "react"
import { Link, useNavigate, useSearchParams } from "react-router"
import { ArrowLeftIcon, HammerIcon, PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { SettingsNav, type SettingsNavSection } from "@/components/ui/settings-nav"
import { sectionId, useActiveSection } from "@/hooks/use-active-section"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useChannelEndpoints, useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useBuild, useBuildFiles, useCreateBuild, useFactoryKinds, useInvalidateBuildFiles } from "@/graphql/hooks/builds"
import { uploadBuildFile } from "@/lib/build-file-upload"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { errorMessage } from "@/components/modules/management/api-errors"
import { groupByField, type Draft } from "@/components/modules/management/field-values"
import { kindsByName, problemsFromError } from "@/components/endpoints/endpoint-values"
import { BuildFilesContext, type BuildFilesContextValue } from "@/components/builds/build-files-context"
import { FieldRow } from "@/components/builds/build-field-row"
import { ConnectionCard, type EndpointOption } from "@/components/builds/connection-card"
import { ConnectionPickerDialog } from "@/components/builds/connection-picker-dialog"
import { SaveEndpointDialog } from "@/components/builds/save-endpoint-dialog"
import {
  channelUnusable,
  connectionDraft,
  connectionNames,
  evaluateForm,
  factoryKindsByName,
  fillFromEndpoint,
  initialForm,
  move,
  platformLabel,
  sourceFromBuild,
  targetFields,
  withTarget,
  type BuildForm as Form,
  type BuildRecordLike,
  type ChannelKind,
  type ConnectionDraft,
  type FactoryKind,
  type FormSource,
} from "@/components/builds/build-values"
import { BUILDS_PATH, useCanBuild } from "@/components/builds/build-access"
import { KindName } from "@/components/modules/module-icon"

/**
 * /builds/new?kind=bob[&from=<build id>] — a build in the scoped operation;
 * with from, filled in from that earlier build ("Build again").
 */
export function NewBuildPage() {
  const scopedOperation = useScopedOperation()
  const [params] = useSearchParams()
  const fromQuery = useBuild(params.get("from") ?? "")
  usePageMetadata({ title: "New build", icon: { kind: "lucide", component: HammerIcon } })

  if (!scopedOperation) return <GettingStartedPanel />
  if (fromQuery.isLoading) return <EditorSkeleton />
  const from = fromQuery.data?.build ?? null
  const kindName = params.get("kind") ?? from?.moduleName ?? ""
  return <EditorLoader operationId={scopedOperation.id} kindName={kindName} from={from} />
}

/** Loads what the form needs: the factory kind, the channels, the endpoints, the files. */
function EditorLoader({
  operationId,
  kindName,
  from,
}: {
  operationId: string
  kindName: string
  /** The earlier build the form starts from, if any. */
  from: BuildRecordLike | null
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
  const channels = kindsByName(channelKinds.data?.channelKinds ?? [])

  return (
    <Editor
      operationId={operationId}
      kind={kind}
      source={from && sourceFromBuild(kind, from, channels)}
      channels={channels}
      endpoints={endpointsQuery.data?.channelEndpoints ?? []}
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
      <Link to={BUILDS_PATH} className="text-sm underline underline-offset-4">
        Back to builds
      </Link>
    </div>
  )
}

function Editor({
  operationId,
  kind,
  source,
  channels,
  endpoints,
  files,
}: {
  operationId: string
  kind: FactoryKind
  source: FormSource | null
  channels: ReadonlyMap<string, ChannelKind>
  /** The operation's endpoints, which connections can be filled from. */
  endpoints: readonly EndpointOption[]
  files: BuildFilesContextValue["files"]
}) {
  const navigate = useNavigate()
  const { canWrite, loading: roleLoading } = useCanBuild(operationId)
  const readOnly = !roleLoading && !canWrite
  const channelIcons = useChannelIcons()

  const [form, setForm] = useState<Form>(() => initialForm(kind, source, channels))
  // The endpoint picker: adding a connection, or filling the one keyed fill.
  const [picker, setPicker] = useState<{ open: boolean; fill: string | null }>({ open: false, fill: null })
  // Saving the connection keyed key as an endpoint.
  const [saving, setSaving] = useState<{ open: boolean; key: string | null }>({ open: false, key: null })
  const [submitted, setSubmitted] = useState(false)
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({})
  const [saveError, setSaveError] = useState<string | null>(null)
  const createBuild = useCreateBuild()
  const invalidateFiles = useInvalidateBuildFiles()
  const pending = createBuild.isPending
  const disabled = pending || readOnly

  const target = kind.targets.find((t) => t.id === form.target)
  const fields = targetFields(kind, target)
  const { input, errors } = evaluateForm(kind, form, channels)
  const shown: Record<string, string> = { ...(submitted ? errors : {}), ...serverErrors }
  const hasErrors = Object.keys(errors).length > 0

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
    const hit = (k: string) => (key.endsWith(".") ? k.startsWith(key) : k === key)
    setServerErrors((prev) => {
      if (!Object.keys(prev).some(hit)) return prev
      return Object.fromEntries(Object.entries(prev).filter(([k]) => !hit(k)))
    })
  }

  function setValueDraft(key: string, d: Draft) {
    setForm((f) => ({ ...f, drafts: { ...f.drafts, [key]: d } }))
    clearServerError(`values.${key}`)
  }

  /** Changes connection i; clears the server's errors under clear (keys or prefixes ending in "."). */
  function updateConnection(i: number, update: (c: ConnectionDraft) => ConnectionDraft, ...clear: string[]) {
    setForm((f) => ({ ...f, connections: f.connections.map((c, j) => (j === i ? update(c) : c)) }))
    for (const key of clear) clearServerError(key)
  }

  /** Changes the list of connections. The server's errors name them by position, so they go. */
  function setConnections(update: (cs: ConnectionDraft[]) => ConnectionDraft[]) {
    setForm((f) => ({ ...f, connections: update(f.connections) }))
    setServerErrors((prev) => {
      const next = Object.fromEntries(Object.entries(prev).filter(([k]) => !k.startsWith("connections")))
      return Object.keys(next).length === Object.keys(prev).length ? prev : next
    })
  }

  function addConnection(moduleName: string, ep: EndpointOption | null) {
    setPicker((p) => ({ ...p, open: false }))
    const ch = channels.get(moduleName)
    if (!ch) return
    const c = connectionDraft(kind, ch, { moduleName })
    setConnections((cs) => [...cs, ep ? fillFromEndpoint(c, ch, ep, connectionNames(cs)) : c])
  }

  function fillConnection(key: string, ep: EndpointOption) {
    setPicker((p) => ({ ...p, open: false }))
    const i = form.connections.findIndex((c) => c.key === key)
    const ch = channels.get(ep.moduleName)
    if (i < 0 || !ch) return
    const taken = connectionNames(form.connections).filter((_, j) => j !== i)
    updateConnection(i, (c) => fillFromEndpoint(c, ch, ep, taken), `connections[${i}].settings.`, `connections[${i}].name`)
  }

  async function submit() {
    setSubmitted(true)
    setSaveError(null)
    if (hasErrors || readOnly) return
    try {
      const res = await createBuild.mutateAsync({
        operationId,
        input: {
          description: input.description,
          moduleName: kind.name,
          target: input.target,
          platform: input.platform,
          values: input.values,
          connections: input.connections,
        },
      })
      toast.success("Build queued")
      navigate(`${BUILDS_PATH}/${res.createBuild.id}`)
    } catch (err) {
      const problems = problemsFromError(err)
      setServerErrors(problems)
      setSaveError(
        problems[""] ??
          (Object.keys(problems).length > 0
            ? "Some settings were rejected; see the marked fields."
            : errorMessage(err, "Failed to start the build")),
      )
    }
  }

  const groups = groupByField(fields, (f) => f)
  // The channels the target speaks, for a new connection, and their endpoints.
  const speaks = (target?.channels ?? []).map((c) => ({ name: c.moduleName, why: channelUnusable(target, c.moduleName, channels) }))
  const usable = speaks.filter((c) => c.why === null).map((c) => c.name)
  const filling = form.connections.findIndex((c) => c.key === picker.fill)
  const savingIndex = form.connections.findIndex((c) => c.key === saving.key)
  const savingConnection = savingIndex < 0 ? undefined : form.connections[savingIndex]
  const pick: Omit<ComponentProps<typeof ConnectionPickerDialog>, "open" | "onClose" | "channels" | "channelIcons"> =
    filling < 0
      ? {
          title: "Add connection",
          description: "Pick an endpoint to copy its settings, then change what you need, or start a blank connection.",
          kinds: usable,
          endpoints: endpoints.filter((e) => usable.includes(e.moduleName)),
          blank: speaks,
          onEndpoint: (ep) => addConnection(ep.moduleName, ep),
          onBlank: (moduleName) => addConnection(moduleName, null),
        }
      : {
          title: `Fill connection ${filling + 1} from an endpoint`,
          description: `Its settings become a copy of the endpoint's, and its name and description the endpoint's unless you wrote your own. The ${kind.name} parameters stay as they are.`,
          kinds: [form.connections[filling].moduleName],
          endpoints: endpoints.filter((e) => e.moduleName === form.connections[filling].moduleName),
          onEndpoint: (ep) => fillConnection(form.connections[filling].key, ep),
        }

  // Side list: what to build, each group of fields, the connections, with a
  // dot for errors or for values set.
  const navSections: SettingsNavSection[] = [
    {
      id: sectionId(WHAT_TO_BUILD),
      title: WHAT_TO_BUILD,
      state: shown.description || shown.target || shown.platform ? "error" : "set",
    },
    ...groups.map(
      (g): SettingsNavSection => ({
        id: sectionId(g.group),
        title: g.group,
        state: g.items.some((f) => shown[`values.${f.key}`])
          ? "error"
          : g.items.some((f) => input.values[f.key] !== undefined)
            ? "set"
            : null,
      }),
    ),
    {
      id: sectionId(CONNECTIONS),
      title: CONNECTIONS,
      state: Object.keys(shown).some((k) => k.startsWith("connections"))
        ? "error"
        : form.connections.length > 0
          ? "set"
          : null,
    },
  ]
  const [active, goTo] = useActiveSection(navSections.map((s) => s.id))

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
          <Link to={BUILDS_PATH} className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeftIcon className="size-4" />
            Builds
          </Link>
          <span className="h-5 w-px bg-border" aria-hidden />
          <h1 className="text-lg font-semibold">New build</h1>
          <span className="text-sm text-muted-foreground">
            <KindName type="factory" name={kind.name} icon={kind.icon} /> · schema v{kind.version} · {kind.instances}{" "}
            running
          </span>
          <div className="flex items-center gap-2 sm:ml-auto">
            {readOnly ? (
              <span className="text-sm text-muted-foreground">Read-only — needs operator in this operation</span>
            ) : (
              <>
                {submitted && hasErrors && <span className="text-sm text-destructive">Fix the marked fields</span>}
                <Button type="button" variant="outline" onClick={() => navigate(BUILDS_PATH)} disabled={pending}>
                  Cancel
                </Button>
                <Button type="submit" disabled={pending}>
                  {pending ? "Starting..." : "Build"}
                </Button>
              </>
            )}
          </div>
        </header>

        <div className="flex gap-10 px-4 py-6">
        <SettingsNav sections={navSections} active={active} onPick={goTo} />
        <div className="@container/settings min-w-0 flex-1">
        <div className="flex max-w-3xl flex-col gap-8 @6xl/settings:max-w-[80rem]">
          {saveError && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{saveError}</div>}

          <SettingsSection id={sectionId(WHAT_TO_BUILD)} title={WHAT_TO_BUILD}>
            <SettingRow
              id="build-description"
              label="Description"
              required
              help="What this build is for, for the rest of the team. Core keeps it with the build; the factory and the minion never get it."
              error={shown.description}
            >
              <Textarea
                id="build-description"
                value={form.description}
                onChange={(e) => {
                  const description = e.target.value
                  setForm((f) => ({ ...f, description }))
                  clearServerError("description")
                }}
                rows={2}
                maxLength={500}
                placeholder="e.g. Service build for the EU front's dry run"
                disabled={disabled}
                aria-invalid={!!shown.description || undefined}
              />
            </SettingRow>
            <SettingRow id="build-target" label="Target" required help={target?.description ?? "What the factory produces."} error={shown.target}>
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
            </SettingRow>
            <SettingRow id="build-platform" label="Platform" required help="Where the minion runs." error={shown.platform}>
              <Select
                value={form.platform === "" ? null : form.platform}
                onValueChange={(v) => setForm((f) => ({ ...f, platform: typeof v === "string" ? v : "" }))}
                disabled={disabled || !target}
              >
                <SelectTrigger id="build-platform" className="w-full max-w-80">
                  <SelectValue placeholder="Select…" />
                </SelectTrigger>
                <SelectContent>
                  {(target?.platforms ?? []).map((p) => (
                    <SelectItem key={p} value={p} description={p}>
                      {platformLabel(p)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </SettingRow>
          </SettingsSection>

          {groups.map((g) => (
            <SettingsSection key={g.group} id={sectionId(g.group)} title={g.group}>
              {g.items.map((f) => (
                <FieldRow
                  key={f.key}
                  idPrefix="build"
                  field={f}
                  draft={form.drafts[f.key]}
                  onChange={(d) => setValueDraft(f.key, d)}
                  error={shown[`values.${f.key}`]}
                  disabled={disabled}
                />
              ))}
            </SettingsSection>
          ))}

          <section id={sectionId(CONNECTIONS)} className="scroll-mt-20" aria-labelledby={`${sectionId(CONNECTIONS)}-title`}>
            <h2 id={`${sectionId(CONNECTIONS)}-title`} className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {CONNECTIONS}
            </h2>
            <div className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                How the minion reaches its channels, tried in this order. Fill the settings in, or from one of the
                operation's endpoints and then change what you need: they are copied, so editing the endpoint later
                changes nothing here.
              </p>
              {shown.connections && <p className="text-sm text-destructive">{shown.connections}</p>}
              {form.connections.map((c, i) => {
                const at = `connections[${i}]`
                const ch = channels.get(c.moduleName)
                return (
                  <ConnectionCard
                    key={c.key}
                    index={i}
                    count={form.connections.length}
                    connection={c}
                    kind={kind}
                    channel={ch}
                    channelIcon={channelIcons.get(c.moduleName)}
                    endpointCount={endpoints.filter((e) => e.moduleName === c.moduleName).length}
                    settings={input.connections[i]?.settings ?? {}}
                    errors={shown}
                    disabled={disabled}
                    onName={(name) => updateConnection(i, (x) => ({ ...x, name, named: name.trim() !== "" }), `${at}.name`)}
                    onDescription={(description) =>
                      updateConnection(i, (x) => ({ ...x, description, described: description.trim() !== "" }), `${at}.description`)
                    }
                    onSetting={(key, d) =>
                      updateConnection(i, (x) => ({ ...x, settings: { ...x.settings, [key]: d } }), `${at}.settings.${key}`)
                    }
                    onParam={(key, d) => updateConnection(i, (x) => ({ ...x, drafts: { ...x.drafts, [key]: d } }), `${at}.values.${key}`)}
                    onFill={() => setPicker({ open: true, fill: c.key })}
                    onSave={() => setSaving({ open: true, key: c.key })}
                    onMove={(by) => setConnections((cs) => move(cs, i, by))}
                    onRemove={() => setConnections((cs) => cs.filter((_, j) => j !== i))}
                  />
                )
              })}
              <Button
                type="button"
                variant="outline"
                className="self-start"
                disabled={disabled || speaks.length === 0}
                onClick={() => setPicker({ open: true, fill: null })}
              >
                <PlusIcon />
                Add connection
              </Button>
            </div>
          </section>
          <ConnectionPickerDialog
            open={picker.open}
            onClose={() => setPicker((p) => ({ ...p, open: false }))}
            channels={channels}
            channelIcons={channelIcons}
            {...pick}
          />
          <SaveEndpointDialog
            open={saving.open && savingIndex >= 0}
            onClose={() => setSaving((s) => ({ ...s, open: false }))}
            operationId={operationId}
            factory={kind.name}
            channel={savingConnection && channels.get(savingConnection.moduleName)}
            channelIcon={savingConnection && channelIcons.get(savingConnection.moduleName)}
            connection={savingConnection}
            index={savingIndex}
            taken={endpoints.map((e) => e.name)}
          />
        </div>
        </div>
        </div>
      </form>
    </BuildFilesContext.Provider>
  )
}

/** The form's own sections, around the factory's groups of fields. */
const WHAT_TO_BUILD = "What to build"
const CONNECTIONS = "Connections"
