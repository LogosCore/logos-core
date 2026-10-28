import { useEffect, useMemo, useRef, useState, type ComponentProps } from "react"
import { HammerIcon, PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { LeaveJsonDialog } from "@/components/ui/leave-json-dialog"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
  useCloseGuard,
  useRequestClose,
} from "@/components/ui/record-modal"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { SettingsNav, type SettingsNavSection } from "@/components/ui/settings-nav"
import { sectionId, useActiveSection } from "@/hooks/use-active-section"
import { useRecordModals } from "@/hooks/use-record-modals"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import type { JsonProblem } from "@/lib/json-edit"
import { whenIdle } from "@/lib/preloadable"
import { useEditorModeStore } from "@/stores/editor-mode"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useBuild, useBuildFiles, useCreateBuild, useFactoryKinds, useInvalidateBuildFiles } from "@/graphql/hooks/builds"
import { uploadBuildFile } from "@/lib/build-file-upload"
import { errorMessage } from "@/components/modules/management/api-errors"
import { groupByField, type Draft } from "@/components/modules/management/field-values"
import { kindsByName, problemsFromError } from "@/components/endpoints/endpoint-values"
import { BuildFilesContext, type BuildFilesContextValue } from "@/components/builds/build-files-context"
import { FieldRow } from "@/components/builds/build-field-row"
import { ConnectionCard, type EndpointOption } from "@/components/builds/connection-card"
import { ConnectionPickerDialog } from "@/components/builds/connection-picker-dialog"
import { SaveEndpointDialog } from "@/components/builds/save-endpoint-dialog"
import { CommandPicker } from "@/components/builds/command-picker"
import {
  channelUnusable,
  connectionDraft,
  connectionNames,
  defaultCommands,
  evaluateForm,
  factoryKindsByName,
  fillFromEndpoint,
  initialForm,
  move,
  pickedCommands,
  platformLabel,
  sourceFromBuild,
  targetCommands,
  targetFields,
  withTarget,
  type BuildForm as Form,
  type BuildRecordLike,
  type ChannelKind,
  type ConnectionDraft,
  type FactoryKind,
  type FormSource,
} from "@/components/builds/build-values"
import {
  BUILD_JSON_LOCKED,
  buildJson,
  buildJsonHints,
  buildProblemPointer,
  documentInput,
  formDocument,
  formFromDocument,
  readBuildJson,
} from "@/components/builds/build-json"
import { useCanBuild } from "@/components/builds/build-access"
import { ModuleIcon } from "@/components/modules/module-icon"

/**
 * A new build in the scoped operation (the build modal, ?newBuild=<factory>);
 * with from, filled in from that earlier build ("Build again").
 */
export function BuildEditor({ factory, from: fromId }: { factory: string; from: string | null }) {
  const scopedOperation = useScopedOperation()
  const fromQuery = useBuild(fromId ?? "")

  if (!scopedOperation) {
    return <NotFound text="Builds belong to an operation: pick one first, in the sidebar." />
  }
  // Empty while loading, as the tables are.
  if (fromQuery.isLoading) return null
  const from = fromQuery.data?.build ?? null
  return <EditorLoader operationId={scopedOperation.id} kindName={factory} from={from} />
}

/** Loads what the form needs: the factory kind, the channels, the files. */
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
  const filesQuery = useBuildFiles(operationId)

  if (kinds.isLoading || channelKinds.isLoading || filesQuery.isLoading) return null
  const kind = factoryKindsByName(kinds.data?.factoryKinds ?? []).get(kindName)
  if (!kind) return <NotFound text={`No "${kindName}" factory declares what it builds.`} />
  const channels = kindsByName(channelKinds.data?.channelKinds ?? [])

  return (
    <Editor
      operationId={operationId}
      kind={kind}
      source={from && sourceFromBuild(kind, from, channels)}
      channels={channels}
      files={(filesQuery.data?.buildFiles ?? []).map((f) => ({ id: f.id, name: f.name, size: f.size, mediaType: f.mediaType }))}
    />
  )
}

function NotFound({ text }: { text: string }) {
  return <RecordModalMessage icon={<HammerIcon className="size-8 opacity-50" />} title="New build" text={text} />
}

function Editor({
  operationId,
  kind,
  source,
  channels,
  files,
}: {
  operationId: string
  kind: FactoryKind
  source: FormSource | null
  channels: ReadonlyMap<string, ChannelKind>
  files: BuildFilesContextValue["files"]
}) {
  const { open } = useRecordModals()
  const requestClose = useRequestClose()
  const { canWrite, loading: roleLoading } = useCanBuild(operationId)
  const readOnly = !roleLoading && !canWrite
  const channelIcons = useChannelIcons()

  const [form, setForm] = useState<Form>(() => initialForm(kind, source, channels))
  // The JSON tab edits the same build as text. While it is open the text
  // leads: each version that reads as a build is copied into the form, so
  // Build, the close guard and the Form tab all see it. The modal opens on
  // the tab picked last, in any record's editor (stores/editor-mode).
  const preferred = useEditorModeStore((s) => s.mode)
  const setPreferred = useEditorModeStore((s) => s.setMode)
  const [tab, setTab] = useState(preferred)
  const [jsonText, setJsonText] = useState(() =>
    preferred === "json" ? buildJson(kind, channels, formDocument(kind, channels, form)) : "",
  )
  const [leavingJson, setLeavingJson] = useState(false)
  const read = tab === "json" ? readBuildJson(jsonText, kind, channels) : null
  const connectionChannels = form.connections.map((c) => c.moduleName).join(",")
  const hints = useMemo(
    () => buildJsonHints(kind, channels, form.target, connectionChannels === "" ? [] : connectionChannels.split(",")),
    [kind, channels, form.target, connectionChannels],
  )
  // Fetched once the modal has settled, so the JSON tab opens at once.
  useEffect(() => whenIdle(preloadJsonEditor), [])

  // Changed from how it opened, or JSON that no longer reads: closing then
  // asks before throwing it away.
  const [initial] = useState(() => snapshotOf(form))
  const saved = useCloseGuard(snapshotOf(form) !== initial || (read !== null && !read.ok))
  const body = useRef<HTMLDivElement>(null)
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
  const hasErrors = read ? !read.ok || read.problems.length > 0 : Object.keys(errors).length > 0
  // The JSON tab marks problems as they appear, as code editors do, and the
  // server's from the last try at their place.
  const jsonProblems: JsonProblem[] = read?.ok
    ? [
        ...read.problems,
        ...Object.entries(serverErrors)
          .filter(([key]) => key !== "")
          .map(([key, message]) => ({ path: buildProblemPointer(key), message })),
      ]
    : []

  function showJson() {
    setJsonText(buildJson(kind, channels, formDocument(kind, channels, form)))
    setTab("json")
    setPreferred("json")
  }

  // Text that does not read as a build cannot be carried over: ask first.
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
    const next = readBuildJson(text, kind, channels)
    if (next.ok) setForm(formFromDocument(kind, channels, next.doc))
  }

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

  function pickCommands(picked: string[]) {
    setForm((f) => ({ ...f, commands: pickedCommands(kind, kind.targets.find((t) => t.id === f.target), picked) }))
    setServerErrors((prev) => {
      const next = Object.fromEntries(Object.entries(prev).filter(([k]) => !isCommandsKey(k)))
      return Object.keys(next).length === Object.keys(prev).length ? prev : next
    })
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
    if (read && !read.ok) setSaveError(`The JSON is not a build: ${read.message}`)
    if (hasErrors || readOnly) return
    // From the JSON tab, what it holds as it reads, without a trip through
    // the form's fields.
    const request = read?.ok ? documentInput(read.doc) : input
    try {
      const res = await createBuild.mutateAsync({
        operationId,
        input: {
          description: request.description,
          moduleName: kind.name,
          target: request.target,
          platform: request.platform,
          values: request.values,
          commands: request.commands,
          connections: request.connections,
        },
      })
      toast.success("Build queued")
      saved()
      open({ kind: "build", id: res.createBuild.id })
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
  const commands = targetCommands(kind, target)
  const defaults = defaultCommands(kind, target)
  const commandsError = Object.entries(shown).find(([k]) => isCommandsKey(k))?.[1]
  // The channels the target speaks, for a new connection, and their endpoints.
  const speaks = (target?.channels ?? []).map((c) => ({ name: c.moduleName, why: channelUnusable(target, c.moduleName, channels) }))
  const usable = speaks.filter((c) => c.why === null).map((c) => c.name)
  const filling = form.connections.findIndex((c) => c.key === picker.fill)
  const savingIndex = form.connections.findIndex((c) => c.key === saving.key)
  const savingConnection = savingIndex < 0 ? undefined : form.connections[savingIndex]
  const pick: Omit<
    ComponentProps<typeof ConnectionPickerDialog>,
    "open" | "onClose" | "operationId" | "channels" | "channelIcons"
  > =
    filling < 0
      ? {
          title: "Add connection",
          description: "Pick an endpoint to copy its settings, then change what you need, or start a blank connection.",
          kinds: usable,
          blank: speaks,
          onEndpoint: (ep) => addConnection(ep.moduleName, ep),
          onBlank: (moduleName) => addConnection(moduleName, null),
        }
      : {
          title: `Fill connection ${filling + 1} from an endpoint`,
          description: `Its settings become a copy of the endpoint's, and its name and description the endpoint's unless you wrote your own. The ${kind.name} parameters stay as they are.`,
          kinds: [form.connections[filling].moduleName],
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
    // A factory without commands builds none: no section for them.
    ...(kind.commands.length === 0
      ? []
      : [
          {
            id: sectionId(COMMANDS),
            title: COMMANDS,
            state: commandsError
              ? ("error" as const)
              : form.commands.join() !== defaults.join()
                ? ("set" as const)
                : null,
          },
        ]),
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
  const [active, goTo] = useActiveSection(
    navSections.map((s) => s.id),
    body,
  )

  return (
    <BuildFilesContext.Provider value={filesCtx}>
      <form
        className="flex min-h-0 flex-1 flex-col"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <Tabs
          value={tab}
          onValueChange={(v) => (v === "json" ? showJson() : showForm())}
          className="min-h-0 flex-1 gap-0"
        >
        <RecordModalHeader>
          {/* The factory's icon as large as in the builds table, beside the
              title and, under it, what builds. */}
          <div className="flex min-w-0 items-center gap-3">
            <ModuleIcon type="factory" icon={kind.icon} className="size-9" />
            <div className="flex min-w-0 flex-col">
              <RecordModalTitle>New build</RecordModalTitle>
              <span className="truncate text-sm text-muted-foreground">
                <span className="font-mono">{kind.name}</span> · schema v{kind.version} · {kind.instances} running
              </span>
            </div>
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
                  {pending ? "Starting..." : "Build"}
                </Button>
              </>
            )}
          </div>
        </RecordModalHeader>

        <TabsContent value="form" className="flex min-h-0 flex-col">
        <RecordModalBody ref={body}>
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

          {kind.commands.length > 0 && (
            <CommandPicker
              id={sectionId(COMMANDS)}
              title={COMMANDS}
              commands={commands}
              picked={form.commands}
              defaults={defaults}
              onChange={pickCommands}
              error={commandsError}
              disabled={disabled}
            />
          )}

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

          <section id={sectionId(CONNECTIONS)} className="scroll-mt-6" aria-labelledby={`${sectionId(CONNECTIONS)}-title`}>
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
                    operationId={operationId}
                    index={i}
                    count={form.connections.length}
                    connection={c}
                    kind={kind}
                    channel={ch}
                    channelIcon={channelIcons.get(c.moduleName)}
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
            operationId={operationId}
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
          />
        </div>
        </div>
        </div>
        </RecordModalBody>
        </TabsContent>

        <TabsContent value="json" className="flex min-h-0 flex-col gap-3 px-4 py-4">
          {saveError && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{saveError}</div>}
          <LazyJsonEditor
            value={jsonText}
            onChange={editJson}
            locked={BUILD_JSON_LOCKED}
            problems={jsonProblems}
            hints={hints}
            readOnly={disabled}
            fileName={`${kind.name}.build.json`}
            aria-label="The build as JSON"
            className="flex-1 rounded-md border"
          />
          <p className="text-xs text-muted-foreground">
            <code className="font-mono">commands</code> are what the minion can do; the target&apos;s required ones stay
            in. Values show the factory&apos;s and the channels&apos; defaults; one left at its default, or emptied, is
            left to the default. A connection&apos;s <code className="font-mono">settings</code> are its channel&apos;s, its{" "}
            <code className="font-mono">values</code> the factory&apos;s for that channel.{" "}
            <code className="font-mono">factory</code> is locked. Suggestions appear as you type; Ctrl+Space (⌥I on a
            Mac) asks for them.
          </p>
        </TabsContent>
        </Tabs>

        <LeaveJsonDialog
          open={leavingJson}
          what="a build"
          problem={read && !read.ok ? read.message : ""}
          onStay={() => setLeavingJson(false)}
          onLeave={() => {
            setLeavingJson(false)
            leaveJson()
          }}
        />
      </form>
    </BuildFilesContext.Provider>
  )
}

/**
 * What the form holds, for telling whether it changed: the connections'
 * bookkeeping (keys, notes, what filled them) aside, which the JSON tab
 * renews on every edit.
 */
function snapshotOf(form: Form): string {
  return JSON.stringify({
    ...form,
    connections: form.connections.map((c) => ({
      moduleName: c.moduleName,
      name: c.name,
      description: c.description,
      settings: c.settings,
      drafts: c.drafts,
    })),
  })
}

/** The form's own sections, around the factory's groups of fields. */
const WHAT_TO_BUILD = "What to build"
const COMMANDS = "Commands"
const CONNECTIONS = "Connections"

/** A problem's key that is the commands': "commands", "commands[2]". */
const isCommandsKey = (key: string) => key === "commands" || key.startsWith("commands[")
